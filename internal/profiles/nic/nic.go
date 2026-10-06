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

// Package nic implements a DRA device profile that exposes kube-ovn Subnets
// as allocatable NIC devices. Each Subnet becomes one device in the
// ResourceSlice for the node. kube-ovn-controller allocates a claimed device's
// address from the Subnet named by its subnetName attribute; the driver then
// plumbs the NIC into the pod.
package nic

import (
	"context"
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	configapi "github.com/soer3n/kube-ovn-dra-driver/api/kube-ovn.io/resource/nic/v1alpha1"
	"github.com/soer3n/kube-ovn-dra-driver/internal/profiles"
)

const ProfileName = "nic"

// kube-ovn Subnet GVR.
var subnetGVR = schema.GroupVersionResource{
	Group:    "kubeovn.io",
	Version:  "v1",
	Resource: "subnets",
}

// kube-ovn Vlan GVR. A Subnet references a Vlan by name via spec.vlan; the Vlan
// holds the 802.1q id (spec.id) and the ProviderNetwork name (spec.provider).
var vlanGVR = schema.GroupVersionResource{
	Group:    "kubeovn.io",
	Version:  "v1",
	Resource: "vlans",
}

// Profile implements profiles.Profile for kube-ovn virtual NICs.
type Profile struct {
	nodeName      string
	dynamicClient dynamic.Interface
}

// NewProfile creates a NIC profile. dynamicClient is used to list kube-ovn
// Subnet CRs at enumeration time.
func NewProfile(nodeName string, dynamicClient dynamic.Interface) Profile {
	return Profile{
		nodeName:      nodeName,
		dynamicClient: dynamicClient,
	}
}

// EnumerateDevices implements profiles.Profile.
// It lists all kube-ovn Subnets and exposes each one as a DRA device.
func (p Profile) EnumerateDevices() (resourceslice.DriverResources, error) {
	ctx := context.Background()
	subnets, err := p.dynamicClient.Resource(subnetGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return resourceslice.DriverResources{}, fmt.Errorf("list kube-ovn subnets: %w", err)
	}

	var devices []resourceapi.Device
	for _, subnet := range subnets.Items {
		spec, _ := subnet.Object["spec"].(map[string]interface{})
		if spec == nil {
			continue
		}

		name := subnet.GetName()
		subnetType := "ovn" // default: OVN overlay
		vlanID := int64(0)
		provider := ""
		providerNetwork := ""
		vpc := ""

		if v, ok := spec["provider"].(string); ok {
			provider = v
		}
		if v, ok := spec["vpc"].(string); ok {
			vpc = v
		}

		// A VLAN underlay subnet does not carry the id on the Subnet itself; it
		// references a Vlan CR via spec.vlan. Resolve that Vlan so claims can
		// select underlay subnets by type, VLAN id or ProviderNetwork.
		if vlanName, _ := spec["vlan"].(string); vlanName != "" {
			vlan, err := p.dynamicClient.Resource(vlanGVR).Get(ctx, vlanName, metav1.GetOptions{})
			if err != nil {
				return resourceslice.DriverResources{}, fmt.Errorf("get vlan %q referenced by subnet %q: %w", vlanName, name, err)
			}
			vspec, _ := vlan.Object["spec"].(map[string]interface{})
			if id, _ := vspec["id"].(int64); id != 0 {
				subnetType = "vlan"
				vlanID = id
				if pn, _ := vspec["provider"].(string); pn != "" {
					providerNetwork = pn
				}
			}
		}

		attrs := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			"nic.kubeovn.io/subnetName": {StringValue: ptr.To(name)},
			"nic.kubeovn.io/subnetType": {StringValue: ptr.To(subnetType)},
		}
		if vlanID != 0 {
			attrs["nic.kubeovn.io/vlanId"] = resourceapi.DeviceAttribute{IntValue: ptr.To(vlanID)}
		}
		if provider != "" {
			attrs["nic.kubeovn.io/provider"] = resourceapi.DeviceAttribute{StringValue: ptr.To(provider)}
		}
		if providerNetwork != "" {
			attrs["nic.kubeovn.io/providerNetwork"] = resourceapi.DeviceAttribute{StringValue: ptr.To(providerNetwork)}
		}
		if vpc != "" {
			attrs["nic.kubeovn.io/vpc"] = resourceapi.DeviceAttribute{StringValue: ptr.To(vpc)}
		}

		devices = append(devices, resourceapi.Device{
			Name:       fmt.Sprintf("subnet-%s", name),
			Attributes: attrs,
			// A kube-ovn Subnet is a shared IP pool, not an exclusive device:
			// many pods can each take an address from it. Allow the device to be
			// allocated to multiple claims (each pod still gets its own address
			// and logical switch port). Requires the DRAConsumableCapacity
			// feature gate on apiserver, scheduler and kubelet.
			AllowMultipleAllocations: ptr.To(true),
		})
	}

	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			p.nodeName: {
				Slices: []resourceslice.Slice{
					{Devices: devices},
				},
			},
		},
	}
	return resources, nil
}

// SchemeBuilder implements profiles.ConfigHandler.
func (p Profile) SchemeBuilder() runtime.SchemeBuilder {
	return runtime.NewSchemeBuilder(configapi.AddToScheme)
}

// Validate implements profiles.ConfigHandler.
func (p Profile) Validate(config runtime.Object) error {
	nicConfig, ok := config.(*configapi.NicConfig)
	if !ok {
		return fmt.Errorf("expected v1alpha1.NicConfig but got: %T", config)
	}
	return nicConfig.Validate()
}

// ApplyConfig implements profiles.ConfigHandler.
// For NIC devices, configuration is expressed as environment variables injected
// via CDI that tell the pod which interface name was assigned.
func (p Profile) ApplyConfig(config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	if config == nil {
		config = configapi.DefaultNicConfig()
	}
	nicConfig, ok := config.(*configapi.NicConfig)
	if !ok {
		return nil, fmt.Errorf("runtime object is not a recognized NicConfig")
	}
	if err := nicConfig.Normalize(); err != nil {
		return nil, fmt.Errorf("normalizing NicConfig: %w", err)
	}

	perDeviceEdits := make(profiles.PerDeviceCDIContainerEdits)
	for _, result := range results {
		envs := []string{
			fmt.Sprintf("KUBE_OVN_NIC_IFACE_%s=%s", result.Device, nicConfig.InterfaceName),
			fmt.Sprintf("KUBE_OVN_NIC_SUBNET_%s=%s", result.Device, subnetNameFromDevice(result.Device)),
		}
		edits := &cdispec.ContainerEdits{Env: envs}
		perDeviceEdits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: edits}
	}
	return perDeviceEdits, nil
}

// subnetNameFromDevice strips the "subnet-" prefix from the device name to
// recover the original kube-ovn Subnet name.
func subnetNameFromDevice(deviceName string) string {
	const prefix = "subnet-"
	if len(deviceName) > len(prefix) && deviceName[:len(prefix)] == prefix {
		return deviceName[len(prefix):]
	}
	return deviceName
}
