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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePCIDevice struct {
	address    string
	vendor     string
	device     string
	class      string
	iommuGroup string
}

func writeFakePCIDeviceWithSymlink(t *testing.T, vfioRoot string, dev fakePCIDevice, pcieRoot string) {
	t.Helper()

	deviceTree := filepath.Join(vfioRoot, "..", "..", "..", "devices", pcieRoot, dev.address)
	require.NoError(t, os.MkdirAll(deviceTree, 0o755))

	writeHexFile(t, filepath.Join(deviceTree, "vendor"), dev.vendor)
	writeHexFile(t, filepath.Join(deviceTree, "device"), dev.device)
	if dev.class != "" {
		writeHexFile(t, filepath.Join(deviceTree, "class"), dev.class)
	}
	if dev.iommuGroup != "" {
		groupDir := filepath.Join(vfioRoot, "..", "..", "..", "kernel", "iommu_groups", dev.iommuGroup)
		require.NoError(t, os.MkdirAll(groupDir, 0o755))
		require.NoError(t, os.Symlink(
			filepath.Join("..", "..", "..", "..", "kernel", "iommu_groups", dev.iommuGroup),
			filepath.Join(deviceTree, "iommu_group"),
		))
	}

	deviceRelPath := filepath.Join("devices", pcieRoot, dev.address)
	deviceSymlinkTarget := filepath.Join("..", "..", "..", deviceRelPath)

	linkPath := filepath.Join(vfioRoot, dev.address)
	require.NoError(t, os.Symlink(deviceSymlinkTarget, linkPath))

	busDevicesDir := filepath.Join(vfioRoot, "..", "..", "devices")
	require.NoError(t, os.MkdirAll(busDevicesDir, 0o755))
	require.NoError(t, os.Symlink(deviceSymlinkTarget, filepath.Join(busDevicesDir, dev.address)))
}

func writeFakePCIDevice(t *testing.T, root string, dev fakePCIDevice) {
	t.Helper()

	devicePath := filepath.Join(root, dev.address)
	require.NoError(t, os.MkdirAll(devicePath, 0o755))

	writeHexFile(t, filepath.Join(devicePath, "vendor"), dev.vendor)
	writeHexFile(t, filepath.Join(devicePath, "device"), dev.device)
	if dev.class != "" {
		writeHexFile(t, filepath.Join(devicePath, "class"), dev.class)
	}
	if dev.iommuGroup != "" {
		groupDir := filepath.Join(root, "iommu_groups", dev.iommuGroup)
		require.NoError(t, os.MkdirAll(groupDir, 0o755))
		require.NoError(t, os.Symlink(
			filepath.Join("..", "..", "..", "..", "iommu_groups", dev.iommuGroup),
			filepath.Join(devicePath, "iommu_group"),
		))
	}
}

func vfioSysfsRoot(machineRoot string) string {
	return filepath.Join(machineRoot, "bus", "pci", "drivers", "vfio-pci")
}

func writeHexFile(t *testing.T, path, value string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("0x"+value+"\n"), 0o644))
}

func TestScanSysfs_NotExist(t *testing.T) {
	devices, err := scanSysfs(filepath.Join(t.TempDir(), "missing"))
	require.NoError(t, err)
	assert.Nil(t, devices)
}

func TestScanSysfs_EmptyDir(t *testing.T) {
	root := t.TempDir()

	devices, err := scanSysfs(root)
	require.NoError(t, err)
	assert.Empty(t, devices)
}

func TestScanSysfs_SkipsNonPCIEntries(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "bind"), []byte("bind\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "unbind"), []byte("unbind\n"), 0o644))
	writeFakePCIDevice(t, root, fakePCIDevice{
		address:    "0000:01:00.0",
		vendor:     "10de",
		device:     "20c2",
		iommuGroup: "42",
	})

	devices, err := scanSysfs(root)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	assert.Equal(t, "0000:01:00.0", devices[0].pciAddress)
}

func TestScanSysfs_SingleDevice(t *testing.T) {
	root := t.TempDir()
	writeFakePCIDevice(t, root, fakePCIDevice{
		address:    "0000:65:00.0",
		vendor:     "10de",
		device:     "20c2",
		class:      "030200",
		iommuGroup: "17",
	})

	devices, err := scanSysfs(root)
	require.NoError(t, err)
	require.Len(t, devices, 1)

	dev := devices[0]
	assert.Equal(t, "0000:65:00.0", dev.pciAddress)
	assert.Equal(t, "10de", dev.vendorID)
	assert.Equal(t, "20c2", dev.deviceID)
	assert.Equal(t, "030200", dev.class)
	assert.Equal(t, int64(17), dev.iommuGroup)
	// PCIe root is resolved via /bus/pci/devices/<BDF>; fake dirs have none.
	assert.Empty(t, dev.pcieRoot)
}

func TestScanSysfs_MultipleDevicesSorted(t *testing.T) {
	root := t.TempDir()
	writeFakePCIDevice(t, root, fakePCIDevice{
		address:    "0000:10:00.0",
		vendor:     "10de",
		device:     "aaaa",
		iommuGroup: "1",
	})
	writeFakePCIDevice(t, root, fakePCIDevice{
		address:    "0000:01:00.0",
		vendor:     "10de",
		device:     "bbbb",
		iommuGroup: "2",
	})

	devices, err := scanSysfs(root)
	require.NoError(t, err)
	require.Len(t, devices, 2)
	assert.Equal(t, "0000:01:00.0", devices[0].pciAddress)
	assert.Equal(t, "0000:10:00.0", devices[1].pciAddress)
}

func TestScanSysfs_SkipsDeviceMissingVendor(t *testing.T) {
	root := t.TempDir()
	devicePath := filepath.Join(root, "0000:02:00.0")
	require.NoError(t, os.MkdirAll(devicePath, 0o755))
	writeHexFile(t, filepath.Join(devicePath, "device"), "20c2")

	devices, err := scanSysfs(root)
	require.NoError(t, err)
	assert.Empty(t, devices)
}

func TestScanSysfs_MissingIommuGroup(t *testing.T) {
	root := t.TempDir()
	writeFakePCIDevice(t, root, fakePCIDevice{
		address: "0000:03:00.0",
		vendor:  "10de",
		device:  "20c2",
	})

	devices, err := scanSysfs(root)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	assert.Equal(t, int64(-1), devices[0].iommuGroup)
}

func TestScanSysfs_MissingPCIeRootSymlink(t *testing.T) {
	machineRoot := t.TempDir()
	vfioRoot := vfioSysfsRoot(machineRoot)
	devicePath := filepath.Join(vfioRoot, "0000:04:00.0")
	require.NoError(t, os.MkdirAll(devicePath, 0o755))
	writeHexFile(t, filepath.Join(devicePath, "vendor"), "10de")
	writeHexFile(t, filepath.Join(devicePath, "device"), "20c2")

	devices, err := scanSysfs(vfioRoot)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	assert.Empty(t, devices[0].pcieRoot)
}

func TestScanSysfs_PCIeRootFromPCIBusDevices(t *testing.T) {
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

	devices, err := scanSysfs(vfioRoot)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	assert.Equal(t, "faca:00:05.0", devices[0].pciAddress)
	assert.Equal(t, "pci0000:faca", devices[0].pcieRoot)
}

func TestSysfsRootFromVFIO(t *testing.T) {
	assert.Equal(t, "/sys", sysfsRootFromVFIO(DefaultSysfsRoot))

	machineRoot := t.TempDir()
	assert.Equal(t, machineRoot, sysfsRootFromVFIO(vfioSysfsRoot(machineRoot)))
}

func TestReadHexFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "vendor")
	require.NoError(t, os.WriteFile(path, []byte("0x10DE\n"), 0o644))

	got, err := readHexFile(path)
	require.NoError(t, err)
	assert.Equal(t, "10de", got)
}

func TestReadHexFile_Empty(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "vendor")
	require.NoError(t, os.WriteFile(path, []byte("0x\n"), 0o644))

	_, err := readHexFile(path)
	require.Error(t, err)
}

func TestReadPCIIommuGroup(t *testing.T) {
	root := t.TempDir()
	writeFakePCIDevice(t, root, fakePCIDevice{
		address:    "0000:04:00.0",
		vendor:     "10de",
		device:     "20c2",
		iommuGroup: "99",
	})

	group, err := readPCIIommuGroup(filepath.Join(root, "0000:04:00.0"))
	require.NoError(t, err)
	assert.Equal(t, int64(99), group)
}

func TestReadSymlinkBasename_Missing(t *testing.T) {
	_, err := readSymlinkBasename(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
}
