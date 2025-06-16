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
	"fmt"
	"maps"

	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"

	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"

	"github.com/salman-5/rasberrypi-pico-driver/pkg/consts"
)

type driver struct {
	client coreclientset.Interface
	helper *kubeletplugin.Helper
	state  *DeviceState
}

func NewDriver(ctx context.Context, config *Config) (*driver, error) {
	driver := &driver{
		client: config.coreclient,
	}
	state, err := NewDeviceState(config)
	if err != nil {
		return nil, err
	}
	driver.state = state

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

	if err := helper.PublishResources(ctx, resources); err != nil {
		return nil, err
	}

	return driver, nil
}

func (d *driver) Shutdown(ctx context.Context) error {
	d.helper.Stop()
	return nil
}
func (d *driver) NodeListAndWatchResources(ctx context.Context, req *drapbv1.NodePrepareResourcesRequest) error {
	model, err := d.client.ResourceV1beta1().ResourceSlices().Get(ctx, "gpu-0", metav1.GetOptions{})
	if err != nil {
		klog.Info("Error with gpu-0")
	}
	klog.Info(model)
	return nil
}
func (d *driver) NodePrepareResources(ctx context.Context, req *drapbv1.NodePrepareResourcesRequest) (*drapbv1.NodePrepareResourcesResponse, error) {
	klog.Infof("NodePrepareResource is called: number of claims: %d", len(req.Claims))
	preparedResources := &drapbv1.NodePrepareResourcesResponse{Claims: map[string]*drapbv1.NodePrepareResourceResponse{}}

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
			CDIDeviceIDs: preparedPB.GetCDIDeviceIDs(),
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
