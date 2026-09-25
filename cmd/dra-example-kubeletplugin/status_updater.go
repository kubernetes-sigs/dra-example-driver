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
	"k8s.io/client-go/util/workqueue"
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
	// server cannot stall the worker indefinitely.
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

// pendingDeviceStatus is the latest status waiting to be published for a claim.
type pendingDeviceStatus struct {
	namespace string
	name      string
	devices   []resourceapi.AllocatedDeviceStatus
	// generation distinguishes successive Enqueue calls for the same claim.
	generation uint64
}

// deviceStatusUpdater publishes ResourceClaim.status.devices asynchronously so
// that API server latency or failures never block NodePrepareResources.
//
// Transient failures are retried with exponential backoff up to
// deviceStatusMaxRetries. Permanent failures (the claim was deleted or
// replaced, the update is invalid, or the driver lacks permission) are not
// retried. Every attempt is counted in the device_status_updates_total metric.
// Retries are logged at V(1), as they are expected while the API server is
// briefly unavailable; giving up is logged as an error with the claim's
// namespace, name and UID.
type deviceStatusUpdater struct {
	update deviceStatusUpdateFunc
	queue  workqueue.TypedRateLimitingInterface[types.UID]

	maxRetries     int
	attemptTimeout time.Duration

	mu         sync.Mutex
	pending    map[types.UID]pendingDeviceStatus
	generation uint64

	wg sync.WaitGroup
}

func newDeviceStatusUpdater(update deviceStatusUpdateFunc) *deviceStatusUpdater {
	return newDeviceStatusUpdaterWithRateLimiter(update,
		workqueue.NewTypedItemExponentialFailureRateLimiter[types.UID](deviceStatusBaseDelay, deviceStatusMaxDelay))
}

func newDeviceStatusUpdaterWithRateLimiter(update deviceStatusUpdateFunc, rateLimiter workqueue.TypedRateLimiter[types.UID]) *deviceStatusUpdater {
	return &deviceStatusUpdater{
		update: update,
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(rateLimiter,
			workqueue.TypedRateLimitingQueueConfig[types.UID]{Name: "device-status"}),
		maxRetries:     deviceStatusMaxRetries,
		attemptTimeout: deviceStatusAttemptTimeout,
		pending:        make(map[types.UID]pendingDeviceStatus),
	}
}

// Start runs the worker until ctx is cancelled or Stop is called.
func (u *deviceStatusUpdater) Start(ctx context.Context) {
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		// Unblock queue.Get when the parent context goes away.
		go func() {
			<-ctx.Done()
			u.queue.ShutDown()
		}()
		for u.processNextItem(ctx) {
		}
	}()
}

// Stop shuts down the queue and waits for the worker to exit. Updates that
// have not been published yet are dropped.
func (u *deviceStatusUpdater) Stop() {
	u.queue.ShutDown()
	u.wg.Wait()
}

// Enqueue schedules devices to be published to the claim's status, replacing
// any update for the same claim that has not been published yet.
func (u *deviceStatusUpdater) Enqueue(claim *resourceapi.ResourceClaim, devices []resourceapi.AllocatedDeviceStatus) {
	u.mu.Lock()
	u.generation++
	u.pending[claim.UID] = pendingDeviceStatus{
		namespace:  claim.Namespace,
		name:       claim.Name,
		devices:    devices,
		generation: u.generation,
	}
	u.mu.Unlock()
	// A fresh update starts with a fresh backoff.
	u.queue.Forget(claim.UID)
	u.queue.Add(claim.UID)
}

// Cancel drops any unpublished update for the claim, e.g. because it was
// unprepared.
func (u *deviceStatusUpdater) Cancel(claimUID types.UID) {
	u.mu.Lock()
	delete(u.pending, claimUID)
	u.mu.Unlock()
	u.queue.Forget(claimUID)
}

func (u *deviceStatusUpdater) processNextItem(ctx context.Context) bool {
	uid, shutdown := u.queue.Get()
	if shutdown {
		return false
	}
	defer u.queue.Done(uid)

	u.mu.Lock()
	p, ok := u.pending[uid]
	u.mu.Unlock()
	if !ok {
		// Cancelled, or already published by an earlier attempt.
		u.queue.Forget(uid)
		return true
	}

	logger := klog.FromContext(ctx).WithValues("namespace", p.namespace, "name", p.name, "uid", uid)
	attempt := u.queue.NumRequeues(uid) + 1

	attemptCtx, cancel := context.WithTimeout(ctx, u.attemptTimeout)
	err := u.update(attemptCtx, p.namespace, p.name, uid, p.devices...)
	cancel()

	switch {
	case err == nil:
		metrics.ObserveDeviceStatusUpdate(metrics.DeviceStatusResultSuccess)
		logger.V(2).Info("Published device status to ResourceClaim", "devices", len(p.devices), "attempt", attempt)
		u.finish(uid, p)
	case ctx.Err() != nil:
		// Shutting down; the attempt was aborted rather than failed.
	case isPermanentDeviceStatusError(err):
		metrics.ObserveDeviceStatusUpdate(metrics.DeviceStatusResultPermanentError)
		logger.Error(err, "Giving up on publishing device status to ResourceClaim: permanent error", "attempt", attempt)
		u.finish(uid, p)
	case attempt > u.maxRetries:
		metrics.ObserveDeviceStatusUpdate(metrics.DeviceStatusResultExhausted)
		logger.Error(err, "Giving up on publishing device status to ResourceClaim: retries exhausted", "attempt", attempt)
		u.finish(uid, p)
	default:
		metrics.ObserveDeviceStatusUpdate(metrics.DeviceStatusResultRetry)
		logger.V(1).Info("Failed to publish device status to ResourceClaim, will retry", "err", err, "attempt", attempt)
		u.queue.AddRateLimited(uid)
	}
	return true
}

// finish forgets uid and removes its pending entry, unless Enqueue replaced it
// with a newer update while this attempt was in flight.
func (u *deviceStatusUpdater) finish(uid types.UID, attempted pendingDeviceStatus) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if cur, ok := u.pending[uid]; ok && cur.generation == attempted.generation {
		delete(u.pending, uid)
		u.queue.Forget(uid)
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
