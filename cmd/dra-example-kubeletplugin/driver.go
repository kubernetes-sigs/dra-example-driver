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
	"os"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"sigs.k8s.io/dra-example-driver/pkg/metrics"
)

type DeviceHealthStatus struct {
	Health  kubeletplugin.HealthStatus
	Message string
}

type driver struct {
	client      coreclientset.Interface
	helper      *kubeletplugin.Helper
	state       *DeviceState
	healthcheck *healthcheck
	cancelCtx   func(error)

	config          *Config
	poolName        string
	simulator       *HealthSimulator
	healthMu        sync.RWMutex
	deviceHealth    map[string]*DeviceHealthStatus
	healthClients   []chan kubeletplugin.DeviceHealthReport
	clientsMu       sync.RWMutex
	healthOverrides map[string]bool
	stopHealthCh    chan struct{}
	healthWg        sync.WaitGroup
}

func NewDriver(ctx context.Context, config *Config) (*driver, error) {
	driver := &driver{
		client:          config.coreclient,
		cancelCtx:       config.cancelMainCtx,
		config:          config,
		poolName:        config.flags.nodeName,
		deviceHealth:    make(map[string]*DeviceHealthStatus),
		healthOverrides: make(map[string]bool),
		stopHealthCh:    make(chan struct{}),
	}

	state, err := NewDeviceState(config)
	if err != nil {
		return nil, err
	}
	driver.state = state

	var deviceNames []string
	for deviceName := range state.allocatable {
		deviceNames = append(deviceNames, deviceName)
		driver.deviceHealth[deviceName] = &DeviceHealthStatus{
			Health:  kubeletplugin.HealthStatusHealthy,
			Message: fmt.Sprintf("Device %s initialized successfully", deviceName),
		}
	}

	driver.simulator = NewHealthSimulator(deviceNames)
	klog.Infof("Device health reporting enabled for %d devices", len(deviceNames))

	helper, err := kubeletplugin.Start(ctx, driver,
		kubeletplugin.KubeClient(config.coreclient),
		kubeletplugin.NodeName(config.flags.nodeName),
		kubeletplugin.DriverName(config.flags.driverName),
		kubeletplugin.RegistrarDirectoryPath(config.flags.kubeletRegistrarDirectoryPath),
		kubeletplugin.PluginDataDirectoryPath(config.DriverPluginPath()),
		kubeletplugin.RollingUpdate(types.UID(config.flags.podUID)),
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

	driver.healthWg.Add(2)
	go driver.healthMonitoringLoop(ctx)
	go driver.watchHealthOverrides(ctx)

	return driver, nil
}

func (d *driver) Shutdown(logger klog.Logger) error {
	if d.healthcheck != nil {
		d.healthcheck.Stop(logger)
	}

	logger.Info("Stopping device health monitoring")
	// Closing stopHealthCh also tells all pending WatchHealthStatus calls to
	// return. The subscriber channels are never closed, they get garbage
	// collected once both the subscriber and notifyClients stop using them.
	close(d.stopHealthCh)

	d.healthWg.Wait()

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

func (d *driver) healthMonitoringLoop(ctx context.Context) {
	defer d.healthWg.Done()

	logger := klog.FromContext(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	logger.Info("Starting device health monitoring loop")
	d.performHealthCheck(logger)

	for {
		select {
		case <-d.stopHealthCh:
			logger.Info("Health monitoring loop stopped")
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.performHealthCheck(logger)
		}
	}
}

const healthAnnotationPrefix = "health.example.com/"

func (d *driver) watchHealthOverrides(ctx context.Context) {
	defer d.healthWg.Done()

	logger := klog.FromContext(ctx)
	podName, _ := os.Hostname()
	namespace := os.Getenv("NAMESPACE")
	if podName == "" || namespace == "" {
		logger.Info("Pod identity not available, health overrides disabled")
		return
	}

	for {
		watcher, err := d.client.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
			FieldSelector: "metadata.name=" + podName,
		})
		if err != nil {
			logger.Error(err, "Failed to watch pod for health overrides")
			select {
			case <-ctx.Done():
				return
			case <-d.stopHealthCh:
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

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

			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			d.applyHealthOverrides(logger, pod.Annotations)
		}
	}
}

func (d *driver) applyHealthOverrides(logger klog.Logger, annotations map[string]string) {
	d.healthMu.Lock()

	activeOverrides := make(map[string]bool)
	for key, value := range annotations {
		if !strings.HasPrefix(key, healthAnnotationPrefix) {
			continue
		}
		deviceName := strings.TrimPrefix(key, healthAnnotationPrefix)
		if _, exists := d.deviceHealth[deviceName]; !exists {
			continue
		}

		activeOverrides[deviceName] = true
		if d.healthOverrides[deviceName] {
			continue
		}

		var scenario HealthScenario
		switch strings.ToLower(value) {
		case "unhealthy":
			scenario = ScenarioTemperatureWarning
		case "unknown":
			scenario = ScenarioCommunicationFailure
		default:
			scenario = ScenarioHealthy
		}

		d.simulator.ForceScenario(deviceName, scenario)
		d.healthOverrides[deviceName] = true
		logger.Info("Health override applied", "device", deviceName, "status", value)
	}

	for deviceName := range d.healthOverrides {
		if !activeOverrides[deviceName] {
			d.simulator.ForceScenario(deviceName, ScenarioHealthy)
			delete(d.healthOverrides, deviceName)
			logger.Info("Health override removed, returning to simulation", "device", deviceName)
		}
	}

	d.healthMu.Unlock()

	d.performHealthCheck(logger)
}

func (d *driver) performHealthCheck(logger klog.Logger) {
	d.healthMu.Lock()

	for deviceName, currentHealth := range d.deviceHealth {
		newHealth, newMessage := d.simulator.GetDeviceHealth(deviceName)
		if currentHealth.Health != newHealth || currentHealth.Message != newMessage {
			currentHealth.Health = newHealth
			currentHealth.Message = newMessage
			logger.Info("Device health changed",
				"device", deviceName,
				"health", newHealth,
				"message", newMessage)
		}
	}

	d.healthMu.Unlock()

	// Notify even when nothing changed: the kubelet treats health data as
	// stale once it is older than the health check timeout, so every pass of
	// the health check re-sends the current report. Because the resend comes
	// from this loop, it doubles as evidence that health checking still works
	// (see the DRAPlugin.WatchHealthStatus contract).
	d.notifyClients()
}

// buildHealthReport snapshots the current health of all devices. The caller
// must hold healthMu.
func (d *driver) buildHealthReport() kubeletplugin.DeviceHealthReport {
	var devices []kubeletplugin.DeviceHealth
	for deviceName, health := range d.deviceHealth {
		devices = append(devices, kubeletplugin.DeviceHealth{
			PoolName:           d.poolName,
			DeviceName:         deviceName,
			Health:             health.Health,
			LastUpdated:        time.Now(),
			HealthCheckTimeout: 60 * time.Second,
			Message:            health.Message,
		})
	}

	return kubeletplugin.DeviceHealthReport{Devices: devices}
}

func (d *driver) notifyClients() {
	d.healthMu.RLock()
	report := d.buildHealthReport()
	d.healthMu.RUnlock()

	d.clientsMu.RLock()
	defer d.clientsMu.RUnlock()

	for _, clientCh := range d.healthClients {
		select {
		case clientCh <- report:
		default:
		}
	}
}

// WatchHealthStatus implements [kubeletplugin.DRAPlugin]. The kubeletplugin
// helper calls it whenever the kubelet subscribes to device health updates and
// takes care of translating the reports into the DRAResourceHealth gRPC API
// version that the kubelet supports.
func (d *driver) WatchHealthStatus(ctx context.Context, reports chan<- kubeletplugin.DeviceHealthReport) error {
	logger := klog.FromContext(ctx)
	logger.Info("New health monitoring client connected")

	// Register a channel through which notifyClients fans out updates.
	clientCh := make(chan kubeletplugin.DeviceHealthReport, 10)
	d.clientsMu.Lock()
	d.healthClients = append(d.healthClients, clientCh)
	d.clientsMu.Unlock()

	// Cleanup on exit
	defer func() {
		d.clientsMu.Lock()
		for i, ch := range d.healthClients {
			if ch == clientCh {
				d.healthClients = append(d.healthClients[:i], d.healthClients[i+1:]...)
				break
			}
		}
		d.clientsMu.Unlock()
		logger.Info("Health monitoring client disconnected")
	}()

	d.healthMu.RLock()
	initialReport := d.buildHealthReport()
	d.healthMu.RUnlock()

	select {
	case <-ctx.Done():
		return nil
	case <-d.stopHealthCh:
		return nil
	case reports <- initialReport:
	}

	// Stream updates
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-d.stopHealthCh:
			return nil
		case report := <-clientCh:
			select {
			case <-ctx.Done():
				return nil
			case <-d.stopHealthCh:
				return nil
			case reports <- report:
			}
		}
	}
}
