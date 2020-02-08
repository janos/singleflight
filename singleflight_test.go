// Copyright (c) 2019, Janoš Guljaš <janos@resenje.org>
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package singleflight_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"resenje.org/singleflight"
)

func TestDo(t *testing.T) {
	var g singleflight.Group

	want := "val"
	got, shared, err := g.Do(context.Background(), "key", func() (interface{}, error) {
		return want, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if shared {
		t.Error("the value should not be shared")
	}
	if got != want {
		t.Errorf("got value %v, want %v", got, want)
	}
}

func TestDo_error(t *testing.T) {
	var g singleflight.Group
	wantErr := errors.New("test error")
	got, _, err := g.Do(context.Background(), "key", func() (interface{}, error) {
		return nil, wantErr
	})
	if err != wantErr {
		t.Errorf("got error %v, want %v", err, wantErr)
	}
	if got != nil {
		t.Errorf("unexpected value %#v", got)
	}
}

func TestDo_multipleCalls(t *testing.T) {
	var g singleflight.Group

	want := "val"
	var counter int32

	n := 10
	got := make([]interface{}, n)
	shared := make([]bool, n)
	err := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			got[i], shared[i], err[i] = g.Do(context.Background(), "key", func() (interface{}, error) {
				atomic.AddInt32(&counter, 1)
				time.Sleep(100 * time.Millisecond)
				return want, nil
			})
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&counter); got != 1 {
		t.Errorf("function called %v times, should only once", got)
	}

	for i := 0; i < n; i++ {
		if err[i] != nil {
			t.Errorf("call %v: unexpected error: %v", i, err[i])
		}
		if !shared[i] {
			t.Errorf("call %v: the value should be shared", i)
		}
		if got[i] != want {
			t.Errorf("call %v: got value %v, want %v", i, got[i], want)
		}
	}
}

func TestDo_callRemoval(t *testing.T) {
	var g singleflight.Group

	wantPrefix := "val"
	counter := 0
	fn := func() (interface{}, error) {
		counter++
		return wantPrefix + strconv.Itoa(counter), nil
	}

	got, shared, err := g.Do(context.Background(), "key", fn)
	if err != nil {
		t.Fatal(err)
	}
	if shared {
		t.Error("the value should not be shared")
	}
	if want := wantPrefix + "1"; got != want {
		t.Errorf("got value %v, want %v", got, want)
	}

	got, shared, err = g.Do(context.Background(), "key", fn)
	if err != nil {
		t.Fatal(err)
	}
	if shared {
		t.Error("the value should not be shared")
	}
	if want := wantPrefix + "2"; got != want {
		t.Errorf("got value %v, want %v", got, want)
	}
}

func TestDo_cancelContext(t *testing.T) {
	var g singleflight.Group

	want := "val"
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	got, shared, err := g.Do(ctx, "key", func() (interface{}, error) {
		time.Sleep(time.Second)
		return want, nil
	})
	if d := time.Since(start); d < 100*time.Microsecond || d > time.Second {
		t.Errorf("unexpected Do call duration %s", d)
	}
	if want := context.Canceled; err != want {
		t.Errorf("got error %v, want %v", err, want)
	}
	if shared {
		t.Error("the value should not be shared")
	}
	if got != nil {
		t.Errorf("unexpected value %#v", got)
	}
}

func TestDo_cancelContextSecond(t *testing.T) {
	var g singleflight.Group

	want := "val"
	fn := func() (interface{}, error) {
		time.Sleep(time.Second)
		return want, nil
	}
	go func() {
		if _, _, err := g.Do(context.Background(), "key", fn); err != nil {
			panic(err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	got, shared, err := g.Do(ctx, "key", fn)
	if d := time.Since(start); d < 100*time.Microsecond || d > time.Second {
		t.Errorf("unexpected Do call duration %s", d)
	}
	if want := context.Canceled; err != want {
		t.Errorf("got error %v, want %v", err, want)
	}
	if !shared {
		t.Error("the value should be shared")
	}
	if got != nil {
		t.Errorf("unexpected value %#v", got)
	}
}

func TestForget(t *testing.T) {
	var g singleflight.Group

	wantPrefix := "val"
	var counter uint64
	firstCall := make(chan struct{})
	fn := func() (interface{}, error) {
		c := atomic.AddUint64(&counter, 1)
		if c == 1 {
			close(firstCall)
			time.Sleep(time.Second)
		}
		return wantPrefix + strconv.FormatUint(c, 10), nil
	}

	go func() {
		if _, _, err := g.Do(context.Background(), "key", fn); err != nil {
			panic(err)
		}
	}()

	<-firstCall
	g.Forget("key")

	got, shared, err := g.Do(context.Background(), "key", fn)
	if err != nil {
		t.Fatal(err)
	}
	if shared {
		t.Error("the value should not be shared")
	}
	if want := wantPrefix + "2"; got != want {
		t.Errorf("got value %v, want %v", got, want)
	}
}
