// Command proto136 measures what ferry writes and reads at a container address,
// which is issue #136's question. Nothing here ships; it is evidence for
// PROTO-136.md.
package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

func main() {
	sections := map[string]func(){
		"1": sec1,
		"2": sec2,
		"3": sec3,
		"4": sec4,
		"5": sec5,
		"6": sec6,
		"7": sec7,
	}

	want := os.Args[1:]
	if len(want) == 0 {
		want = []string{"1", "2", "3", "4", "5", "6", "7"}
	}

	for _, k := range want {
		f, ok := sections[k]
		if !ok {
			fmt.Println("no section", k)

			continue
		}

		f()
	}
}

// row prints one address / value line, aligned.
func row(addr, val string) {
	fmt.Printf("  %-24s %s\n", addr, val)
}

// show renders a Value the way a report should read it.
func show(v ferry.Value) string {
	switch v.Kind() {
	case ferry.KindAbsent:
		return "Absent"
	case ferry.KindNull:
		return "Null"
	case ferry.KindString:
		s, _ := v.AsString()

		return fmt.Sprintf("String(%q)", s)
	case ferry.KindNumber:
		s, _ := v.AsNumber()

		return "Number(" + s + ")"
	case ferry.KindBool:
		b, _ := v.AsBool()

		return fmt.Sprintf("Bool(%v)", b)
	case ferry.KindBytes:
		b, _ := v.AsBytes()

		return fmt.Sprintf("Bytes(%q)", string(b))
	default:
		return v.GoString()
	}
}

// sorted renders a recorded map in address order.
func sorted(m map[ferry.Path]ferry.Value) []ferry.Path {
	out := make([]ferry.Path, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.SortFunc(out, ferry.Path.Compare)

	return out
}

func head(s string) {
	fmt.Println()
	fmt.Println("=== " + s)
}

func sub(s string) {
	fmt.Println()
	fmt.Println("-- " + s)
}

// indent pushes a multi-line error under a label.
func indent(err error) string {
	if err == nil {
		return "<nil>"
	}

	return strings.ReplaceAll(err.Error(), "\n", "\n     ")
}
