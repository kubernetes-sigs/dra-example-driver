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

// DefaultSysfsRoot is the kernel-supplied directory listing every PCI
// BDF currently bound to vfio-pci. Each entry is a symlink into
// /sys/devices/.../<BDF>/ where vendor, device, class, and iommu_group
// live.
const DefaultSysfsRoot = "/sys/bus/pci/drivers/vfio-pci"

type vfioPciDevice struct {
	pciAddress string
	vendorID   string
	deviceID   string
	class      string
	iommuGroup int64
	pcieRoot   string
}
