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
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	coreclientset "k8s.io/client-go/kubernetes"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
)

// newTestDriver builds just enough of a driver to exercise the device-health
// logic without starting the kubeletplugin helper or a Kubernetes client.
func newTestDriver(deviceNames []string) *driver {
	d := &driver{
		poolName:        "test-node",
		deviceHealth:    true,
		lastHealth:      make(map[string]deviceHealthSnapshot),
		healthOverrides: make(map[string]string),
		subscribers:     make(map[chan struct{}]struct{}),
		stopHealthCh:    make(chan struct{}),

		// Short enough that the periodic and reconnect paths run within a
		// test, long enough not to spin.
		healthPollInterval: 20 * time.Millisecond,
		healthWatchBackoff: 20 * time.Millisecond,
	}
	d.devices = append([]string(nil), deviceNames...)
	sort.Strings(d.devices)
	d.simulator = NewHealthSimulator(d.devices, false)
	return d
}

func healthOf(r kubeletplugin.DeviceHealthReport, name string) kubeletplugin.HealthStatus {
	for _, dh := range r.Devices {
		if dh.DeviceName == name {
			return dh.Health
		}
	}
	return ""
}

func TestBuildHealthReport(t *testing.T) {
	d := newTestDriver([]string{"gpu-1", "gpu-0"})
	report := d.buildHealthReport()

	require.Len(t, report.Devices, 2)
	// devices are iterated in sorted order.
	assert.Equal(t, "gpu-0", report.Devices[0].DeviceName)
	assert.Equal(t, "gpu-1", report.Devices[1].DeviceName)
	for _, dh := range report.Devices {
		assert.Equal(t, "test-node", dh.PoolName)
		assert.Equal(t, kubeletplugin.HealthStatusHealthy, dh.Health)
		assert.Equal(t, 60*time.Second, dh.HealthCheckTimeout)
		assert.NotEmpty(t, dh.Message)
	}
}

func TestApplyHealthOverridesLifecycle(t *testing.T) {
	d := newTestDriver([]string{"gpu-0", "gpu-1"})
	logger := klog.Background()

	// Apply "unhealthy" to gpu-0 only.
	d.applyHealthOverrides(logger, map[string]string{"health.example.com/gpu-0": "unhealthy"})
	assert.Equal(t, kubeletplugin.HealthStatusUnhealthy, healthOf(d.buildHealthReport(), "gpu-0"))
	assert.Equal(t, kubeletplugin.HealthStatusHealthy, healthOf(d.buildHealthReport(), "gpu-1"))
	assert.Equal(t, "unhealthy", d.healthOverrides["gpu-0"])

	// Change gpu-0 to "unknown".
	d.applyHealthOverrides(logger, map[string]string{"health.example.com/gpu-0": "unknown"})
	assert.Equal(t, kubeletplugin.HealthStatusUnknown, healthOf(d.buildHealthReport(), "gpu-0"))

	// Unknown/unrelated annotation keys are ignored; gpu-0 keeps its override.
	d.applyHealthOverrides(logger, map[string]string{
		"health.example.com/gpu-0":          "unknown",
		"health.example.com/does-not-exist": "unhealthy",
		"unrelated/annotation":              "x",
	})
	assert.Equal(t, kubeletplugin.HealthStatusUnknown, healthOf(d.buildHealthReport(), "gpu-0"))

	// Removing all overrides returns the device to healthy simulation.
	d.applyHealthOverrides(logger, map[string]string{})
	assert.Equal(t, kubeletplugin.HealthStatusHealthy, healthOf(d.buildHealthReport(), "gpu-0"))
	_, ok := d.healthOverrides["gpu-0"]
	assert.False(t, ok, "override bookkeeping should be cleared")
}

func TestApplyHealthOverridesIgnoresInvalidValues(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})
	logger := klog.Background()

	// A typo must NOT silently force the device healthy or create an override.
	d.applyHealthOverrides(logger, map[string]string{"health.example.com/gpu-0": "degraded"})
	assert.Equal(t, kubeletplugin.HealthStatusHealthy, healthOf(d.buildHealthReport(), "gpu-0"))
	_, ok := d.healthOverrides["gpu-0"]
	assert.False(t, ok, "invalid value must not create an override")

	// Case and surrounding whitespace are normalized for mapping and storage.
	d.applyHealthOverrides(logger, map[string]string{"health.example.com/gpu-0": "  UNHEALTHY "})
	assert.Equal(t, kubeletplugin.HealthStatusUnhealthy, healthOf(d.buildHealthReport(), "gpu-0"))
	assert.Equal(t, "unhealthy", d.healthOverrides["gpu-0"])

	// Replacing a valid override with garbage returns the device to simulation.
	d.applyHealthOverrides(logger, map[string]string{"health.example.com/gpu-0": "nonsense"})
	assert.Equal(t, kubeletplugin.HealthStatusHealthy, healthOf(d.buildHealthReport(), "gpu-0"))
	_, ok = d.healthOverrides["gpu-0"]
	assert.False(t, ok, "override must be cleared when replaced by an invalid value")
}

func TestWatchHealthStatusDisabledReturnsErrHealthNotSupported(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})
	d.deviceHealth = false // simulate --device-health=false

	reports := make(chan kubeletplugin.DeviceHealthReport, 1)
	err := d.WatchHealthStatus(context.Background(), reports)

	require.ErrorIs(t, err, kubeletplugin.ErrHealthNotSupported)
	assert.Empty(t, reports, "no report should be sent when device health is disabled")
}

func TestNotifySubscribersCoalesces(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})
	sig := make(chan struct{}, 1)
	d.subscribers[sig] = struct{}{}

	// Two wakes with no reader in between must coalesce into one pending signal
	// and never block.
	d.notifySubscribers()
	d.notifySubscribers()
	require.Len(t, sig, 1)
}

func TestWatchHealthStatusStreamsUpdates(t *testing.T) {
	d := newTestDriver([]string{"gpu-0", "gpu-1"})
	logger := klog.Background()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reports := make(chan kubeletplugin.DeviceHealthReport)
	done := make(chan error, 1)
	go func() { done <- d.WatchHealthStatus(ctx, reports) }()

	// Reading the initial snapshot also guarantees the subscriber is registered.
	initial := receiveReport(t, reports)
	assert.Equal(t, kubeletplugin.HealthStatusHealthy, healthOf(initial, "gpu-0"))

	// Flip gpu-0 unhealthy; the subscriber must wake and resend.
	d.applyHealthOverrides(logger, map[string]string{"health.example.com/gpu-0": "unhealthy"})
	eventuallyHealth(t, reports, "gpu-0", kubeletplugin.HealthStatusUnhealthy)

	// Cancelling the context ends the stream and deregisters the subscriber.
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("WatchHealthStatus did not return after context cancel")
	}
	d.clientsMu.RLock()
	assert.Empty(t, d.subscribers, "subscriber must be deregistered on exit")
	d.clientsMu.RUnlock()
}

// TestConcurrentHealthAccess runs subscribers, overrides, and polls concurrently
// so `go test -race` can flag any data race across the health code paths.
func TestConcurrentHealthAccess(t *testing.T) {
	d := newTestDriver([]string{"gpu-0", "gpu-1", "gpu-2"})
	logger := klog.Background()
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	// Subscribers that continuously drain reports until the context is cancelled.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reports := make(chan kubeletplugin.DeviceHealthReport)
			sub := make(chan error, 1)
			go func() { sub <- d.WatchHealthStatus(ctx, reports) }()
			for {
				select {
				case <-reports:
				case <-sub:
					return
				}
			}
		}()
	}

	// Concurrent override churn, which drives notifySubscribers via pollDeviceHealth.
	values := []string{"healthy", "unhealthy", "unknown"}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				d.applyHealthOverrides(logger, map[string]string{
					fmt.Sprintf("health.example.com/gpu-%d", i%3): values[j%len(values)],
				})
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	cancel()
	wg.Wait()

	d.clientsMu.RLock()
	assert.Empty(t, d.subscribers, "all subscribers must be deregistered after shutdown")
	d.clientsMu.RUnlock()
}

func receiveReport(t *testing.T, reports <-chan kubeletplugin.DeviceHealthReport) kubeletplugin.DeviceHealthReport {
	t.Helper()
	select {
	case r := <-reports:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for report")
		return kubeletplugin.DeviceHealthReport{}
	}
}

func eventuallyHealth(t *testing.T, reports <-chan kubeletplugin.DeviceHealthReport, name string, want kubeletplugin.HealthStatus) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case r := <-reports:
			if healthOf(r, name) == want {
				return
			}
		case <-deadline:
			t.Fatalf("did not observe %s = %s in time", name, want)
		}
	}
}

// --- deviceHealthLoop ---

func TestDeviceHealthLoopPollsOnStartAndStopsOnShutdown(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})
	sig := make(chan struct{}, 1)
	d.subscribers[sig] = struct{}{}

	d.healthWg.Add(1)
	go d.deviceHealthLoop(context.Background())

	// The loop must poll once immediately on start, which wakes subscribers.
	select {
	case <-sig:
	case <-time.After(5 * time.Second):
		t.Fatal("deviceHealthLoop did not poll on start")
	}
	d.healthMu.Lock()
	_, logged := d.lastHealth["gpu-0"]
	d.healthMu.Unlock()
	assert.True(t, logged, "initial poll must record device health")

	// Every tick re-polls and wakes subscribers again, which is what keeps the
	// kubelet's view of the device fresh.
	for i := 0; i < 3; i++ {
		select {
		case <-sig:
		case <-time.After(5 * time.Second):
			t.Fatalf("deviceHealthLoop did not poll periodically (tick %d)", i)
		}
	}

	close(d.stopHealthCh)
	waitForHealthGoroutines(t, d)
}

func TestDeviceHealthLoopStopsOnContextCancel(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})
	ctx, cancel := context.WithCancel(context.Background())

	d.healthWg.Add(1)
	go d.deviceHealthLoop(ctx)

	cancel()
	waitForHealthGoroutines(t, d)
}

// --- sendReport / WatchHealthStatus shutdown paths ---

func TestSendReportStopsWhenContextCancelled(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Unbuffered and never read: the only way out is the cancelled context.
	reports := make(chan kubeletplugin.DeviceHealthReport)
	assert.True(t, d.sendReport(ctx, reports))
}

func TestSendReportStopsOnShutdown(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})
	close(d.stopHealthCh)

	reports := make(chan kubeletplugin.DeviceHealthReport)
	assert.True(t, d.sendReport(context.Background(), reports))
}

func TestWatchHealthStatusReturnsWhenInitialSendIsStopped(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reports := make(chan kubeletplugin.DeviceHealthReport)
	require.NoError(t, d.WatchHealthStatus(ctx, reports))

	d.clientsMu.RLock()
	assert.Empty(t, d.subscribers, "subscriber must be deregistered on exit")
	d.clientsMu.RUnlock()
}

func TestWatchHealthStatusReturnsOnShutdown(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})

	reports := make(chan kubeletplugin.DeviceHealthReport)
	done := make(chan error, 1)
	go func() { done <- d.WatchHealthStatus(context.Background(), reports) }()

	// Consume the initial snapshot so the call is parked waiting for a wake.
	receiveReport(t, reports)

	// Shutdown (not context cancellation) must end the stream.
	close(d.stopHealthCh)
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("WatchHealthStatus did not return after shutdown")
	}
	d.clientsMu.RLock()
	assert.Empty(t, d.subscribers, "subscriber must be deregistered on exit")
	d.clientsMu.RUnlock()
}

func TestWatchHealthStatusReturnsWhenResendIsStopped(t *testing.T) {
	d := newTestDriver([]string{"gpu-0"})

	reports := make(chan kubeletplugin.DeviceHealthReport)
	done := make(chan error, 1)
	go func() { done <- d.WatchHealthStatus(context.Background(), reports) }()
	receiveReport(t, reports)

	// Wake the subscriber but never read the resend, so the call is parked in
	// sendReport; shutdown must unblock it and end the stream.
	d.notifySubscribers()
	close(d.stopHealthCh)
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("WatchHealthStatus did not return when the resend was stopped")
	}
}

// --- watchHealthOverrides ---

const (
	testPodName   = "dra-example-driver-kubeletplugin-abc12"
	testNamespace = "dra-example-driver"
)

// newWatchTestDriver wires a fake clientset into a test driver so that
// watchHealthOverrides can be driven with synthetic pod watch events.
func newWatchTestDriver(deviceNames []string, client coreclientset.Interface, podName, namespace string) *driver {
	d := newTestDriver(deviceNames)
	d.client = client
	d.config = &Config{flags: &Flags{podName: podName, namespace: namespace}}
	return d
}

// startOverrideWatcher runs watchHealthOverrides in the background; use
// waitForHealthGoroutines to block until it has exited.
func startOverrideWatcher(ctx context.Context, d *driver) {
	d.healthWg.Add(1)
	go d.watchHealthOverrides(ctx)
}

func waitForHealthGoroutines(t *testing.T, d *driver) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		d.healthWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("health goroutines did not exit in time")
	}
}

func driverPod(annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testPodName,
			Namespace:   testNamespace,
			Annotations: annotations,
		},
	}
}

func overrideOf(d *driver, device string) (string, bool) {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	v, ok := d.healthOverrides[device]
	return v, ok
}

// installPodWatch makes every pod watch on the fake clientset return a fresh
// fake watcher (handed to the caller through watchers) and records the
// number of watch attempts and the field selector used.
func installPodWatch(client *fakeclient.Clientset) (watchers chan *watch.FakeWatcher, attempts *atomic.Int32, selectors chan string) {
	watchers = make(chan *watch.FakeWatcher, 8)
	selectors = make(chan string, 8)
	attempts = &atomic.Int32{}
	client.PrependWatchReactor("pods", func(action k8stesting.Action) (bool, watch.Interface, error) {
		attempts.Add(1)
		if wa, ok := action.(k8stesting.WatchAction); ok {
			selectors <- wa.GetWatchRestrictions().Fields.String()
		}
		fw := watch.NewFake()
		watchers <- fw
		return true, fw, nil
	})
	return watchers, attempts, selectors
}

func TestWatchHealthOverridesRequiresPodIdentity(t *testing.T) {
	for name, tc := range map[string]struct{ podName, namespace string }{
		"no pod name":  {podName: "", namespace: testNamespace},
		"no namespace": {podName: testPodName, namespace: ""},
		"neither":      {},
	} {
		t.Run(name, func(t *testing.T) {
			client := fakeclient.NewClientset()
			_, attempts, _ := installPodWatch(client)
			d := newWatchTestDriver([]string{"gpu-0"}, client, tc.podName, tc.namespace)

			startOverrideWatcher(context.Background(), d)
			waitForHealthGoroutines(t, d)

			assert.Zero(t, attempts.Load(), "no pod watch must be started without pod identity")
		})
	}
}

func TestWatchHealthOverridesAppliesPodAnnotationEvents(t *testing.T) {
	client := fakeclient.NewClientset()
	watchers, _, selectors := installPodWatch(client)
	d := newWatchTestDriver([]string{"gpu-0", "gpu-1"}, client, testPodName, testNamespace)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startOverrideWatcher(ctx, d)
	fw := <-watchers

	// The watch must be scoped to this driver pod only.
	assert.Equal(t, "metadata.name="+testPodName, <-selectors)

	unhealthy := map[string]string{"health.example.com/gpu-0": "unhealthy"}

	// Added carries the current annotation set and is applied.
	fw.Add(driverPod(unhealthy))
	assert.Eventually(t, func() bool {
		return healthOf(d.buildHealthReport(), "gpu-0") == kubeletplugin.HealthStatusUnhealthy
	}, 5*time.Second, 10*time.Millisecond, "Added event must apply the override")

	// A Bookmark carries an annotation-stripped object and must be ignored, or
	// it would spuriously clear the override. Non-pod objects are ignored too.
	fw.Action(watch.Bookmark, driverPod(nil))
	fw.Action(watch.Modified, &corev1.ConfigMap{})
	// The fake watcher hands events over synchronously, so once this Deleted
	// event (which the loop also ignores) has been accepted the two above have
	// been fully processed.
	fw.Delete(driverPod(unhealthy))
	assert.Equal(t, kubeletplugin.HealthStatusUnhealthy, healthOf(d.buildHealthReport(), "gpu-0"),
		"Bookmark and non-pod events must not touch overrides")
	v, ok := overrideOf(d, "gpu-0")
	assert.True(t, ok)
	assert.Equal(t, "unhealthy", v)

	// Modified with the annotation removed clears the override.
	fw.Modify(driverPod(nil))
	assert.Eventually(t, func() bool {
		_, ok := overrideOf(d, "gpu-0")
		return !ok && healthOf(d.buildHealthReport(), "gpu-0") == kubeletplugin.HealthStatusHealthy
	}, 5*time.Second, 10*time.Millisecond, "Modified event must clear the override")

	// Shutdown: closing stopHealthCh cancels the watch context; a real watcher
	// then closes its result channel, which the fake one only does on Stop.
	close(d.stopHealthCh)
	fw.Stop()
	waitForHealthGoroutines(t, d)
}

func TestWatchHealthOverridesStopsOnEventAfterContextCancel(t *testing.T) {
	client := fakeclient.NewClientset()
	watchers, _, _ := installPodWatch(client)
	d := newWatchTestDriver([]string{"gpu-0"}, client, testPodName, testNamespace)

	ctx, cancel := context.WithCancel(context.Background())
	startOverrideWatcher(ctx, d)
	fw := <-watchers

	// Cancel while the loop is parked on the result channel, then deliver one
	// more event: the loop must notice the cancellation and stop the watcher.
	cancel()
	fw.Modify(driverPod(map[string]string{"health.example.com/gpu-0": "unhealthy"}))
	waitForHealthGoroutines(t, d)

	assert.True(t, fw.IsStopped(), "watcher must be stopped on exit")
	_, ok := overrideOf(d, "gpu-0")
	assert.False(t, ok, "event delivered after cancellation must not be applied")
}

func TestWatchHealthOverridesStopsOnEventAfterShutdown(t *testing.T) {
	client := fakeclient.NewClientset()
	watchers, _, _ := installPodWatch(client)
	d := newWatchTestDriver([]string{"gpu-0"}, client, testPodName, testNamespace)

	startOverrideWatcher(context.Background(), d)
	fw := <-watchers

	close(d.stopHealthCh)
	fw.Modify(driverPod(map[string]string{"health.example.com/gpu-0": "unhealthy"}))
	waitForHealthGoroutines(t, d)

	assert.True(t, fw.IsStopped(), "watcher must be stopped on exit")
	_, ok := overrideOf(d, "gpu-0")
	assert.False(t, ok, "event delivered after shutdown must not be applied")
}

func TestWatchHealthOverridesBacksOffOnWatchError(t *testing.T) {
	client := fakeclient.NewClientset()
	attempts := &atomic.Int32{}
	client.PrependWatchReactor("pods", func(action k8stesting.Action) (bool, watch.Interface, error) {
		attempts.Add(1)
		return true, nil, errors.New("apiserver unavailable")
	})
	d := newWatchTestDriver([]string{"gpu-0"}, client, testPodName, testNamespace)

	ctx, cancel := context.WithCancel(context.Background())
	startOverrideWatcher(ctx, d)

	// A failed watch is retried after a back-off rather than immediately.
	assert.Eventually(t, func() bool { return attempts.Load() >= 3 }, 5*time.Second, 5*time.Millisecond,
		"watch must be retried after the back-off")

	// Cancelling during the back-off returns promptly.
	cancel()
	waitForHealthGoroutines(t, d)
}

func TestWatchHealthOverridesReconnectsAfterWatchCloses(t *testing.T) {
	client := fakeclient.NewClientset()
	watchers, attempts, _ := installPodWatch(client)
	d := newWatchTestDriver([]string{"gpu-0"}, client, testPodName, testNamespace)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startOverrideWatcher(ctx, d)

	// A server-side watch close must be followed by a fresh watch, and events
	// on the new watch are applied as before.
	first := <-watchers
	first.Stop()
	second := <-watchers
	assert.Equal(t, int32(2), attempts.Load())

	second.Add(driverPod(map[string]string{"health.example.com/gpu-0": "unhealthy"}))
	assert.Eventually(t, func() bool {
		return healthOf(d.buildHealthReport(), "gpu-0") == kubeletplugin.HealthStatusUnhealthy
	}, 5*time.Second, 10*time.Millisecond, "override must be applied on the reconnected watch")

	cancel()
	second.Stop()
	waitForHealthGoroutines(t, d)
}

func TestWatchHealthOverridesReturnsOnShutdownDuringBackoff(t *testing.T) {
	client := fakeclient.NewClientset()
	watchers, attempts, _ := installPodWatch(client)
	d := newWatchTestDriver([]string{"gpu-0"}, client, testPodName, testNamespace)
	// Make the back-off long so shutdown is what ends the wait, not a reconnect.
	d.healthWatchBackoff = time.Hour

	startOverrideWatcher(context.Background(), d)
	fw := <-watchers

	// A server-side watch close lands the loop in its reconnect back-off;
	// shutdown during the back-off returns without reconnecting.
	fw.Stop()
	close(d.stopHealthCh)
	waitForHealthGoroutines(t, d)
	assert.Equal(t, int32(1), attempts.Load(), "shutdown during back-off must not reconnect")
}
