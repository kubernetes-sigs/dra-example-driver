//go:build linux

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

package vfiogpu

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/deviceattribute"

	configapi "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
)

func profileWithFakeSysfs(t *testing.T, nodeName string, devices ...fakePCIDevice) Profile {
	t.Helper()

	root := t.TempDir()
	for _, dev := range devices {
		writeFakePCIDevice(t, root, dev)
	}

	return Profile{
		nodeName:   nodeName,
		driverName: "vfio-gpu.example.com",
		sysfsRoot:  root,
	}
}

func TestNewProfile(t *testing.T) {
	profile := NewProfile("test-node", "vfio-gpu.example.com")

	assert.Equal(t, "test-node", profile.nodeName)
	assert.Equal(t, "vfio-gpu.example.com", profile.driverName)
	assert.Empty(t, profile.sysfsRoot)
}

func TestResolvedSysfsRoot_Default(t *testing.T) {
	profile := NewProfile("test-node", "driver")
	assert.Equal(t, DefaultSysfsRoot, profile.resolvedSysfsRoot())
}

func TestEnumerateDevices_EmptySysfs(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node")

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	require.Contains(t, resources.Pools, "test-node")
	pool := resources.Pools["test-node"]
	require.Len(t, pool.Slices, 1)
	assert.Empty(t, pool.Slices[0].Devices)
}

func TestEnumerateDevices_SingleDevice(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node", fakePCIDevice{
		address:    "0000:65:00.0",
		vendor:     "10de",
		device:     "20c2",
		class:      "030200",
		iommuGroup: "17",
	})

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	slice := resources.Pools["test-node"].Slices[0]
	require.Len(t, slice.Devices, 1)

	device := slice.Devices[0]
	assert.Equal(t, "pci-0", device.Name)
	assert.Equal(t, int64(0), *device.Attributes["index"].IntValue)
	assert.Equal(t, "10de", *device.Attributes["vendorID"].StringValue)
	assert.Equal(t, "20c2", *device.Attributes["deviceID"].StringValue)
	assert.Equal(t, "030200", *device.Attributes["class"].StringValue)
	assert.Equal(t, int64(17), *device.Attributes["iommuGroup"].IntValue)
	assert.Equal(t, "1.0.0", *device.Attributes["driverVersion"].VersionValue)

	pciBusID := deviceattribute.StandardDeviceAttributePCIBusID
	require.Contains(t, device.Attributes, pciBusID)
	assert.Equal(t, "0000:65:00.0", *device.Attributes[pciBusID].StringValue)

	// PCIe root is resolved via deviceattribute.GetPCIeRootAttributeByPCIBusID; fake dirs have none.
	assert.NotContains(t, device.Attributes, deviceattribute.StandardDeviceAttributePCIeRoot)
}

func TestEnumerateDevices_PCIeRoot(t *testing.T) {
	machineRoot := t.TempDir()
	vfioRoot := vfioSysfsRoot(machineRoot)
	require.NoError(t, os.MkdirAll(vfioRoot, 0o755))
	writeFakePCIDeviceWithSymlink(t, vfioRoot, fakePCIDevice{
		address:    "faca:00:05.0",
		vendor:     "e1a5",
		device:     "d0c5",
		class:      "030200",
		iommuGroup: "5",
	}, "pci0000:faca")

	profile := Profile{
		nodeName:   "test-node",
		driverName: "vfio-gpu.example.com",
		sysfsRoot:  vfioRoot,
	}

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	device := resources.Pools["test-node"].Slices[0].Devices[0]
	require.Contains(t, device.Attributes, deviceattribute.StandardDeviceAttributePCIeRoot)
	assert.Equal(t, "pci0000:faca", *device.Attributes[deviceattribute.StandardDeviceAttributePCIeRoot].StringValue)
}

func TestEnumerateDevices_MultipleDevices(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node",
		fakePCIDevice{
			address:    "0000:10:00.0",
			vendor:     "10de",
			device:     "aaaa",
			iommuGroup: "1",
		},
		fakePCIDevice{
			address:    "0000:01:00.0",
			vendor:     "10de",
			device:     "bbbb",
			iommuGroup: "2",
		},
	)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	devices := resources.Pools["test-node"].Slices[0].Devices
	require.Len(t, devices, 2)
	assert.Equal(t, "pci-0", devices[0].Name)
	assert.Equal(t, "pci-1", devices[1].Name)
	assert.Equal(t, int64(0), *devices[0].Attributes["index"].IntValue)
	assert.Equal(t, int64(1), *devices[1].Attributes["index"].IntValue)
}

func TestValidate_ValidConfig(t *testing.T) {
	profile := NewProfile("test-node", "driver")
	cfg := configapi.DefaultVfioGpuConfig()

	require.NoError(t, profile.Validate(cfg))
}

func TestValidate_InvalidType(t *testing.T) {
	profile := NewProfile("test-node", "driver")

	err := profile.Validate(&resourceapi.ResourceClaim{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected v1alpha1.GpuConfig")
}

func TestApplyConfig_NilResults(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node")

	edits, err := profile.ApplyConfig(configapi.DefaultVfioGpuConfig(), nil)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestApplyConfig_NilConfigUsesDefault(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node", fakePCIDevice{
		address:    "0000:01:00.0",
		vendor:     "10de",
		device:     "20c2",
		iommuGroup: "5",
	})

	results := []*resourceapi.DeviceRequestAllocationResult{
		{Device: "pci-0"},
	}

	edits, err := profile.ApplyConfig(nil, results)
	require.NoError(t, err)
	require.Len(t, edits, 1)
}

func TestApplyConfig_InvalidConfigType(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node")

	_, err := profile.ApplyConfig(&resourceapi.ResourceClaim{}, []*resourceapi.DeviceRequestAllocationResult{
		{Device: "pci-0"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime object is not a recognized configuration")
}

func TestApplyConfig_InvalidMode(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node")
	cfg := configapi.DefaultVfioGpuConfig()
	cfg.Mode = "invalid"

	_, err := profile.ApplyConfig(cfg, []*resourceapi.DeviceRequestAllocationResult{
		{Device: "pci-0"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error validating vfio config")
}

func TestApplyConfig_Success(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node", fakePCIDevice{
		address:    "0000:01:00.0",
		vendor:     "10de",
		device:     "20c2",
		iommuGroup: "5",
	})

	results := []*resourceapi.DeviceRequestAllocationResult{
		{Device: "pci-0"},
	}

	edits, err := profile.ApplyConfig(configapi.DefaultVfioGpuConfig(), results)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	edit := edits["pci-0"]
	require.NotNil(t, edit)
	require.Len(t, edit.DeviceNodes, 2)
	assert.Equal(t, "/dev/vfio/5", edit.DeviceNodes[0].Path)
	assert.Equal(t, "rwm", edit.DeviceNodes[0].Permissions)
	assert.Equal(t, "/dev/vfio/vfio", edit.DeviceNodes[1].Path)
	assert.Equal(t, "rwm", edit.DeviceNodes[1].Permissions)
}

func TestApplyConfig_DeviceNotFound(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node")

	_, err := profile.ApplyConfig(configapi.DefaultVfioGpuConfig(), []*resourceapi.DeviceRequestAllocationResult{
		{Device: "pci-0"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vfio-gpu sysfs scan no longer sees allocated device")
}

func TestApplyConfig_NoIommuGroup(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node", fakePCIDevice{
		address: "0000:01:00.0",
		vendor:  "10de",
		device:  "20c2",
	})

	_, err := profile.ApplyConfig(configapi.DefaultVfioGpuConfig(), []*resourceapi.DeviceRequestAllocationResult{
		{Device: "pci-0"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no IOMMU group")
}

func TestDeviceName(t *testing.T) {
	assert.Equal(t, "pci-0", deviceName(0))
	assert.Equal(t, "pci-3", deviceName(3))
}

func TestScanByName(t *testing.T) {
	profile := profileWithFakeSysfs(t, "test-node",
		fakePCIDevice{
			address:    "0000:02:00.0",
			vendor:     "10de",
			device:     "1111",
			iommuGroup: "3",
		},
		fakePCIDevice{
			address:    "0000:01:00.0",
			vendor:     "10de",
			device:     "2222",
			iommuGroup: "4",
		},
	)

	devices, err := profile.scanByName()
	require.NoError(t, err)
	require.Len(t, devices, 2)

	dev0, ok := devices["pci-0"]
	require.True(t, ok)
	assert.Equal(t, "0000:01:00.0", dev0.pciAddress)
	assert.Equal(t, "2222", dev0.deviceID)
}
