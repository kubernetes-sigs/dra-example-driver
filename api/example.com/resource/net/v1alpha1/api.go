/*
 * Copyright 2023 The Kubernetes Authors.
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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const NetConfigKind = "NetConfig"

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NetConfig holds the set of parameters for configuring a network device.
type NetConfig struct {
	metav1.TypeMeta `json:",inline"`
	BandwidthBurst  *BandwidthBurstEntry `json:"bwBurst,omitempty"`
}

// DefaultNetConfig provides the default bandwidth configuration.
func DefaultNetConfig() *NetConfig {
	return &NetConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupName + "/" + Version,
			Kind:       NetConfigKind,
		},
		BandwidthBurst: &BandwidthBurstEntry{}, // no limit
	}
}

// Normalize updates a NetConfig config with implied default values based on other settings.
func (c *NetConfig) Normalize() error {
	if c == nil {
		return fmt.Errorf("config is 'nil'")
	}
	if c.BandwidthBurst == nil {
		c.BandwidthBurst = &BandwidthBurstEntry{}
	}
	return nil
}

// BandwidthBurstEntry defines burst rate in bits for traffic through container. 0 for no limit.
type BandwidthBurstEntry struct {
	IngressBurst uint64 `json:"ingressBurst"`
	EgressBurst  uint64 `json:"egressBurst"`
}
