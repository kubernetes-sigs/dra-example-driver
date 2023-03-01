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
type AllocatedDevices map[string]AllocatedDeviceInfo
type ClaimAllocations map[string]AllocatedDevices

type GpuInfo struct {
	uuid  string
	model string
}

type AllocatedDeviceInfo struct {
	gpu *GpuInfo
}

func (i AllocatedDeviceInfo) Type() string {
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
	allocated   ClaimAllocations
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
		allocated:   make(ClaimAllocations),
	}

	err = state.syncAllocatedDevicesFromCRDSpec(&config.nascrd.Spec)
	if err != nil {
		return nil, fmt.Errorf("unable to sync allocated devices from CRD: %v", err)
	}

	return state, nil
}

func (s *DeviceState) Allocate(claimUid string, request nascrd.RequestedDevices) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	if len(s.allocated[claimUid]) != 0 {
		return s.cdi.GetClaimDevices(claimUid, s.allocated[claimUid]), nil
	}

	s.allocated[claimUid] = make(AllocatedDevices)

	var err error
	switch request.Type() {
	case nascrd.GpuDeviceType:
		err = s.allocateGpus(claimUid, request.Gpu.Devices)
	}
	if err != nil {
		return nil, fmt.Errorf("allocation failed: %v", err)
	}

	err = s.cdi.CreateClaimSpecFile(claimUid, s.allocated[claimUid])
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %v", err)
	}

	return s.cdi.GetClaimDevices(claimUid, s.allocated[claimUid]), nil
}

func (s *DeviceState) Free(claimUid string) error {
	s.Lock()
	defer s.Unlock()

	if s.allocated[claimUid] == nil {
		return nil
	}

	for _, device := range s.allocated[claimUid] {
		var err error
		switch device.Type() {
		case nascrd.GpuDeviceType:
			err = s.freeGpu(device.gpu)
		}
		if err != nil {
			return fmt.Errorf("free failed: %v", err)
		}
	}

	delete(s.allocated, claimUid)

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
	s.syncAllocatedDevicesToCRDSpec(outspec)
	return outspec
}

func (s *DeviceState) allocateGpus(claimUid string, devices []nascrd.RequestedGpu) error {
	for _, device := range devices {
		if _, exists := s.allocatable[device.UUID]; !exists {
			return fmt.Errorf("requested GPU does not exist: %v", device.UUID)
		}

		allocated := AllocatedDevices{
			device.UUID: AllocatedDeviceInfo{
				gpu: s.allocatable[device.UUID].GpuInfo,
			},
		}

		s.allocated[claimUid][device.UUID] = allocated[device.UUID]
	}

	return nil
}

func (s *DeviceState) freeGpu(gpu *GpuInfo) error {
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

func (s *DeviceState) syncAllocatedDevicesFromCRDSpec(spec *nascrd.NodeAllocationStateSpec) error {
	gpus := s.allocatable

	allocated := make(ClaimAllocations)
	for claim, devices := range spec.ClaimAllocations {
		allocated[claim] = make(AllocatedDevices)
		for _, d := range devices {
			switch d.Type() {
			case nascrd.GpuDeviceType:
				allocated[claim][d.Gpu.UUID] = AllocatedDeviceInfo{
					gpu: gpus[d.Gpu.UUID].GpuInfo,
				}
			}
		}
	}

	s.allocated = allocated
	return nil
}

func (s *DeviceState) syncAllocatedDevicesToCRDSpec(spec *nascrd.NodeAllocationStateSpec) {
	outcas := make(map[string]nascrd.AllocatedDevices)
	for claim, devices := range s.allocated {
		var allocated []nascrd.AllocatedDevice
		for uuid, device := range devices {
			outdevice := nascrd.AllocatedDevice{}
			switch device.Type() {
			case nascrd.GpuDeviceType:
				outdevice.Gpu = &nascrd.AllocatedGpu{
					UUID:        uuid,
					ProductName: device.gpu.model,
				}
			}
			allocated = append(allocated, outdevice)
		}
		outcas[claim] = allocated
	}
	spec.ClaimAllocations = outcas
}
