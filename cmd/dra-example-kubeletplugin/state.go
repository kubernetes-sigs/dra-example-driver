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
	"fmt"
	"sync"

	nascrd "github.com/kubernetes-sigs/dra-example-driver/api/example.com/resource/gpu/nas/v1alpha1"
)

type AllocatableDevices map[string]*AllocatableDeviceInfo
type PreparedDevices map[string]PreparedDeviceInfo
type PreparedClaims map[string]PreparedDevices

type GpuInfo struct {
	uuid  string
	model string
}

type PreparedDeviceInfo struct {
	gpu *GpuInfo
}

func (i PreparedDeviceInfo) Type() string {
	if i.gpu != nil {
		return nascrd.GpuDeviceType
	}
	return nascrd.UnknownDeviceType
}

type AllocatableDeviceInfo struct {
	*GpuInfo
}

type DeviceState struct {
	sync.Mutex
	cdi         *CDIHandler
	allocatable AllocatableDevices
	prepared    PreparedClaims
}

func NewDeviceState(config *Config) (*DeviceState, error) {
	allocatable, err := enumerateAllPossibleDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %v", err)
	}

	cdi, err := NewCDIHandler(config)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI handler: %v", err)
	}

	err = cdi.CreateCommonSpecFile()
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for common edits: %v", err)
	}

	state := &DeviceState{
		cdi:         cdi,
		allocatable: allocatable,
		prepared:    make(PreparedClaims),
	}

	err = state.syncPreparedDevicesFromCRDSpec(&config.nascrd.Spec)
	if err != nil {
		return nil, fmt.Errorf("unable to sync prepared devices from CRD: %v", err)
	}

	return state, nil
}

func (s *DeviceState) Prepare(claimUid string, allocation nascrd.AllocatedDevices) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	if len(s.prepared[claimUid]) != 0 {
		return s.cdi.GetClaimDevices(claimUid, s.prepared[claimUid]), nil
	}

	s.prepared[claimUid] = make(PreparedDevices)

	var err error
	switch allocation.Type() {
	case nascrd.GpuDeviceType:
		err = s.prepareGpus(claimUid, allocation.Gpu.Devices)
	}
	if err != nil {
		return nil, fmt.Errorf("allocation failed: %v", err)
	}

	err = s.cdi.CreateClaimSpecFile(claimUid, s.prepared[claimUid])
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %v", err)
	}

	return s.cdi.GetClaimDevices(claimUid, s.prepared[claimUid]), nil
}

func (s *DeviceState) Unprepare(claimUid string) error {
	s.Lock()
	defer s.Unlock()

	if s.prepared[claimUid] == nil {
		return nil
	}

	for _, device := range s.prepared[claimUid] {
		var err error
		switch device.Type() {
		case nascrd.GpuDeviceType:
			err = s.unprepareGpu(device.gpu)
		}
		if err != nil {
			return fmt.Errorf("unprepare failed: %v", err)
		}
	}

	delete(s.prepared, claimUid)

	err := s.cdi.DeleteClaimSpecFile(claimUid)
	if err != nil {
		return fmt.Errorf("unable to delete CDI spec file for claim: %v", err)
	}

	return nil
}

func (s *DeviceState) GetUpdatedSpec(inspec *nascrd.NodeAllocationStateSpec) *nascrd.NodeAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	outspec := inspec.DeepCopy()
	s.syncAllocatableDevicesToCRDSpec(outspec)
	s.syncPreparedDevicesToCRDSpec(outspec)
	return outspec
}

func (s *DeviceState) prepareGpus(claimUid string, devices []nascrd.AllocatedGpu) error {
	for _, device := range devices {
		if _, exists := s.allocatable[device.UUID]; !exists {
			return fmt.Errorf("allocated GPU does not exist: %v", device.UUID)
		}

		prepared := PreparedDevices{
			device.UUID: PreparedDeviceInfo{
				gpu: s.allocatable[device.UUID].GpuInfo,
			},
		}

		s.prepared[claimUid][device.UUID] = prepared[device.UUID]
	}

	return nil
}

func (s *DeviceState) unprepareGpu(gpu *GpuInfo) error {
	return nil
}

func (s *DeviceState) syncAllocatableDevicesToCRDSpec(spec *nascrd.NodeAllocationStateSpec) {
	gpus := make(map[string]nascrd.AllocatableDevice)
	for _, device := range s.allocatable {
		gpus[device.uuid] = nascrd.AllocatableDevice{
			Gpu: &nascrd.AllocatableGpu{
				UUID:        device.uuid,
				ProductName: device.model,
			},
		}
	}

	var allocatable []nascrd.AllocatableDevice
	for _, device := range gpus {
		allocatable = append(allocatable, device)
	}

	spec.AllocatableDevices = allocatable
}

func (s *DeviceState) syncPreparedDevicesFromCRDSpec(spec *nascrd.NodeAllocationStateSpec) error {
	gpus := s.allocatable

	prepared := make(PreparedClaims)
	for claim, devices := range spec.PreparedClaims {
		prepared[claim] = make(PreparedDevices)
		for _, d := range devices {
			switch d.Type() {
			case nascrd.GpuDeviceType:
				prepared[claim][d.Gpu.UUID] = PreparedDeviceInfo{
					gpu: gpus[d.Gpu.UUID].GpuInfo,
				}
			}
		}
	}

	s.prepared = prepared
	return nil
}

func (s *DeviceState) syncPreparedDevicesToCRDSpec(spec *nascrd.NodeAllocationStateSpec) {
	outcas := make(map[string]nascrd.PreparedDevices)
	for claim, devices := range s.prepared {
		var prepared []nascrd.PreparedDevice
		for uuid, device := range devices {
			outdevice := nascrd.PreparedDevice{}
			switch device.Type() {
			case nascrd.GpuDeviceType:
				outdevice.Gpu = &nascrd.PreparedGpu{
					UUID: uuid,
				}
			}
			prepared = append(prepared, outdevice)
		}
		outcas[claim] = prepared
	}
	spec.PreparedClaims = outcas
}
