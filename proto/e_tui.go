package main

// The interactive shell, for the ONE question in #9 that a table answers badly:
// does the rendering read well?
//
// `Error()` is one line and `%+v` is the report, and whether that is the right
// split is a feel question - it depends on what a report looks like at three
// errors and at forty, and on whether the one-line form is still actionable
// once it elides. So this is a thin shell over e_error.go that lets the set be
// built up by hand and re-rendered after every action.
//
// Throwaway. The model behind it is the part that lifts.
//
//	GOTOOLCHAIN=go1.27rc2 go run . etui

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	bold = "\x1b[1m"
	dim  = "\x1b[2m"
	rst  = "\x1b[0m"
)

type tuiState struct {
	errs    []error
	verbose bool
	n       int // a counter so each added address is distinct
}

func (s *tuiState) add(e error) { s.errs = append(s.errs, e); s.n++ }

func (s *tuiState) render() {
	fmt.Print("\x1b[2J\x1b[H")
	fmt.Printf("%s#9 error rendering%s  %s(proto/9-errors, throwaway)%s\n\n", bold, rst, dim, rst)

	counts := map[string]int{}
	for _, e := range s.errs {
		for _, c := range classes {
			if errors.Is(e, c) {
				counts[c.Error()]++
			}
		}
		if errors.Is(e, ErrDriver) {
			counts["driver"]++
		}
	}
	fmt.Printf("%selements%s %d", bold, rst, len(s.errs))
	if len(counts) > 0 {
		var parts []string
		for _, c := range []string{ErrSchema.Error(), ErrMissing.Error(), ErrValue.Error(), ErrPlane.Error(), ErrDriver.Error()} {
			if counts[c] > 0 {
				parts = append(parts, fmt.Sprintf("%s=%d", c, counts[c]))
			}
		}
		fmt.Printf("   %s%s%s", dim, strings.Join(parts, " "), rst)
	}
	fmt.Printf("\n\n")

	err := join(s.errs...)
	if err == nil {
		fmt.Printf("%s(no errors: join returns nil)%s\n", dim, rst)
	} else {
		fmt.Printf("%s%%v%s   %s\n\n", bold, rst, err)
		if s.verbose {
			fmt.Printf("%s%%+v%s\n%+v\n", bold, rst, err)
		} else {
			fmt.Printf("%s(press f to show %%+v)%s\n", dim, rst)
		}
		fmt.Printf("\n%swrapped%s %s\n", bold, rst, fmt.Errorf("loading config: %w", err))
	}

	fmt.Printf("\n%s%s%s\n", dim, strings.Repeat("-", 78), rst)
	fmt.Printf("%s[v]%s value  %s[m]%s missing  %s[s]%s schema  %s[p]%s plane/driver  %s[c]%s close  %s[k]%s cancel\n",
		bold, rst, bold, rst, bold, rst, bold, rst, bold, rst, bold, rst)
	fmt.Printf("%s[x]%s +20 value  %s[u]%s undo  %s[r]%s reset  %s[f]%s toggle %%+v  %s[q]%s quit\n",
		bold, rst, bold, rst, bold, rst, bold, rst, bold, rst)
	fmt.Print("\n> ")
}

func runE9TUI() {
	s := &tuiState{}
	in := bufio.NewScanner(os.Stdin)
	s.render()
	for in.Scan() {
		switch strings.TrimSpace(in.Text()) {
		case "v":
			s.add(errAt(mWalk, ErrValue, path("db", fmt.Sprintf("field%d", s.n)), "value did not parse as int").
				withCause(errors.New(`strconv.ParseInt: parsing "hunter2": invalid syntax`)))
		case "m":
			s.add(errAt(mWalk, ErrMissing, path("tls", fmt.Sprintf("cert%d", s.n)), "required, and the plane supplied nothing"))
		case "s":
			s.add(errAt(mCompile, ErrSchema, path(fmt.Sprintf("Field%d", s.n)),
				`field carries no ferry tag: every exported field must name the segment it addresses, or be marked ferry:"-"`))
		case "p":
			s.add(fromDriver(mWalk, path("kv", fmt.Sprintf("k%d", s.n)), true, errors.New("kv: read timeout after 2s")))
		case "c":
			s.add(fromDriver(mClose, Path{}, false, errors.New("kv: flush failed")))
		case "k":
			s.add(&Error{mom: mWalk, msg: "cancelled", cause: errors.New("context canceled")})
		case "x":
			for range 20 {
				s.add(errAt(mWalk, ErrValue, path("bulk", fmt.Sprintf("f%02d", s.n)), "value did not parse as int"))
			}
		case "u":
			if len(s.errs) > 0 {
				s.errs = s.errs[:len(s.errs)-1]
			}
		case "r":
			s.errs, s.n = nil, 0
		case "f":
			s.verbose = !s.verbose
		case "q":
			fmt.Print("\x1b[2J\x1b[H")
			return
		}
		s.render()
	}
}
