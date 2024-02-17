// Copyright (c) 2019, Janoš Guljaš <janos@resenje.org>
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package singleflight_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"resenje.org/singleflight"

	"go.uber.org/goleak"
)

func TestDo(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	var g singleflight.Group[string, string]

	want := "val"
	got, shared, err := g.Do(context.Background(), "key", func(_ context.Context) (string, error) {
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

func TestDo_concurrentAccess(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	var g singleflight.Group[string, string]

	want := "val"
	key := "key"
	var wg sync.WaitGroup
	n := 100

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			got, shared, err := g.Do(context.Background(), key, func(_ context.Context) (string, error) {
				return want, nil
			})
			if err != nil {
				t.Error(err)
			}
			_ = shared // read the shared to test the concurrent access
			if got != want {
				t.Errorf("got value %v, want %v", got, want)
			}
			time.Sleep(5 * time.Millisecond)
		}(i)
	}
	wg.Wait()
}

func TestDo_error(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	var g singleflight.Group[string, string]
	wantErr := errors.New("test error")
	got, _, err := g.Do(context.Background(), "key", func(_ context.Context) (string, error) {
		return "", wantErr
	})
	if err != wantErr {
		t.Errorf("got error %v, want %v", err, wantErr)
	}
	if got != "" {
		t.Errorf("unexpected value %#v", got)
	}
}

func TestDo_multipleCalls(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	var g singleflight.Group[string, string]

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
			got[i], shared[i], err[i] = g.Do(context.Background(), "key", func(_ context.Context) (string, error) {
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
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	var g singleflight.Group[string, string]

	wantPrefix := "val"
	counter := 0
	fn := func(_ context.Context) (string, error) {
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
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	done := make(chan struct{})
	defer close(done)

	var g singleflight.Group[string, string]

	want := "val"
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	got, shared, err := g.Do(ctx, "key", func(_ context.Context) (string, error) {
		select {
		case <-time.After(time.Second):
		case <-done:
		}
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
	if got != "" {
		t.Errorf("unexpected value %#v", got)
	}
}

func TestDo_cancelContextSecond(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	done := make(chan struct{})
	defer close(done)

	var g singleflight.Group[string, string]

	want := "val"
	fn := func(_ context.Context) (string, error) {
		select {
		case <-time.After(time.Second):
		case <-done:
		}
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
	if got != "" {
		t.Errorf("unexpected value %#v", got)
	}
}

func TestDo_callDoAfterCancellation(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	done := make(chan struct{})
	defer close(done)

	var g singleflight.Group[string, string]

	callCounter := new(atomic.Uint64)
	fn := func(_ context.Context) (string, error) {
		callCounter.Add(1)
		select {
		case <-time.After(time.Second):
		case <-done:
		}
		return "", nil
	}

	go func() {
		// keep the function call active for long period (1 second)
		if _, _, err := g.Do(context.Background(), "key", fn); err != nil {
			panic(err)
		}
	}()

	{ // make another call that is canceled shortly (100 milliseconds)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, _, err := g.Do(ctx, "key", fn)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}

	want := uint64(1)

	if got := callCounter.Load(); got != want {
		t.Errorf("got call counter %v, want %v", got, want)
	}

	{ // make another call after the previous call cancellation
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, _, err := g.Do(ctx, "key", fn)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}

	if got := callCounter.Load(); got != want {
		t.Errorf("got call counter %v, want %v", got, want)
	}
}

func TestForget(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	done := make(chan struct{})
	defer close(done)

	var g singleflight.Group[string, string]

	wantPrefix := "val"
	var counter uint64
	firstCall := make(chan struct{})
	fn := func(_ context.Context) (string, error) {
		c := atomic.AddUint64(&counter, 1)
		if c == 1 {
			close(firstCall)
			select {
			case <-time.After(time.Second):
			case <-done:
			}
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

func TestDo_multipleCallsCanceled(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	const n = 5

	for lastCall := 0; lastCall < n; lastCall++ {
		lastCall := lastCall
		t.Run(fmt.Sprintf("last call %v of %v", lastCall, n), func(t *testing.T) {
			done := make(chan struct{})
			defer close(done)

			var g singleflight.Group[string, string]

			var counter int32

			fnCalled := make(chan struct{})
			fnErrChan := make(chan error)
			var mu sync.Mutex
			contexts := make([]context.Context, n)
			cancelFuncs := make([]context.CancelFunc, n)
			var wg sync.WaitGroup
			wg.Add(n)
			for i := 0; i < n; i++ {
				go func(i int) {
					defer wg.Done()
					ctx, cancel := context.WithCancel(context.Background())
					mu.Lock()
					contexts[i] = ctx
					cancelFuncs[i] = cancel
					mu.Unlock()
					_, _, _ = g.Do(ctx, "key", func(ctx context.Context) (string, error) {
						atomic.AddInt32(&counter, 1)
						close(fnCalled)
						var err error
						select {
						case <-ctx.Done():
							err = ctx.Err()
							if err == nil {
								err = errors.New("got unexpected <nil> error from context")
							}
						case <-time.After(10 * time.Second):
							err = errors.New("unexpected timeout, context not canceled")
						case <-done:
						}

						fnErrChan <- err

						return "", nil
					})
				}(i)
			}
			select {
			case <-fnCalled:
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting for function to be called")
			}

			// Ensure that n goroutines are waiting at the select case in Group.wait.
			// Update the line number on changes.
			waitStacks(t, "resenje.org/singleflight/singleflight.go:68", n, 2*time.Second)

			// cancel all but one calls
			for i := 0; i < n; i++ {
				if i == lastCall {
					continue
				}
				mu.Lock()
				cancelFuncs[i]()
				<-contexts[i].Done()
				mu.Unlock()
			}

			select {
			case err := <-fnErrChan:
				t.Fatalf("got unexpected error in function: %v", err)
			default:
			}

			// Ensure that only the last goroutine is waiting at the select case in Group.wait.
			// Update the line number on changes.
			waitStacks(t, "resenje.org/singleflight/singleflight.go:68", 1, 2*time.Second)

			mu.Lock()
			cancelFuncs[lastCall]()
			mu.Unlock()

			wg.Wait()

			select {
			case err := <-fnErrChan:
				if err != context.Canceled {
					t.Fatalf("got unexpected error in function %v, want %v", err, context.Canceled)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting for the error")
			}
		})
	}
}

func TestDo_preserveContextValues(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	var g singleflight.Group[string, any]

	type KeyType string
	const key KeyType = "foo"

	callerCtx := context.WithValue(context.Background(), key, "bar")

	val, _, err := g.Do(callerCtx, "key", func(ctx context.Context) (any, error) {
		return ctx.Value(key), nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if val != "bar" {
		t.Error("the context should not lose the values")
	}
}

func waitStacks(t *testing.T, loc string, count int, timeout time.Duration) {
	t.Helper()

	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		// Ensure that exact n goroutines are waiting at the desired stack trace.
		var buf bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&buf, 2); err != nil {
			t.Fatal(err)
		}
		c := strings.Count(buf.String(), loc)
		if c == count {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
