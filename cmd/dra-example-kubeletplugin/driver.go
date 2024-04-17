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

	resourceapi "k8s.io/api/resource/v1alpha2"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha3"

	nascrd "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/nas/v1alpha1"
	nasclient "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/nas/v1alpha1/client"
)

var _ drapbv1.NodeServer = &driver{}

type driver struct {
	doneCh    chan struct{}
	nascr     *nascrd.NodeAllocationState
	nasclient *nasclient.Client
	state     *DeviceState
}

func NewDriver(ctx context.Context, config *Config) (*driver, error) {
	var d *driver
	client := nasclient.New(config.nascr, config.exampleclient.NasV1alpha1())
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := client.GetOrCreate(ctx)
		if err != nil {
			return err
		}

		err = client.UpdateStatus(ctx, nascrd.NodeAllocationStateStatusNotReady)
		if err != nil {
			return err
		}

		state, err := NewDeviceState(config)
		if err != nil {
			return err
		}

		updatedSpec, err := state.GetUpdatedSpec(&config.nascr.Spec)
		if err != nil {
			return fmt.Errorf("error getting updated CR spec: %v", err)
		}

		err = client.Update(ctx, updatedSpec)
		if err != nil {
			return err
		}

		err = client.UpdateStatus(ctx, nascrd.NodeAllocationStateStatusReady)
		if err != nil {
			return err
		}

		d = &driver{
			nascr:     config.nascr,
			nasclient: client,
			state:     state,
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return d, nil
}

func (d *driver) Shutdown(ctx context.Context) error {
	defer close(d.doneCh)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.nasclient.Get(ctx)
		if err != nil {
			return err
		}
		return d.nasclient.UpdateStatus(ctx, nascrd.NodeAllocationStateStatusNotReady)
	})
}

func (d *driver) NodeListAndWatchResources(req *drapbv1.NodeListAndWatchResourcesRequest, stream drapbv1.Node_NodeListAndWatchResourcesServer) error {
	model := d.state.getResourceModelFromAllocatableDevices()
	resp := &drapbv1.NodeListAndWatchResourcesResponse{
		Resources: []*resourceapi.ResourceModel{&model},
	}

	if err := stream.Send(resp); err != nil {
		return err
	}

	//nolint:all,S1000: should use for range instead of for { select {} } (gosimple)
	for {
		select {
		case <-d.doneCh:
			return nil
		}
		// TODO: Update with case for when GPUs go unhealthy
	}
}

func (d *driver) NodePrepareResources(ctx context.Context, req *drapbv1.NodePrepareResourcesRequest) (*drapbv1.NodePrepareResourcesResponse, error) {
	klog.Infof("NodePrepareResource is called: number of claims: %d", len(req.Claims))
	preparedResources := &drapbv1.NodePrepareResourcesResponse{Claims: map[string]*drapbv1.NodePrepareResourceResponse{}}

	// In production version some common operations of d.nodeUnprepareResources
	// should be done outside of the loop, for instance updating the CR could
	// be done once after all HW was prepared.
	for _, claim := range req.Claims {
		preparedResources.Claims[claim.Uid] = d.nodePrepareResource(ctx, claim)
	}

	return preparedResources, nil
}

func (d *driver) nodePrepareResource(ctx context.Context, claim *drapbv1.Claim) *drapbv1.NodePrepareResourceResponse {
	if len(claim.StructuredResourceHandle) > 0 {
		if err := d.allocateDevices(ctx, claim); err != nil {
			return &drapbv1.NodePrepareResourceResponse{
				Error: fmt.Sprintf("error allocating devices for claim %v: %v", claim.Uid, err),
			}
		}
	}

	prepared, err := d.prepare(ctx, claim.Uid)
	if err != nil {
		return &drapbv1.NodePrepareResourceResponse{
			Error: fmt.Sprintf("error preparing devices for claim %v: %v", claim.Uid, err),
		}
	}

	klog.Infof("Returning newly prepared devices for claim '%v': %s", claim.Uid, prepared)
	return &drapbv1.NodePrepareResourceResponse{CDIDevices: prepared}
}

func (d *driver) NodeUnprepareResources(ctx context.Context, req *drapbv1.NodeUnprepareResourcesRequest) (*drapbv1.NodeUnprepareResourcesResponse, error) {
	klog.Infof("NodeUnPrepareResource is called: number of claims: %d", len(req.Claims))
	unpreparedResources := &drapbv1.NodeUnprepareResourcesResponse{Claims: map[string]*drapbv1.NodeUnprepareResourceResponse{}}

	for _, claim := range req.Claims {
		unpreparedResources.Claims[claim.Uid] = d.nodeUnprepareResource(ctx, claim)
	}

	return unpreparedResources, nil
}

func (d *driver) nodeUnprepareResource(ctx context.Context, claim *drapbv1.Claim) *drapbv1.NodeUnprepareResourceResponse {
	if len(claim.StructuredResourceHandle) > 0 {
		if err := d.deallocateDevices(ctx, claim); err != nil {
			return &drapbv1.NodeUnprepareResourceResponse{
				Error: fmt.Sprintf("error deallocating devices for claim %v: %v", claim.Uid, err),
			}
		}
	}

	if err := d.unprepare(ctx, claim.Uid); err != nil {
		return &drapbv1.NodeUnprepareResourceResponse{
			Error: fmt.Sprintf("error unpreparing devices for claim %v: %v", claim.Uid, err),
		}
	}

	return &drapbv1.NodeUnprepareResourceResponse{}
}

func (d *driver) prepare(ctx context.Context, claimUID string) ([]string, error) {
	var err error
	var prepared []string
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err = d.nasclient.Get(ctx)
		if err != nil {
			return err
		}

		prepared, err = d.state.Prepare(claimUID, d.nascr.Spec.AllocatedClaims[claimUID])
		if err != nil {
			return err
		}

		updatedSpec, err := d.state.GetUpdatedSpec(&d.nascr.Spec)
		if err != nil {
			return fmt.Errorf("error getting updated CR spec: %v", err)
		}

		err = d.nasclient.Update(ctx, updatedSpec)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

func (d *driver) unprepare(ctx context.Context, claimUID string) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.nasclient.Get(ctx)
		if err != nil {
			return err
		}

		err = d.state.Unprepare(claimUID)
		if err != nil {
			return err
		}

		updatedSpec, err := d.state.GetUpdatedSpec(&d.nascr.Spec)
		if err != nil {
			return fmt.Errorf("error getting updated CR spec: %v", err)
		}

		err = d.nasclient.Update(ctx, updatedSpec)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (d *driver) allocateDevices(ctx context.Context, claim *drapbv1.Claim) error {
	allocated := nascrd.AllocatedDevices{
		Gpu: &nascrd.AllocatedGpus{},
	}

	for _, r := range claim.StructuredResourceHandle[0].Results {
		name := r.AllocationResultModel.NamedResources.Name
		gpu := nascrd.AllocatedGpu{
			UUID: fmt.Sprintf("GPU-%s", name[4:]),
		}
		allocated.Gpu.Devices = append(allocated.Gpu.Devices, gpu)
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.nasclient.Get(ctx)
		if err != nil {
			return err
		}

		if len(d.nascr.Spec.AllocatedClaims) == 0 {
			d.nascr.Spec.AllocatedClaims = make(map[string]nascrd.AllocatedDevices)
		}
		d.nascr.Spec.AllocatedClaims[claim.Uid] = allocated

		err = d.nasclient.Update(ctx, &d.nascr.Spec)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (d *driver) deallocateDevices(ctx context.Context, claim *drapbv1.Claim) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.nasclient.Get(ctx)
		if err != nil {
			return err
		}

		delete(d.nascr.Spec.AllocatedClaims, claim.Uid)

		err = d.nasclient.Update(ctx, &d.nascr.Spec)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}
