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
	"fmt"
	"sync"
	"time"

	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"sigs.k8s.io/dra-example-driver/pkg/metrics"
)

const (
	// deviceStatusBaseDelay and deviceStatusMaxDelay bound the exponential
	// backoff between attempts to publish device status for a single claim.
	deviceStatusBaseDelay = 500 * time.Millisecond
	deviceStatusMaxDelay  = 30 * time.Second
	// deviceStatusMaxRetries is how many times a transient failure is retried
	// before the update is given up on. With the delays above this is roughly
	// five minutes of retrying.
	deviceStatusMaxRetries = 15
	// deviceStatusAttemptTimeout bounds a single attempt so that a hung API
	// server cannot stall the attempt indefinitely.
	deviceStatusAttemptTimeout = 30 * time.Second
)

// errClaimReplaced is returned when the ResourceClaim with the expected
// namespace/name now has a different UID, i.e. the claim that was prepared no
// longer exists.
var errClaimReplaced = errors.New("ResourceClaim was replaced")

// deviceStatusUpdateFunc writes device status to a ResourceClaim. It must fail
// with errClaimReplaced if the claim found under ns/name does not have the
// given UID.
type deviceStatusUpdateFunc func(ctx context.Context, ns, name string, uid types.UID, devices ...resourceapi.AllocatedDeviceStatus) error

// statusAttempt is one in-flight publish for a claim. Cancelling it aborts
// the API call and the backoff wait.
type statusAttempt struct {
	cancel context.CancelFunc
}

// deviceStatusUpdater publishes ResourceClaim.status.devices asynchronously so
// that API server latency or failures never block NodePrepareResources.
//
// Each claim has its own goroutine. Transient failures are retried with
// exponential backoff up to deviceStatusMaxRetries. The delay is capped, but
// the attempt limit is not: wait.ExponentialBackoffWithContext stops as soon
// as Backoff.Cap is reached, which would end the retries early, so the
// goroutine steps a wait.Backoff itself. Permanent failures (the claim was
// deleted or replaced, the update is invalid, or the driver lacks permission)
// are not retried. Cancel and a newer Enqueue for the same claim abort the
// in-flight call so a released claim cannot publish afterwards. Every attempt
// is counted in the device_status_updates_total metric. Retries are logged at
// V(1), as they are expected while the API server is briefly unavailable;
// giving up is logged as an error with the claim's namespace, name and UID.
type deviceStatusUpdater struct {
	update deviceStatusUpdateFunc

	maxAttempts    int
	attemptTimeout time.Duration
	backoff        wait.Backoff

	mu       sync.Mutex
	cancel   context.CancelFunc
	done     <-chan struct{}
	attempts map[types.UID]*statusAttempt

	wg sync.WaitGroup
}

func newDeviceStatusUpdater(update deviceStatusUpdateFunc) *deviceStatusUpdater {
	return &deviceStatusUpdater{
		update:         update,
		maxAttempts:    deviceStatusMaxRetries + 1,
		attemptTimeout: deviceStatusAttemptTimeout,
		backoff: wait.Backoff{
			Duration: deviceStatusBaseDelay,
			Factor:   2,
			Cap:      deviceStatusMaxDelay,
		},
		attempts: make(map[types.UID]*statusAttempt),
	}
}

// Start retains a child of ctx. Cancelling the parent, or Stop, aborts every
// in-flight attempt. Start must be called before Enqueue.
func (u *deviceStatusUpdater) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	u.mu.Lock()
	u.cancel = cancel
	u.done = ctx.Done()
	u.mu.Unlock()

	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		<-ctx.Done()
		u.mu.Lock()
		defer u.mu.Unlock()
		for _, attempt := range u.attempts {
			attempt.cancel()
		}
	}()
}

// Stop cancels in-flight attempts and waits for them to exit. Updates that
// have not been published yet are dropped. Enqueue after Stop drops the update.
func (u *deviceStatusUpdater) Stop() {
	u.mu.Lock()
	cancel := u.cancel
	// Clear cancel before waiting so an Enqueue that has not yet called wg.Add
	// cannot start a goroutine this Wait would miss.
	u.cancel = nil
	u.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	u.wg.Wait()
}

// Enqueue publishes devices to the claim's status in the background, replacing
// any in-flight update for the same claim. The replacement starts a fresh
// backoff. The call does not wait for the API server.
//
// ctx carries the caller's logging values. Cancellation of ctx does not stop
// the update: WithoutCancel detaches it from the gRPC context, which ends
// when NodePrepareResources returns. Stop and a cancelled Start context still
// abort the attempt.
func (u *deviceStatusUpdater) Enqueue(ctx context.Context, claim *resourceapi.ResourceClaim, devices []resourceapi.AllocatedDeviceStatus) {
	u.mu.Lock()
	if u.cancel == nil {
		u.mu.Unlock()
		klog.FromContext(ctx).Error(nil, "device status updater is not started; dropping device status update", "uid", claim.UID)
		return
	}
	if prev := u.attempts[claim.UID]; prev != nil {
		prev.cancel()
	}
	// Detach from the request context, then keep a cancel func so Unprepare,
	// a newer Enqueue, and shutdown can abort the API call.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	attempt := &statusAttempt{cancel: cancel}
	u.attempts[claim.UID] = attempt
	u.wg.Add(1)
	u.mu.Unlock()

	devices = append([]resourceapi.AllocatedDeviceStatus(nil), devices...)
	ns, name, uid := claim.Namespace, claim.Name, claim.UID
	go func() {
		defer u.wg.Done()
		defer u.finishAttempt(uid, attempt)
		u.publish(runCtx, ns, name, uid, devices)
	}()
}

// Cancel aborts any in-flight or pending update for the claim, so an update
// that Unprepare interrupted cannot publish afterwards.
func (u *deviceStatusUpdater) Cancel(claimUID types.UID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if prev := u.attempts[claimUID]; prev != nil {
		prev.cancel()
		delete(u.attempts, claimUID)
	}
}

func (u *deviceStatusUpdater) finishAttempt(uid types.UID, attempt *statusAttempt) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if cur, ok := u.attempts[uid]; ok && cur == attempt {
		delete(u.attempts, uid)
	}
}

func (u *deviceStatusUpdater) publish(ctx context.Context, ns, name string, uid types.UID, devices []resourceapi.AllocatedDeviceStatus) {
	logger := klog.FromContext(ctx).WithValues("namespace", ns, "name", name, "uid", uid)
	backoff := u.backoff
	backoff.Steps = u.maxAttempts
	for attempt := 1; attempt <= u.maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		attemptCtx, cancel := context.WithTimeout(ctx, u.attemptTimeout)
		err := u.update(attemptCtx, ns, name, uid, devices...)
		cancel()

		switch {
		case err == nil:
			metrics.ObserveDeviceStatusUpdate(metrics.DeviceStatusResultSuccess)
			logger.V(2).Info("Published device status to ResourceClaim", "devices", len(devices), "attempt", attempt)
			return
		case ctx.Err() != nil:
			metrics.ObserveDeviceStatusUpdate(metrics.DeviceStatusResultDropped)
			logger.V(2).Info("Dropped device status update", "err", err, "attempt", attempt)
			return
		case isPermanentDeviceStatusError(err):
			metrics.ObserveDeviceStatusUpdate(metrics.DeviceStatusResultPermanentError)
			logger.Error(err, "Giving up on publishing device status to ResourceClaim: permanent error", "attempt", attempt)
			return
		case attempt == u.maxAttempts:
			metrics.ObserveDeviceStatusUpdate(metrics.DeviceStatusResultExhausted)
			logger.Error(err, "Giving up on publishing device status to ResourceClaim: retries exhausted", "attempt", attempt)
			return
		default:
			metrics.ObserveDeviceStatusUpdate(metrics.DeviceStatusResultRetry)
			logger.V(1).Info("Failed to publish device status to ResourceClaim, will retry", "err", err, "attempt", attempt)
		}

		timer := time.NewTimer(backoff.Step())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// isPermanentDeviceStatusError reports whether retrying the update cannot
// succeed.
func isPermanentDeviceStatusError(err error) bool {
	return errors.Is(err, errClaimReplaced) ||
		apierrors.IsNotFound(err) ||
		apierrors.IsGone(err) ||
		apierrors.IsInvalid(err) ||
		apierrors.IsBadRequest(err) ||
		apierrors.IsForbidden(err) ||
		apierrors.IsMethodNotSupported(err)
}

// checkClaimUID returns errClaimReplaced if claim is not the one with the
// expected UID.
func checkClaimUID(claim *resourceapi.ResourceClaim, uid types.UID) error {
	if claim.UID != uid {
		return fmt.Errorf("%w: expected UID %s, found %s", errClaimReplaced, uid, claim.UID)
	}
	return nil
}
