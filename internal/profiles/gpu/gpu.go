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

package gpu

import (
	"fmt"
	"math/rand"

	"github.com/google/uuid"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	configapi "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
	"sigs.k8s.io/dra-example-driver/internal/profiles"
)

const ProfileName = "gpu"

type Profile struct {
	nodeName             string
	numGPUs              int
	partitionableDevices bool
	partitionsPerGPU     int
}

func NewProfile(nodeName string, numGPUs int) Profile {
	return Profile{
		nodeName: nodeName,
		numGPUs:  numGPUs,
	}
}

// NewPartitionableProfile creates a profile with partitionable devices support.
// Each GPU will have a shared counter set for memory and compute resources,
// and will expose multiple partition devices that consume from those counters.
func NewPartitionableProfile(nodeName string, numGPUs int, partitionsPerGPU int) Profile {
	return Profile{
		nodeName:             nodeName,
		numGPUs:              numGPUs,
		partitionableDevices: true,
		partitionsPerGPU:     partitionsPerGPU,
	}
}

func (p Profile) EnumerateDevices() (resourceslice.DriverResources, error) {
	seed := p.nodeName
	uuids := generateUUIDs(seed, p.numGPUs)

	// If partitionable devices are enabled, create devices with shared counters
	if p.partitionableDevices {
		return p.enumeratePartitionableDevices(uuids)
	}

	// Standard non-partitionable devices
	var devices []resourceapi.Device
	for i, uuid := range uuids {
		device := resourceapi.Device{
			Name: fmt.Sprintf("gpu-%d", i),
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"index": {
					IntValue: ptr.To(int64(i)),
				},
				"uuid": {
					StringValue: ptr.To(uuid),
				},
				"model": {
					StringValue: ptr.To("LATEST-GPU-MODEL"),
				},
				"driverVersion": {
					VersionValue: ptr.To("1.0.0"),
				},
			},
			Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
				"memory": {
					Value: resource.MustParse("80Gi"),
				},
			},
		}
		devices = append(devices, device)
	}

	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			p.nodeName: {
				Slices: []resourceslice.Slice{
					{
						Devices: devices,
					},
				},
			},
		},
	}

	return resources, nil
}

// enumeratePartitionableDevices creates devices with shared counters for partitionable devices.
// Each physical GPU is represented as a CounterSet with memory and compute counters.
// Multiple partition devices are created that consume from these counters.
func (p Profile) enumeratePartitionableDevices(uuids []string) (resourceslice.DriverResources, error) {
	var devices []resourceapi.Device
	var sharedCounters []resourceapi.CounterSet

	// Memory per GPU in bytes (80Gi)
	memoryPerGPU := resource.MustParse("80Gi")
	// Compute units per GPU (abstract units representing GPU compute capacity)
	computePerGPU := resource.MustParse("100")

	for i, uuid := range uuids {
		// Create a CounterSet for each physical GPU
		// This represents the shared resources (memory, compute) of the GPU
		counterSetName := fmt.Sprintf("gpu-%d-counters", i)
		counterSet := resourceapi.CounterSet{
			Name: counterSetName,
			Counters: map[string]resourceapi.Counter{
				"memory": {
					Value: memoryPerGPU,
				},
				"compute": {
					Value: computePerGPU,
				},
			},
		}
		sharedCounters = append(sharedCounters, counterSet)

		// Calculate resources per partition
		partitions := p.partitionsPerGPU
		if partitions <= 0 {
			partitions = 4 // Default to 4 partitions per GPU
		}
		memoryPerPartition := memoryPerGPU.Value() / int64(partitions)
		computePerPartition := computePerGPU.Value() / int64(partitions)

		// Create partition devices that consume from the counter set
		for j := 0; j < partitions; j++ {
			device := resourceapi.Device{
				Name: fmt.Sprintf("gpu-%d-partition-%d", i, j),
				Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
					"index": {
						IntValue: ptr.To(int64(i)),
					},
					"partition": {
						IntValue: ptr.To(int64(j)),
					},
					"uuid": {
						StringValue: ptr.To(uuid),
					},
					"model": {
						StringValue: ptr.To("LATEST-GPU-MODEL"),
					},
					"driverVersion": {
						VersionValue: ptr.To("1.0.0"),
					},
					"partitionable": {
						BoolValue: ptr.To(true),
					},
				},
				Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
					"memory": {
						Value: *resource.NewQuantity(memoryPerPartition, resource.BinarySI),
					},
				},
				// This device consumes from the shared counter set
				ConsumesCounters: []resourceapi.DeviceCounterConsumption{
					{
						CounterSet: counterSetName,
						Counters: map[string]resourceapi.Counter{
							"memory": {
								Value: *resource.NewQuantity(memoryPerPartition, resource.BinarySI),
							},
							"compute": {
								Value: *resource.NewQuantity(computePerPartition, resource.DecimalSI),
							},
						},
					},
				},
			}
			devices = append(devices, device)
		}

		// Also create a "full GPU" device that consumes all resources
		// This allows allocating the entire GPU if needed
		fullDevice := resourceapi.Device{
			Name: fmt.Sprintf("gpu-%d-full", i),
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"index": {
					IntValue: ptr.To(int64(i)),
				},
				"uuid": {
					StringValue: ptr.To(uuid),
				},
				"model": {
					StringValue: ptr.To("LATEST-GPU-MODEL"),
				},
				"driverVersion": {
					VersionValue: ptr.To("1.0.0"),
				},
				"partitionable": {
					BoolValue: ptr.To(true),
				},
				"full": {
					BoolValue: ptr.To(true),
				},
			},
			Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
				"memory": {
					Value: memoryPerGPU,
				},
			},
			// Full GPU consumes all counters
			ConsumesCounters: []resourceapi.DeviceCounterConsumption{
				{
					CounterSet: counterSetName,
					Counters: map[string]resourceapi.Counter{
						"memory": {
							Value: memoryPerGPU,
						},
						"compute": {
							Value: computePerGPU,
						},
					},
				},
			},
		}
		devices = append(devices, fullDevice)
	}

	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			p.nodeName: {
				Slices: []resourceslice.Slice{
					{
						Devices:        devices,
						SharedCounters: sharedCounters,
					},
				},
			},
		},
	}

	return resources, nil
}

func generateUUIDs(seed string, count int) []string {
	rand := rand.New(rand.NewSource(hash(seed)))

	uuids := make([]string, count)
	for i := 0; i < count; i++ {
		charset := make([]byte, 16)
		rand.Read(charset)
		uuid, _ := uuid.FromBytes(charset)
		uuids[i] = "gpu-" + uuid.String()
	}

	return uuids
}

func hash(s string) int64 {
	h := int64(0)
	for _, c := range s {
		h = 31*h + int64(c)
	}
	return h
}

// SchemeBuilder implements [profiles.ConfigHandler].
func (p Profile) SchemeBuilder() runtime.SchemeBuilder {
	return runtime.NewSchemeBuilder(
		configapi.AddToScheme,
	)
}

// Validate implements [profiles.ConfigHandler].
func (p Profile) Validate(config runtime.Object) error {
	gpuConfig, ok := config.(*configapi.GpuConfig)
	if !ok {
		return fmt.Errorf("expected v1alpha1.GpuConfig but got: %T", config)
	}
	return gpuConfig.Validate()
}

// ApplyConfig implements [profiles.ConfigHandler].
func (p Profile) ApplyConfig(config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	if config == nil {
		config = configapi.DefaultGpuConfig()
	}
	if config, ok := config.(*configapi.GpuConfig); ok {
		return applyGpuConfig(config, results)
	}
	return nil, fmt.Errorf("runtime object is not a recognized configuration")
}

// In this example driver there is no actual configuration applied. We simply
// define a set of environment variables to be injected into the containers
// that include a given device. A real driver would likely need to do some sort
// of hardware configuration as well, based on the config passed in.
func applyGpuConfig(config *configapi.GpuConfig, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	perDeviceEdits := make(profiles.PerDeviceCDIContainerEdits)

	// Normalize the config to set any implied defaults.
	if err := config.Normalize(); err != nil {
		return nil, fmt.Errorf("error normalizing GPU config: %w", err)
	}

	// Validate the config to ensure its integrity.
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("error validating GPU config: %w", err)
	}

	for _, result := range results {
		envs := []string{
			fmt.Sprintf("GPU_DEVICE_%s=%s", result.Device[4:], result.Device),
		}

		if config.Sharing != nil {
			envs = append(envs, fmt.Sprintf("GPU_DEVICE_%s_SHARING_STRATEGY=%s", result.Device[4:], config.Sharing.Strategy))
		}

		switch {
		case config.Sharing.IsTimeSlicing():
			tsconfig, err := config.Sharing.GetTimeSlicingConfig()
			if err != nil {
				return nil, fmt.Errorf("unable to get time slicing config for device %v: %w", result.Device, err)
			}
			envs = append(envs, fmt.Sprintf("GPU_DEVICE_%s_TIMESLICE_INTERVAL=%v", result.Device[4:], tsconfig.Interval))
		case config.Sharing.IsSpacePartitioning():
			spconfig, err := config.Sharing.GetSpacePartitioningConfig()
			if err != nil {
				return nil, fmt.Errorf("unable to get space partitioning config for device %v: %w", result.Device, err)
			}
			envs = append(envs, fmt.Sprintf("GPU_DEVICE_%s_PARTITION_COUNT=%v", result.Device[4:], spconfig.PartitionCount))
		}

		edits := &cdispec.ContainerEdits{
			Env: envs,
		}

		perDeviceEdits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: edits}
	}

	return perDeviceEdits, nil
}
