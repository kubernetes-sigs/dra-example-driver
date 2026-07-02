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
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/dra-example-driver/internal/profiles"
)

func TestNewProfile(t *testing.T) {
	profile := NewProfile("test-node", 4, 0, false, false, false)

	assert.Equal(t, "test-node", profile.nodeName)
	assert.Equal(t, 4, profile.numGPUs)
	assert.Equal(t, 0, profile.partitionsPerGPU)
	assert.False(t, profile.enableDeviceStatus)
	assert.Equal(t, false, profile.bindingConditions)
	assert.Equal(t, false, profile.allowMultipleAllocations)
}

func TestNewProfile_WithAllOptions(t *testing.T) {
	profile := NewProfile("test-node", 2, 4, false, true, true)

	assert.Equal(t, "test-node", profile.nodeName)
	assert.Equal(t, 2, profile.numGPUs)
	assert.Equal(t, 4, profile.partitionsPerGPU)
	assert.Equal(t, true, profile.bindingConditions)
	assert.Equal(t, true, profile.allowMultipleAllocations)
}

func TestEnumerateDevices_Standard(t *testing.T) {
	profile := NewProfile("test-node", 2, 0, false, false, false)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	// Should have one pool for the node
	require.Contains(t, resources.Pools, "test-node")
	pool := resources.Pools["test-node"]

	// Should have one slice
	require.Len(t, pool.Slices, 1)
	slice := pool.Slices[0]

	// Should have 2 devices (one per GPU)
	require.Len(t, slice.Devices, 2)

	// Verify device names
	assert.Equal(t, "gpu-0", slice.Devices[0].Name)
	assert.Equal(t, "gpu-1", slice.Devices[1].Name)

	// Verify no shared counters in standard mode
	assert.Empty(t, slice.SharedCounters)

	// Verify no ConsumesCounters in standard mode
	assert.Empty(t, slice.Devices[0].ConsumesCounters)
	assert.Empty(t, slice.Devices[1].ConsumesCounters)

	// Verify device attributes
	for i, device := range slice.Devices {
		assert.NotNil(t, device.Attributes["index"].IntValue)
		assert.Equal(t, int64(i), *device.Attributes["index"].IntValue)
		assert.NotNil(t, device.Attributes["uuid"].StringValue)
		assert.NotNil(t, device.Attributes["model"].StringValue)
		assert.Equal(t, "LATEST-GPU-MODEL", *device.Attributes["model"].StringValue)

		// AllowMultipleAllocations should be false (not enabled)
		require.NotNil(t, device.AllowMultipleAllocations)
		assert.False(t, *device.AllowMultipleAllocations)

		// No RequestPolicy when allowMultipleAllocations is disabled
		memoryKey := resourceapi.QualifiedName("memory")
		assert.Contains(t, device.Capacity, memoryKey)
		assert.Equal(t, resource.MustParse("80Gi"), device.Capacity[memoryKey].Value)
		assert.Nil(t, device.Capacity[memoryKey].RequestPolicy)

		computeKey := resourceapi.QualifiedName("compute")
		assert.Contains(t, device.Capacity, computeKey)
		assert.Equal(t, resource.MustParse("100"), device.Capacity[computeKey].Value)
		assert.Nil(t, device.Capacity[computeKey].RequestPolicy)
	}
}

func TestEnumerateDevices_AllowMultipleAllocations(t *testing.T) {
	profile := NewProfile("test-node", 1, 0, false, false, true)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	device := resources.Pools["test-node"].Slices[0].Devices[0]

	require.NotNil(t, device.AllowMultipleAllocations)
	assert.True(t, *device.AllowMultipleAllocations)

	// memory: ValidRange{Min: 1Gi, Step: 1Gi, Max: 80Gi}, Default: 80Gi
	memoryCap := device.Capacity[resourceapi.QualifiedName("memory")]
	assert.Equal(t, resource.MustParse("80Gi"), memoryCap.Value)
	require.NotNil(t, memoryCap.RequestPolicy)
	assert.Equal(t, resource.MustParse("80Gi"), *memoryCap.RequestPolicy.Default)
	require.NotNil(t, memoryCap.RequestPolicy.ValidRange)
	assert.Equal(t, resource.MustParse("1Gi"), *memoryCap.RequestPolicy.ValidRange.Min)
	assert.Equal(t, resource.MustParse("1Gi"), *memoryCap.RequestPolicy.ValidRange.Step)
	assert.Equal(t, resource.MustParse("80Gi"), *memoryCap.RequestPolicy.ValidRange.Max)

	// compute: ValidRange{Min: 1, Step: 1, Max: 100}, Default: 100
	computeCap := device.Capacity[resourceapi.QualifiedName("compute")]
	assert.Equal(t, resource.MustParse("100"), computeCap.Value)
	require.NotNil(t, computeCap.RequestPolicy)
	assert.Equal(t, resource.MustParse("100"), *computeCap.RequestPolicy.Default)
	require.NotNil(t, computeCap.RequestPolicy.ValidRange)
	assert.Equal(t, resource.MustParse("1"), *computeCap.RequestPolicy.ValidRange.Min)
	assert.Equal(t, resource.MustParse("1"), *computeCap.RequestPolicy.ValidRange.Step)
	assert.Equal(t, resource.MustParse("100"), *computeCap.RequestPolicy.ValidRange.Max)
}

func TestEnumerateDevices_Partitionable(t *testing.T) {
	profile := NewProfile("test-node", 2, 4, false, false, false)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	// Should have one pool for the node
	require.Contains(t, resources.Pools, "test-node")
	pool := resources.Pools["test-node"]

	// Should have two slices: one for shared counters, one for devices
	require.Len(t, pool.Slices, 2)
	counterSlice := pool.Slices[0]
	deviceSlice := pool.Slices[1]

	// Should have shared counters (one per GPU) in counter slice
	require.Len(t, counterSlice.SharedCounters, 2)
	assert.Empty(t, counterSlice.Devices)

	// Verify counter set names and values
	for i, counterSet := range counterSlice.SharedCounters {
		assert.Equal(t, "gpu-"+string(rune('0'+i))+"-counters", counterSet.Name)
		assert.Contains(t, counterSet.Counters, "memory")
		assert.Contains(t, counterSet.Counters, "compute")
		assert.Equal(t, resource.MustParse("80Gi"), counterSet.Counters["memory"].Value)
		assert.Equal(t, resource.MustParse("100"), counterSet.Counters["compute"].Value)
	}

	// Should have devices in device slice: 4 partitions + 1 full per GPU = 10 total
	require.Len(t, deviceSlice.Devices, 10)
	assert.Empty(t, deviceSlice.SharedCounters)

	// Count partition devices and full devices
	partitionCount := 0
	fullCount := 0
	for _, device := range deviceSlice.Devices {
		if fullAttr, exists := device.Attributes["full"]; exists && fullAttr.BoolValue != nil && *fullAttr.BoolValue {
			fullCount++
		} else {
			partitionCount++
		}
	}
	assert.Equal(t, 8, partitionCount) // 4 partitions * 2 GPUs
	assert.Equal(t, 2, fullCount)      // 1 full device * 2 GPUs
}

func TestEnumerateDevices_PartitionableDeviceAttributes(t *testing.T) {
	profile := NewProfile("test-node", 1, 2, false, false, false)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	// Devices are in the second slice (first is counters)
	deviceSlice := resources.Pools["test-node"].Slices[1]

	// Should have 3 devices: 2 partitions + 1 full
	require.Len(t, deviceSlice.Devices, 3)

	// Check partition devices
	for i := 0; i < 2; i++ {
		device := deviceSlice.Devices[i]
		assert.Equal(t, "gpu-0-partition-"+string(rune('0'+i)), device.Name)

		// Verify partitionable attribute
		assert.NotNil(t, device.Attributes["partitionable"])
		assert.True(t, *device.Attributes["partitionable"].BoolValue)

		// Verify partition index
		assert.NotNil(t, device.Attributes["partition"])
		assert.Equal(t, int64(i), *device.Attributes["partition"].IntValue)

		// Verify ConsumesCounters
		require.Len(t, device.ConsumesCounters, 1)
		assert.Equal(t, "gpu-0-counters", device.ConsumesCounters[0].CounterSet)
		assert.Contains(t, device.ConsumesCounters[0].Counters, "memory")
		assert.Contains(t, device.ConsumesCounters[0].Counters, "compute")

		// Verify partition gets 1/2 of resources (2 partitions)
		expectedMemory := resource.MustParse("40Gi") // 80Gi / 2
		expectedCompute := resource.MustParse("50")  // 100 / 2
		actualMemory := device.ConsumesCounters[0].Counters["memory"].Value
		actualCompute := device.ConsumesCounters[0].Counters["compute"].Value
		assert.Equal(t, expectedMemory.Value(), actualMemory.Value())
		assert.Equal(t, expectedCompute.Value(), actualCompute.Value())
	}

	// Check full device
	fullDevice := deviceSlice.Devices[2]
	assert.Equal(t, "gpu-0-full", fullDevice.Name)
	assert.NotNil(t, fullDevice.Attributes["full"])
	assert.True(t, *fullDevice.Attributes["full"].BoolValue)

	// Full device should consume all resources
	require.Len(t, fullDevice.ConsumesCounters, 1)
	assert.Equal(t, resource.MustParse("80Gi"), fullDevice.ConsumesCounters[0].Counters["memory"].Value)
	assert.Equal(t, resource.MustParse("100"), fullDevice.ConsumesCounters[0].Counters["compute"].Value)
}

func TestEnumerateDevices_AllowMultipleAllocations_AndPartitions(t *testing.T) {
	profile := NewProfile("test-node", 1, 2, false, false, true)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	pool := resources.Pools["test-node"]

	// Two slices: counter slice + device slice
	require.Len(t, pool.Slices, 2)
	deviceSlice := pool.Slices[1]

	// 2 partition devices + 1 full device
	require.Len(t, deviceSlice.Devices, 3)

	// Partition devices: AllowMultipleAllocations=true + RequestPolicy scoped to partition size
	for i := 0; i < 2; i++ {
		device := deviceSlice.Devices[i]
		assert.Equal(t, fmt.Sprintf("gpu-0-partition-%d", i), device.Name)

		require.NotNil(t, device.AllowMultipleAllocations)
		assert.True(t, *device.AllowMultipleAllocations)

		// memory capacity: Value=40Gi, RequestPolicy{Default:40Gi, ValidRange{Min:1Gi,Step:1Gi,Max:40Gi}}
		memoryCap := device.Capacity[resourceapi.QualifiedName("memory")]
		assert.Equal(t, resource.MustParse("40Gi"), memoryCap.Value)
		require.NotNil(t, memoryCap.RequestPolicy)
		assert.Equal(t, resource.MustParse("40Gi"), *memoryCap.RequestPolicy.Default)
		require.NotNil(t, memoryCap.RequestPolicy.ValidRange)
		assert.Equal(t, resource.MustParse("1Gi"), *memoryCap.RequestPolicy.ValidRange.Min)
		assert.Equal(t, resource.MustParse("1Gi"), *memoryCap.RequestPolicy.ValidRange.Step)
		assert.Equal(t, resource.MustParse("40Gi"), *memoryCap.RequestPolicy.ValidRange.Max)

		// compute capacity: Value=50, RequestPolicy{Default:50, ValidRange{Min:1,Step:1,Max:50}}
		fifty := resource.MustParse("50")
		computeCap := device.Capacity[resourceapi.QualifiedName("compute")]
		assert.Equal(t, fifty.Value(), computeCap.Value.Value())
		require.NotNil(t, computeCap.RequestPolicy)
		assert.Equal(t, fifty.Value(), computeCap.RequestPolicy.Default.Value())
		require.NotNil(t, computeCap.RequestPolicy.ValidRange)
		assert.Equal(t, resource.MustParse("1"), *computeCap.RequestPolicy.ValidRange.Min)
		assert.Equal(t, resource.MustParse("1"), *computeCap.RequestPolicy.ValidRange.Step)
		assert.Equal(t, fifty.Value(), computeCap.RequestPolicy.ValidRange.Max.Value())

		// ConsumesCounters still present
		require.Len(t, device.ConsumesCounters, 1)
		assert.Equal(t, "gpu-0-counters", device.ConsumesCounters[0].CounterSet)
	}

	// Full device: AllowMultipleAllocations=true + RequestPolicy scoped to full GPU size
	fullDevice := deviceSlice.Devices[2]
	assert.Equal(t, "gpu-0-full", fullDevice.Name)

	require.NotNil(t, fullDevice.AllowMultipleAllocations)
	assert.True(t, *fullDevice.AllowMultipleAllocations)

	// memory capacity: Value=80Gi, RequestPolicy{Default:80Gi, ValidRange{Min:1Gi,Step:1Gi,Max:80Gi}}
	fullMemoryCap := fullDevice.Capacity[resourceapi.QualifiedName("memory")]
	assert.Equal(t, resource.MustParse("80Gi"), fullMemoryCap.Value)
	require.NotNil(t, fullMemoryCap.RequestPolicy)
	assert.Equal(t, resource.MustParse("80Gi"), *fullMemoryCap.RequestPolicy.Default)
	require.NotNil(t, fullMemoryCap.RequestPolicy.ValidRange)
	assert.Equal(t, resource.MustParse("1Gi"), *fullMemoryCap.RequestPolicy.ValidRange.Min)
	assert.Equal(t, resource.MustParse("1Gi"), *fullMemoryCap.RequestPolicy.ValidRange.Step)
	assert.Equal(t, resource.MustParse("80Gi"), *fullMemoryCap.RequestPolicy.ValidRange.Max)

	// compute capacity: Value=100, RequestPolicy{Default:100, ValidRange{Min:1,Step:1,Max:100}}
	fullComputeCap := fullDevice.Capacity[resourceapi.QualifiedName("compute")]
	assert.Equal(t, resource.MustParse("100"), fullComputeCap.Value)
	require.NotNil(t, fullComputeCap.RequestPolicy)
	assert.Equal(t, resource.MustParse("100"), *fullComputeCap.RequestPolicy.Default)
	require.NotNil(t, fullComputeCap.RequestPolicy.ValidRange)
	assert.Equal(t, resource.MustParse("1"), *fullComputeCap.RequestPolicy.ValidRange.Min)
	assert.Equal(t, resource.MustParse("1"), *fullComputeCap.RequestPolicy.ValidRange.Step)
	assert.Equal(t, resource.MustParse("100"), *fullComputeCap.RequestPolicy.ValidRange.Max)

	// ConsumesCounters still present
	require.Len(t, fullDevice.ConsumesCounters, 1)
	assert.Equal(t, "gpu-0-counters", fullDevice.ConsumesCounters[0].CounterSet)
}

func TestEnumerateDevices_ConsistentUUIDs(t *testing.T) {
	// UUIDs should be consistent for the same node name
	profile1 := NewProfile("test-node", 2, 0, false, false, false)
	profile2 := NewProfile("test-node", 2, 0, false, false, false)

	resources1, err := profile1.EnumerateDevices()
	require.NoError(t, err)
	resources2, err := profile2.EnumerateDevices()
	require.NoError(t, err)

	slice1 := resources1.Pools["test-node"].Slices[0]
	slice2 := resources2.Pools["test-node"].Slices[0]

	for i := range slice1.Devices {
		uuid1 := *slice1.Devices[i].Attributes["uuid"].StringValue
		uuid2 := *slice2.Devices[i].Attributes["uuid"].StringValue
		assert.Equal(t, uuid1, uuid2, "UUIDs should be consistent for the same node")
	}
}

func TestEnumerateDevices_DifferentNodesHaveDifferentUUIDs(t *testing.T) {
	profile1 := NewProfile("node-1", 1, 0, false, false, false)
	profile2 := NewProfile("node-2", 1, 0, false, false, false)

	resources1, err := profile1.EnumerateDevices()
	require.NoError(t, err)
	resources2, err := profile2.EnumerateDevices()
	require.NoError(t, err)

	uuid1 := *resources1.Pools["node-1"].Slices[0].Devices[0].Attributes["uuid"].StringValue
	uuid2 := *resources2.Pools["node-2"].Slices[0].Devices[0].Attributes["uuid"].StringValue

	assert.NotEqual(t, uuid1, uuid2, "Different nodes should have different UUIDs")
}

func TestBuildDeviceStatus_Disabled(t *testing.T) {
	var _ profiles.DeviceStatusBuilder = NewProfile("test-node", 1, 0, true, false, false)

	profile := NewProfile("test-node", 1, 0, false, false, false)
	allocatable := map[string]resourceapi.Device{
		"gpu-0": {Name: "gpu-0"},
	}
	result := &resourceapi.DeviceRequestAllocationResult{
		Device: "gpu-0",
		Driver: "gpu.example.com",
		Pool:   "test-node",
	}

	got := profile.BuildDeviceStatus(allocatable, result)
	assert.Nil(t, got, "BuildDeviceStatus must return nil when device status is disabled")
}

func TestBuildDeviceStatus_Enabled(t *testing.T) {
	profile := NewProfile("test-node", 1, 0, true, false, false)
	allocatable := map[string]resourceapi.Device{
		"gpu-0": {
			Name: "gpu-0",
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"uuid":          {StringValue: ptr.To("gpu-abc")},
				"model":         {StringValue: ptr.To("LATEST-GPU-MODEL")},
				"driverVersion": {VersionValue: ptr.To("1.0.0")},
				// "index" must NOT be included in published status.
				"index": {IntValue: ptr.To(int64(0))},
			},
		},
	}
	result := &resourceapi.DeviceRequestAllocationResult{
		Device: "gpu-0",
		Driver: "gpu.example.com",
		Pool:   "test-node",
	}

	got := profile.BuildDeviceStatus(allocatable, result)
	require.NotNil(t, got)
	assert.Equal(t, "gpu-0", got.Device)
	assert.Equal(t, "gpu.example.com", got.Driver)
	assert.Equal(t, "test-node", got.Pool)
	require.NotNil(t, got.Data)

	var data map[string]resourceapi.DeviceAttribute
	require.NoError(t, json.Unmarshal(got.Data.Raw, &data))
	assert.Contains(t, data, "uuid")
	assert.Contains(t, data, "model")
	assert.Contains(t, data, "driverVersion")
	assert.NotContains(t, data, "index", "only uuid/model/driverVersion should be published")
	assert.Equal(t, "gpu-abc", *data["uuid"].StringValue)
}

func TestBuildDeviceStatus_UnknownDevice(t *testing.T) {
	profile := NewProfile("test-node", 1, 0, true, false, false)
	result := &resourceapi.DeviceRequestAllocationResult{
		Device: "gpu-0",
		Driver: "gpu.example.com",
		Pool:   "test-node",
	}

	// No matching entry in allocatable; we should still return a status object
	// (Driver/Pool/Device are stamped) with an empty data map.
	got := profile.BuildDeviceStatus(map[string]resourceapi.Device{}, result)
	require.NotNil(t, got)
	require.NotNil(t, got.Data)

	var data map[string]resourceapi.DeviceAttribute
	require.NoError(t, json.Unmarshal(got.Data.Raw, &data))
	assert.Empty(t, data)
}

func TestApplyConfig(t *testing.T) {
	profile := NewProfile("test-node", 2, 0, false, false, false)

	tests := []struct {
		name     string
		config   runtime.Object
		results  []*resourceapi.DeviceRequestAllocationResult
		wantEnvs map[string][]string
	}{
		{
			name:   "consumed memory and compute capacity",
			config: nil,
			results: []*resourceapi.DeviceRequestAllocationResult{
				{
					Device: "gpu-0",
					ConsumedCapacity: map[resourceapi.QualifiedName]resource.Quantity{
						"memory":  resource.MustParse("16Gi"),
						"compute": resource.MustParse("50"),
					},
				},
			},
			wantEnvs: map[string][]string{
				"gpu-0": {
					"GPU_DEVICE_0=gpu-0",
					"GPU_DEVICE_0_SHARING_STRATEGY=TimeSlicing",
					"GPU_DEVICE_0_TIMESLICE_INTERVAL=Default",
					"GPU_DEVICE_0_MEMORY=16Gi",
					"GPU_DEVICE_0_COMPUTE=50",
				},
			},
		},
		{
			name:   "AllowMultipleAllocations: edits are keyed by device+shareID",
			config: nil,
			results: []*resourceapi.DeviceRequestAllocationResult{
				{
					Device:  "gpu-0",
					ShareID: ptr.To(types.UID("share-abc")),
					ConsumedCapacity: map[resourceapi.QualifiedName]resource.Quantity{
						"memory":  resource.MustParse("16Gi"),
						"compute": resource.MustParse("20"),
					},
				},
			},
			// state.go looks up edits by helpers.GetCDIDeviceID = "gpu-0-share-abc"
			wantEnvs: map[string][]string{
				"gpu-0-share-abc": {
					"GPU_DEVICE_0=gpu-0",
					"GPU_DEVICE_0_SHARING_STRATEGY=TimeSlicing",
					"GPU_DEVICE_0_TIMESLICE_INTERVAL=Default",
					"GPU_DEVICE_0_MEMORY=16Gi",
					"GPU_DEVICE_0_COMPUTE=20",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits, err := profile.ApplyConfig(tt.config, tt.results)
			require.NoError(t, err)

			for device, wantEnv := range tt.wantEnvs {
				require.Contains(t, edits, device)
				assert.ElementsMatch(t, wantEnv, edits[device].Env)
			}
		})
	}
}
