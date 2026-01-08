/*
 * Copyright 2023 The Kubernetes Authors.
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
	"sync"
	"time"

	"google.golang.org/grpc"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"

	"sigs.k8s.io/dra-example-driver/pkg/consts"
)

// DeviceHealthStatus represents the health state of a device
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

	// Health monitoring
	config        *Config
	simulator     *HealthSimulator
	healthMu      sync.RWMutex
	deviceHealth  map[string]map[string]*DeviceHealthStatus // poolName -> deviceName -> health
	healthClients []chan *drahealthv1alpha1.NodeWatchResourcesResponse
	clientsMu     sync.RWMutex
	stopHealthCh  chan struct{}
	healthWg      sync.WaitGroup
}

func NewDriver(ctx context.Context, config *Config) (*driver, error) {
	driver := &driver{
		client:       config.coreclient,
		cancelCtx:    config.cancelMainCtx,
		config:       config,
		deviceHealth: make(map[string]map[string]*DeviceHealthStatus),
		stopHealthCh: make(chan struct{}),
	}

	state, err := NewDeviceState(config)
	if err != nil {
		return nil, err
	}
	driver.state = state

	// Initialize health monitoring if enabled
	if config.flags.enableHealthReporting {
		poolName := config.flags.nodeName
		driver.deviceHealth[poolName] = make(map[string]*DeviceHealthStatus)

		for deviceName := range state.allocatable {
			driver.deviceHealth[poolName][deviceName] = &DeviceHealthStatus{
				Health:  drahealthv1alpha1.HealthStatus_HEALTHY,
				Message: fmt.Sprintf("Device %s initialized successfully", deviceName),
			}
		}

		driver.simulator = NewHealthSimulator(config.flags.numDevices)
		klog.Infof("Device health reporting enabled for %d devices", config.flags.numDevices)
	}

	helper, err := kubeletplugin.Start(
		ctx,
		driver,
		kubeletplugin.KubeClient(config.coreclient),
		kubeletplugin.NodeName(config.flags.nodeName),
		kubeletplugin.DriverName(consts.DriverName),
		kubeletplugin.RegistrarDirectoryPath(config.flags.kubeletRegistrarDirectoryPath),
		kubeletplugin.PluginDataDirectoryPath(config.DriverPluginPath()),
	)
	if err != nil {
		return nil, err
	}
	driver.helper = helper

	devices := make([]resourceapi.Device, 0, len(state.allocatable))
	for device := range maps.Values(state.allocatable) {
		devices = append(devices, device)
	}
	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			config.flags.nodeName: {
				Slices: []resourceslice.Slice{
					{
						Devices: devices,
					},
				},
			},
		},
	}

	driver.healthcheck, err = startHealthcheck(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("start healthcheck: %w", err)
	}

	if err := helper.PublishResources(ctx, resources); err != nil {
		return nil, err
	}

	// Start health monitoring loop if enabled
	if config.flags.enableHealthReporting {
		driver.healthWg.Add(1)
		go driver.healthMonitoringLoop(ctx)
	}

	return driver, nil
}

func (d *driver) Shutdown(logger klog.Logger) error {
	if d.healthcheck != nil {
		d.healthcheck.Stop(logger)
	}

	// Stop health monitoring
	if d.config.flags.enableHealthReporting {
		logger.Info("Stopping device health monitoring")
		close(d.stopHealthCh)

		// Close all client channels
		d.clientsMu.Lock()
		for _, clientCh := range d.healthClients {
			close(clientCh)
		}
		d.healthClients = nil
		d.clientsMu.Unlock()

		d.healthWg.Wait()
	}

	d.helper.Stop()
	return nil
}

func (d *driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	klog.Infof("PrepareResourceClaims is called: number of claims: %d", len(claims))
	result := make(map[types.UID]kubeletplugin.PrepareResult)

	for _, claim := range claims {
		result[claim.UID] = d.prepareResourceClaim(ctx, claim)
	}

	return result, nil
}

func (d *driver) prepareResourceClaim(_ context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	preparedPBs, err := d.state.Prepare(claim)
	if err != nil {
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

	klog.Infof("Returning newly prepared devices for claim '%v': %v", claim.UID, prepared)
	return kubeletplugin.PrepareResult{Devices: prepared}
}

func (d *driver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	klog.Infof("UnprepareResourceClaims is called: number of claims: %d", len(claims))
	result := make(map[types.UID]error)

	for _, claim := range claims {
		result[claim.UID] = d.unprepareResourceClaim(ctx, claim)
	}

	return result, nil
}

func (d *driver) unprepareResourceClaim(_ context.Context, claim kubeletplugin.NamespacedObject) error {
	if err := d.state.Unprepare(string(claim.UID)); err != nil {
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

// Health monitoring methods

// healthMonitoringLoop periodically checks device health and updates status
func (d *driver) healthMonitoringLoop(ctx context.Context) {
	defer d.healthWg.Done()

	logger := klog.FromContext(ctx)
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	logger.Info("Starting device health monitoring loop")

	// Perform initial health check
	d.performHealthCheck(logger)

	for {
		select {
		case <-d.stopHealthCh:
			logger.Info("Health monitoring loop stopped")
			return
		case <-ctx.Done():
			logger.Info("Context cancelled, stopping health monitoring")
			return
		case <-ticker.C:
			d.performHealthCheck(logger)
		}
	}
}

// performHealthCheck simulates health checks and updates device status
func (d *driver) performHealthCheck(logger klog.Logger) {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()

	poolName := d.config.flags.nodeName
	deviceHealthMap := d.deviceHealth[poolName]

	hasChanges := false
	for deviceName, currentHealth := range deviceHealthMap {
		// Get simulated health status from the health simulator
		newHealth, newMessage := d.simulator.GetDeviceHealth(deviceName)

		// Check if health status or message changed
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

	// If there are changes, notify all streaming clients
	if hasChanges {
		d.notifyClients()
	}
}

// notifyClients sends health updates to all connected streaming clients
func (d *driver) notifyClients() {
	poolName := d.config.flags.nodeName
	deviceHealthMap := d.deviceHealth[poolName]

	// Build the response with all current device health statuses
	var devices []*drahealthv1alpha1.DeviceHealth
	for deviceName, health := range deviceHealthMap {
		devices = append(devices, &drahealthv1alpha1.DeviceHealth{
			Device: &drahealthv1alpha1.DeviceIdentifier{
				PoolName:   poolName,
				DeviceName: deviceName,
			},
			Health:                    health.Health,
			LastUpdatedTime:           time.Now().Unix(),
			Message:                   health.Message,
			HealthCheckTimeoutSeconds: 60, // 60 second timeout
		})
	}

	response := &drahealthv1alpha1.NodeWatchResourcesResponse{
		Devices: devices,
	}

	// Send to all connected clients
	d.clientsMu.RLock()
	defer d.clientsMu.RUnlock()

	for _, clientCh := range d.healthClients {
		select {
		case clientCh <- response:
			// Successfully sent
		default:
			// Client channel is full or closed, skip
		}
	}
}

// NodeWatchResources implements the streaming RPC for health updates
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

	// Send initial health status immediately
	d.healthMu.RLock()
	poolName := d.config.flags.nodeName
	deviceHealthMap := d.deviceHealth[poolName]

	var initialDevices []*drahealthv1alpha1.DeviceHealth
	for deviceName, health := range deviceHealthMap {
		initialDevices = append(initialDevices, &drahealthv1alpha1.DeviceHealth{
			Device: &drahealthv1alpha1.DeviceIdentifier{
				PoolName:   poolName,
				DeviceName: deviceName,
			},
			Health:                    health.Health,
			LastUpdatedTime:           time.Now().Unix(),
			Message:                   health.Message,
			HealthCheckTimeoutSeconds: 60,
		})
	}
	d.healthMu.RUnlock()

	initialResponse := &drahealthv1alpha1.NodeWatchResourcesResponse{
		Devices: initialDevices,
	}

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
