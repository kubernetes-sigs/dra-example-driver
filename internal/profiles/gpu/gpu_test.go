/*
 * Copyright 2025 The Kubernetes Authors.
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestNewProfile(t *testing.T) {
	profile := NewProfile("test-node", 4)

	assert.Equal(t, "test-node", profile.nodeName)
	assert.Equal(t, 4, profile.numGPUs)
	assert.False(t, profile.partitionableDevices)
	assert.Equal(t, 0, profile.partitionsPerGPU)
}

func TestNewPartitionableProfile(t *testing.T) {
	profile := NewPartitionableProfile("test-node", 2, 4)

	assert.Equal(t, "test-node", profile.nodeName)
	assert.Equal(t, 2, profile.numGPUs)
	assert.True(t, profile.partitionableDevices)
	assert.Equal(t, 4, profile.partitionsPerGPU)
}

func TestEnumerateDevices_Standard(t *testing.T) {
	profile := NewProfile("test-node", 2)

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

		// Verify capacity
		memoryKey := resourceapi.QualifiedName("memory")
		assert.Contains(t, device.Capacity, memoryKey)
		assert.Equal(t, resource.MustParse("80Gi"), device.Capacity[memoryKey].Value)
	}
}

func TestEnumerateDevices_Partitionable(t *testing.T) {
	profile := NewPartitionableProfile("test-node", 2, 4)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	// Should have one pool for the node
	require.Contains(t, resources.Pools, "test-node")
	pool := resources.Pools["test-node"]

	// Should have one slice
	require.Len(t, pool.Slices, 1)
	slice := pool.Slices[0]

	// Should have shared counters (one per GPU)
	require.Len(t, slice.SharedCounters, 2)

	// Verify counter set names and values
	for i, counterSet := range slice.SharedCounters {
		assert.Equal(t, "gpu-"+string(rune('0'+i))+"-counters", counterSet.Name)
		assert.Contains(t, counterSet.Counters, "memory")
		assert.Contains(t, counterSet.Counters, "compute")
		assert.Equal(t, resource.MustParse("80Gi"), counterSet.Counters["memory"].Value)
		assert.Equal(t, resource.MustParse("100"), counterSet.Counters["compute"].Value)
	}

	// Should have devices: 4 partitions + 1 full per GPU = 5 devices per GPU = 10 total
	require.Len(t, slice.Devices, 10)

	// Count partition devices and full devices
	partitionCount := 0
	fullCount := 0
	for _, device := range slice.Devices {
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
	profile := NewPartitionableProfile("test-node", 1, 2)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	slice := resources.Pools["test-node"].Slices[0]

	// Should have 3 devices: 2 partitions + 1 full
	require.Len(t, slice.Devices, 3)

	// Check partition devices
	for i := 0; i < 2; i++ {
		device := slice.Devices[i]
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
	fullDevice := slice.Devices[2]
	assert.Equal(t, "gpu-0-full", fullDevice.Name)
	assert.NotNil(t, fullDevice.Attributes["full"])
	assert.True(t, *fullDevice.Attributes["full"].BoolValue)

	// Full device should consume all resources
	require.Len(t, fullDevice.ConsumesCounters, 1)
	assert.Equal(t, resource.MustParse("80Gi"), fullDevice.ConsumesCounters[0].Counters["memory"].Value)
	assert.Equal(t, resource.MustParse("100"), fullDevice.ConsumesCounters[0].Counters["compute"].Value)
}

func TestEnumerateDevices_PartitionableDefaultPartitions(t *testing.T) {
	// Test with 0 partitions - should default to 4
	profile := NewPartitionableProfile("test-node", 1, 0)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	slice := resources.Pools["test-node"].Slices[0]

	// Should have 5 devices: 4 partitions (default) + 1 full
	assert.Len(t, slice.Devices, 5)
}

func TestEnumerateDevices_ConsistentUUIDs(t *testing.T) {
	// UUIDs should be consistent for the same node name
	profile1 := NewProfile("test-node", 2)
	profile2 := NewProfile("test-node", 2)

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
	profile1 := NewProfile("node-1", 1)
	profile2 := NewProfile("node-2", 1)

	resources1, err := profile1.EnumerateDevices()
	require.NoError(t, err)
	resources2, err := profile2.EnumerateDevices()
	require.NoError(t, err)

	uuid1 := *resources1.Pools["node-1"].Slices[0].Devices[0].Attributes["uuid"].StringValue
	uuid2 := *resources2.Pools["node-2"].Slices[0].Devices[0].Attributes["uuid"].StringValue

	assert.NotEqual(t, uuid1, uuid2, "Different nodes should have different UUIDs")
}
