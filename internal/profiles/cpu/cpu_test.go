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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestNewProfile(t *testing.T) {
	profile := NewProfile("test-node", "cpu.example.com", 2, 4)

	assert.Equal(t, "test-node", profile.nodeName)
	assert.Equal(t, "cpu.example.com", profile.driverName)
	assert.Equal(t, 2, profile.numNUMANodes)
	assert.Equal(t, 4, profile.cpusPerNUMANode)
}

func TestEnumerateDevices(t *testing.T) {
	const cpusPerNUMA = 4
	profile := NewProfile("test-node", "cpu.example.com", 3, cpusPerNUMA)
	wantKey := profile.CapacityKey()

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	require.Contains(t, resources.Pools, "test-node")
	pool := resources.Pools["test-node"]
	require.Len(t, pool.Slices, 1)
	devices := pool.Slices[0].Devices
	require.Len(t, devices, 3)

	wantCPU := *resource.NewQuantity(int64(cpusPerNUMA), resource.DecimalSI)
	for i, device := range devices {
		assert.Equal(t, fmt.Sprintf("numa-%d", i), device.Name)
		assert.NotNil(t, device.AllowMultipleAllocations)
		assert.True(t, *device.AllowMultipleAllocations)

		numaID := device.Attributes["numaNodeID"]
		require.NotNil(t, numaID.IntValue)
		assert.Equal(t, int64(i), *numaID.IntValue)

		cap, ok := device.Capacity[wantKey]
		require.True(t, ok, "device %q missing %q capacity entry", device.Name, wantKey)
		assert.Equal(t, 0, cap.Value.Cmp(wantCPU))

		require.Len(t, device.NodeAllocatableResourceMappings, 1)
		cpuMapping, ok := device.NodeAllocatableResourceMappings[corev1.ResourceCPU]
		require.True(t, ok)
		require.NotNil(t, cpuMapping.CapacityKey)
		assert.Equal(t, wantKey, *cpuMapping.CapacityKey)
	}
}

func TestApplyConfig(t *testing.T) {
	profile := NewProfile("test-node", "cpu.example.com", 2, 4)
	results := []*resourceapi.DeviceRequestAllocationResult{{
		Device: "numa-0",
		ConsumedCapacity: map[resourceapi.QualifiedName]resource.Quantity{
			profile.CapacityKey(): resource.MustParse("2"),
		},
	}}

	edits, err := profile.ApplyConfig(nil, results)
	require.NoError(t, err)
	require.Contains(t, edits, "numa-0")
	assert.ElementsMatch(t,
		[]string{"CPU_DEVICE_0=numa-0", "CPU_DEVICE_0_CONSUMED_CPU=2"},
		edits["numa-0"].Env,
	)
}
