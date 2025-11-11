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

	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"
)

type DeviceHealthStatus struct {
	Health  drahealthv1alpha1.HealthStatus
	Message string
}

type driver struct {
	drahealthv1alpha1.UnimplementedDRAResourceHealthServer

	client      coreclientset.Interface
	helper      *kubeletplugin.Helper
	state       *DeviceState
	healthcheck *healthcheck
	cancelCtx   func(error)

	config        *Config
	poolName      string
	simulator     *HealthSimulator
	healthMu      sync.RWMutex
	deviceHealth  map[string]*DeviceHealthStatus
	healthClients []chan *drahealthv1alpha1.NodeWatchResourcesResponse
	clientsMu     sync.RWMutex
	healthOverrides map[string]bool
	stopHealthCh    chan struct{}
	healthWg        sync.WaitGroup
}

func NewDriver(ctx context.Context, config *Config) (*driver, error) {
	driver := &driver{
		client:       config.coreclient,
		cancelCtx:    config.cancelMainCtx,
		config:       config,
		poolName:     config.flags.nodeName,
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
			Health:  drahealthv1alpha1.HealthStatus_HEALTHY,
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
	close(d.stopHealthCh)

	d.clientsMu.Lock()
	for _, clientCh := range d.healthClients {
		close(clientCh)
	}
	d.healthClients = nil
	d.clientsMu.Unlock()

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

func (d *driver) prepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	logger := klog.FromContext(ctx)
	logger.Info("Preparing claim", "uid", claim.UID, "namespace", claim.Namespace, "name", claim.Name)
	preparedPBs, err := d.state.Prepare(claim)
	if err != nil {
		logger.Error(err, "Error preparing devices for claim", "uid", claim.UID)
		return kubeletplugin.PrepareResult{
			Err: fmt.Errorf("error preparing devices for claim %v: %w", claim.UID, err),
		}
	}
	var prepared []kubeletplugin.Device
	for _, preparedPB := range preparedPBs {
		prepared = append(prepared, kubeletplugin.Device{
			Requests:     preparedPB.GetRequestNames(),
			PoolName:     preparedPB.GetPoolName(),
			DeviceName:   preparedPB.GetDeviceName(),
			CDIDeviceIDs: preparedPB.GetCdiDeviceIds(),
		})
	}

	logger.Info("Returning newly prepared devices for claim", "uid", claim.UID, "devices", prepared)
	return kubeletplugin.PrepareResult{Devices: prepared}
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

func (d *driver) unprepareResourceClaim(_ context.Context, claim kubeletplugin.NamespacedObject) error {
	if err := d.state.Unprepare(claim.UID); err != nil {
		return fmt.Errorf("error unpreparing devices for claim %v: %w", claim.UID, err)
	}

	return nil
}

func (d *driver) HandleError(ctx context.Context, err error, msg string) {
	utilruntime.HandleErrorWithContext(ctx, err, msg)
	if !errors.Is(err, kubeletplugin.ErrRecoverable) && d.cancelCtx != nil {
		d.cancelCtx(fmt.Errorf("fatal background error: %w", err))
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

	hasChanges := false
	for deviceName, currentHealth := range d.deviceHealth {
		newHealth, newMessage := d.simulator.GetDeviceHealth(deviceName)
		if currentHealth.Health != newHealth || currentHealth.Message != newMessage {
			currentHealth.Health = newHealth
			currentHealth.Message = newMessage
			hasChanges = true
			logger.Info("Device health changed",
				"device", deviceName,
				"health", newHealth.String(),
				"message", newMessage)
		}
	}

	d.healthMu.Unlock()

	if hasChanges {
		d.notifyClients()
	}
}

func (d *driver) buildHealthResponse() *drahealthv1alpha1.NodeWatchResourcesResponse {
	var devices []*drahealthv1alpha1.DeviceHealth
	for deviceName, health := range d.deviceHealth {
		devices = append(devices, &drahealthv1alpha1.DeviceHealth{
			Device: &drahealthv1alpha1.DeviceIdentifier{
				PoolName:   d.poolName,
				DeviceName: deviceName,
			},
			Health:                    health.Health,
			LastUpdatedTime:           time.Now().Unix(),
			Message:                   health.Message,
			HealthCheckTimeoutSeconds: 60,
		})
	}

	return &drahealthv1alpha1.NodeWatchResourcesResponse{
		Devices: devices,
	}
}

func (d *driver) notifyClients() {
	d.healthMu.RLock()
	response := d.buildHealthResponse()
	d.healthMu.RUnlock()

	d.clientsMu.RLock()
	defer d.clientsMu.RUnlock()

	for _, clientCh := range d.healthClients {
		select {
		case clientCh <- response:
		default:
		}
	}
}

func (d *driver) NodeWatchResources(
	req *drahealthv1alpha1.NodeWatchResourcesRequest,
	stream grpc.ServerStreamingServer[drahealthv1alpha1.NodeWatchResourcesResponse],
) error {
	logger := klog.FromContext(stream.Context())
	logger.Info("New health monitoring client connected")

	// Create a channel for this client
	clientCh := make(chan *drahealthv1alpha1.NodeWatchResourcesResponse, 10)

	// Register the client
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
		close(clientCh)
		logger.Info("Health monitoring client disconnected")
	}()

	d.healthMu.RLock()
	initialResponse := d.buildHealthResponse()
	d.healthMu.RUnlock()

	if err := stream.Send(initialResponse); err != nil {
		return fmt.Errorf("failed to send initial health status: %w", err)
	}

	// Stream updates
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case response, ok := <-clientCh:
			if !ok {
				// Channel closed, exit
				return nil
			}
			if err := stream.Send(response); err != nil {
				return fmt.Errorf("failed to send health update: %w", err)
			}
		}
	}
}
