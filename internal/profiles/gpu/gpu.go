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
	"sigs.k8s.io/dra-example-driver/pkg/consts"
)

const (
	CDIVendor = "k8s." + consts.DriverName
	CDIClass  = "gpu"
)

func EnumerateAllPossibleDevices(nodeName string, numGPUs int) func() (resourceslice.DriverResources, error) {
	return func() (resourceslice.DriverResources, error) {
		seed := nodeName
		uuids := generateUUIDs(seed, numGPUs)

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
				nodeName: {
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

// ApplyConfig applies a configuration to a set of device allocation results.
//
// In this example driver there is no actual configuration applied. We simply
// define a set of environment variables to be injected into the containers
// that include a given device. A real driver would likely need to do some sort
// of hardware configuration as well, based on the config passed in.
func ApplyConfig(config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	if config == nil {
		config = configapi.DefaultGpuConfig()
	}
	if config, ok := config.(*configapi.GpuConfig); ok {
		return applyGpuConfig(config, results)
	}
	return nil, fmt.Errorf("runtime object is not a recognized configuration")
}

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
