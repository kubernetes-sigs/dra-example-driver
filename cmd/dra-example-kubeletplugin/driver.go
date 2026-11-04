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
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/dra-example-driver/pkg/metrics"
)

// deviceHealthSnapshot is the last health we logged for a device. It exists only
// so pollDeviceHealth can detect and log transitions, and what is published to
// claim conditions; the authoritative health always comes from the simulator
// (buildHealthReport reads it on demand).
type deviceHealthSnapshot struct {
	health  kubeletplugin.HealthStatus
	message string
}

// deviceHealthyConditionType is the type of the condition published in
// ResourceClaim.status.devices[].conditions to reflect device health.
const deviceHealthyConditionType = "Healthy"

// conditionPublishTimeout bounds one pass of publishing conditions to claims, so
// a slow API server cannot hold up the health resends for long.
const conditionPublishTimeout = 10 * time.Second

// preparedClaim is what is needed to publish health conditions for a claim that
// is currently prepared: where to write, and which of its devices are ours.
type preparedClaim struct {
	namespace, name string
	devices         []preparedClaimDevice
}

type preparedClaimDevice struct {
	pool, device string
	shareID      *string
	// published is the health last written to the claim, nil if none yet.
	published *deviceHealthSnapshot
}

type driver struct {
	client coreclientset.Interface
	helper *kubeletplugin.Helper
	state  *DeviceState
	// healthcheck is the driver-process gRPC liveness probe. It is unrelated to
	// the per-device KEP-4680 health reporting below.
	healthcheck *healthcheck
	cancelCtx   func(error)

	config   *Config
	poolName string

	// --- Device health reporting (KEP-4680) ---

	// deviceHealth reflects the --device-health flag. When false the driver
	// advertises no health service, starts no simulator or health goroutines,
	// and WatchHealthStatus returns ErrHealthNotSupported.
	deviceHealth bool
	simulator    *HealthSimulator
	// devices holds the device names, sorted, for stable report/log iteration.
	// It is immutable after startup and so needs no lock.
	devices []string

	// healthMu guards the two device-health bookkeeping maps below.
	healthMu sync.Mutex
	// lastHealth is the most recently logged health per device, used only to
	// detect transitions in pollDeviceHealth.
	lastHealth map[string]deviceHealthSnapshot
	// healthOverrides tracks the last applied health.example.com/<device>
	// annotation value per device, so value changes re-apply the override.
	healthOverrides map[string]string

	// preparedMu guards preparedClaims, which maps the UID of each claim that is
	// prepared (while device health is enabled) to the devices it holds. It is
	// kept in memory only and not rebuilt from the checkpoint, so a claim
	// prepared before a restart is tracked again only when the kubelet prepares
	// it again.
	preparedMu     sync.Mutex
	preparedClaims map[types.UID]preparedClaim
	// conditionMu serializes publishing health conditions, so that a slow write
	// for an older transition cannot land after, and overwrite, a newer one.
	conditionMu sync.Mutex

	// clientsMu guards subscribers.
	clientsMu sync.RWMutex
	// subscribers are per-WatchHealthStatus wake channels. A send is a
	// coalescing "rebuild and resend" signal rather than the report itself, so
	// the newest state is never queued behind stale reports or dropped.
	subscribers map[chan struct{}]struct{}

	// stopHealthCh is closed on shutdown to unblock the health goroutines and
	// every in-flight WatchHealthStatus call.
	stopHealthCh chan struct{}
	// stopHealthOnce makes Shutdown idempotent: closing stopHealthCh twice
	// would panic.
	stopHealthOnce sync.Once
	healthWg       sync.WaitGroup

	// healthPollInterval and healthWatchBackoff default to the constants below;
	// tests shorten them to exercise the periodic and reconnect paths.
	healthPollInterval time.Duration
	healthWatchBackoff time.Duration
}

func NewDriver(ctx context.Context, config *Config) (*driver, error) {
	driver := &driver{
		client:          config.coreclient,
		cancelCtx:       config.cancelMainCtx,
		config:          config,
		poolName:        config.flags.nodeName,
		lastHealth:      make(map[string]deviceHealthSnapshot),
		healthOverrides: make(map[string]string),
		preparedClaims:  make(map[types.UID]preparedClaim),
		subscribers:     make(map[chan struct{}]struct{}),
		stopHealthCh:    make(chan struct{}),

		healthPollInterval: deviceHealthPollInterval,
		healthWatchBackoff: healthOverrideWatchBackoff,
	}

	state, err := NewDeviceState(config)
	if err != nil {
		return nil, err
	}
	driver.state = state

	// Device health reporting (KEP-4680) is opt-out: on by default, disabled with
	// --device-health=false (DEVICE_HEALTH). When disabled we build no simulator
	// and start no health goroutines; WatchHealthStatus returns
	// ErrHealthNotSupported and the kubelet never subscribes.
	logger := klog.FromContext(ctx)
	driver.deviceHealth = config.flags.deviceHealth
	if driver.deviceHealth {
		for deviceName := range state.allocatable {
			driver.devices = append(driver.devices, deviceName)
		}
		sort.Strings(driver.devices)

		driver.simulator = NewHealthSimulator(driver.devices, config.flags.simulateHealthChanges)
		logger.Info("Device health reporting enabled",
			"devices", len(driver.devices), "autoSimulation", config.flags.simulateHealthChanges)
	} else {
		logger.Info("Device health reporting disabled; WatchHealthStatus will report ErrHealthNotSupported",
			"flag", "--device-health=false")
	}

	helper, err := kubeletplugin.Start(ctx, driver,
		kubeletplugin.KubeClient(config.coreclient),
		kubeletplugin.NodeName(config.flags.nodeName),
		kubeletplugin.DriverName(config.flags.driverName),
		kubeletplugin.RegistrarDirectoryPath(config.flags.kubeletRegistrarDirectoryPath),
		kubeletplugin.PluginDataDirectoryPath(config.DriverPluginPath()),
		kubeletplugin.RollingUpdate(types.UID(config.flags.podUID)),
		// Advertise the device health service (KEP-4680) unless it was disabled
		// via --device-health=false. This is exactly how a driver author opts
		// out: pass HealthService(false) and return ErrHealthNotSupported from
		// WatchHealthStatus (see below).
		kubeletplugin.HealthService(config.flags.deviceHealth),
	)
	if err != nil {
		return nil, err
	}
	driver.helper = helper

	driver.healthcheck, err = startHealthcheck(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("start healthcheck: %w", err)
	}

	if err := helper.PublishResources(ctx, state.driverResources); err != nil {
		return nil, err
	}

	if driver.deviceHealth {
		driver.healthWg.Add(2)
		go driver.deviceHealthLoop(ctx)
		go driver.watchHealthOverrides(ctx)
	}

	return driver, nil
}

func (d *driver) Shutdown(logger klog.Logger) error {
	if d.healthcheck != nil {
		d.healthcheck.Stop(logger)
	}

	// Mirror the guarded startup in NewDriver: the health goroutines only
	// exist when device health reporting is enabled.
	if d.deviceHealth {
		logger.Info("Stopping device health monitoring")
		// Closing stopHealthCh unblocks the deviceHealthLoop select, tells every
		// in-flight WatchHealthStatus call to return, and (via the watcher-cancel
		// goroutine in watchHealthOverrides) closes the pod watch so that loop
		// unblocks too. Each WatchHealthStatus call deregisters its own subscriber
		// channel as it exits.
		d.stopHealthOnce.Do(func() { close(d.stopHealthCh) })
		d.healthWg.Wait()
	}

	d.helper.Stop()
	return nil
}

func (d *driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	logger := klog.FromContext(ctx)
	logger.Info("PrepareResourceClaims is called", "numClaims", len(claims))
	result := make(map[types.UID]kubeletplugin.PrepareResult)

	for _, claim := range claims {
		result[claim.UID] = d.prepareResourceClaim(ctx, claim)
	}

	return result, nil
}

func (d *driver) prepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) (result kubeletplugin.PrepareResult) {
	logger := klog.FromContext(ctx)
	logger.Info("Preparing claim", "uid", claim.UID, "namespace", claim.Namespace, "name", claim.Name)

	start := time.Now()
	defer func() {
		metrics.ObservePrepareClaim(result.Err, time.Since(start))
	}()

	preparedDevices, err := d.state.Prepare(ctx, claim)
	if err != nil {
		logger.Error(err, "Error preparing devices for claim", "uid", claim.UID)
		result = kubeletplugin.PrepareResult{
			Err: fmt.Errorf("error preparing devices for claim %v: %w", claim.UID, err),
		}
		return result
	}
	var prepared []kubeletplugin.Device
	for _, preparedDevice := range preparedDevices {
		prepared = append(prepared, kubeletplugin.Device{
			Requests:     preparedDevice.GetRequestNames(),
			PoolName:     preparedDevice.GetPoolName(),
			DeviceName:   preparedDevice.GetDeviceName(),
			CDIDeviceIDs: preparedDevice.GetCdiDeviceIds(),
			ShareID:      preparedDevice.ShareID,
		})
	}

	d.trackClaim(claim)

	logger.Info("Returning newly prepared devices for claim", "uid", claim.UID, "devices", prepared)
	result = kubeletplugin.PrepareResult{Devices: prepared}
	return result
}

func (d *driver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	logger := klog.FromContext(ctx)
	logger.Info("UnprepareResourceClaims is called", "numClaims", len(claims))
	result := make(map[types.UID]error)

	for _, claim := range claims {
		result[claim.UID] = d.unprepareResourceClaim(ctx, claim)
	}

	return result, nil
}

func (d *driver) unprepareResourceClaim(_ context.Context, claim kubeletplugin.NamespacedObject) (err error) {
	start := time.Now()
	defer func() {
		metrics.ObserveUnprepareClaim(err, time.Since(start))
	}()

	if err = d.state.Unprepare(claim.UID); err != nil {
		return fmt.Errorf("error unpreparing devices for claim %v: %w", claim.UID, err)
	}
	d.untrackClaim(claim.UID)

	return nil
}

func (d *driver) HandleError(ctx context.Context, err error, msg string) {
	utilruntime.HandleErrorWithContext(ctx, err, msg)
	if !errors.Is(err, kubeletplugin.ErrRecoverable) {
		metrics.FatalBackgroundErrorsTotal.Inc()
		if d.cancelCtx != nil {
			d.cancelCtx(fmt.Errorf("fatal background error: %w", err))
		}
	}
}

func (d *driver) deviceHealthLoop(ctx context.Context) {
	defer d.healthWg.Done()

	logger := klog.FromContext(ctx)
	ticker := time.NewTicker(d.healthPollInterval)
	defer ticker.Stop()

	logger.Info("Starting device health monitoring loop")
	d.pollDeviceHealth(ctx, logger)

	for {
		select {
		case <-d.stopHealthCh:
			logger.Info("Health monitoring loop stopped")
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.pollDeviceHealth(ctx, logger)
		}
	}
}

const healthAnnotationPrefix = "health.example.com/"

const (
	// deviceHealthPollInterval is how often the driver re-polls the simulator
	// and re-sends device health. It MUST stay comfortably below
	// deviceHealthCheckTimeout: the kubelet decays a device to stale/Unknown if
	// no report arrives within that window, so polling less often than the
	// timeout would make healthy devices flap to stale between polls.
	deviceHealthPollInterval = 30 * time.Second
	// deviceHealthCheckTimeout is the freshness window reported to the kubelet in
	// each DeviceHealth.HealthCheckTimeout. Keep it greater than
	// deviceHealthPollInterval (see above).
	deviceHealthCheckTimeout = 60 * time.Second
	// healthOverrideWatchBackoff is how long watchHealthOverrides waits before
	// re-establishing a pod watch that failed to start or was closed.
	healthOverrideWatchBackoff = 5 * time.Second
)

func (d *driver) watchHealthOverrides(ctx context.Context) {
	defer d.healthWg.Done()

	logger := klog.FromContext(ctx)
	podName := d.config.flags.podName
	namespace := d.config.flags.namespace
	if podName == "" || namespace == "" {
		logger.Info("Pod identity not available (POD_NAME/NAMESPACE unset), health overrides disabled")
		return
	}

	// Cancel the watch on shutdown as well as on ctx cancellation, so a goroutine
	// blocked in range watcher.ResultChan() (which only unblocks when the watch's
	// context is cancelled) wakes up even if the parent ctx is still live.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-d.stopHealthCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	for {
		watcher, err := d.client.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
			FieldSelector: "metadata.name=" + podName,
		})
		if err != nil {
			logger.Error(err, "Failed to watch pod for health overrides")
		} else {
			for event := range watcher.ResultChan() {
				select {
				case <-ctx.Done():
					watcher.Stop()
					return
				case <-d.stopHealthCh:
					watcher.Stop()
					return
				default:
				}

				// The server reports watch failures (e.g. 410 Gone when the
				// resource version expired) as an Error event and then closes the
				// stream. Log it and reconnect explicitly rather than relying on
				// the channel closing.
				if event.Type == watch.Error {
					logger.Error(apierrors.FromObject(event.Object), "Pod watch for health overrides returned an error, reconnecting")
					watcher.Stop()
					break
				}

				// Only additions and modifications carry the current annotation
				// set. Ignore other event types: Bookmark carries an
				// annotation-stripped object that would spuriously clear
				// overrides, and Deleted means this driver's own pod is being
				// torn down, so any override state dies with the process anyway.
				if event.Type != watch.Added && event.Type != watch.Modified {
					continue
				}
				pod, ok := event.Object.(*corev1.Pod)
				if !ok {
					continue
				}
				d.applyHealthOverrides(ctx, logger, pod.Annotations)
			}
		}

		// The watch failed to start or its result channel closed (a server-side
		// watch close is normal). Back off before reconnecting so a persistently
		// failing or immediately-closing watch cannot hot-loop against the API
		// server, and return promptly on shutdown.
		select {
		case <-ctx.Done():
			return
		case <-d.stopHealthCh:
			return
		case <-time.After(d.healthWatchBackoff):
		}
	}
}

func (d *driver) applyHealthOverrides(ctx context.Context, logger klog.Logger, annotations map[string]string) {
	d.healthMu.Lock()

	activeOverrides := make(map[string]bool)
	for key, value := range annotations {
		if !strings.HasPrefix(key, healthAnnotationPrefix) {
			continue
		}
		deviceName := strings.TrimPrefix(key, healthAnnotationPrefix)
		if !d.simulator.HasDevice(deviceName) {
			continue
		}

		// Normalize once so mapping, de-duplication, and stored value all agree
		// on case and surrounding whitespace.
		normalized := strings.ToLower(strings.TrimSpace(value))
		var scenario HealthScenario
		switch normalized {
		case "healthy":
			scenario = ScenarioHealthy
		case "unhealthy":
			scenario = ScenarioTemperatureWarning
		case "unknown":
			scenario = ScenarioUnknown
		default:
			// Unrecognized value: ignore it rather than silently forcing the
			// device healthy. Leaving it out of activeOverrides means a device
			// whose override was replaced with garbage returns to simulation.
			logger.Info("Ignoring unrecognized health override value; supported values are healthy, unhealthy, unknown",
				"device", deviceName, "value", value)
			continue
		}

		activeOverrides[deviceName] = true
		if prev, ok := d.healthOverrides[deviceName]; ok && prev == normalized {
			continue
		}

		d.simulator.ForceScenario(deviceName, scenario)
		d.healthOverrides[deviceName] = normalized
		logger.Info("Health override applied", "device", deviceName, "status", normalized)
	}

	for deviceName := range d.healthOverrides {
		if !activeOverrides[deviceName] {
			d.simulator.ClearOverride(deviceName)
			delete(d.healthOverrides, deviceName)
			logger.Info("Health override removed, returning to simulation", "device", deviceName)
		}
	}

	d.healthMu.Unlock()

	d.pollDeviceHealth(ctx, logger)
}

// pollDeviceHealth advances the simulation one step per device, logs any health
// transitions, wakes subscribers to resend, and brings the Healthy condition of
// prepared claims up to date. It may run from both the periodic loop and the
// override watcher, so it takes healthMu.
func (d *driver) pollDeviceHealth(ctx context.Context, logger klog.Logger) {
	d.healthMu.Lock()
	for _, deviceName := range d.devices {
		health, message := d.simulator.GetDeviceHealth(deviceName)
		if prev, ok := d.lastHealth[deviceName]; !ok || prev.health != health || prev.message != message {
			d.lastHealth[deviceName] = deviceHealthSnapshot{health: health, message: message}
			logger.Info("Device health changed",
				"device", deviceName,
				"health", health,
				"message", message)
		}
	}
	d.healthMu.Unlock()

	// Wake every subscriber to resend, even when nothing changed: the kubelet
	// marks a device's health stale once it is older than HealthCheckTimeout, so
	// each poll re-sends within that window. Because the resend is driven by
	// this poll, if the poll loop ever stops the resends stop too and the
	// kubelet correctly decays the data to stale. A real driver would drive this
	// from an actual device probe (see the reference GPU/TPU drivers); here the
	// "probe" is the simulator, which cannot fail.
	d.notifySubscribers()

	d.publishHealthConditions(ctx, logger)
}

// trackClaim records the devices of a prepared claim so that health changes can
// be published to it. It does nothing when device health is disabled.
func (d *driver) trackClaim(claim *resourceapi.ResourceClaim) {
	if !d.deviceHealth || claim.Status.Allocation == nil {
		return
	}
	pc := preparedClaim{namespace: claim.Namespace, name: claim.Name}
	for _, result := range claim.Status.Allocation.Devices.Results {
		if result.Driver != d.state.driverName {
			continue
		}
		pd := preparedClaimDevice{pool: result.Pool, device: result.Device}
		if result.ShareID != nil {
			pd.shareID = ptr.To(string(*result.ShareID))
		}
		pc.devices = append(pc.devices, pd)
	}

	d.preparedMu.Lock()
	defer d.preparedMu.Unlock()
	d.preparedClaims[claim.UID] = pc
}

func (d *driver) untrackClaim(uid types.UID) {
	d.preparedMu.Lock()
	defer d.preparedMu.Unlock()
	delete(d.preparedClaims, uid)
}

// publishHealthConditions reconciles the Healthy condition of every prepared
// claim device with the latest health recorded by pollDeviceHealth. Each pass
// writes only the devices whose last published health differs, so a claim
// prepared after a transition gets its condition on the next poll, a failed
// write is retried on the next poll, and an unchanged device costs no API call.
// A message-only change counts as a difference, so with --simulate-health-changes
// the drifting temperature in the message is written through.
func (d *driver) publishHealthConditions(ctx context.Context, logger klog.Logger) {
	d.conditionMu.Lock()
	defer d.conditionMu.Unlock()

	d.healthMu.Lock()
	current := maps.Clone(d.lastHealth)
	d.healthMu.Unlock()

	type claimUpdate struct {
		uid             types.UID
		namespace, name string
		statuses        []resourceapi.AllocatedDeviceStatus
		// sent are the devices in statuses, with published set to what is sent.
		sent []preparedClaimDevice
	}
	var updates []claimUpdate
	d.preparedMu.Lock()
	for uid, pc := range d.preparedClaims {
		update := claimUpdate{uid: uid, namespace: pc.namespace, name: pc.name}
		for _, pd := range pc.devices {
			snapshot, ok := current[pd.device]
			if !ok || (pd.published != nil && *pd.published == snapshot) {
				continue
			}
			condition := deviceHealthCondition(snapshot.health, snapshot.message)
			update.statuses = append(update.statuses, resourceapi.AllocatedDeviceStatus{
				Driver:     d.state.driverName,
				Pool:       pd.pool,
				Device:     pd.device,
				ShareID:    pd.shareID,
				Conditions: []metav1.Condition{condition},
			})
			pd.published = &snapshot
			update.sent = append(update.sent, pd)
		}
		if len(update.statuses) > 0 {
			updates = append(updates, update)
		}
	}
	d.preparedMu.Unlock()
	if len(updates) == 0 {
		return
	}

	// One deadline for the whole pass, so a hung API server delays the next
	// resend by this much at most however many claims there are. Shutdown cuts
	// it short too, as it waits for the health goroutines.
	ctx, cancel := context.WithTimeout(ctx, conditionPublishTimeout)
	defer cancel()
	go func() {
		select {
		case <-d.stopHealthCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	for _, update := range updates {
		if ctx.Err() != nil {
			// Shutting down or out of time: nothing is marked published, so the
			// remaining claims are retried on the next pass.
			return
		}
		if err := d.state.updateDeviceStatus(ctx, update.namespace, update.name, update.statuses...); err != nil {
			logger.Error(err, "Failed to update device health condition on ResourceClaim",
				"namespace", update.namespace, "name", update.name)
			continue
		}
		d.markPublished(update.uid, update.sent)
	}
}

// markPublished records what was written for a claim, unless it was unprepared
// in the meantime.
func (d *driver) markPublished(uid types.UID, sent []preparedClaimDevice) {
	d.preparedMu.Lock()
	defer d.preparedMu.Unlock()
	pc, ok := d.preparedClaims[uid]
	if !ok {
		return
	}
	for _, s := range sent {
		for i := range pc.devices {
			if pd := &pc.devices[i]; pd.pool == s.pool && pd.device == s.device && ptr.Equal(pd.shareID, s.shareID) {
				pd.published = s.published
			}
		}
	}
}

// deviceHealthCondition maps a device health state to the claim condition that
// reports it.
func deviceHealthCondition(health kubeletplugin.HealthStatus, message string) metav1.Condition {
	condition := metav1.Condition{Type: deviceHealthyConditionType, Message: message}
	switch health {
	case kubeletplugin.HealthStatusHealthy:
		condition.Status, condition.Reason = metav1.ConditionTrue, "Healthy"
	case kubeletplugin.HealthStatusUnhealthy:
		condition.Status, condition.Reason = metav1.ConditionFalse, "Unhealthy"
	default:
		condition.Status, condition.Reason = metav1.ConditionUnknown, "HealthUnknown"
	}
	return condition
}

// buildHealthReport snapshots the current health of all devices straight from
// the simulator, the single source of truth. It takes no driver lock: the
// device list is immutable after startup and the simulator guards its own state.
func (d *driver) buildHealthReport() kubeletplugin.DeviceHealthReport {
	devices := make([]kubeletplugin.DeviceHealth, 0, len(d.devices))
	for _, deviceName := range d.devices {
		health, message := d.simulator.PeekDeviceHealth(deviceName)
		devices = append(devices, kubeletplugin.DeviceHealth{
			PoolName:           d.poolName,
			DeviceName:         deviceName,
			Health:             health,
			LastUpdated:        time.Now(),
			HealthCheckTimeout: deviceHealthCheckTimeout,
			Message:            message,
		})
	}
	return kubeletplugin.DeviceHealthReport{Devices: devices}
}

// notifySubscribers wakes every active WatchHealthStatus call so it rebuilds and
// resends a fresh report. The wake channels have capacity 1, so a signal that
// finds one already pending is coalesced (non-blocking send): no report is
// queued or dropped, the subscriber always rebuilds the latest state.
func (d *driver) notifySubscribers() {
	d.clientsMu.RLock()
	defer d.clientsMu.RUnlock()
	for sig := range d.subscribers {
		select {
		case sig <- struct{}{}:
		default:
		}
	}
}

// WatchHealthStatus implements [kubeletplugin.DRAPlugin]. The kubeletplugin
// helper calls it whenever the kubelet subscribes to device health updates and
// takes care of translating the reports into the DRAResourceHealth gRPC API
// version that the kubelet supports.
func (d *driver) WatchHealthStatus(ctx context.Context, reports chan<- kubeletplugin.DeviceHealthReport) error {
	// Opt-out path (--device-health=false): report that this driver has no
	// device health to stream. The kubelet treats this as "health unsupported"
	// and does not subscribe. This mirrors what a driver without a health source
	// should return.
	if !d.deviceHealth {
		return kubeletplugin.ErrHealthNotSupported
	}

	logger := klog.FromContext(ctx)
	logger.Info("New health monitoring client connected")

	// Register a capacity-1 wake channel. notifySubscribers signals it and we
	// rebuild a fresh report on each wake, so the newest state always wins.
	sig := make(chan struct{}, 1)
	d.clientsMu.Lock()
	d.subscribers[sig] = struct{}{}
	d.clientsMu.Unlock()

	defer func() {
		d.clientsMu.Lock()
		delete(d.subscribers, sig)
		d.clientsMu.Unlock()
		logger.Info("Health monitoring client disconnected")
	}()

	// Send an initial snapshot immediately, then resend whenever woken.
	if d.sendReport(ctx, reports) {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-d.stopHealthCh:
			return nil
		case <-sig:
			if d.sendReport(ctx, reports) {
				return nil
			}
		}
	}
}

// sendReport builds a fresh report and delivers it, returning true if the driver
// is shutting down (or the kubelet went away) and the caller should stop.
func (d *driver) sendReport(ctx context.Context, reports chan<- kubeletplugin.DeviceHealthReport) (stopped bool) {
	report := d.buildHealthReport()
	select {
	case <-ctx.Done():
		return true
	case <-d.stopHealthCh:
		return true
	case reports <- report:
		return false
	}
}
