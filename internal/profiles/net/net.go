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
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	configapi "sigs.k8s.io/dra-example-driver/api/example.com/resource/net/v1alpha1"
	"sigs.k8s.io/dra-example-driver/internal/profiles"
	"sigs.k8s.io/dra-example-driver/internal/profiles/helpers"
)

const ProfileName = "net"

type Profile struct {
	nodeName string
	numNets  int
}

func NewProfile(nodeName string, numNets int) Profile {
	return Profile{
		nodeName: nodeName,
		numNets:  numNets,
	}
}

func (p Profile) EnumerateDevices() (resourceslice.DriverResources, error) {
	seed := p.nodeName
	uuids := helpers.GenerateUUIDs(seed, "net", p.numNets)

	// bandwidth capacity commonly defined for both ingress and egress
	bandwidthCapacity := resourceapi.DeviceCapacity{
		Value: resource.MustParse("100Gi"),
		RequestPolicy: &resourceapi.CapacityRequestPolicy{
			Default: ptr.To(resource.MustParse("1Gi")), // equal division 100Gi / 100
			ValidRange: &resourceapi.CapacityRequestPolicyRange{
				Min:  ptr.To(resource.MustParse("100Mi")), // prevent zero-consuming
				Max:  ptr.To(resource.MustParse("100Gi")), // optional
				Step: ptr.To(resource.MustParse("1Mi")),
			},
		},
	}

	var devices []resourceapi.Device
	for i, uuid := range uuids {
		device := resourceapi.Device{
			Name: fmt.Sprintf("nic-%d", i),
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"index": {
					IntValue: ptr.To(int64(i)),
				},
				"uuid": {
					StringValue: ptr.To(uuid),
				},
				"model": {
					StringValue: ptr.To("LATEST-NET-MODEL"),
				},
				"driverVersion": {
					VersionValue: ptr.To("1.0.0"),
				},
			},
			Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
				"vfs": {
					Value: resource.MustParse("100"),
					RequestPolicy: &resourceapi.CapacityRequestPolicy{
						Default: ptr.To(resource.MustParse("1")),
						ValidValues: []resource.Quantity{
							resource.MustParse("1"), // always consume 1
						},
					},
				},
				"ingressBandwidth": bandwidthCapacity,
				"egressBandwidth":  bandwidthCapacity,
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

// SchemeBuilder implements [profiles.ConfigHandler].
func (p Profile) SchemeBuilder() runtime.SchemeBuilder {
	return runtime.NewSchemeBuilder(
		configapi.AddToScheme,
	)
}

// Validate implements [profiles.ConfigHandler].
func (p Profile) Validate(config runtime.Object) error {
	netConfig, ok := config.(*configapi.NetConfig)
	if !ok {
		return fmt.Errorf("expected v1alpha1.NetConfig but got: %T", config)
	}
	return netConfig.Validate()
}

// ApplyConfig implements [profiles.ConfigHandler].
func (p Profile) ApplyConfig(config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	if config == nil {
		config = configapi.DefaultNetConfig()
	}
	if config, ok := config.(*configapi.NetConfig); ok {
		return applyNetConfig(config, results)
	}
	return nil, fmt.Errorf("runtime object is not a recognized configuration")
}

// In this example driver there is no actual configuration applied. We simply
// define a set of environment variables to be injected into the containers
// that include a given device. A real driver would likely need to do some sort
// of hardware configuration as well, based on the config passed in.
func applyNetConfig(config *configapi.NetConfig, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	perDeviceEdits := make(profiles.PerDeviceCDIContainerEdits)

	// Normalize the config to set any implied defaults.
	if err := config.Normalize(); err != nil {
		return nil, fmt.Errorf("error normalizing Net config: %w", err)
	}

	// Validate the config to ensure its integrity.
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("error validating Net config: %w", err)
	}

	for _, result := range results {
		envs := []string{
			fmt.Sprintf("NET_DEVICE_%s=%s", result.Device[4:], result.Device),
		}

		if config.BandwidthBurst != nil {
			if config.BandwidthBurst.IngressBurst > 0 {
				envs = append(envs, fmt.Sprintf("NET_DEVICE_%s_INGRESS_BURST=%d", result.Device[4:], config.BandwidthBurst.IngressBurst))
			}
			if config.BandwidthBurst.EgressBurst > 0 {
				envs = append(envs, fmt.Sprintf("NET_DEVICE_%s_EGRESS_BURST=%d", result.Device[4:], config.BandwidthBurst.EgressBurst))
			}
		}
		if ingressRate, found := result.ConsumedCapacity["ingressBandwidth"]; found {
			envs = append(envs, fmt.Sprintf("NET_DEVICE_%s_INGRESS_RATE=%d", result.Device[4:], ingressRate.AsDec()))
		}
		if egressRate, found := result.ConsumedCapacity["egressBandwidth"]; found {
			envs = append(envs, fmt.Sprintf("NET_DEVICE_%s_EGRESS_RATE=%d", result.Device[4:], egressRate.AsDec()))
		}

		edits := &cdispec.ContainerEdits{
			Env: envs,
		}

		perDeviceEdits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: edits}
	}

	return perDeviceEdits, nil
}
