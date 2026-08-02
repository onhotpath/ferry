//go:build windows

package main

// W4: the same driver against a real hive, plus the two questions that need
// one - permissions, and what case preservation does to a map key.

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

func runW4() {
	ctx := context.Background()
	base := `Software\ferry-proto-15-w4`
	wCleanReal(base)
	defer wCleanReal(base)
	st := wReal{root: registry.CURRENT_USER}

	fmt.Println("(a) W3's round trip, against a real hive rather than the fake")
	orig := WNoBool{
		Name: "svc", Port: 8080, Big: 1 << 40, Neg: -1,
		Blob: []byte{0x00, 0xff, 0x41}, Timeout: 30e9,
	}
	if err := Dump(ctx, orig, WRegSink{Store: st, Base: base, NumberAs: wDWORD}); err != nil {
		fmt.Println("    dump:", err)
	}
	fmt.Println("    what the hive holds:")
	for _, n := range wNames(st, base) {
		v, _, _ := st.GetValue(base, n)
		fmt.Printf("      %-10s %s\n", n, v)
	}
	got, err := Load[WNoBool](ctx, WRegSource{Store: st, Base: base}, WithSched(tAggregating))
	fmt.Printf("    load err=%v\n    equal=%v\n", errShortW(err), fmt.Sprint(orig) == fmt.Sprint(got))
	fmt.Printf("    %+v\n", got)

	fmt.Println("\n(b) the bool, on a real hive")
	bb := `Software\ferry-proto-15-w4-bool`
	wCleanReal(bb)
	defer wCleanReal(bb)
	k, _, _ := registry.CreateKey(registry.CURRENT_USER, bb, registry.ALL_ACCESS)
	_ = k.SetDWordValue("on", 1)
	k.Close()
	gb, errb := Load[WBoolConf](ctx, WRegSource{Store: st, Base: bb}, WithSched(tAggregating))
	fmt.Printf("    a REG_DWORD 1 written by any other program -> On=%v err=%v\n", gb.On, errShortW(errb))

	fmt.Println("\n(c) REG_MULTI_SZ, on a real hive")
	mb := `Software\ferry-proto-15-w4-multi`
	wCleanReal(mb)
	defer wCleanReal(mb)
	k2, _, _ := registry.CreateKey(registry.CURRENT_USER, mb, registry.ALL_ACCESS)
	_ = k2.SetStringsValue("name", []string{"a", "b,c", ""})
	k2.Close()
	_, errm := Load[WNoBool](ctx, WRegSource{Store: st, Base: mb}, WithSched(tAggregating))
	fmt.Printf("    ferry loading it -> %v\n", errShortW(errm))

	fmt.Println("\n(d) the read-only refusal, ADR-0004's OpenWriterFunc")
	fmt.Println("    W0 measured that this runner is elevated and an administrator, so")
	fmt.Println("    HKLM is writable and a probe against it would measure nothing. The")
	fmt.Println("    refusal is produced by the ACCESS MASK on the handle instead, which")
	fmt.Println("    is a real ERROR_ACCESS_DENIED from the same API.")
	roStore := wReal{root: registry.CURRENT_USER, ro: true}
	err = Dump(ctx, orig, WRegSink{Store: roStore, Base: base, NumberAs: wDWORD, ReadOnly: true})
	fmt.Printf("    ReadOnly sink            -> %v\n", errShortW(err))
	fmt.Printf("    errors.Is(err, ErrReadOnly) -> %v\n", errors.Is(err, ErrReadOnly))
	fmt.Println("    and the raw API, so the refusal is not the prototype's invention:")
	ro, err := registry.OpenKey(registry.CURRENT_USER, base, registry.QUERY_VALUE)
	if err == nil {
		fmt.Printf("    SetStringValue via QUERY_VALUE handle -> %v\n", errShortW(ro.SetStringValue("x", "y")))
		ro.Close()
	}
	fmt.Println("    ADR-0004 puts this inside OpenWriterFunc rather than at Bind, and the")
	fmt.Println("    Registry is the plane where that is most obviously right: the access")
	fmt.Println("    mask is chosen when the key is OPENED, so there is nothing at bind")
	fmt.Println("    time that could have known.")
	fmt.Println("    A hive denied even to an administrator, for completeness:")
	_, _, err = registry.CreateKey(registry.LOCAL_MACHINE, `SECURITY\ferry-proto-15`, registry.ALL_ACCESS)
	fmt.Printf("    HKLM\\SECURITY -> %v\n", errShortW(err))

	fmt.Println("\n(e) case preservation, and the map key that depends on it")
	fmt.Println("    W0 measured that the Registry matches case-insensitively and")
	fmt.Println("    PRESERVES the spelling a value was created with. That is why this")
	fmt.Println("    driver folds only when CHECKING injectivity and writes the original")
	fmt.Println("    spelling - see w1_address.go. Run, a map key with capitals:")
	cb := `Software\ferry-proto-15-w4-case`
	wCleanReal(cb)
	defer wCleanReal(cb)
	in := WCaseConf{Limits: map[string]int{"Prod": 1, "staging": 2}}
	if err := Dump(ctx, in, WRegSink{Store: st, Base: cb, NumberAs: wDWORD}); err != nil {
		fmt.Println("    dump:", err)
	}
	fmt.Printf("    the value names the hive now holds: %q\n", wNames(st, cb+`\limits`))
	back, err := Load[WCaseConf](ctx, WRegSource{Store: st, Base: cb}, WithSched(tAggregating))
	fmt.Printf("    loaded back: %v  err=%v  equal=%v\n", back.Limits, errShortW(err),
		fmt.Sprint(in.Limits) == fmt.Sprint(back.Limits))
	fmt.Println("    A driver that folded its emitted key would write `prod`, and the map")
	fmt.Println("    key would come back lower-cased - a silent round-trip failure that")
	fmt.Println("    ADR-0003's injectivity rule cannot catch, because one key is")
	fmt.Println("    trivially injective. That is the refinement this plane forces on")
	fmt.Println("    ADR-0003's \"a driver may fold, as part of its key function\".")

	fmt.Println("\n(f) a value and a subkey sharing a name, which core refuses and the")
	fmt.Println("    Registry allows")
	pb := `Software\ferry-proto-15-w4-prefix`
	wCleanReal(pb)
	defer wCleanReal(pb)
	pk, _, _ := registry.CreateKey(registry.CURRENT_USER, pb, registry.ALL_ACCESS)
	_ = pk.SetStringValue("db", "a value at /db")
	sk, _, err := registry.CreateKey(pk, "db", registry.ALL_ACCESS)
	if err == nil {
		_ = sk.SetStringValue("host", "a value at /db/host")
		sk.Close()
	}
	vn, _ := pk.ReadValueNames(0)
	sn, _ := pk.ReadSubKeyNames(0)
	pk.Close()
	fmt.Printf("    the hive holds values=%q and subkeys=%q at once\n", vn, sn)
	fmt.Printf("    core's prefix-free rule on the same pair: %v\n",
		errShortW(prefixFree([]Path{path("db"), path("db", "host")})))
	fmt.Println("    So ADR-0003's rule is strictly stricter than this plane needs. Its")
	fmt.Println("    stated justification is that core adopts the constraint TREE planes")
	fmt.Println("    impose so a schema is representable on every plane, and its own")
	fmt.Println("    four-planes table puts `ok` in the registry column for the")
	fmt.Println("    neighbouring row. The cost it priced - \"a schema nobody writes")
	fmt.Println("    deliberately\" - is confirmed, and the reason it is paid here is that")
	fmt.Println("    the Registry has TWO namespaces per key and ferry's address model has")
	fmt.Println("    one.")
}

func wNames(st wStore, sub string) []string {
	n, _ := st.ValueNames(sub)
	return n
}

// runW7 checks that REG_MULTI_SZ is an ordinary type on a real Windows
// install rather than a curiosity this ticket reached for.
//
// It reads well-known SYSTEM keys and prints the TYPE and the element COUNT
// only. No value data is printed: this repository is public and a runner's
// pending-rename list is full of its own file paths.
func runW7() {
	fmt.Println("is REG_MULTI_SZ actually common, or did this ticket reach for it?")
	fmt.Printf("  %-58s %-14s %s\n", "value", "type", "elements")
	for _, c := range []struct{ key, name string }{
		{`SYSTEM\CurrentControlSet\Services\Dnscache`, "DependOnService"},
		{`SYSTEM\CurrentControlSet\Services\LanmanWorkstation`, "DependOnService"},
		{`SYSTEM\CurrentControlSet\Control\Session Manager`, "PendingFileRenameOperations"},
		{`SYSTEM\CurrentControlSet\Control\Session Manager\Memory Management`, "PagingFiles"},
		{`SYSTEM\CurrentControlSet\Control\Network`, "FilterClasses"},
	} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, c.key, registry.QUERY_VALUE)
		if err != nil {
			fmt.Printf("  %-58s %s\n", c.name, "(key not present)")
			continue
		}
		_, typ, err := k.GetValue(c.name, nil)
		if err != nil {
			k.Close()
			fmt.Printf("  %-58s %s\n", c.name, "(value not present)")
			continue
		}
		n := -1
		if ss, _, e := k.GetStringsValue(c.name); e == nil {
			n = len(ss)
		}
		k.Close()
		fmt.Printf("  %-58s %-14s %d\n", c.name, wTypeName(typ), n)
	}
}
