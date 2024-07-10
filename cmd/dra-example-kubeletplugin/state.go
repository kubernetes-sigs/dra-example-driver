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
	"strings"
	"sync"

	resourceapi "k8s.io/api/resource/v1alpha2"

	gpucrd "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
)

type AllocatableDevices map[string]*AllocatableDeviceInfo
type PreparedClaims map[string]*PreparedDevices

type GpuInfo struct {
	uuid  string
	model string
}

type AllocatedGpus struct {
	Devices []string
}

type AllocatedDevices struct {
	Gpu *AllocatedGpus
}

type PreparedGpus struct {
	Devices []*GpuInfo
}

type PreparedDevices struct {
	Gpu *PreparedGpus
}

func (d AllocatedDevices) Type() string {
	if d.Gpu != nil {
		return gpucrd.GpuDeviceType
	}
	return gpucrd.UnknownDeviceType
}

func (d PreparedDevices) Type() string {
	if d.Gpu != nil {
		return gpucrd.GpuDeviceType
	}
	return gpucrd.UnknownDeviceType
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

	return state, nil
}

func (s *DeviceState) Prepare(claimUID string, allocation AllocatedDevices) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	if s.prepared[claimUID] != nil {
		cdiDevices, err := s.cdi.GetClaimDevices(claimUID, s.prepared[claimUID])
		if err != nil {
			return nil, fmt.Errorf("unable to get CDI devices names: %v", err)
		}
		return cdiDevices, nil
	}

	prepared := &PreparedDevices{}

	var err error
	switch allocation.Type() {
	case gpucrd.GpuDeviceType:
		prepared.Gpu, err = s.prepareGpus(claimUID, allocation.Gpu)
	default:
		err = fmt.Errorf("unknown device type: %v", allocation.Type())
	}
	if err != nil {
		return nil, fmt.Errorf("praparation failed: %v", err)
	}

	err = s.cdi.CreateClaimSpecFile(claimUID, prepared)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %v", err)
	}

	s.prepared[claimUID] = prepared

	cdiDevices, err := s.cdi.GetClaimDevices(claimUID, s.prepared[claimUID])
	if err != nil {
		return nil, fmt.Errorf("unable to get CDI devices names: %v", err)
	}

	return cdiDevices, nil
}

func (s *DeviceState) Unprepare(claimUID string) error {
	s.Lock()
	defer s.Unlock()

	if s.prepared[claimUID] == nil {
		return nil
	}

	switch s.prepared[claimUID].Type() {
	case gpucrd.GpuDeviceType:
		err := s.unprepareGpus(claimUID, s.prepared[claimUID])
		if err != nil {
			return fmt.Errorf("unprepare failed: %v", err)
		}
	default:
		return fmt.Errorf("unknown device type: %v", s.prepared[claimUID].Type())
	}

	err := s.cdi.DeleteClaimSpecFile(claimUID)
	if err != nil {
		return fmt.Errorf("unable to delete CDI spec file for claim: %v", err)
	}

	delete(s.prepared, claimUID)

	return nil
}

func (s *DeviceState) prepareGpus(claimUID string, allocated *AllocatedGpus) (*PreparedGpus, error) {
	prepared := &PreparedGpus{}

	for _, device := range allocated.Devices {
		gpuInfo := s.allocatable[device].GpuInfo

		if _, exists := s.allocatable[device]; !exists {
			return nil, fmt.Errorf("requested GPU is not allocatable: %v", device)
		}

		prepared.Devices = append(prepared.Devices, gpuInfo)
	}

	return prepared, nil
}

func (s *DeviceState) unprepareGpus(claimUID string, devices *PreparedDevices) error {
	return nil
}

func (s *DeviceState) getResourceModelFromAllocatableDevices() resourceapi.ResourceModel {
	var instances []resourceapi.NamedResourcesInstance
	for _, device := range s.allocatable {
		instance := resourceapi.NamedResourcesInstance{
			Name: strings.ToLower(device.uuid),
		}
		instances = append(instances, instance)
	}

	model := resourceapi.ResourceModel{
		NamedResources: &resourceapi.NamedResourcesResources{Instances: instances},
	}

	return model
}
