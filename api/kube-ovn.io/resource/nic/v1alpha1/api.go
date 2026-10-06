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

package v1alpha1

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const NicConfigKind = "NicConfig"

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NicConfig holds the set of parameters for configuring a kube-ovn virtual NIC.
type NicConfig struct {
	metav1.TypeMeta `json:",inline"`

	// InterfaceName is the name to use for the interface inside the pod network namespace.
	// Defaults to "net1".
	// +optional
	InterfaceName string `json:"interfaceName,omitempty"`
}

// DefaultNicConfig returns the default NIC configuration.
func DefaultNicConfig() *NicConfig {
	return &NicConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupName + "/" + Version,
			Kind:       NicConfigKind,
		},
		InterfaceName: "net1",
	}
}

// Normalize sets implied defaults.
func (c *NicConfig) Normalize() error {
	if c.InterfaceName == "" {
		c.InterfaceName = "net1"
	}
	return nil
}

// maxInterfaceNameLength is the kernel's IFNAMSIZ minus the terminating NUL.
const maxInterfaceNameLength = 15

// Validate checks the configuration. An empty InterfaceName is valid and
// defaults to "net1".
func (c *NicConfig) Validate() error {
	name := c.InterfaceName
	switch {
	case name == "":
		return nil
	case len(name) > maxInterfaceNameLength:
		return fmt.Errorf("interfaceName %q is longer than %d characters", name, maxInterfaceNameLength)
	case name == "." || name == "..":
		return fmt.Errorf("interfaceName %q is not a valid interface name", name)
	case strings.ContainsAny(name, "/: \t\n"):
		return fmt.Errorf("interfaceName %q must not contain '/', ':' or whitespace", name)
	}
	return nil
}
