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

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/utils/ptr"

	nicconfig "github.com/soer3n/kube-ovn-dra-driver/api/kube-ovn.io/resource/nic/v1alpha1"
	nicprofile "github.com/soer3n/kube-ovn-dra-driver/internal/profiles/nic"
	"github.com/soer3n/kube-ovn-dra-driver/pkg/nicprepare"
	"github.com/soer3n/kube-ovn-dra-driver/pkg/plumbing"
)

const (
	testDriver   = "nic.kubeovn.io"
	testNode     = "node1"
	testNS       = "default"
	testPod      = "pod1"
	testPodUID   = "pod-uid-1"
	testProvider = "blue.default.ovn"
	testDevice   = "subnet-blue"
)

// newTestDeviceState builds a DeviceState for the nic profile on one node with
// the kube-ovn subnet "blue", using temporary CDI and checkpoint directories.
func newTestDeviceState(t *testing.T, pods ...runtime.Object) *DeviceState {
	t.Helper()
	state, err := NewDeviceState(newTestConfig(t, pods...))
	require.NoError(t, err)
	return state
}

func newTestConfig(t *testing.T, pods ...runtime.Object) *Config {
	t.Helper()
	subnet := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kubeovn.io/v1",
		"kind":       "Subnet",
		"metadata":   map[string]interface{}{"name": "blue"},
		"spec":       map[string]interface{}{"provider": testProvider},
	}}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "kubeovn.io", Version: "v1", Resource: "subnets"}: "SubnetList",
		{Group: "kubeovn.io", Version: "v1", Resource: "vlans"}:   "VlanList",
	}, subnet)
	config := &Config{
		flags: &Flags{
			nodeName:                    testNode,
			cdiRoot:                     t.TempDir(),
			kubeletPluginsDirectoryPath: t.TempDir(),
			profile:                     nicprofile.ProfileName,
			driverName:                  testDriver,
		},
		coreclient: fake.NewSimpleClientset(pods...),
		profile:    nicprofile.NewProfile(testNode, dynamicClient),
		nicStore:   plumbing.NewNICStore(),
	}
	return config
}

// allocatedPod returns the pod with kube-ovn's allocation annotations for the
// given provider keys.
func allocatedPod(keys ...string) *corev1.Pod {
	annotations := map[string]string{}
	for i, key := range keys {
		annotations[key+".kubernetes.io/allocated"] = "true"
		annotations[key+".kubernetes.io/ip_address"] = []string{"10.10.0.5", "10.10.0.6"}[i]
		annotations[key+".kubernetes.io/mac_address"] = []string{"00:00:00:aa:bb:01", "00:00:00:aa:bb:02"}[i]
		annotations[key+".kubernetes.io/cidr"] = "10.10.0.0/24"
		annotations[key+".kubernetes.io/gateway"] = "10.10.0.1"
	}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: testPod, Namespace: testNS, UID: testPodUID, Annotations: annotations}}
}

func nicConfig(iface string, requests ...string) resourceapi.DeviceAllocationConfiguration {
	return resourceapi.DeviceAllocationConfiguration{
		Source:   resourceapi.AllocationConfigSourceClaim,
		Requests: requests,
		DeviceConfiguration: resourceapi.DeviceConfiguration{Opaque: &resourceapi.OpaqueDeviceConfiguration{
			Driver:     testDriver,
			Parameters: runtime.RawExtension{Raw: []byte(`{"apiVersion":"nic.resource.kube-ovn.io/v1alpha1","kind":"NicConfig","interfaceName":"` + iface + `"}`)},
		}},
	}
}

// testClaim returns a claim reserved for the test pod with one result per
// request, all on the blue subnet device.
func testClaim(configs []resourceapi.DeviceAllocationConfiguration, requests ...string) *resourceapi.ResourceClaim {
	var results []resourceapi.DeviceRequestAllocationResult
	for _, request := range requests {
		results = append(results, resourceapi.DeviceRequestAllocationResult{Request: request, Driver: testDriver, Pool: testNode, Device: testDevice})
	}
	results = append(results, resourceapi.DeviceRequestAllocationResult{Request: "gpu", Driver: "gpu.example.com", Pool: testNode, Device: "gpu0"})
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: testNS, UID: "claim-uid-1"},
		Status: resourceapi.ResourceClaimStatus{
			Allocation:  &resourceapi.AllocationResult{Devices: resourceapi.DeviceAllocationResult{Results: results, Config: configs}},
			ReservedFor: []resourceapi.ResourceClaimConsumerReference{{Resource: "pods", Name: testPod, UID: testPodUID}},
		},
	}
}

func TestPrepareUnprepare(t *testing.T) {
	// Two NICs of the pod on one provider are keyed by interface name.
	state := newTestDeviceState(t, allocatedPod(testProvider+".net1", testProvider+".net2"))
	claimA := testClaim(nil, "a")
	claimB := testClaim([]resourceapi.DeviceAllocationConfiguration{nicConfig("net2", "b")}, "b")
	claimB.UID = "claim-uid-2"

	for _, claim := range []*resourceapi.ResourceClaim{claimA, claimB} {
		devices, err := state.Prepare(context.Background(), claim)
		require.NoError(t, err)
		require.Len(t, devices, 1, "the result of the other driver must be skipped")
		assert.Equal(t, testDevice, devices[0].DeviceName)
		assert.Len(t, devices[0].CdiDeviceIds, 2, "claim device and common device")
	}

	specs := state.nicStore.Specs(testPodUID)
	require.Len(t, specs, 2)
	byIface := map[string]plumbing.Spec{}
	for _, spec := range specs {
		byIface[spec.IfaceName] = spec
	}
	assert.Equal(t, "pod1.default."+testProvider+".net1", byIface["net1"].IfaceID)
	assert.Equal(t, "10.10.0.5/24", byIface["net1"].IP)
	assert.Equal(t, "pod1.default."+testProvider+".net2", byIface["net2"].IfaceID)
	assert.Equal(t, "00:00:00:aa:bb:02", byIface["net2"].MAC)

	// A repeated prepare is served from the checkpoint.
	again, err := state.Prepare(context.Background(), claimA)
	require.NoError(t, err)
	assert.Len(t, again, 1)
	assert.Len(t, state.nicStore.Specs(testPodUID), 2, "a checkpointed claim must not be added twice")

	require.NoError(t, state.Unprepare(string(claimA.UID)))
	specs = state.nicStore.Specs(testPodUID)
	require.Len(t, specs, 1, "unprepare drops the claim's NICs")
	assert.Equal(t, "net2", specs[0].IfaceName)
	for _, claim := range []*resourceapi.ResourceClaim{claimA, claimB} {
		require.NoError(t, state.Unprepare(string(claim.UID)))
		require.NoError(t, state.Unprepare(string(claim.UID)), "unprepare is idempotent")
	}
	assert.Empty(t, state.nicStore.Specs(testPodUID))
	checkpoint := newCheckpoint()
	require.NoError(t, state.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint))
	assert.Empty(t, checkpoint.V1.PreparedClaims)
}

func TestPrepareTwoRequestsOnOneDevice(t *testing.T) {
	state := newTestDeviceState(t, allocatedPod(testProvider+".net1", testProvider+".net2"))
	claim := testClaim([]resourceapi.DeviceAllocationConfiguration{nicConfig("net2", "b")}, "a", "b")

	devices, err := state.Prepare(context.Background(), claim)
	require.NoError(t, err, "two requests on one shared subnet device must get distinct CDI devices")
	require.Len(t, devices, 2)
	assert.NotEqual(t, devices[0].CdiDeviceIds[1], devices[1].CdiDeviceIds[1])

	specs := state.nicStore.Specs(testPodUID)
	require.Len(t, specs, 2)
	ifaces := []string{specs[0].IfaceName, specs[1].IfaceName}
	assert.ElementsMatch(t, []string{"net1", "net2"}, ifaces)
	require.NoError(t, state.Unprepare(string(claim.UID)))
}

func TestRestartRestoresNICs(t *testing.T) {
	config := newTestConfig(t, allocatedPod(testProvider))
	state, err := NewDeviceState(config)
	require.NoError(t, err)
	claim := testClaim(nil, "a")
	_, err = state.Prepare(context.Background(), claim)
	require.NoError(t, err)

	// A restarted plugin starts with an empty NIC store and the same checkpoint.
	restarted := *config
	restarted.nicStore = plumbing.NewNICStore()
	state, err = NewDeviceState(&restarted)
	require.NoError(t, err)
	specs := restarted.nicStore.Specs(testPodUID)
	require.Len(t, specs, 1)
	assert.Equal(t, string(claim.UID), specs[0].ClaimUID)
	assert.Equal(t, "pod1.default."+testProvider, specs[0].IfaceID)
	assert.Equal(t, "10.10.0.5/24", specs[0].IP)

	require.NoError(t, state.Unprepare(string(claim.UID)))
	assert.Empty(t, restarted.nicStore.Specs(testPodUID))
}

func TestPrepareErrors(t *testing.T) {
	state := newTestDeviceState(t, allocatedPod(testProvider))

	unallocated := testClaim(nil, "a")
	unallocated.Status.Allocation = nil
	_, err := state.Prepare(context.Background(), unallocated)
	assert.ErrorContains(t, err, "not yet allocated")

	unknownDevice := testClaim(nil, "a")
	unknownDevice.Status.Allocation.Devices.Results[0].Device = "subnet-red"
	_, err = state.Prepare(context.Background(), unknownDevice)
	assert.ErrorContains(t, err, "not allocatable")

	badConfig := testClaim([]resourceapi.DeviceAllocationConfiguration{nicConfig("much-too-long-name")}, "a")
	badConfig.Status.Allocation.Devices.Config[0].Opaque.Parameters.Raw = []byte(`{"kind":"Unknown"}`)
	_, err = state.Prepare(context.Background(), badConfig)
	assert.ErrorContains(t, err, "opaque device configs")

	notReserved := testClaim(nil, "a")
	notReserved.Status.ReservedFor = nil
	_, err = state.Prepare(context.Background(), notReserved)
	assert.ErrorContains(t, err, "NIC IPAM")
	assert.Empty(t, state.nicStore.Specs(testPodUID))
}

func TestNicSpecFromConfig(t *testing.T) {
	cfg := &nicprepare.NicDeviceConfig{
		PodUID:            testPodUID,
		PodName:           "virt-launcher-vm1-abcde",
		PodNamespace:      testNS,
		IfaceName:         "net1",
		Provider:          "vlan100.default.ovn",
		LogicalSwitchPort: "vm1.default.vlan100.default.ovn",
		IP:                "172.17.0.10/24",
		ExtraIPs:          []string{"fd00::10/120"},
		MAC:               "00:00:00:aa:bb:cc",
		Gateway:           "172.17.0.1",
	}
	assert.Equal(t, plumbing.Spec{
		PodUID:       testPodUID,
		PodName:      "virt-launcher-vm1-abcde",
		PodNamespace: testNS,
		IfaceName:    "net1",
		IP:           "172.17.0.10/24",
		ExtraIPs:     []string{"fd00::10/120"},
		MAC:          "00:00:00:aa:bb:cc",
		Gateway:      "172.17.0.1",
		Provider:     "vlan100.default.ovn",
		IfaceID:      "vm1.default.vlan100.default.ovn",
	}, nicSpecFromConfig(cfg))
}

func TestGetOpaqueDeviceConfigs(t *testing.T) {
	state := newTestDeviceState(t)
	classConfig := nicConfig("class0")
	classConfig.Source = resourceapi.AllocationConfigSourceClass
	otherDriver := nicConfig("other")
	otherDriver.Opaque.Driver = "gpu.example.com"

	configs, err := GetOpaqueDeviceConfigs(state.configDecoder, testDriver,
		[]resourceapi.DeviceAllocationConfiguration{nicConfig("claim0", "a"), otherDriver, classConfig})
	require.NoError(t, err)
	require.Len(t, configs, 2)
	assert.Equal(t, "class0", ifaceOf(configs[0].Config))
	assert.Equal(t, "claim0", ifaceOf(configs[1].Config), "claim configs take precedence over class configs")
	assert.Equal(t, []string{"a"}, configs[1].Requests)

	invalidSource := nicConfig("x")
	invalidSource.Source = "bogus"
	_, err = GetOpaqueDeviceConfigs(state.configDecoder, testDriver, []resourceapi.DeviceAllocationConfiguration{invalidSource})
	assert.Error(t, err)

	notOpaque := nicConfig("x")
	notOpaque.Opaque = nil
	_, err = GetOpaqueDeviceConfigs(state.configDecoder, testDriver, []resourceapi.DeviceAllocationConfiguration{notOpaque})
	assert.Error(t, err)
}

func ifaceOf(config runtime.Object) string {
	if c, ok := config.(*nicconfig.NicConfig); ok {
		return c.InterfaceName
	}
	return ""
}

func TestCheckAdminAccess(t *testing.T) {
	state := &DeviceState{}
	claim := testClaim(nil, "a")
	assert.False(t, state.checkAdminAccess(claim))
	assert.False(t, state.checkAdminAccess(nil))
	claim.Status.Allocation.Devices.Results[0].AdminAccess = ptr.To(true)
	assert.True(t, state.checkAdminAccess(claim))
}

func TestDriverPrepareUnprepareResourceClaims(t *testing.T) {
	d := &driver{state: newTestDeviceState(t, allocatedPod(testProvider))}
	ok := testClaim(nil, "a")
	failing := testClaim(nil, "a")
	failing.UID = "claim-uid-2"
	failing.Status.Allocation = nil

	results, err := d.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{ok, failing})
	require.NoError(t, err)
	require.NoError(t, results[ok.UID].Err)
	require.Len(t, results[ok.UID].Devices, 1)
	assert.Equal(t, []string{"a"}, results[ok.UID].Devices[0].Requests)
	assert.Equal(t, testDevice, results[ok.UID].Devices[0].DeviceName)
	assert.Error(t, results[failing.UID].Err)

	unprepared, err := d.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{{UID: ok.UID}})
	require.NoError(t, err)
	assert.NoError(t, unprepared[ok.UID])
}
