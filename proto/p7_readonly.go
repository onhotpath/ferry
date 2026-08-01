package main

// P7: one interface or two, and where a read-only plane says so.
//
// ADR-0002 already established that env has no honest Dump. This probe checks
// what each answer costs at the type level, and whether the Writer needs
// three methods or fewer.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func p7ReadOnly() {
	head("P7  Source and Sink: one interface or two, and read-only planes")

	ctx := context.Background()

	// (a) Static: a plane with no honest Dump simply is not a Sink. The
	//     refusal is a compile error at the call site, not a runtime error.
	fmt.Println("    (a) statically read-only")
	var _ Source = EnvSource{}
	var _ Source = QuerySource{}
	var _ Source = YAMLSource{}
	var _ Sink = YAMLSink{}
	var _ Sink = KVSink{}
	_, envIsSink := any(EnvSource{}).(Sink)
	_, queryIsSink := any(QuerySource{}).(Sink)
	fmt.Printf("        EnvSource   implements Sink? %v\n", envIsSink)
	fmt.Printf("        QuerySource implements Sink? %v\n", queryIsSink)
	fmt.Println("        With one combined interface both would have to declare a")
	fmt.Println("        Dump they cannot honour, and ADR-0002 already refused to put")
	fmt.Println("        half a driver in core for exactly this reason. Two")
	fmt.Println("        interfaces make the refusal free and checkable by the type")
	fmt.Println("        system instead of by prose.")

	// (b) Dynamic: writable in principle, not right now.
	fmt.Println("\n    (b) dynamically read-only")
	kv := newKV(map[string]string{"cfg/a": "1"})
	kv.readOnly = true
	_, err := bindOpenSink(ctx, KVSink{KV: kv, Prefix: "cfg/"}, NewAddressSet([]Path{path("a")}))
	fmt.Printf("        kv with no write ACL, at Open : %v (ErrReadOnly? %v)\n",
		err, errors.Is(err, ErrReadOnly))

	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)
	ro := filepath.Join(dir, "ro")
	os.Mkdir(ro, 0o555)
	_, err = bindOpenSink(ctx, YAMLSink{Path: filepath.Join(ro, "out.yaml")}, NewAddressSet([]Path{path("a")}))
	fmt.Printf("        yaml into a 0555 dir, at Open : %v\n", oneLine(err))
	fmt.Println("        Both land at Open, before a single value has been produced.")
	fmt.Println("        That matters because Dump is the direction that runs a walk")
	fmt.Println("        over the user's struct: failing at Open costs nothing, and")
	fmt.Println("        failing at the first Set has already half-written the plane.")

	// (c) Does the Writer need Abort, or is Commit enough?
	fmt.Println("\n    (c) does Writer need three methods?")
	out := filepath.Join(dir, "out.yaml")
	w, _ := bindOpenSink(ctx, YAMLSink{Path: out}, NewAddressSet([]Path{path("a")}))
	_ = w.Set(ctx, path("a"), String("1"))
	w.Abort()
	left, _ := filepath.Glob(filepath.Join(dir, ".ferry-*"))
	fmt.Printf("        after Abort  : temp files left behind = %d\n", len(left))

	w, _ = bindOpenSink(ctx, YAMLSink{Path: out}, NewAddressSet([]Path{path("a")}))
	_ = w.Set(ctx, path("a"), String("1"))
	left, _ = filepath.Glob(filepath.Join(dir, ".ferry-*"))
	fmt.Printf("        dropped, no Abort : temp files left behind = %d\n", len(left))
	fmt.Println("        So Abort earns its place: a sink that stages its write needs")
	fmt.Println("        somewhere to clean up when the walk fails partway, and ferry")
	fmt.Println("        aggregating errors (5.4) makes a partway failure the normal")
	fmt.Println("        case rather than the exotic one.")

	// (d) Where partial dump would attach, since ADR-0001 milestoned it.
	fmt.Println("\n    (d) ADR-0001's delta and partial dump")
	fmt.Println("        The commitment was that the sink contract does not preclude")
	fmt.Println("        it. It does not: the Writer sees the whole address set at")
	fmt.Println("        Open and then a subset of Sets, so 'what was not Set' is")
	fmt.Println("        already computable by the driver at Commit with no new")
	fmt.Println("        method. Whether ferry ever exposes that is #8's and a later")
	fmt.Println("        Option's, not this ADR's.")
}

func oneLine(err error) string {
	if err == nil {
		return "<nil>"
	}
	return firstLine(err.Error())
}
