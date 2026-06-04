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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"k8s.io/dynamic-resource-allocation/deviceattribute"
	"k8s.io/klog/v2"
)

func scanSysfs(root string) ([]vfioPciDevice, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read vfio-gpu sysfs root %q: %w", root, err)
	}

	devices := make([]vfioPciDevice, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if _, err := deviceattribute.GetPCIBusIDAttribute(name); err != nil {
			continue
		}

		devicePath := filepath.Join(root, name)
		dev, err := readPCIDevice(devicePath, name, sysfsRootFromVFIO(root))
		if err != nil {
			continue
		}
		devices = append(devices, dev)
	}

	sort.Slice(devices, func(i, j int) bool {
		return devices[i].pciAddress < devices[j].pciAddress
	})

	return devices, nil
}

// sysfsRootFromVFIO maps a vfio-pci driver directory to the sysfs root
// that contains bus/, devices/, etc. For [DefaultSysfsRoot] this is /sys.
func sysfsRootFromVFIO(vfioRoot string) string {
	return filepath.Clean(filepath.Join(vfioRoot, "..", "..", "..", ".."))
}

// readPCIDevice resolves a single PCI sysfs entry under
// /sys/bus/pci/drivers/vfio-pci/. Its presence there is what makes
// this BDF passthrough-ready; per-device files (vendor, device,
// class, iommu_group) are read through the symlink into
// /sys/devices/.../<BDF>/.
func readPCIDevice(devicePath, address, sysfsRoot string) (vfioPciDevice, error) {
	klog.Background().Info("read PCI device", "devicePath", devicePath)
	vendor, err := readHexFile(filepath.Join(devicePath, "vendor"))
	if err != nil {
		return vfioPciDevice{}, fmt.Errorf("read vendor for %q: %w", address, err)
	}
	device, err := readHexFile(filepath.Join(devicePath, "device"))
	if err != nil {
		return vfioPciDevice{}, fmt.Errorf("read device for %q: %w", address, err)
	}

	class, _ := readHexFile(filepath.Join(devicePath, "class"))

	iommuGroup, err := readPCIIommuGroup(devicePath)
	if err != nil {
		iommuGroup = -1
	}

	pcieRoot := ""
	if pciRootAttr, err := deviceattribute.GetPCIeRootAttributeByPCIBusID(
		address,
		deviceattribute.WithFSFromRoot(sysfsRoot),
	); err == nil && pciRootAttr.Value.StringValue != nil {
		pcieRoot = *pciRootAttr.Value.StringValue
	}

	klog.Background().Info("resolved PCIe root", "pcieRoot", pcieRoot)
	return vfioPciDevice{
		pciAddress: address,
		pcieRoot:   pcieRoot,
		vendorID:   vendor,
		deviceID:   device,
		class:      class,
		iommuGroup: iommuGroup,
	}, nil
}

// readHexFile reads a sysfs file whose contents are a 0x-prefixed
// hexadecimal integer (e.g. "0xe1a5\n") and returns the lowercase hex
// digits without the prefix. Whitespace is trimmed.
func readHexFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := strings.ToLower(strings.TrimSpace(string(raw)))
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return "", fmt.Errorf("empty hex value in %q", path)
	}
	return s, nil
}

// readPCIIommuGroup returns the IOMMU group number for a PCI device.
// The kernel exposes <devicePath>/iommu_group as a symlink to
// /sys/kernel/iommu_groups/<N>; we just take the basename of the
// target and parse it as a decimal integer.
func readPCIIommuGroup(devicePath string) (int64, error) {
	base, err := readSymlinkBasename(filepath.Join(devicePath, "iommu_group"))
	if err != nil {
		return 0, err
	}
	g, err := strconv.ParseInt(base, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse iommu group %q: %w", base, err)
	}
	return g, nil
}

// readSymlinkBasename resolves a symlink and returns the basename of
// its target.
func readSymlinkBasename(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	return filepath.Base(target), nil
}
