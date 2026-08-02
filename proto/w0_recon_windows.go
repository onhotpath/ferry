//go:build windows

package main

// W0: reconnaissance, not a probe.
//
// The handoff's stated trap for this ticket is "a Registry key you have write
// permission on". GitHub's windows-latest runner may well be elevated, in
// which case a probe asserting "ferry refuses a write it has no permission for"
// would go green having tested nothing. So the first thing that runs on the
// runner is a report of what the runner can actually do, and every later probe
// is designed against that rather than against an assumption.
//
// It prints only values it wrote itself. This repository is public and the
// environment is not inspected.

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const onWindows = true

const wProbeKey = `Software\ferry-proto-15`

func w15probes() []w15probe {
	return []w15probe{
		{"W0", "reconnaissance: what this runner can actually do", runW0, true},
		{"W1", "does ADR-0003's address model express a Registry path", runW1, false},
		{"W2", "what an Enumerator can say about a plane with no list type", runW2, false},
		{"W3", "do natively typed values fit, or is there a lossy trip through strings", runW3, false},
		{"W4", "the same driver against a real hive: permissions, case, namespaces", runW4, true},
		{"W5", "the second data point #25 asked for", runW5, false},
	}
}

func runW0() {
	fmt.Println("--- token ---")
	tok := windows.GetCurrentProcessToken()
	elevated := tok.IsElevated()
	fmt.Printf("  process token elevated              : %v\n", elevated)
	admin, err := isAdmin()
	fmt.Printf("  member of BUILTIN\\Administrators    : %v  (err=%v)\n", admin, err)

	fmt.Println("\n--- what a write reaches ---")
	for _, c := range []struct {
		label string
		root  registry.Key
		path  string
		want  string
	}{
		{"HKCU\\" + wProbeKey, registry.CURRENT_USER, wProbeKey, "expected to SUCCEED"},
		{"HKLM\\SOFTWARE\\ferry-proto-15", registry.LOCAL_MACHINE, `SOFTWARE\ferry-proto-15`, "the question"},
		{"HKLM\\SECURITY\\ferry-proto-15", registry.LOCAL_MACHINE, `SECURITY\ferry-proto-15`, "expected to be DENIED"},
	} {
		k, _, err := registry.CreateKey(c.root, c.path, registry.ALL_ACCESS)
		if err == nil {
			err = k.SetStringValue("probe", "ok")
			k.Close()
			registry.DeleteKey(c.root, c.path)
		}
		fmt.Printf("  create+write %-34s %-22s -> %v\n", c.label, c.want, errShort(err))
	}

	fmt.Println("\n--- the fallback: a handle opened without write rights ---")
	fmt.Println("  If the runner can write everywhere, the read-only refusal ADR-0004")
	fmt.Println("  puts in OpenWriterFunc has to be produced by the ACCESS MASK on the")
	fmt.Println("  handle rather than by an ACL, so this is the one that matters.")
	k, _, err := registry.CreateKey(registry.CURRENT_USER, wProbeKey, registry.ALL_ACCESS)
	if err != nil {
		fmt.Println("  could not create the probe key:", errShort(err))
		return
	}
	k.Close()
	ro, err := registry.OpenKey(registry.CURRENT_USER, wProbeKey, registry.QUERY_VALUE)
	if err != nil {
		fmt.Println("  open QUERY_VALUE:", errShort(err))
	} else {
		err = ro.SetStringValue("probe", "should not work")
		fmt.Printf("  SetStringValue through a QUERY_VALUE handle -> %v\n", errShort(err))
		fmt.Printf("  is it ERROR_ACCESS_DENIED                   -> %v\n", isAccessDenied(err))
		ro.Close()
	}
	rw, err := registry.OpenKey(registry.CURRENT_USER, wProbeKey, registry.SET_VALUE)
	if err == nil {
		fmt.Printf("  SetStringValue through a SET_VALUE handle   -> %v\n", errShort(rw.SetStringValue("probe", "ok")))
		rw.Close()
	}

	fmt.Println("\n--- the five value types, round-tripped through x/sys ---")
	k2, _, err := registry.CreateKey(registry.CURRENT_USER, wProbeKey, registry.ALL_ACCESS)
	if err != nil {
		fmt.Println("  create:", errShort(err))
		return
	}
	defer func() {
		k2.Close()
		registry.DeleteKey(registry.CURRENT_USER, wProbeKey)
	}()

	fmt.Printf("  %-14s %-10s %s\n", "value name", "type", "what came back")
	_ = k2.SetStringValue("sz", "hello")
	s, t, _ := k2.GetStringValue("sz")
	fmt.Printf("  %-14s %-10s %q\n", "sz", wTypeName(t), s)

	_ = k2.SetExpandStringValue("expand", "%SystemRoot%\\x")
	e, t, _ := k2.GetStringValue("expand")
	ex, _ := registry.ExpandString(e)
	fmt.Printf("  %-14s %-10s raw=%q  expanded=%q\n", "expand", wTypeName(t), e, ex)

	_ = k2.SetDWordValue("dword", 8080)
	d, t, _ := k2.GetIntegerValue("dword")
	fmt.Printf("  %-14s %-10s %d\n", "dword", wTypeName(t), d)

	_ = k2.SetQWordValue("qword", 1<<40)
	q, t, _ := k2.GetIntegerValue("qword")
	fmt.Printf("  %-14s %-10s %d\n", "qword", wTypeName(t), q)

	_ = k2.SetStringsValue("multi", []string{"a", "b,c", ""})
	m, t, err := k2.GetStringsValue("multi")
	fmt.Printf("  %-14s %-10s %q  err=%v\n", "multi", wTypeName(t), m, errShort(err))

	_ = k2.SetBinaryValue("binary", []byte{0x00, 0xff, 0x41})
	b, t, _ := k2.GetBinaryValue("binary")
	fmt.Printf("  %-14s %-10s %q\n", "binary", wTypeName(t), string(b))

	fmt.Println("\n--- the three shapes #15 has to know about ---")
	fmt.Println("  a value name containing a backslash:")
	err = k2.SetStringValue(`a\b`, "x")
	fmt.Printf("    SetStringValue(`a\\b`) -> %v\n", errShort(err))
	names, _ := k2.ReadValueNames(0)
	fmt.Printf("    the names now present  -> %q\n", names)

	fmt.Println("  an EMPTY value name, which is the Registry's default value:")
	err = k2.SetStringValue("", "the-default-value")
	dv, _, _ := k2.GetStringValue("")
	fmt.Printf("    SetStringValue(\"\") -> %v, reads back %q\n", errShort(err), dv)

	fmt.Println("  a value and a subkey with the SAME name under one key:")
	sub, _, err := registry.CreateKey(k2, "collide", registry.ALL_ACCESS)
	if err == nil {
		sub.Close()
	}
	err2 := k2.SetStringValue("collide", "a value")
	sk, _ := k2.ReadSubKeyNames(0)
	vn, _ := k2.ReadValueNames(0)
	fmt.Printf("    subkey err=%v  value err=%v\n", errShort(err), errShort(err2))
	fmt.Printf("    subkeys=%q  values=%q\n", sk, vn)
	fmt.Println("    If both exist, ADR-0003's prefix-free rule is STRICTER than the")
	fmt.Println("    Registry needs, which is the opposite of what its four-planes table")
	fmt.Println("    predicts for a tree plane.")
	registry.DeleteKey(k2, "collide")

	fmt.Println("\n--- case sensitivity, which decides the driver's injectivity obligation ---")
	_ = k2.SetStringValue("CaseProbe", "upper")
	lo, _, err := k2.GetStringValue("caseprobe")
	fmt.Printf("  wrote \"CaseProbe\", read \"caseprobe\" -> %q err=%v\n", lo, errShort(err))
	names2, _ := k2.ReadValueNames(0)
	fmt.Printf("  ReadValueNames reports the spelling  -> %q\n", pick(names2, "aseprobe"))
}

func isAdmin() (bool, error) {
	sid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false, err
	}
	return windows.Token(0).IsMember(sid)
}

func isAccessDenied(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "access is denied")
}

func errShort(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func wTypeName(t uint32) string {
	switch t {
	case registry.SZ:
		return "REG_SZ"
	case registry.EXPAND_SZ:
		return "EXPAND_SZ"
	case registry.BINARY:
		return "BINARY"
	case registry.DWORD:
		return "DWORD"
	case registry.QWORD:
		return "QWORD"
	case registry.MULTI_SZ:
		return "MULTI_SZ"
	}
	return fmt.Sprintf("type(%d)", t)
}

func pick(names []string, sub string) []string {
	var out []string
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), sub) {
			out = append(out, n)
		}
	}
	return out
}

var _ = os.Getenv
