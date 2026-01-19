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

package v1alpha1

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGpuConfigNormalize(t *testing.T) {
	tests := map[string]struct {
		netConfig   *NetConfig
		expected    *NetConfig
		expectedErr error
	}{
		"nil NetConfig": {
			netConfig:   nil,
			expectedErr: errors.New("config is 'nil'"),
		},
		"empty GpuConfig": {
			netConfig: &NetConfig{},
			expected: &NetConfig{
				BandwidthBurst: &BandwidthBurstEntry{},
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.netConfig.Normalize()
			assert.Equal(t, test.expected, test.netConfig)
			assert.Equal(t, test.expectedErr, err)
		})
	}
}
