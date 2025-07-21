// Copyright (c) 2025, Janoš Guljaš <janos@resenje.org>
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal_test

import (
	"context"
	"testing"

	sync_singleflight "golang.org/x/sync/singleflight"
	"resenje.org/singleflight"
)

// BenchmarkThisSingleflight benchmarks the current singleflight implementation.
// It simulates high contention by having many goroutines request the same key.
func BenchmarkThisSingleflight(b *testing.B) {
	var g singleflight.Group[string, string]
	fn := func(ctx context.Context) (string, error) {
		return "value", nil
	}
	key := "key"

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = g.Do(context.Background(), key, fn)
		}
	})
}

// BenchmarkStandardSingleflight benchmarks the standard library's singleflight implementation.
// It uses the same high-contention simulation for a fair comparison.
func BenchmarkStandardSingleflight(b *testing.B) {
	var g sync_singleflight.Group
	fn := func() (interface{}, error) {
		return "value", nil
	}
	key := "key"

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = g.Do(key, fn)
		}
	})
}
