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

package profiles

import (
	"errors"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
)

type PerDeviceCDIContainerEdits map[string]*cdiapi.ContainerEdits

type PreparedDevice struct {
	drapbv1.Device
	ContainerEdits *cdiapi.ContainerEdits
}

type PreparedDevices []*PreparedDevice

func (pds PreparedDevices) GetDevices() []*drapbv1.Device {
	var devices []*drapbv1.Device
	for _, pd := range pds {
		devices = append(devices, &pd.Device)
	}
	return devices
}

// Profile describes a kind of device that can be managed by the driver.
type Profile interface {
	ConfigHandler
	EnumerateDevices() (resourceslice.DriverResources, error)
}

// ConfigHandler handles opaque configuration set for requests in ResourceClaims.
type ConfigHandler interface {
	// SchemeBuilder produces a [runtime.Scheme] for the profile's configuration types.
	SchemeBuilder() runtime.SchemeBuilder
	// Validate returns nil for valid configuration, or an error explaining why the configuration is invalid.
	Validate(config runtime.Object) error
	// ApplyConfig applies a configuration to a set of device allocation
	// results. When `config` is nil, the profile's default configuration should
	// be applied.
	ApplyConfig(config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (PerDeviceCDIContainerEdits, error)
}

// NoopConfigHandler implements a [ConfigHandler] that does not allow
// configuration.
type NoopConfigHandler struct{}

// ApplyConfig implements [ConfigHandler].
func (n NoopConfigHandler) ApplyConfig(config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (PerDeviceCDIContainerEdits, error) {
	if config != nil {
		return nil, errors.New("configuration not allowed")
	}
	return nil, nil
}

// SchemeBuilder implements [ConfigHandler].
func (n NoopConfigHandler) SchemeBuilder() runtime.SchemeBuilder {
	return runtime.NewSchemeBuilder()
}

// Validate implements [ConfigHandler].
func (n NoopConfigHandler) Validate(config runtime.Object) error {
	return errors.New("configuration not allowed")
}
