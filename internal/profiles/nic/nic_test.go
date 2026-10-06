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

package nic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/utils/ptr"

	configapi "github.com/soer3n/kube-ovn-dra-driver/api/kube-ovn.io/resource/nic/v1alpha1"
)

const testNode = "node1"

func kubeOVNObject(kind, name string, spec map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kubeovn.io/v1",
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": name},
		"spec":       spec,
	}}
}

func newTestProfile(objects ...runtime.Object) Profile {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{subnetGVR: "SubnetList", vlanGVR: "VlanList"}, objects...)
	return NewProfile(testNode, client)
}

func TestEnumerateDevices(t *testing.T) {
	p := newTestProfile(
		kubeOVNObject("Subnet", "blue", map[string]interface{}{"provider": "blue.default.ovn", "vpc": "ovn-cluster"}),
		kubeOVNObject("Subnet", "vlan100", map[string]interface{}{"provider": "vlan100.default.ovn", "vlan": "vlan100"}),
		kubeOVNObject("Vlan", "vlan100", map[string]interface{}{"id": int64(100), "provider": "external"}),
	)
	resources, err := p.EnumerateDevices()
	require.NoError(t, err)
	require.Contains(t, resources.Pools, testNode)
	devices := map[string]resourceapi.Device{}
	for _, d := range resources.Pools[testNode].Slices[0].Devices {
		devices[d.Name] = d
	}
	require.Len(t, devices, 2)

	blue := devices["subnet-blue"]
	assert.Equal(t, ptr.To("blue"), blue.Attributes["nic.kubeovn.io/subnetName"].StringValue)
	assert.Equal(t, ptr.To("ovn"), blue.Attributes["nic.kubeovn.io/subnetType"].StringValue)
	assert.Equal(t, ptr.To("blue.default.ovn"), blue.Attributes["nic.kubeovn.io/provider"].StringValue)
	assert.Equal(t, ptr.To("ovn-cluster"), blue.Attributes["nic.kubeovn.io/vpc"].StringValue)
	assert.NotContains(t, blue.Attributes, resourceapi.QualifiedName("nic.kubeovn.io/vlanId"))
	assert.Equal(t, ptr.To(true), blue.AllowMultipleAllocations)

	vlan := devices["subnet-vlan100"]
	assert.Equal(t, ptr.To("vlan"), vlan.Attributes["nic.kubeovn.io/subnetType"].StringValue)
	assert.Equal(t, ptr.To(int64(100)), vlan.Attributes["nic.kubeovn.io/vlanId"].IntValue)
	assert.Equal(t, ptr.To("external"), vlan.Attributes["nic.kubeovn.io/providerNetwork"].StringValue)
}

func TestEnumerateDevicesMissingVlan(t *testing.T) {
	p := newTestProfile(kubeOVNObject("Subnet", "vlan100", map[string]interface{}{"vlan": "vlan100"}))
	_, err := p.EnumerateDevices()
	assert.Error(t, err)
}

func TestApplyConfig(t *testing.T) {
	p := newTestProfile()
	results := []*resourceapi.DeviceRequestAllocationResult{{Request: "a", Device: "subnet-blue"}, {Request: "b", Device: "subnet-blue"}}

	edits, err := p.ApplyConfig(nil, results)
	require.NoError(t, err)
	require.Len(t, edits, 2, "one entry per result, even on a shared device")
	assert.ElementsMatch(t, []string{"KUBE_OVN_NIC_NET1_SUBNET=blue", "KUBE_OVN_NIC_NET1_DEVICE=subnet-blue"},
		edits["a/subnet-blue"].Env)

	edits, err = p.ApplyConfig(&configapi.NicConfig{InterfaceName: "eth-9"}, results[1:])
	require.NoError(t, err)
	assert.Contains(t, edits["b/subnet-blue"].Env, "KUBE_OVN_NIC_ETH_9_SUBNET=blue")

	_, err = p.ApplyConfig(&runtime.Unknown{}, results)
	assert.Error(t, err)
}

func TestValidate(t *testing.T) {
	p := newTestProfile()
	assert.NoError(t, p.Validate(&configapi.NicConfig{InterfaceName: "net2"}))
	assert.Error(t, p.Validate(&configapi.NicConfig{InterfaceName: "much-too-long-name"}))
	assert.Error(t, p.Validate(&runtime.Unknown{}))
}

func TestSchemeBuilder(t *testing.T) {
	scheme := runtime.NewScheme()
	sb := newTestProfile().SchemeBuilder()
	require.NoError(t, sb.AddToScheme(scheme))
	assert.True(t, scheme.Recognizes(configapi.SchemeGroupVersion.WithKind(configapi.NicConfigKind)))
}

func TestSubnetNameFromDevice(t *testing.T) {
	assert.Equal(t, "blue", subnetNameFromDevice("subnet-blue"))
	assert.Equal(t, "subnet-", subnetNameFromDevice("subnet-"))
	assert.Equal(t, "other", subnetNameFromDevice("other"))
}
