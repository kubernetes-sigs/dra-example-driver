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

	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha2"

	nascrd "github.com/kubernetes-sigs/dra-example-driver/api/example.com/resource/gpu/nas/v1alpha1"
	nasclient "github.com/kubernetes-sigs/dra-example-driver/api/example.com/resource/gpu/nas/v1alpha1/client"
)

var _ drapbv1.NodeServer = &driver{}

type driver struct {
	nascrd    *nascrd.NodeAllocationState
	nasclient *nasclient.Client
	state     *DeviceState
}

func NewDriver(config *Config) (*driver, error) {
	var d *driver
	client := nasclient.New(config.nascrd, config.exampleclient.NasV1alpha1())
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := client.GetOrCreate()
		if err != nil {
			return err
		}

		err = client.UpdateStatus(nascrd.NodeAllocationStateStatusNotReady)
		if err != nil {
			return err
		}

		state, err := NewDeviceState(config)
		if err != nil {
			return err
		}

		err = client.Update(state.GetUpdatedSpec(&config.nascrd.Spec))
		if err != nil {
			return err
		}

		err = client.UpdateStatus(nascrd.NodeAllocationStateStatusReady)
		if err != nil {
			return err
		}

		d = &driver{
			nascrd:    config.nascrd,
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

func (d *driver) Shutdown() error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.nasclient.Get()
		if err != nil {
			return err
		}
		return d.nasclient.UpdateStatus(nascrd.NodeAllocationStateStatusNotReady)
	})
}

func (d *driver) NodePrepareResource(ctx context.Context, req *drapbv1.NodePrepareResourceRequest) (*drapbv1.NodePrepareResourceResponse, error) {
	klog.Infof("NodePrepareResource is called: request: %+v", req)

	var err error
	var prepared []string
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		prepared, err = d.Prepare(req.ClaimUid)
		if err != nil {
			return fmt.Errorf("error allocating devices for claim '%v': %v", req.ClaimUid, err)
		}

		err = d.nasclient.Update(d.state.GetUpdatedSpec(&d.nascrd.Spec))
		if err != nil {
			if err := d.state.Unprepare(req.ClaimUid); err != nil {
				klog.Errorf("Failed to unprepare after claim '%v' Update() error: %v", req.ClaimUid, err)
			}
			return err
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error preparing resource: %v", err)
	}

	klog.Infof("Prepared devices for claim '%v': %s", req.ClaimUid, prepared)
	return &drapbv1.NodePrepareResourceResponse{CdiDevices: prepared}, nil
}

func (d *driver) NodeUnprepareResource(ctx context.Context, req *drapbv1.NodeUnprepareResourceRequest) (*drapbv1.NodeUnprepareResourceResponse, error) {
	klog.Infof("NodeUnprepareResource is called: request: %+v", req)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.Unprepare(req.ClaimUid)
		if err != nil {
			return fmt.Errorf("error unpreparing devices for claim '%v': %v", req.ClaimUid, err)
		}

		err = d.nasclient.Update(d.state.GetUpdatedSpec(&d.nascrd.Spec))
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error unpreparing resource: %v", err)
	}

	klog.Infof("Unprepared devices for claim '%v'", req.ClaimUid)
	return &drapbv1.NodeUnprepareResourceResponse{}, nil
}

func (d *driver) Prepare(claimUID string) ([]string, error) {
	err := d.nasclient.Get()
	if err != nil {
		return nil, err
	}
	prepared, err := d.state.Prepare(claimUID, d.nascrd.Spec.AllocatedClaims[claimUID])
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

func (d *driver) Unprepare(claimUID string) error {
	err := d.nasclient.Get()
	if err != nil {
		return err
	}
	err = d.state.Unprepare(claimUID)
	if err != nil {
		return err
	}
	return nil
}
