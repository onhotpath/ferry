package main

// P3: the ticket comment says dump-side collision is "silent data loss with a
// nondeterministic winner". Check the second half. reflect field order is
// source order and is deterministic, so a serial dump may lose deterministically,
// which is a different and in some ways worse failure than a race.

import (
	"fmt"
	"reflect"
	"sync"
)

type flatSink struct {
	mu  sync.Mutex
	kv  map[string]string
	seq []string
}

func (s *flatSink) put(k, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kv[k] = v
	s.seq = append(s.seq, k)
}

type collider struct {
	A string `ferry:"host"`
	B string `ferry:"host"`
	C string `ferry:"host"`
}

func dumpNaive(v any, concurrent bool) map[string]string {
	sink := &flatSink{kv: map[string]string{}}
	rv := reflect.ValueOf(v)
	rt := rv.Type()
	var wg sync.WaitGroup
	for i := range rt.NumField() {
		f := rt.Field(i)
		key := f.Tag.Get("ferry")
		val := rv.Field(i).String()
		if concurrent {
			wg.Add(1)
			go func() { defer wg.Done(); sink.put(key, val) }()
			continue
		}
		sink.put(key, val)
	}
	wg.Wait()
	return sink.kv
}

func p3DumpLoss() {
	head("P3  what a dump-side collision actually does")

	in := collider{A: "first", B: "second", C: "third"}

	for _, mode := range []struct {
		label      string
		concurrent bool
	}{{"serial walk", false}, {"concurrent walk", true}} {
		winners := map[string]int{}
		for range 300 {
			winners[dumpNaive(in, mode.concurrent)["host"]]++
		}
		fmt.Printf("    %-16s over 300 dumps of 3 colliding fields: %v\n", mode.label, winners)
	}

	fmt.Println("    (fields written, in walk order, one dump):")
	sink := &flatSink{kv: map[string]string{}}
	rv := reflect.ValueOf(in)
	for i := range rv.NumField() {
		sink.put(rv.Type().Field(i).Tag.Get("ferry"), rv.Field(i).String())
	}
	fmt.Printf("        writes=%v surviving=%v lost=%d\n", sink.seq, sink.kv, len(sink.seq)-len(sink.kv))

	// And with the P2 rule in force: the type never compiles, so no dump runs.
	_, err := compile[collider]()
	fmt.Printf("    with the schema-compile rule in force: %v\n", err != nil)
}
