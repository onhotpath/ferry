package ferry

import (
	"fmt"
	"strconv"
	"strings"
)

// Core does not call reflect.StructTag.Get or Lookup, and this file is the
// reason (ADR-0008).
//
// The scanning loop below is Lookup's own, with the error paths kept rather
// than collapsed into a break. That is the difference between three silent
// failure modes and three diagnoses, measured on the same six tags:
//
//	ferry:"host,required"              read
//	ferry:"origins,default=["value"]"  truncated at the bare quote by Get, and the
//	                                   json and yaml tags on the same field destroyed
//	ferry:"a\,b"                       invisible to Lookup rather than wrong, because
//	                                   \, is not an escape Go defines
//	ferry:"first" ferry:"second"       two tags, and Get returns the first
//
// go vet catches two of the three and go test catches none, because structtag
// is not in the analyser subset go test runs.

// read is what the scanner found on one field: the tag value, and whether the
// field carried the key at all.
//
// found is the distinction Lookup cannot make. A field that genuinely carries
// no ferry tag and one whose tag could not be read are the same answer from
// Lookup and are different errors here.
type read struct {
	value string
	found bool
}

// scanTag reads one struct tag key out of a field's raw tag text.
//
// The refusal is scoped, because a malformed tag is not always ferry's: a field
// whose json tag is malformed and whose ferry tag was read cleanly is go vet's
// problem, and ferry refuses a tag that does not parse only where the failure
// is its own. See tagScan.mine.
func scanTag(raw, key string) (read, error) {
	s := tagScan{raw: raw, key: key}
	err := s.run()

	return s.out, err
}

// tagScan is the scan in progress. It is a struct rather than a pile of
// arguments because the attribution rule needs the whole of it.
type tagScan struct {
	raw string
	key string
	out read
	// end is where the key's own entry stopped, which is what tells a failure
	// caused by that entry from one belonging to another library's tag.
	end int
}

func (s *tagScan) run() error {
	for rest, done := s.raw, false; !done; {
		var err error

		if rest, done, err = s.step(rest); err != nil {
			return err
		}
	}

	return nil
}

// step consumes one key:"value" entry, returning what is left of the tag and
// whether the scan is over.
func (s *tagScan) step(rest string) (left string, done bool, err error) {
	// Where this entry starts, with the separating space excluded, because a
	// space is what tells one library's entry from the residue of another's.
	rest = strings.TrimLeft(rest, " ")
	at := len(s.raw) - len(rest)

	e, state := scanEntry(rest)
	if state == scanDone {
		return "", true, nil
	}

	if state != scanOK {
		return "", true, s.failure(state, e.key, rest, at)
	}

	if e.key != s.key {
		return e.rest, false, nil
	}

	return e.rest, false, s.take(e.quoted, len(s.raw)-len(e.rest))
}

// take records the value of the key's own entry, and refuses the two things Get
// answers with silence: a value that is not a Go quoted string, and a second
// entry under the same key.
func (s *tagScan) take(quoted string, end int) error {
	v, err := strconv.Unquote(quoted)
	if err != nil {
		return fmt.Errorf("%s tag value %s is not a valid Go quoted string: a struct tag value is unquoted by "+
			"strconv.Unquote, so it may not contain a bare double quote and may not contain an escape Go does "+
			"not define: %w", s.key, quoted, err)
	}

	if s.out.found {
		return fmt.Errorf("the field carries two %s tags, %q and %q: reflect.StructTag.Get returns the first, "+
			"and go vet does not check it", s.key, s.out.value, v)
	}

	s.out = read{value: v, found: true}
	s.end = end

	return nil
}

// failure diagnoses a malformed entry, or returns nil where the malformation is
// not ferry's to refuse.
func (s *tagScan) failure(state scanState, key, rest string, at int) error {
	if !s.mine(at) {
		return nil
	}

	if state == scanUnterminated {
		return fmt.Errorf("struct tag key %q has an unterminated quoted value", key)
	}

	return fmt.Errorf("struct tag is not in the conventional `key:\"value\"` form, at %s: the usual cause is a "+
		"bare double quote inside a %s tag, which a struct tag value cannot contain", strconv.Quote(rest), s.key)
}

// mine reports whether a scan failure is ferry's to refuse.
//
// Two cases, and the second is the one that keeps another library's mistake out
// of ferry's report. Before the key has been read, a tag that does not parse is
// ferry's exactly when the key opens an entry in it at all: the failure is why
// ferry cannot see its own tag. After the key has been read, a failure is
// ferry's only where it begins at the byte its own entry stopped at - which is
// a value truncated at a bare double quote, with the residue scanning as
// garbage - and a failure past a separating space belongs to whoever wrote the
// entry it is in.
func (s *tagScan) mine(at int) bool {
	if s.out.found {
		return at == s.end
	}

	return opensEntry(s.raw, s.key)
}

// opensEntry reports whether key opens a key:"..." entry anywhere in raw, at a
// real struct-tag key boundary.
//
// The boundary is the whole of it. strings.Contains matched ferry inside
// xferry, and env inside myenv, so another library's malformed tag was refused
// as ferry's own and failed the compile (#261). ADR-0021 has ferry reading
// declared foreign keys beside its own, which needs the two kept apart exactly.
//
// A key is bounded on the left by the start of the tag or by a byte no key may
// contain, which is scanKeyEnd's rule read backwards, and on the right by the
// colon and quote that introduce its value.
func opensEntry(raw, key string) bool {
	want := key + `:"`

	for i := 0; i+len(want) <= len(raw); {
		j := strings.Index(raw[i:], want)
		if j < 0 {
			return false
		}

		at := i + j
		if at == 0 || isKeyStop(raw[at-1]) {
			return true
		}

		i = at + 1
	}

	return false
}

// scanState says how one entry ended, which is the whole of what this scanner
// adds to Lookup's loop: Lookup breaks on both malformations and reports
// neither.
type scanState uint8

const (
	scanOK           scanState = iota // one key:"value" pair was consumed
	scanDone                          // nothing but spaces was left
	scanNotPair                       // the text is not in key:"value" form
	scanUnterminated                  // the quoted value has no closing quote
)

// tagEntry is one key:"value" pair, with the value still quoted: it is unquoted
// only for the key ferry was asked about, which is Lookup's own behaviour and
// is why another library's undefined escape is not ferry's problem.
type tagEntry struct {
	key    string
	quoted string
	rest   string
}

func scanEntry(s string) (tagEntry, scanState) {
	s = strings.TrimLeft(s, " ")
	if s == "" {
		return tagEntry{}, scanDone
	}

	i := scanKeyEnd(s)
	if i == 0 || i+1 >= len(s) || s[i] != ':' || s[i+1] != '"' {
		return tagEntry{}, scanNotPair
	}

	key := s[:i]
	s = s[i+1:]

	j, ok := scanQuotedEnd(s)
	if !ok {
		return tagEntry{key: key}, scanUnterminated
	}

	return tagEntry{key: key, quoted: s[:j], rest: s[j:]}, scanOK
}

// scanKeyEnd is where a key stops: at the colon that introduces its value, or
// at a byte no key may contain.
func scanKeyEnd(s string) int {
	for i := range len(s) {
		if isKeyStop(s[i]) {
			return i
		}
	}

	return len(s)
}

// isKeyStop is the byte set no struct tag key may contain, which is what bounds
// one key against another.
func isKeyStop(c byte) bool {
	return c <= ' ' || c == ':' || c == '"' || c == del
}

// scanQuotedEnd is one past the closing quote of the Go quoted string s opens
// with, or false where there is not one. A backslash escapes the byte after it,
// which is what makes a value ending in a lone backslash swallow the rest of
// the tag.
func scanQuotedEnd(s string) (int, bool) {
	for i := 1; i < len(s); i++ {
		if s[i] == '"' {
			return i + 1, true
		}

		if s[i] == '\\' {
			i++
		}
	}

	return 0, false
}
