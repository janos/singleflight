// Copyright (c) 2019, Janoš Guljaš <janos@resenje.org>
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package singleflight provides a duplicate function call suppression
// mechanism similar to golang.org/x/sync/singleflight with support
// for context cancelation.
package singleflight

import (
	"context"
	"sync"
)

// Group represents a class of work and forms a namespace in
// which units of work can be executed with duplicate suppression.
type Group struct {
	calls map[string]*call // lazily initialized
	mu    sync.Mutex       // protects calls
}

// Do executes and returns the results of the given function, making
// sure that only one execution is in-flight for a given key at a
// time. If a duplicate comes in, the duplicate caller waits for the
// original to complete and receives the same results.
// Passed context terminates the execution of Do function, not the passed
// function fn. If there are  multiple callers, context passed to one caller
// does not effect the execution and returned values of others.
// The return value shared indicates whether v was given to multiple callers.
func (g *Group) Do(ctx context.Context, key string, fn func() (interface{}, error)) (v interface{}, shared bool, err error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*call)
	}

	if c, ok := g.calls[key]; ok {
		c.shared = true
		g.mu.Unlock()

		return g.wait(ctx, key, c)
	}

	c := &call{
		done: make(chan struct{}),
	}
	g.calls[key] = c
	g.mu.Unlock()

	go func() {
		c.val, c.err = fn()
		close(c.done)
	}()

	return g.wait(ctx, key, c)
}

// wait for function passed to Do to finish or context to be done.
func (g *Group) wait(ctx context.Context, key string, c *call) (v interface{}, shared bool, err error) {
	select {
	case <-c.done:
		v = c.val
		err = c.err
	case <-ctx.Done():
		err = ctx.Err()
	}
	g.mu.Lock()
	if !c.forgotten {
		delete(g.calls, key)
	}
	g.mu.Unlock()
	return v, c.shared, err
}

// Forget tells the singleflight to forget about a key. Future calls
// to Do for this key will call the function rather than waiting for
// an earlier call to complete.
func (g *Group) Forget(key string) {
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		c.forgotten = true
	}
	delete(g.calls, key)
	g.mu.Unlock()
}

// call stores information about as single function call passed to Do function.
type call struct {
	// val and err hold the state about results of the function call.
	val interface{}
	err error

	// done channel signals that the function call is done.
	done chan struct{}

	// forgotten indicates whether Forget was called with this call's key
	// while the call was still in flight.
	forgotten bool

	// shared indicates if results val and err are passed to multiple callers.
	shared bool
}
