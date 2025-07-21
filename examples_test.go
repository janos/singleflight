// Copyright (c) 2025, Janoš Guljaš <janos@resenje.org>
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package singleflight_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"resenje.org/singleflight"
)

// ExampleGroup_Do demonstrates the basic usage of singleflight.
// Multiple concurrent calls to Do with the same key will result in the
// underlying function being called only once.
func ExampleGroup_Do() {
	var g singleflight.Group[string, string]
	var wg sync.WaitGroup
	var callCount int32

	fn := func(ctx context.Context) (string, error) {
		atomic.AddInt32(&callCount, 1)
		time.Sleep(10 * time.Millisecond) // Simulate work
		return "result", nil
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, shared, err := g.Do(context.Background(), "key", fn)
			if err != nil {
				log.Printf("unexpected error: %v", err)
				return
			}
			fmt.Printf("Got result: %s, Shared: %t\n", v, shared)
		}()
	}

	wg.Wait()
	fmt.Printf("Function was called %d time(s).\n", atomic.LoadInt32(&callCount))

	// Unordered output:
	// Got result: result, Shared: true
	// Got result: result, Shared: true
	// Got result: result, Shared: true
	// Got result: result, Shared: true
	// Got result: result, Shared: true
	// Got result: result, Shared: true
	// Got result: result, Shared: true
	// Got result: result, Shared: true
	// Got result: result, Shared: true
	// Got result: result, Shared: true
	// Function was called 1 time(s).
}

// ExampleGroup_Do_error shows how an error returned by the function
// is propagated to all callers.
func ExampleGroup_Do_error() {
	var g singleflight.Group[string, any]
	wantErr := errors.New("something went wrong")

	v, shared, err := g.Do(context.Background(), "key", func(ctx context.Context) (any, error) {
		return nil, wantErr
	})

	fmt.Printf("Value: %v, Shared: %t, Error: %v\n", v, shared, err)

	// Output:
	// Value: <nil>, Shared: false, Error: something went wrong
}

// ExampleGroup_Do_panic demonstrates that if the executed function panics,
// the panic is recovered and returned as an error to all waiting callers.
func ExampleGroup_Do_panic() {
	var g singleflight.Group[string, string]
	var wg sync.WaitGroup

	fn := func(_ context.Context) (string, error) {
		panic("critical failure")
	}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					// Check if the recovered error contains the panic message and stack trace.
					if err, ok := r.(error); ok && strings.Contains(err.Error(), "critical failure") && strings.Contains(err.Error(), "stack") {
						fmt.Println("Recovered expected panic")
					}
				}
			}()
			_, _, _ = g.Do(context.Background(), "key", fn)
		}()
	}

	wg.Wait()

	// Unordered output:
	// Recovered expected panic
	// Recovered expected panic
	// Recovered expected panic
}

// ExampleGroup_Do_contextValues demonstrates that context values from the
// first caller are passed to the executed function.
func ExampleGroup_Do_contextValues() {
	var g singleflight.Group[string, string]
	type key string
	const traceIDKey key = "traceID"

	// The first caller has a context with a trace ID.
	ctx1 := context.WithValue(context.Background(), traceIDKey, "abc-123")

	// The second caller has a different context.
	ctx2 := context.WithValue(context.Background(), traceIDKey, "xyz-789")

	var wg sync.WaitGroup
	wg.Add(2)

	var resultFromFn string
	go func() {
		defer wg.Done()
		_, _, _ = g.Do(ctx1, "key", func(ctx context.Context) (string, error) {
			// This function will be executed with ctx1.
			id, _ := ctx.Value(traceIDKey).(string)
			resultFromFn = fmt.Sprintf("executed with trace ID: %s", id)
			return resultFromFn, nil
		})
	}()

	// Give the first goroutine a head start to ensure it's the first caller.
	time.Sleep(5 * time.Millisecond)

	go func() {
		defer wg.Done()
		// This caller will wait and get the result from the first call.
		// Its context value ("xyz-789") will not be seen by the function.
		_, _, _ = g.Do(ctx2, "key", func(ctx context.Context) (string, error) { return "", nil })
	}()

	wg.Wait()

	fmt.Println(resultFromFn)

	// Output:
	// executed with trace ID: abc-123
}
