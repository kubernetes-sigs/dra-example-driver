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
	"maps"
	"math/rand"
	"strings"

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
	nodeName          string
	numGPUs           int
	partitionsPerGPU  int
	bindingConditions bool
}

func NewProfile(nodeName string, numGPUs int, partitionsPerGPU int, bindingConditions bool) Profile {
	return Profile{
		nodeName:          nodeName,
		numGPUs:           numGPUs,
		partitionsPerGPU:  partitionsPerGPU,
		bindingConditions: bindingConditions,
	}
}

func (p Profile) EnumerateDevices() (resourceslice.DriverResources, error) {
	seed := p.nodeName
	uuids := generateUUIDs(seed, p.numGPUs)

	memoryPerGPU := resource.MustParse("80Gi")
	computePerGPU := resource.MustParse("100")

	var devices []resourceapi.Device
	var sharedCounters []resourceapi.CounterSet

	var partitionMemory, partitionCompute resource.Quantity
	if p.partitionsPerGPU > 0 {
		partitionMemory = *resource.NewQuantity(memoryPerGPU.Value()/int64(p.partitionsPerGPU), resource.BinarySI)
		partitionCompute = *resource.NewQuantity(computePerGPU.Value()/int64(p.partitionsPerGPU), resource.DecimalSI)
	}

	for i, uuid := range uuids {
		attrs := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
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
		}

		if p.partitionsPerGPU > 0 {
			counterSetName := fmt.Sprintf("gpu-%d-counters", i)
			sharedCounters = append(sharedCounters, resourceapi.CounterSet{
				Name: counterSetName,
				Counters: map[string]resourceapi.Counter{
					"memory": {
						Value: memoryPerGPU,
					},
					"compute": {
						Value: computePerGPU,
					},
				},
			})

			for j := 0; j < p.partitionsPerGPU; j++ {
				partitionAttrs := maps.Clone(attrs)
				partitionAttrs["partition"] = resourceapi.DeviceAttribute{IntValue: ptr.To(int64(j))}
				partitionAttrs["partitionable"] = resourceapi.DeviceAttribute{BoolValue: ptr.To(true)}

				devices = append(devices, resourceapi.Device{
					Name:       fmt.Sprintf("gpu-%d-partition-%d", i, j),
					Attributes: partitionAttrs,
					Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
						"memory": {
							Value: partitionMemory,
						},
					},
					ConsumesCounters: []resourceapi.DeviceCounterConsumption{
						{
							CounterSet: counterSetName,
							Counters: map[string]resourceapi.Counter{
								"memory": {
									Value: partitionMemory,
								},
								"compute": {
									Value: partitionCompute,
								},
							},
						},
					},
				})
			}

			// Full GPU device that consumes all resources from the counter set
			fullAttrs := maps.Clone(attrs)
			fullAttrs["partitionable"] = resourceapi.DeviceAttribute{BoolValue: ptr.To(true)}
			fullAttrs["full"] = resourceapi.DeviceAttribute{BoolValue: ptr.To(true)}

			devices = append(devices, resourceapi.Device{
				Name:       fmt.Sprintf("gpu-%d-full", i),
				Attributes: fullAttrs,
				Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
					"memory": {
						Value: memoryPerGPU,
					},
				},
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
			})
		} else {
			devices = append(devices, resourceapi.Device{
				Name:       fmt.Sprintf("gpu-%d", i),
				Attributes: attrs,
				Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
					"memory": {
						Value: memoryPerGPU,
					},
				},
			})
		}
	}

	if p.bindingConditions {
		for i := range devices {
			devices[i].BindingConditions = []string{profiles.BindingConditions}
			devices[i].BindingFailureConditions = []string{profiles.BindingFailureConditions}
		}
	}

	slices := []resourceslice.Slice{{Devices: devices}}
	if len(sharedCounters) > 0 {
		slices = []resourceslice.Slice{
			{SharedCounters: sharedCounters},
			{Devices: devices},
		}
	}

	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			p.nodeName: {
				Slices: slices,
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

func envVarSafeID(id string) string {
	return strings.ToUpper(strings.ReplaceAll(id, "-", "_"))
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
		// Device names are prefixed with "gpu-" (e.g. "gpu-0", "gpu-0-partition-1", "gpu-0-full").
		envID := envVarSafeID(result.Device[4:])
		envs := []string{
			fmt.Sprintf("GPU_DEVICE_%s=%s", envID, result.Device),
		}

		if config.Sharing != nil {
			envs = append(envs, fmt.Sprintf("GPU_DEVICE_%s_SHARING_STRATEGY=%s", envID, config.Sharing.Strategy))
		}

		switch {
		case config.Sharing.IsTimeSlicing():
			tsconfig, err := config.Sharing.GetTimeSlicingConfig()
			if err != nil {
				return nil, fmt.Errorf("unable to get time slicing config for device %v: %w", result.Device, err)
			}
			envs = append(envs, fmt.Sprintf("GPU_DEVICE_%s_TIMESLICE_INTERVAL=%v", envID, tsconfig.Interval))
		case config.Sharing.IsSpacePartitioning():
			spconfig, err := config.Sharing.GetSpacePartitioningConfig()
			if err != nil {
				return nil, fmt.Errorf("unable to get space partitioning config for device %v: %w", result.Device, err)
			}
			envs = append(envs, fmt.Sprintf("GPU_DEVICE_%s_PARTITION_COUNT=%v", envID, spconfig.PartitionCount))
		}

		edits := &cdispec.ContainerEdits{
			Env: envs,
		}

		perDeviceEdits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: edits}
	}

	return perDeviceEdits, nil
}
