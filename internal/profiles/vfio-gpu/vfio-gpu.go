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
//
// The profile discovers PCI functions already bound to vfio-pci on the
// node (see [DefaultSysfsRoot]) and publishes them in a ResourceSlice.
// Prepared devices expose /dev/vfio group nodes for VM workloads, not
// for the simulated GPU devices used by the "gpu" profile with containers.
//
// It is a separate profile from "gpu" so operators can enable passthrough
// without mixing sysfs discovery with the example driver's fake GPUs.This only works for VMs.
// TO-DO: In the future, combine the gpu and vfio-gpu profiles into a single profile to support both VMs and containers.
package vfiogpu

import (
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/dynamic-resource-allocation/deviceattribute"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	configapi "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
	"sigs.k8s.io/dra-example-driver/internal/profiles"
)

const ProfileName = "vfio-gpu"

// Profile is the vfio-gpu device profile. It advertises one DRA
// device per PCI BDF symlink found under [DefaultSysfsRoot] (the
// kernel-supplied list of devices already bound to vfio-pci) and
// surfaces their attributes (PCI bus ID, PCIe root, vendor/device/class,
// IOMMU group) in the published ResourceSlice.
type Profile struct {
	nodeName   string
	driverName string
	sysfsRoot  string
}

func (p Profile) resolvedSysfsRoot() string {
	if p.sysfsRoot != "" {
		return p.sysfsRoot
	}
	return DefaultSysfsRoot
}

func NewProfile(nodeName, driverName string) Profile {
	return Profile{
		nodeName:   nodeName,
		driverName: driverName,
	}
}

func deviceName(index int) string {
	return fmt.Sprintf("pci-%d", index)
}

// scanByName runs ScanSysfs and returns the results keyed by the DRA
// device name that EnumerateDevices assigned.
func (p Profile) scanByName() (map[string]vfioPciDevice, error) {
	root := p.resolvedSysfsRoot()
	scanned, err := scanSysfs(root)
	if err != nil {
		return nil, fmt.Errorf("scan vfio-pci sysfs at %q: %w", root, err)
	}
	out := make(map[string]vfioPciDevice, len(scanned))
	for i, s := range scanned {
		out[deviceName(i)] = s
	}
	return out, nil
}

// EnumerateDevices implements [profiles.Profile]. It scans the
// configured vfio-gpu sysfs tree and returns one DRA device per BDF.
//
// NodePrepareResources and NodeUnprepareResources need no vfio-gpu-specific
// code in this profile. The driver neither binds nor unbinds
// hardware during prepare or unprepare.
func (p Profile) EnumerateDevices() (resourceslice.DriverResources, error) {
	root := p.resolvedSysfsRoot()
	scanned, err := scanSysfs(root)
	if err != nil {
		return resourceslice.DriverResources{}, fmt.Errorf("scan vfio-pci sysfs at %q: %w", root, err)
	}

	devices := make([]resourceapi.Device, 0, len(scanned))
	for i, s := range scanned {
		attrs := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			"index":         {IntValue: ptr.To(int64(i))},
			"vendorID":      {StringValue: ptr.To(s.vendorID)},
			"deviceID":      {StringValue: ptr.To(s.deviceID)},
			"driverVersion": {VersionValue: ptr.To("1.0.0")},
		}

		if s.pciAddress != "" {
			attrs[deviceattribute.StandardDeviceAttributePCIBusID] = resourceapi.DeviceAttribute{
				StringValue: ptr.To(s.pciAddress),
			}
		}

		if s.class != "" {
			attrs["class"] = resourceapi.DeviceAttribute{
				StringValue: ptr.To(s.class),
			}
		}
		if s.iommuGroup >= 0 {
			attrs["iommuGroup"] = resourceapi.DeviceAttribute{
				IntValue: ptr.To(s.iommuGroup),
			}
		}

		if s.pcieRoot != "" {
			attrs[deviceattribute.StandardDeviceAttributePCIeRoot] = resourceapi.DeviceAttribute{
				StringValue: ptr.To(s.pcieRoot),
			}
		}

		devices = append(devices, resourceapi.Device{
			Name:       deviceName(i),
			Attributes: attrs,
		})
	}

	return resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			p.nodeName: {
				Slices: []resourceslice.Slice{{Devices: devices}},
			},
		},
	}, nil
}

func (p Profile) SchemeBuilder() runtime.SchemeBuilder {
	return runtime.NewSchemeBuilder(configapi.AddToScheme)
}

func (p Profile) Validate(config runtime.Object) error {
	cfg, ok := config.(*configapi.GpuConfig)
	if !ok {
		return fmt.Errorf("expected v1alpha1.GpuConfig but got: %T", config)
	}
	return cfg.Validate()
}

func (p Profile) ApplyConfig(config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	if config == nil {
		config = configapi.DefaultVfioGpuConfig()
	}
	cfg, ok := config.(*configapi.GpuConfig)
	if !ok {
		return nil, fmt.Errorf("runtime object is not a recognized configuration: %T", config)
	}
	if err := cfg.Normalize(); err != nil {
		return nil, fmt.Errorf("error normalizing vfio config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("error validating vfio config: %w", err)
	}

	if len(results) == 0 {
		return nil, nil
	}

	devices, err := p.scanByName()
	if err != nil {
		return nil, err
	}

	perDeviceEdits := make(profiles.PerDeviceCDIContainerEdits, len(results))
	for _, result := range results {
		dev, ok := devices[result.Device]
		if !ok {
			return nil, fmt.Errorf("vfio-gpu sysfs scan no longer sees allocated device %q (currently visible: %d); was it unbound from vfio-pci?", result.Device, len(devices))
		}
		if dev.iommuGroup < 0 {
			return nil, fmt.Errorf("vfio-gpu device %q (BDF %s) has no IOMMU group; the kernel must be booted with intel_iommu=on / amd_iommu=on for vfio-pci passthrough", result.Device, dev.pciAddress)
		}

		edits := &cdispec.ContainerEdits{
			DeviceNodes: []*cdispec.DeviceNode{
				{Path: fmt.Sprintf("/dev/vfio/%d", dev.iommuGroup), Permissions: "rwm"},
				{Path: "/dev/vfio/vfio", Permissions: "rwm"},
			},
		}
		perDeviceEdits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: edits}
	}

	return perDeviceEdits, nil
}
