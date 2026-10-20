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
	"github.com/stretchr/testify/require"

	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/component-base/metrics/legacyregistry"

	"sigs.k8s.io/dra-example-driver/pkg/metrics"
)

// fakeStatusUpdate returns errors from errs in order, then nil, and records
// every call.
type fakeStatusUpdate struct {
	mu    sync.Mutex
	errs  []error
	calls int
	block chan struct{}
}

func (f *fakeStatusUpdate) update(ctx context.Context, _, _ string, _ types.UID, _ ...resourceapi.AllocatedDeviceStatus) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.errs) == 0 {
		return nil
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	return err
}

func (f *fakeStatusUpdate) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newTestStatusUpdater(t *testing.T, update deviceStatusUpdateFunc) *deviceStatusUpdater {
	t.Helper()
	u := newDeviceStatusUpdater(update)
	u.maxAttempts = 4
	u.backoff.Duration = time.Millisecond
	u.backoff.Cap = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	u.Start(ctx)
	t.Cleanup(func() {
		cancel()
		u.Stop()
	})
	return u
}

func testStatusClaim(uid string) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "claim", UID: types.UID(uid)}}
}

func (u *deviceStatusUpdater) hasPending(uid types.UID) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, ok := u.attempts[uid]
	return ok
}

var testDeviceStatuses = []resourceapi.AllocatedDeviceStatus{{Driver: "gpu.example.com", Pool: "node", Device: "gpu-0"}}

func TestDeviceStatusUpdater(t *testing.T) {
	transient := apierrors.NewServiceUnavailable("unavailable")
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "resource.k8s.io", Resource: "resourceclaims"}, "claim")

	tests := map[string]struct {
		errs        []error
		wantCalls   int
		wantOutcome string
	}{
		"success on first attempt":                  {errs: nil, wantCalls: 1, wantOutcome: metrics.DeviceStatusResultSuccess},
		"transient errors are retried":              {errs: []error{transient, transient}, wantCalls: 3, wantOutcome: metrics.DeviceStatusResultSuccess},
		"not found is not retried":                  {errs: []error{notFound}, wantCalls: 1, wantOutcome: metrics.DeviceStatusResultPermanentError},
		"replaced claim is not retried":             {errs: []error{errClaimReplaced}, wantCalls: 1, wantOutcome: metrics.DeviceStatusResultPermanentError},
		"transient retries are bounded":             {errs: []error{transient, transient, transient, transient, transient, transient}, wantCalls: 4, wantOutcome: metrics.DeviceStatusResultExhausted},
		"arbitrary errors are treated as transient": {errs: []error{errors.New("connection refused")}, wantCalls: 2, wantOutcome: metrics.DeviceStatusResultSuccess},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			retriesBefore := deviceStatusUpdates(t, metrics.DeviceStatusResultRetry)
			outcomeBefore := deviceStatusUpdates(t, tc.wantOutcome)
			f := &fakeStatusUpdate{errs: tc.errs}
			u := newTestStatusUpdater(t, f.update)
			claim := testStatusClaim("uid-1")

			u.Enqueue(context.Background(), claim, testDeviceStatuses)

			require.Eventually(t, func() bool { return !u.hasPending(claim.UID) }, 5*time.Second, time.Millisecond)
			// Give any erroneous extra retry a chance to happen.
			time.Sleep(20 * time.Millisecond)
			assert.Equal(t, tc.wantCalls, f.Calls())
			assert.Equal(t, float64(tc.wantCalls-1), deviceStatusUpdates(t, metrics.DeviceStatusResultRetry)-retriesBefore, "retry count")
			assert.Equal(t, float64(1), deviceStatusUpdates(t, tc.wantOutcome)-outcomeBefore, "final outcome count")
		})
	}
}

func deviceStatusUpdates(t *testing.T, result string) float64 {
	t.Helper()
	families, err := legacyregistry.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != "dra_example_driver_device_status_updates_total" {
			continue
		}
		for _, m := range family.GetMetric() {
			for _, label := range m.GetLabel() {
				if label.GetName() == "result" && label.GetValue() == result {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	t.Fatalf("no device_status_updates_total series with result=%q", result)
	return 0
}

func TestDeviceStatusUpdaterEnqueueDoesNotBlock(t *testing.T) {
	f := &fakeStatusUpdate{block: make(chan struct{})}
	u := newTestStatusUpdater(t, f.update)
	defer close(f.block)

	done := make(chan struct{})
	go func() {
		u.Enqueue(context.Background(), testStatusClaim("uid-1"), testDeviceStatuses)
		u.Enqueue(context.Background(), testStatusClaim("uid-2"), testDeviceStatuses)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Enqueue blocked on a hung status update")
	}
}

func TestDeviceStatusUpdaterCancelAbortsInFlightUpdate(t *testing.T) {
	started := make(chan struct{})
	returned := make(chan struct{})
	var calls sync.Mutex
	n := 0
	u := newDeviceStatusUpdater(func(ctx context.Context, _, _ string, _ types.UID, _ ...resourceapi.AllocatedDeviceStatus) error {
		calls.Lock()
		n++
		call := n
		calls.Unlock()
		if call == 1 {
			close(started)
			<-ctx.Done()
			close(returned)
			return ctx.Err()
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); u.Stop() }()
	u.Start(ctx)
	claim := testStatusClaim("uid-1")

	droppedBefore := deviceStatusUpdates(t, metrics.DeviceStatusResultDropped)
	u.Enqueue(ctx, claim, testDeviceStatuses)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("update did not start")
	}
	u.Cancel(claim.UID)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("Cancel did not abort the in-flight update")
	}
	time.Sleep(20 * time.Millisecond)

	assert.False(t, u.hasPending(claim.UID))
	calls.Lock()
	defer calls.Unlock()
	assert.Equal(t, 1, n, "a cancelled update must not be retried or published later")
	assert.Equal(t, float64(1), deviceStatusUpdates(t, metrics.DeviceStatusResultDropped)-droppedBefore)
}

func TestDeviceStatusUpdaterStopAbortsInFlightUpdate(t *testing.T) {
	started := make(chan struct{})
	u := newDeviceStatusUpdater(func(ctx context.Context, _, _ string, _ types.UID, _ ...resourceapi.AllocatedDeviceStatus) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	// The parent context stays active. Stop must cancel the updater's own
	// context, otherwise this call blocks until the attempt timeout (30s).
	u.Start(context.Background())
	u.Enqueue(context.Background(), testStatusClaim("uid-1"), testDeviceStatuses)

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("update did not start")
	}

	stopped := make(chan struct{})
	go func() {
		u.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return while an update was in flight")
	}
}

func TestDeviceStatusUpdaterSupersededAttemptIsDropped(t *testing.T) {
	claim := testStatusClaim("uid-1")
	replaced := []resourceapi.AllocatedDeviceStatus{{Driver: "gpu.example.com", Pool: "node", Device: "gpu-1"}}
	started := make(chan struct{})

	var mu sync.Mutex
	var published []resourceapi.AllocatedDeviceStatus
	calls := 0

	u := newTestStatusUpdater(t, func(ctx context.Context, _, _ string, _ types.UID, devices ...resourceapi.AllocatedDeviceStatus) error {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		mu.Lock()
		published = append([]resourceapi.AllocatedDeviceStatus(nil), devices...)
		mu.Unlock()
		return nil
	})

	droppedBefore := deviceStatusUpdates(t, metrics.DeviceStatusResultDropped)
	retriesBefore := deviceStatusUpdates(t, metrics.DeviceStatusResultRetry)
	u.Enqueue(context.Background(), claim, testDeviceStatuses)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first update did not start")
	}
	// Replace the payload while the first attempt is still in flight. The
	// old attempt must be cancelled instead of publishing or consuming the
	// new attempt's retry budget.
	u.Enqueue(context.Background(), claim, replaced)

	require.Eventually(t, func() bool { return !u.hasPending(claim.UID) }, 5*time.Second, time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, replaced, published)
	assert.Equal(t, float64(1), deviceStatusUpdates(t, metrics.DeviceStatusResultDropped)-droppedBefore)
	assert.Equal(t, float64(0), deviceStatusUpdates(t, metrics.DeviceStatusResultRetry)-retriesBefore)
}

func TestUpdateDeviceStatusChecksClaimUID(t *testing.T) {
	existing := &resourceapi.ResourceClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "claim", UID: "new-uid"}}
	s := &DeviceState{coreClient: fakeclient.NewClientset(existing)}
	status := resourceapi.AllocatedDeviceStatus{
		Driver: "gpu.example.com", Pool: "node", Device: "gpu-0",
		Data: &runtime.RawExtension{Raw: []byte(`{"uuid":"x"}`)},
	}

	err := s.updateDeviceStatus(context.Background(), "ns", "claim", "old-uid", status)
	require.ErrorIs(t, err, errClaimReplaced)
	assert.True(t, isPermanentDeviceStatusError(err))

	require.NoError(t, s.updateDeviceStatus(context.Background(), "ns", "claim", "new-uid", status))
	got, err := s.coreClient.ResourceV1().ResourceClaims("ns").Get(context.Background(), "claim", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, got.Status.Devices, 1)
	assert.Equal(t, status.Data, got.Status.Devices[0].Data)

	err = s.updateDeviceStatus(context.Background(), "ns", "missing", "uid", status)
	assert.True(t, apierrors.IsNotFound(err))
	assert.True(t, isPermanentDeviceStatusError(err))
}
