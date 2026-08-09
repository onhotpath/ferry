// Package main is the #309 root-leaf evidence harness. PROTOTYPE ONLY.
package main

import (
	"context"
	"maps"
	"strings"
	"sync"
)

// memKV is an in-memory kv.Client.
type memKV struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemKV() *memKV { return &memKV{m: map[string][]byte{}} }

func (k *memKV) Get(_ context.Context, key string) ([]byte, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	v, ok := k.m[key]

	return v, ok, nil
}

func (k *memKV) List(_ context.Context, prefix string) (map[string][]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	out := map[string][]byte{}

	for key, v := range k.m {
		if strings.HasPrefix(key, prefix) {
			out[key] = v
		}
	}

	return out, nil
}

func (k *memKV) Put(_ context.Context, key string, value []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.m[key] = value

	return nil
}

func (k *memKV) Delete(_ context.Context, key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	delete(k.m, key)

	return nil
}

func (k *memKV) snapshot() map[string][]byte {
	k.mu.Lock()
	defer k.mu.Unlock()

	return maps.Clone(k.m)
}
