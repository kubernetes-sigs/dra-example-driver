/*
 * Copyright The Kubernetes Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/dynamic-resource-allocation/resourceslice"
)

func TestRefreshDevicesLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var refreshes, publishes int
	results := []struct {
		changed bool
		err     error
	}{{true, nil}, {false, nil}, {false, errors.New("list failed")}, {true, nil}}
	refresh := func() (resourceslice.DriverResources, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		r := results[refreshes]
		refreshes++
		return resourceslice.DriverResources{}, r.changed, r.err
	}
	publish := func(context.Context, resourceslice.DriverResources) error {
		mu.Lock()
		defer mu.Unlock()
		publishes++
		return nil
	}
	counts := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return refreshes, publishes
	}

	trigger := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		refreshDevicesLoop(ctx, trigger, 10*time.Millisecond, refresh, publish)
	}()

	// A burst of triggers is one refresh.
	trigger <- struct{}{}
	time.Sleep(2 * time.Millisecond)
	select {
	case trigger <- struct{}{}:
	default:
	}
	assert.Eventually(t, func() bool { r, p := counts(); return r == 1 && p == 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	r, _ := counts()
	assert.Equal(t, 1, r, "the burst must be covered by one refresh")

	// Unchanged devices and refresh errors publish nothing; the loop goes on.
	for want := 2; want <= 4; want++ {
		trigger <- struct{}{}
		assert.Eventually(t, func() bool { r, _ := counts(); return r == want }, time.Second, 5*time.Millisecond)
	}
	assert.Eventually(t, func() bool { _, p := counts(); return p == 2 }, time.Second, 5*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the loop did not stop with its context")
	}
}
