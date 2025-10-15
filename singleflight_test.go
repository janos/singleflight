// Copyright (c) 2019, Janoš Guljaš <janos@resenje.org>
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package singleflight_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"resenje.org/singleflight"
)

func TestDo(t *testing.T) {
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
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, string]

		want := "val"
		key := "key"
		var wg sync.WaitGroup
		n := 100

		for range n {
			wg.Go(func() {
				got, shared, err := g.Do(context.Background(), key, func(_ context.Context) (string, error) {
					time.Sleep(5 * time.Millisecond)
					return want, nil
				})
				if err != nil {
					t.Error(err)
				}
				_ = shared // read the shared to test the concurrent access
				if got != want {
					t.Errorf("got value %v, want %v", got, want)
				}
			})
		}
		wg.Wait()
	})
}

func TestDo_error(t *testing.T) {
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
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, string]

		want := "val"
		var counter int32

		n := 10
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				got, shared, err := g.Do(context.Background(), "key", func(_ context.Context) (string, error) {
					atomic.AddInt32(&counter, 1)
					time.Sleep(100 * time.Millisecond)
					return want, nil
				})
				if err != nil {
					t.Errorf("call %v: unexpected error: %v", i, err)
				}
				if !shared {
					t.Errorf("call %v: the value should be shared", i)
				}
				if got != want {
					t.Errorf("call %v: got value %v, want %v", i, got, want)
				}
			})
		}
		wg.Wait()

		if got := atomic.LoadInt32(&counter); got != 1 {
			t.Errorf("function called %v times, should only once", got)
		}
	})
}

func TestDo_callRemoval(t *testing.T) {
	var g singleflight.Group[string, string]

	wantPrefix := "val"
	counter := 0
	fn := func(_ context.Context) (string, error) {
		counter++
		return fmt.Sprintf("%s%d", wantPrefix, counter), nil
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
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, string]

		want := "val"
		ctx, cancel := context.WithCancel(t.Context())

		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		got, shared, err := g.Do(ctx, "key", func(ctx context.Context) (string, error) {
			<-ctx.Done() // This will block until the context is canceled by the test.
			return want, nil
		})

		if !errors.Is(err, context.Canceled) {
			t.Errorf("got error %v, want %v", err, context.Canceled)
		}
		if shared {
			t.Error("the value should not be shared")
		}
		if got != "" {
			t.Errorf("unexpected value %#v", got)
		}
	})
}

func TestDo_cancelContextSecond(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, string]

		fn := func(ctx context.Context) (string, error) {
			time.Sleep(200 * time.Millisecond) // This sleep is longer than the cancellation timer.
			return "val", nil
		}

		var wg sync.WaitGroup
		wg.Go(func() {
			// This call proceeds normally.
			_, _, _ = g.Do(t.Context(), "key", fn)
		})

		// Give the first goroutine a chance to start the Do call.
		synctest.Wait()

		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		got, shared, err := g.Do(ctx, "key", fn)

		if !errors.Is(err, context.Canceled) {
			t.Errorf("got error %v, want %v", err, context.Canceled)
		}
		if !shared {
			t.Error("the value should be shared")
		}
		if got != "" {
			t.Errorf("unexpected value %#v", got)
		}
		wg.Wait()
	})
}

func TestDo_callDoAfterCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, string]
		callCounter := new(atomic.Uint64)

		fn := func(_ context.Context) (string, error) {
			callCounter.Add(1)
			time.Sleep(1 * time.Second)
			return "done", nil
		}

		var wg sync.WaitGroup
		// Start the first long-running call.
		wg.Go(func() {
			_, _, _ = g.Do(t.Context(), "key", fn)
		})
		synctest.Wait() // Ensure the first call is running.

		// Make a second call that times out.
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		_, _, err := g.Do(ctx, "key", fn)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}

		if got := callCounter.Load(); got != 1 {
			t.Errorf("got call counter %v, want 1", got)
		}

		// Make a third call that should also time out while the first is still running.
		ctx2, cancel2 := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel2()
		_, _, err = g.Do(ctx2, "key", fn)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded on third call, got %v", err)
		}

		if got := callCounter.Load(); got != 1 {
			t.Errorf("got call counter %v, want 1", got)
		}
		wg.Wait()
	})
}

func TestDo_panic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const numGoroutines = 3
		const panicMessage = "test-panic-message"
		var g singleflight.Group[string, string]
		var wg sync.WaitGroup

		for range numGoroutines {
			wg.Go(func() {
				defer func() {
					r := recover()
					if r == nil {
						t.Error("expected a panic")
						return
					}
					err, ok := r.(error)
					if !ok || !strings.Contains(err.Error(), panicMessage) {
						t.Errorf("got unexpected panic value %+#v", r)
					}
				}()
				_, _, _ = g.Do(t.Context(), "key", func(_ context.Context) (string, error) {
					time.Sleep(10 * time.Millisecond)
					panic(panicMessage)
				})
			})
		}

		wg.Wait()

		// The work for "key" should be complete, and we should be able to
		// start a new call for the same key without panicking.
		const want = "hello"
		got, shared, err := g.Do(t.Context(), "key", func(_ context.Context) (string, error) {
			return want, nil
		})
		if got != want || shared || err != nil {
			t.Errorf("unexpected result (value=%v, shared=%v, err=%v)", got, shared, err)
		}
	})
}

func TestForget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, string]
		var counter atomic.Uint64

		fn := func(_ context.Context) (string, error) {
			c := counter.Add(1)
			if c == 1 {
				time.Sleep(time.Second) // First call is long.
			}
			return fmt.Sprintf("val%d", c), nil
		}

		var wg sync.WaitGroup
		// Start the first call in the background.
		wg.Go(func() {
			_, _, _ = g.Do(t.Context(), "key", fn)
		})

		// Wait until the first call's function is executing.
		synctest.Wait()
		if counter.Load() != 1 {
			t.Fatal("first call did not start")
		}

		g.Forget("key")

		// This second call should not wait for the first one.
		got, shared, err := g.Do(t.Context(), "key", fn)
		if err != nil {
			t.Fatal(err)
		}
		if shared {
			t.Error("the value should not be shared")
		}
		if want := "val2"; got != want {
			t.Errorf("got value %v, want %v", got, want)
		}
		if counter.Load() != 2 {
			t.Error("expected function to be called twice")
		}
		wg.Wait()
	})
}

func TestForgetMisbehaving(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, int]
		var wg sync.WaitGroup

		// Start a long-running operation.
		var firstFinished bool
		wg.Go(func() {
			_, _, _ = g.Do(t.Context(), "key", func(ctx context.Context) (int, error) {
				time.Sleep(time.Second)
				firstFinished = true
				return 1, nil
			})
		})

		// Wait for it to be in-flight, then forget it.
		synctest.Wait()
		g.Forget("key")

		// Start a second long-running operation for the same key.
		// This should execute immediately.
		var secondFinished bool
		wg.Go(func() {
			_, _, _ = g.Do(t.Context(), "key", func(ctx context.Context) (int, error) {
				time.Sleep(500 * time.Millisecond)
				secondFinished = true
				return 2, nil
			})
		})

		// Wait for the second operation to be in-flight.
		synctest.Wait()

		// While the second is running, make a third call.
		// This one should wait for the second call to complete.
		v, shared, err := g.Do(t.Context(), "key", func(ctx context.Context) (int, error) {
			t.Fatal("third function should not be called")
			return 3, nil
		})

		wg.Wait()

		if err != nil {
			t.Fatal(err)
		}
		if !shared {
			t.Error("third call should have shared the result of the second")
		}
		if v != 2 {
			t.Errorf("got %d, want 2", v)
		}
		if !secondFinished {
			t.Error("second call should have finished")
		}
		if !firstFinished {
			t.Error("first call should have finished in the background")
		}
	})
}

func TestDo_multipleCallsCanceled(t *testing.T) {
	const n = 5
	for lastCallToRemain := range n {
		t.Run(fmt.Sprintf("last_call_%d", lastCallToRemain), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var g singleflight.Group[string, string]
				var fnDidRun atomic.Bool
				fnErrChan := make(chan error, 1)

				ctxs := make([]context.Context, n)
				cancels := make([]context.CancelFunc, n)
				for i := range n {
					ctxs[i], cancels[i] = context.WithCancel(t.Context())
				}

				var wg sync.WaitGroup

				for i := range n {
					wg.Go(func() {
						_, _, _ = g.Do(ctxs[i], "key", func(ctx context.Context) (string, error) {
							fnDidRun.Store(true)
							<-ctx.Done()
							fnErrChan <- ctx.Err()
							return "", nil
						})
					})
				}

				synctest.Wait()
				if !fnDidRun.Load() {
					t.Fatal("function was not called")
				}

				for i := range n {
					if i != lastCallToRemain {
						cancels[i]()
					}
				}

				synctest.Wait()

				// The function's context should not be canceled yet. We can check by
				// seeing if the fnErrChan is empty.
				select {
				case err := <-fnErrChan:
					t.Fatalf("function context was canceled prematurely: %v", err)
				default:
					// Good, no error yet.
				}

				cancels[lastCallToRemain]()

				wg.Wait()

				select {
				case fnErr := <-fnErrChan:
					if !errors.Is(fnErr, context.Canceled) {
						t.Fatalf("function context had wrong error; got %v, want %v", fnErr, context.Canceled)
					}
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for function to report its error")
				}
			})
		})
	}
}

func TestDo_preserveContextValues(t *testing.T) {
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

// TestDo_FirstCallerCancelled verifies that the underlying function continues to
// execute even if the first caller's context is canceled, as long as other
// callers are still waiting.
func TestDo_FirstCallerCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, string]
		want := "result"
		fnDidComplete := new(atomic.Bool)

		fn := func(ctx context.Context) (string, error) {
			// This sleep is longer than the first caller's cancellation.
			time.Sleep(200 * time.Millisecond)
			// The function's context should not be canceled, so this should complete.
			if ctx.Err() != nil {
				return "", fmt.Errorf("function context was unexpectedly canceled: %w", ctx.Err())
			}
			fnDidComplete.Store(true)
			return want, nil
		}

		ctx1, cancel1 := context.WithCancel(t.Context())
		ctx2 := t.Context() // This context will not be cancelled.

		var wg sync.WaitGroup

		var err1 error
		// Start the first caller, which will trigger the function execution.
		wg.Go(func() {
			_, _, err1 = g.Do(ctx1, "key", fn)
		})

		// Wait for the function to be in-flight.
		synctest.Wait()

		var got2 string
		var shared2 bool
		var err2 error
		// Start the second caller, which will wait on the same function call.
		wg.Go(func() {
			got2, shared2, err2 = g.Do(ctx2, "key", fn)
		})

		// Wait for the second caller to also be waiting.
		synctest.Wait()

		// Cancel the first caller's context.
		cancel1()

		wg.Wait()

		// Verify the first caller got a cancellation error.
		if !errors.Is(err1, context.Canceled) {
			t.Errorf("first caller error = %v; want context.Canceled", err1)
		}

		// Verify the second caller got the correct result.
		if err2 != nil {
			t.Errorf("second caller error = %v; want nil", err2)
		}
		if got2 != want {
			t.Errorf("second caller got = %q; want %q", got2, want)
		}
		if !shared2 {
			t.Error("second caller should have a shared result")
		}

		// Verify the underlying function actually ran to completion.
		if !fnDidComplete.Load() {
			t.Error("function did not run to completion")
		}
	})
}

// TestDoAndForget_Race is a stress test to ensure that there are no race
// conditions between concurrent Do and Forget calls. This test should be run
// with the -race flag.
func TestDoAndForget_Race(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, string]
		key := "key"
		fn := func(ctx context.Context) (string, error) {
			// A small sleep makes a race more likely.
			time.Sleep(1 * time.Microsecond)
			return "value", nil
		}

		var wg sync.WaitGroup
		// Increase the number of goroutines to increase contention.
		const numGoroutines = 10

		// Each goroutine will rapidly call Do and Forget.
		for range numGoroutines {
			wg.Go(func() {
				// Run for a fixed number of iterations.
				for range 100 {
					_, _, _ = g.Do(t.Context(), key, fn)
					g.Forget(key)
				}
			})
		}

		wg.Wait()
	})
}

func TestPanicError_Unwrap(t *testing.T) {
	t.Run("with error value", func(t *testing.T) {
		var g singleflight.Group[string, string]
		wantErr := errors.New("test error")

		var recovered any
		func() {
			defer func() {
				recovered = recover()
			}()
			_, _, _ = g.Do(context.Background(), "key", func(ctx context.Context) (string, error) {
				panic(wantErr)
			})
		}()

		if recovered == nil {
			t.Fatal("expected a panic")
		}

		recoveredErr, ok := recovered.(error)
		if !ok {
			t.Fatalf("recovered value is not an error: %v", recovered)
		}

		if !errors.Is(recoveredErr, wantErr) {
			t.Errorf("errors.Is failed, expected to find %v in %v", wantErr, recoveredErr)
		}
	})

	t.Run("with non-error value", func(t *testing.T) {
		var g singleflight.Group[string, string]
		panicValue := "not an error"

		var recovered any
		func() {
			defer func() {
				recovered = recover()
			}()
			_, _, _ = g.Do(context.Background(), "key", func(ctx context.Context) (string, error) {
				panic(panicValue)
			})
		}()

		if recovered == nil {
			t.Fatal("expected a panic")
		}

		recoveredErr, ok := recovered.(error)
		if !ok {
			t.Fatalf("recovered value is not an error: %v", recovered)
		}

		if unwrapped := errors.Unwrap(recoveredErr); unwrapped != nil {
			t.Errorf("expected unwrapped error to be nil, got %v", unwrapped)
		}
	})
}
