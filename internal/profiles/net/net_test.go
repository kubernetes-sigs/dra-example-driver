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

package net

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	configapi "sigs.k8s.io/dra-example-driver/api/example.com/resource/net/v1alpha1"
)

func TestNewProfile(t *testing.T) {
	profile := NewProfile("test-node", 4)

	assert.Equal(t, "test-node", profile.nodeName)
	assert.Equal(t, 4, profile.numNets)
}

func TestEnumerateDevices(t *testing.T) {
	profile := NewProfile("test-node", 3)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	// Should have one pool for the node
	require.Contains(t, resources.Pools, "test-node")
	pool := resources.Pools["test-node"]

	// Should have one slice
	require.Len(t, pool.Slices, 1)
	slice := pool.Slices[0]

	// Should have 3 devices (one per NIC)
	require.Len(t, slice.Devices, 3)

	// Verify device names
	assert.Equal(t, "nic-0", slice.Devices[0].Name)
	assert.Equal(t, "nic-1", slice.Devices[1].Name)
	assert.Equal(t, "nic-2", slice.Devices[2].Name)

	// Verify no shared counters
	assert.Empty(t, slice.SharedCounters)

	// Verify AllowMultipleAllocations is set (required when RequestPolicy is present)
	for _, device := range slice.Devices {
		assert.NotNil(t, device.AllowMultipleAllocations)
		assert.True(t, *device.AllowMultipleAllocations)
	}

	// Verify device attributes
	for i, device := range slice.Devices {
		assert.NotNil(t, device.Attributes["index"].IntValue)
		assert.Equal(t, int64(i), *device.Attributes["index"].IntValue)
		assert.NotNil(t, device.Attributes["uuid"].StringValue)
		assert.NotNil(t, device.Attributes["model"].StringValue)
		assert.Equal(t, "LATEST-NET-MODEL", *device.Attributes["model"].StringValue)
		assert.NotNil(t, device.Attributes["driverVersion"].VersionValue)
		assert.Equal(t, "1.0.0", *device.Attributes["driverVersion"].VersionValue)

		// Verify capacity
		vfsKey := resourceapi.QualifiedName("vfs")
		ingressKey := resourceapi.QualifiedName("ingressBandwidth")
		egressKey := resourceapi.QualifiedName("egressBandwidth")

		assert.Contains(t, device.Capacity, vfsKey)
		assert.Equal(t, resource.MustParse("100"), device.Capacity[vfsKey].Value)

		assert.Contains(t, device.Capacity, ingressKey)
		assert.Equal(t, resource.MustParse("100G"), device.Capacity[ingressKey].Value)

		assert.Contains(t, device.Capacity, egressKey)
		assert.Equal(t, resource.MustParse("100G"), device.Capacity[egressKey].Value)
	}
}

func TestEnumerateDevices_CapacityRequestPolicy(t *testing.T) {
	profile := NewProfile("test-node", 1)

	resources, err := profile.EnumerateDevices()
	require.NoError(t, err)

	device := resources.Pools["test-node"].Slices[0].Devices[0]

	// Verify VFS capacity request policy
	vfsCapacity := device.Capacity[resourceapi.QualifiedName("vfs")]
	require.NotNil(t, vfsCapacity.RequestPolicy)
	assert.Equal(t, resource.MustParse("1"), *vfsCapacity.RequestPolicy.Default)
	require.NotNil(t, vfsCapacity.RequestPolicy.ValidValues)
	assert.Len(t, vfsCapacity.RequestPolicy.ValidValues, 1)
	assert.Equal(t, resource.MustParse("1"), vfsCapacity.RequestPolicy.ValidValues[0])

	// Verify ingress bandwidth capacity request policy
	ingressCapacity := device.Capacity[resourceapi.QualifiedName("ingressBandwidth")]
	require.NotNil(t, ingressCapacity.RequestPolicy)
	assert.Equal(t, resource.MustParse("1G"), *ingressCapacity.RequestPolicy.Default)
	require.NotNil(t, ingressCapacity.RequestPolicy.ValidRange)
	assert.Equal(t, resource.MustParse("100M"), *ingressCapacity.RequestPolicy.ValidRange.Min)
	assert.Equal(t, resource.MustParse("100G"), *ingressCapacity.RequestPolicy.ValidRange.Max)
	assert.Equal(t, resource.MustParse("1M"), *ingressCapacity.RequestPolicy.ValidRange.Step)

	// Verify egress bandwidth capacity request policy (should be same as ingress)
	egressCapacity := device.Capacity[resourceapi.QualifiedName("egressBandwidth")]
	require.NotNil(t, egressCapacity.RequestPolicy)
	assert.Equal(t, resource.MustParse("1G"), *egressCapacity.RequestPolicy.Default)
	require.NotNil(t, egressCapacity.RequestPolicy.ValidRange)
	assert.Equal(t, resource.MustParse("100M"), *egressCapacity.RequestPolicy.ValidRange.Min)
	assert.Equal(t, resource.MustParse("100G"), *egressCapacity.RequestPolicy.ValidRange.Max)
	assert.Equal(t, resource.MustParse("1M"), *egressCapacity.RequestPolicy.ValidRange.Step)
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

func TestApplyConfig_Default(t *testing.T) {
	profile := NewProfile("test-node", 2)
	results := []*resourceapi.DeviceRequestAllocationResult{
		{
			Device: "nic-0",
			ConsumedCapacity: map[resourceapi.QualifiedName]resource.Quantity{
				"ingressBandwidth": resource.MustParse("10G"),
				"egressBandwidth":  resource.MustParse("5G"),
			},
		},
	}

	edits, err := profile.ApplyConfig(nil, results)
	require.NoError(t, err)
	require.Contains(t, edits, "nic-0")

	// With default config (no burst), should only have rate environment variables
	// 10G = 10000000000, 5G = 5000000000
	assert.Contains(t, edits["nic-0"].Env, "NET_DEVICE_0_INGRESS_RATE=10000000000")
	assert.Contains(t, edits["nic-0"].Env, "NET_DEVICE_0_EGRESS_RATE=5000000000")
}

func TestApplyConfig_WithBurstConfig(t *testing.T) {
	profile := NewProfile("test-node", 2)
	config := &configapi.NetConfig{
		BandwidthBurst: &configapi.BandwidthBurstEntry{
			// Burst values in bits - maximum amount of bits available instantaneously
			// For 10Gbps rate: 10M bits burst allows ~1ms of burst traffic
			// For 5Gbps rate: 5M bits burst allows ~1ms of burst traffic
			// Reference: http://man7.org/linux/man-pages/man8/tbf.8.html
			IngressBurst: 10000000, // 10Mb burst for 10Gbps rate
			EgressBurst:  5000000,  // 5Mb burst for 5Gbps rate
		},
	}
	results := []*resourceapi.DeviceRequestAllocationResult{
		{
			Device: "nic-1",
			ConsumedCapacity: map[resourceapi.QualifiedName]resource.Quantity{
				"ingressBandwidth": resource.MustParse("10G"),
				"egressBandwidth":  resource.MustParse("5G"),
			},
		},
	}

	edits, err := profile.ApplyConfig(config, results)
	require.NoError(t, err)
	require.Contains(t, edits, "nic-1")

	// Should have both burst and rate environment variables
	// Rate: 10G = 10000000000 bits/sec, 5G = 5000000000 bits/sec
	// Burst: in bits, maximum amount available instantaneously (TBF token bucket size)
	assert.Contains(t, edits["nic-1"].Env, "NET_DEVICE_1_INGRESS_BURST=10000000")
	assert.Contains(t, edits["nic-1"].Env, "NET_DEVICE_1_EGRESS_BURST=5000000")
	assert.Contains(t, edits["nic-1"].Env, "NET_DEVICE_1_INGRESS_RATE=10000000000")
	assert.Contains(t, edits["nic-1"].Env, "NET_DEVICE_1_EGRESS_RATE=5000000000")
}

func TestApplyConfig_MultipleDevices(t *testing.T) {
	profile := NewProfile("test-node", 3)
	results := []*resourceapi.DeviceRequestAllocationResult{
		{
			Device: "nic-0",
			ConsumedCapacity: map[resourceapi.QualifiedName]resource.Quantity{
				"ingressBandwidth": resource.MustParse("10G"),
				"egressBandwidth":  resource.MustParse("10G"),
			},
		},
		{
			Device: "nic-2",
			ConsumedCapacity: map[resourceapi.QualifiedName]resource.Quantity{
				"ingressBandwidth": resource.MustParse("5G"),
				"egressBandwidth":  resource.MustParse("5G"),
			},
		},
	}

	edits, err := profile.ApplyConfig(nil, results)
	require.NoError(t, err)

	// Should have edits for both devices
	require.Contains(t, edits, "nic-0")
	require.Contains(t, edits, "nic-2")

	assert.Len(t, edits["nic-0"].Env, 2)
	assert.Len(t, edits["nic-2"].Env, 2)
}

func TestApplyConfig_WithShareID(t *testing.T) {
	profile := NewProfile("test-node", 1)
	results := []*resourceapi.DeviceRequestAllocationResult{
		{
			Device:  "nic-0",
			ShareID: ptr.To(types.UID("shared-net-1")),
			ConsumedCapacity: map[resourceapi.QualifiedName]resource.Quantity{
				"ingressBandwidth": resource.MustParse("1G"),
				"egressBandwidth":  resource.MustParse("1G"),
			},
		},
	}

	edits, err := profile.ApplyConfig(nil, results)
	require.NoError(t, err)

	// With shareID, the device ID should include the share ID
	expectedDeviceID := "nic-0-shared-net-1"
	require.Contains(t, edits, expectedDeviceID)
	assert.Len(t, edits[expectedDeviceID].Env, 2)
}

func TestValidate_ValidConfig(t *testing.T) {
	profile := NewProfile("test-node", 1)
	config := &configapi.NetConfig{
		BandwidthBurst: &configapi.BandwidthBurstEntry{
			IngressBurst: 10000000, // 10Mb in bits
			EgressBurst:  5000000,  // 5Mb in bits
		},
	}

	err := profile.Validate(config)
	assert.NoError(t, err)
}

func TestValidate_InvalidConfigType(t *testing.T) {
	profile := NewProfile("test-node", 1)

	// Test with invalid config - BandwidthBurst should not be nil after normalization
	config := &configapi.NetConfig{}
	config.BandwidthBurst = nil

	err := profile.Validate(config)
	assert.Error(t, err)
}
