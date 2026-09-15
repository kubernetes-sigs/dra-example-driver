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

package helpers

import (
	"strings"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/deviceattribute"
)

// DefaultPCIeRoots are used when pcieRoot publishing is enabled but no roots
// are configured explicitly.
var DefaultPCIeRoots = []string{"pci0000:00", "pci0000:80"}

// ParsePCIeRoots splits a comma-separated list of PCIe root values.
func ParsePCIeRoots(s string) []string {
	if s == "" {
		return nil
	}
	var roots []string
	for _, part := range strings.Split(s, ",") {
		root := strings.TrimSpace(part)
		if root != "" {
			roots = append(roots, root)
		}
	}
	return roots
}

// ResolvePCIeRoots returns configured roots when set, otherwise DefaultPCIeRoots.
func ResolvePCIeRoots(configured []string) []string {
	if len(configured) > 0 {
		return configured
	}
	return DefaultPCIeRoots
}

// TopologyAttributesForDeviceIndex returns standard PCI topology attributes for
// a mock device index. When roots is empty, nil is returned.
func TopologyAttributesForDeviceIndex(index int, roots []string) map[resourceapi.QualifiedName]resourceapi.DeviceAttribute {
	if len(roots) == 0 {
		return nil
	}

	pcieRoot := roots[index%len(roots)]
	return map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
		deviceattribute.StandardDeviceAttributePCIeRoot: {
			StringValue: &pcieRoot,
		},
	}
}
