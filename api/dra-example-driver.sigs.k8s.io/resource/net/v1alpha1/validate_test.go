/*
 * Copyright 2024 The Kubernetes Authors.
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
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNetConfigValidate(t *testing.T) {
	tests := map[string]struct {
		netConfig *NetConfig
		expected  error
	}{
		"empty NetConfig": {
			netConfig: &NetConfig{},
			expected:  errors.New("no burst set"),
		},
		"default NetConfig": {
			netConfig: DefaultNetConfig(),
			expected:  nil,
		},
		"valid NetConfig with custom burst value": {
			netConfig: &NetConfig{
				BandwidthBurst: &BandwidthBurstEntry{
					IngressBurst: 8,
					EgressBurst:  8,
				},
			},
			expected: nil,
		},
		"invalid NetConfig with too-large ingress burst": {
			netConfig: &NetConfig{
				BandwidthBurst: &BandwidthBurstEntry{
					IngressBurst: uint64(math.MaxUint32) * 8,
				},
			},
			expected: errors.New("invalid ingressBurst: burst cannot be more than 4GB"),
		},
		"invalid NetConfig with too-large egress burst": {
			netConfig: &NetConfig{
				BandwidthBurst: &BandwidthBurstEntry{
					EgressBurst: uint64(math.MaxUint32) * 8,
				},
			},
			expected: errors.New("invalid egressBurst: burst cannot be more than 4GB"),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.netConfig.Validate()
			assert.Equal(t, test.expected, err)
		})
	}
}
