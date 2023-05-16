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
type PreparedClaims map[string]*PreparedDevices

type GpuInfo struct {
	uuid  string
	model string
}

type PreparedGpus struct {
	Devices []*GpuInfo
}

type PreparedDevices struct {
	Gpu *PreparedGpus
}

func (d PreparedDevices) Type() string {
	if d.Gpu != nil {
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

func (s *DeviceState) Prepare(claimUID string, allocation nascrd.AllocatedDevices) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	if s.prepared[claimUID] != nil {
		return s.cdi.GetClaimDevices(claimUID, s.prepared[claimUID]), nil
	}

	prepared := &PreparedDevices{}

	var err error
	switch allocation.Type() {
	case nascrd.GpuDeviceType:
		prepared.Gpu, err = s.prepareGpus(claimUID, allocation.Gpu)
	}
	if err != nil {
		return nil, fmt.Errorf("allocation failed: %v", err)
	}

	err = s.cdi.CreateClaimSpecFile(claimUID, prepared)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %v", err)
	}

	s.prepared[claimUID] = prepared

	return s.cdi.GetClaimDevices(claimUID, s.prepared[claimUID]), nil
}

func (s *DeviceState) Unprepare(claimUID string) error {
	s.Lock()
	defer s.Unlock()

	if s.prepared[claimUID] == nil {
		return nil
	}

	switch s.prepared[claimUID].Type() {
	case nascrd.GpuDeviceType:
		err := s.unprepareGpus(claimUID, s.prepared[claimUID])
		if err != nil {
			return fmt.Errorf("unprepare failed: %v", err)
		}
	}

	err := s.cdi.DeleteClaimSpecFile(claimUID)
	if err != nil {
		return fmt.Errorf("unable to delete CDI spec file for claim: %v", err)
	}

	delete(s.prepared, claimUID)

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

func (s *DeviceState) prepareGpus(claimUID string, allocated *nascrd.AllocatedGpus) (*PreparedGpus, error) {
	prepared := &PreparedGpus{}

	for _, device := range allocated.Devices {
		gpuInfo := s.allocatable[device.UUID].GpuInfo

		if _, exists := s.allocatable[device.UUID]; !exists {
			return nil, fmt.Errorf("requested GPU does not exist: %v", device.UUID)
		}

		prepared.Devices = append(prepared.Devices, gpuInfo)
	}

	return prepared, nil
}

func (s *DeviceState) unprepareGpus(claimUID string, devices *PreparedDevices) error {
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
		switch devices.Type() {
		case nascrd.GpuDeviceType:
			prepared[claim] = &PreparedDevices{}
			for _, d := range devices.Gpu.Devices {
				prepared[claim].Gpu.Devices = append(prepared[claim].Gpu.Devices, gpus[d.UUID].GpuInfo)
			}
		}
	}

	s.prepared = prepared
	return nil
}

func (s *DeviceState) syncPreparedDevicesToCRDSpec(spec *nascrd.NodeAllocationStateSpec) {
	outcas := make(map[string]nascrd.PreparedDevices)
	for claim, devices := range s.prepared {
		var prepared nascrd.PreparedDevices
		switch devices.Type() {
		case nascrd.GpuDeviceType:
			prepared.Gpu = &nascrd.PreparedGpus{}
			for _, device := range devices.Gpu.Devices {
				outdevice := nascrd.PreparedGpu{
					UUID: device.uuid,
				}
				prepared.Gpu.Devices = append(prepared.Gpu.Devices, outdevice)
			}
		}
		outcas[claim] = prepared
	}
	spec.PreparedClaims = outcas
}
