# Singleflight

[![GoDoc](https://godoc.org/resenje.org/singleflight?status.svg)](https://godoc.org/resenje.org/singleflight)
[![Go](https://github.com/janos/singleflight/workflows/Go/badge.svg)](https://github.com/janos/singleflight/actions?query=workflow%3AGo)
[![Go Report Card](https://goreportcard.com/badge/resenje.org/singleflight)](https://goreportcard.com/report/resenje.org/singleflight)

Package `singleflight` provides a generic, context-aware duplicate function call suppression mechanism for Go.

It's an enhanced alternative to the standard `golang.org/x/sync/singleflight`, designed to solve common concurrency problems with better ergonomics and more intelligent cancellation handling.

## Motivation: The "Thundering Herd" Problem

A common challenge in high-concurrency applications is the "thundering herd" problem. This issue arises when a single resource is requested by a multitude of clients simultaneously, leading the service to inundate its backend systems with redundant operations. For example, a high-traffic web service might issue thousands of identical queries to a database when a popular user's profile is accessed concurrently, which can easily overload the system.

The standard Go `x/sync/singleflight` package was designed to solve this by de-duplicating concurrent function calls with the same key, ensuring that an expensive operation is only executed once.

This package builds on that solid foundation by introducing modern Go features. It adds type safety with generics and provides a more sophisticated context cancellation model. This allows for more efficient resource management in complex concurrent applications where multiple callers might have different deadlines or cancellation signals.

## Key Features

* **Full Support for Generics:** Provides a type-safe API, eliminating the need for type assertions.

* **Context Cancellation:** The function's context is only canceled when *all* waiting callers have had their contexts canceled. This prevents premature cancellations while ensuring resources are cleaned up as soon as they are no longer needed.

* **Drop-in Enhancement:** Offers a familiar API that's easy to adopt for anyone familiar with the standard `singleflight`.

## Installation

```sh
go get resenje.org/singleflight
```

## Basic Usage

Here's a simple example of how to protect a function from concurrent calls.

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"resenje.org/singleflight"
)

func main() {
	var g singleflight.Group[string, string]
	var wg sync.WaitGroup
	n := 5

	// Make 5 concurrent calls for the same key.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()

			// The function will only be executed for the first call.
			// All other calls will wait and receive the same result.
			v, shared, err := g.Do(context.Background(), "user-profile", func(ctx context.Context) (string, error) {
				fmt.Println("Fetching user profile from database...")
				time.Sleep(100 * time.Millisecond) // Simulate slow DB query
				return "User data for John Doe", nil
			})

			if err != nil {
				fmt.Printf("Goroutine %d: encountered an error: %v\n", i, err)
				return
			}

			fmt.Printf("Goroutine %d: got result '%s' (shared: %t)\n", i, v, shared)
		}(i)
	}

	wg.Wait()
}
```

Output:

```
Fetching user profile from database...
Goroutine 2: got result 'User data for John Doe' (shared: true)
Goroutine 0: got result 'User data for John Doe' (shared: true)
Goroutine 4: got result 'User data for John Doe' (shared: true)
Goroutine 3: got result 'User data for John Doe' (shared: true)
Goroutine 1: got result 'User data for John Doe' (shared: true)
```

"Fetching user profile..." was only printed once, and all five goroutines received the same result.

## Performance

This package is highly performant, with no overhead comparable to the standard library's `x/sync/singleflight`.

Benchmarks run on a multi-core machine under high contention show the following:

```
goos: darwin
goarch: arm64
pkg: resenje.org/singleflight/internal
cpu: Apple M1 Pro
BenchmarkThisSingleflight-10             4508893               261.8 ns/op             0 B/op          0 allocs/op
BenchmarkStandardSingleflight-10         4493191               268.5 ns/op            67 B/op          0 allocs/op
PASS
```

The results show that this implementation is in the same performance class as the standard library, making it a safe and efficient choice for production use.
