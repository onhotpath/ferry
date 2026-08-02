package main

// P14: does every writer need closing?
//
// The slim contract makes Close a required Writer method. That is a claim that
// every sink has an end-of-dump step, and it is worth checking against the
// sinks that exist rather than assuming.
//
// dagger's answer to "not every Step needs this" is middlewareSkipper: an
// unexported optional interface, discovered by assertion. The read side of
// ferry already works that way, with Enumerator on Reader. This probe asks
// whether the write side can be symmetric, and what the risk is.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The candidate split: one required method, one optional interface.
type W1 interface {
	Set(context.Context, Path, Value) error
}

type WCloser interface {
	Close(ctx context.Context, cause error) error
}

// closeWriter is what ferry's engine would do at the end of a dump.
func closeWriter(ctx context.Context, w W1, cause error) error {
	c, ok := w.(WCloser)
	if !ok {
		return cause
	}
	return errors.Join(cause, c.Close(ctx, cause))
}

func p14Closer() {
	head("P14  does every writer need closing?")

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)

	// (a) Which of the sinks actually has an end-of-dump step?
	fmt.Println("    (a) sinks, and whether they have anything to do at the end")
	rows := []struct{ sink, needs, why string }{
		{"yaml file", "yes", "the document does not exist until it is serialised"},
		{"kv, transactional", "yes", "one Txn for the whole dump"},
		{"kv, write-per-Set", "no", "each Set already reached the plane"},
		{"recorder (ferrytest)", "no", "it is a map"},
		{"env, as []string", "no", "appending to a slice needs no commit"},
		{"http PUT per key", "no", "each Set is a request"},
	}
	for _, r := range rows {
		fmt.Printf("        %-22s %-5s %s\n", r.sink, r.needs, r.why)
	}
	fmt.Println("        Four of six have nothing to do, so a required Close is")
	fmt.Println("        `return nil` boilerplate in the majority case.")

	// (b) The boilerplate, and the worse thing hiding inside it.
	fmt.Println("\n    (b) what the boilerplate actually costs")
	fmt.Println("        func (w recWriter) Close(context.Context, error) error { return nil }")
	fmt.Println("        That line is not merely noise. It is indistinguishable from")
	fmt.Println("        a driver that SHOULD have rolled back and did not, and")
	fmt.Println("        nothing in the type system tells the two apart.")

	// (c) So: make it optional. Does the failure stay loud?
	//     This is the question that decides it. A sink that needs Close and
	//     does not implement it writes nothing at all.
	fmt.Println("\n    (c) a sink that NEEDS Close and forgets it")
	out := filepath.Join(dir, "out.yaml")
	os.WriteFile(out, []byte("keep: me\n"), 0o644)

	forgetful := &forgetfulYAML{path: out}
	_ = forgetful.Set(ctx, path("a"), String("1"))
	_ = closeWriter(ctx, forgetful, nil)
	b, _ := os.ReadFile(out)
	fmt.Printf("        plane after a 'successful' dump : %q\n", string(b))
	leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	fmt.Printf("        conformance: dump then load round trip -> %v\n",
		roundTripHolds(string(b), "a"))
	fmt.Printf("        temp files leaked               : %d\n", len(leftovers))
	fmt.Println("        The dump silently did nothing. That is exactly the failure")
	fmt.Println("        ADR-0001 rules out, so the question is whether it is caught.")

	// (d) It is caught, and by a suite that has to exist anyway.
	fmt.Println("\n    (d) the conformance case that catches it")
	fmt.Println("        ADR-0001 already obliges a driver-fidelity suite, and its")
	fmt.Println("        most basic case is: Dump a value, Load it back, compare.")
	fmt.Printf("        forgetful sink passes that case? %v\n", roundTripHolds(string(b), "a"))
	fmt.Println("        A sink that needs Close and omits it fails the first")
	fmt.Println("        conformance case there is. So the risk of making Close")
	fmt.Println("        optional is covered by a test that ships regardless -")
	fmt.Println("        which is ADR-0002 route (b) doing the job it was admitted for.")

	// (e) The same question on the read side, which the slim contract missed.
	fmt.Println("\n    (e) and the read side has the same gap")
	fmt.Println("        A Reader can hold a resource too: a per-load connection, a")
	fmt.Println("        file handle a streaming source did not want to slurp, a")
	fmt.Println("        lease. The slim contract gives it nowhere to release.")
	fmt.Println("        Symmetric answer: the same optional Closer on Reader.")
	fmt.Println("        The four drivers here need it nowhere, which is why it went")
	fmt.Println("        unnoticed until Close was questioned on the write side.")

	// (f) What the contract looks like after.
	fmt.Println("\n    (f) resulting surface")
	fmt.Printf("        %-30s %s\n", "Reader", "Get                     (1 method)")
	fmt.Printf("        %-30s %s\n", "  optional Enumerator", "Children")
	fmt.Printf("        %-30s %s\n", "  optional Closer", "Close(ctx, cause)")
	fmt.Printf("        %-30s %s\n", "Writer", "Set                     (1 method)")
	fmt.Printf("        %-30s %s\n", "  optional Closer", "Close(ctx, cause)")
	fmt.Println("        One required method each way, and one optional interface")
	fmt.Println("        shared by both directions rather than two spellings.")
}

// forgetfulYAML stages into a temp file and never commits, which is what a
// driver author gets if Close is optional and they did not read the docs.
type forgetfulYAML struct {
	path string
	buf  map[string]string
}

func (w *forgetfulYAML) Set(_ context.Context, p Path, v Value) error {
	if w.buf == nil {
		w.buf = map[string]string{}
	}
	w.buf[p.String()] = v.Text()
	return nil
}

func roundTripHolds(planeContents, key string) bool {
	return len(planeContents) > 0 && containsKey(planeContents, key)
}

func containsKey(s, k string) bool {
	for i := 0; i+len(k) <= len(s); i++ {
		if s[i:i+len(k)] == k {
			return true
		}
	}
	return false
}
