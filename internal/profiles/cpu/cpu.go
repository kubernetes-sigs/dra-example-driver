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

package cpu

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	"sigs.k8s.io/dra-example-driver/internal/profiles"
)

const ProfileName = "cpu"

// CPUCapacitySuffix is appended to the driver name (e.g. "cpu.example.com")
// to form the per-device capacity key (e.g. "cpu.example.com/cpu"). Keeping
// the key derived from the driver name lets multiple driver installs (with
// distinct names) coexist without their capacity keys colliding.
const CPUCapacitySuffix = "cpu"

type Profile struct {
	nodeName        string
	driverName      string
	numNUMANodes    int
	cpusPerNUMANode int
}

func NewProfile(nodeName, driverName string, numNUMANodes, cpusPerNUMANode int) Profile {
	return Profile{
		nodeName:        nodeName,
		driverName:      driverName,
		numNUMANodes:    numNUMANodes,
		cpusPerNUMANode: cpusPerNUMANode,
	}
}

// CapacityKey returns the device-capacity key the profile uses to expose CPU
// as consumable capacity. It is also the key user-facing ResourceClaims must
// reference in `capacity.requests`.
func (p Profile) CapacityKey() resourceapi.QualifiedName {
	return resourceapi.QualifiedName(p.driverName + "/" + CPUCapacitySuffix)
}

func (p Profile) EnumerateDevices() (resourceslice.DriverResources, error) {
	capacityKey := p.CapacityKey()
	devices := make([]resourceapi.Device, p.numNUMANodes)
	for i := 0; i < p.numNUMANodes; i++ {
		devices[i] = resourceapi.Device{
			Name:                     fmt.Sprintf("numa-%d", i),
			AllowMultipleAllocations: ptr.To(true),
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"numaNodeID": {IntValue: ptr.To(int64(i))},
			},
			Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
				capacityKey: {Value: *resource.NewQuantity(int64(p.cpusPerNUMANode), resource.DecimalSI)},
			},
			NodeAllocatableResourceMappings: map[corev1.ResourceName]resourceapi.NodeAllocatableResourceMapping{
				corev1.ResourceCPU: {CapacityKey: &capacityKey},
			},
		}
	}

	return resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			p.nodeName: {Slices: []resourceslice.Slice{{Devices: devices}}},
		},
	}, nil
}

// SchemeBuilder implements [profiles.ConfigHandler]. The CPU profile does not
// accept opaque configuration yet.
func (p Profile) SchemeBuilder() runtime.SchemeBuilder {
	return runtime.NewSchemeBuilder()
}

// Validate implements [profiles.ConfigHandler].
func (p Profile) Validate(config runtime.Object) error {
	if config != nil {
		return errors.New("configuration not allowed")
	}
	return nil
}

// ApplyConfig implements [profiles.ConfigHandler]. It rejects any non-nil
// configuration and otherwise injects env vars per allocated NUMA device so
// the demo container can show which device was allocated and how much CPU
// capacity was consumed.
func (p Profile) ApplyConfig(config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	if config != nil {
		return nil, errors.New("configuration not allowed")
	}

	capacityKey := p.CapacityKey()
	edits := make(profiles.PerDeviceCDIContainerEdits, len(results))
	for _, result := range results {
		// Device names are "numa-<index>"; trim the prefix for the env var.
		envID := result.Device[len("numa-"):]
		envs := []string{
			fmt.Sprintf("CPU_DEVICE_%s=%s", envID, result.Device),
		}
		if cpu, ok := result.ConsumedCapacity[capacityKey]; ok {
			envs = append(envs, fmt.Sprintf("CPU_DEVICE_%s_CONSUMED_CPU=%s", envID, cpu.String()))
		}
		edits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: &cdispec.ContainerEdits{
			Env: envs,
		}}
	}
	return edits, nil
}
