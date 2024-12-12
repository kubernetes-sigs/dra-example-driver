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
	"testing"
)

func TestGpuConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		gpuConfig *GpuConfig
		expected  string
	}{
		{
			name:      "empty GpuConfig",
			gpuConfig: &GpuConfig{},
			expected:  "no sharing strategy set",
		},
		{
			name: "empty GpuConfig.Sharing",
			gpuConfig: &GpuConfig{
				Sharing: &GpuSharing{},
			},
			expected: "unknown GPU sharing strategy: ",
		},
		{
			name: "empty GpuConfig.Sharing.TimeSlicingConfig",
			gpuConfig: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy:          TimeSlicingStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{},
				},
			},
			expected: "unknown time-slice interval: ",
		},
		{
			name: "valid GpuConfig with TimeSlicing",
			gpuConfig: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: TimeSlicingStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: MediumTimeSlice,
					},
				},
			},
			expected: "",
		},
		{
			name: "negative GpuConfig.Sharing.SpacePartitioningConfig.PartitionCount",
			gpuConfig: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: SpacePartitioningStrategy,
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: -1,
					},
				},
			},
			expected: "invalid partition count: -1",
		},
		{
			name: "valid GpuConfig with SpacePartitioning",
			gpuConfig: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: SpacePartitioningStrategy,
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: 1000,
					},
				},
			},
			expected: "",
		},
		{
			name:      "default GpuConfig",
			gpuConfig: DefaultGpuConfig(),
			expected:  "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.gpuConfig.Validate()
			if test.expected != "" {
				if err == nil {
					t.Errorf("expected error %q, but got no error", test.expected)
				} else if err.Error() != test.expected {
					t.Errorf("expected error %q, but got %v", test.expected, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected Validate to succeed, got error %v", err)
				}
			}
		})
	}
}
