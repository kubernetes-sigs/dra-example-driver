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

	"github.com/google/go-cmp/cmp"
)

func TestGpuConfigNormalize(t *testing.T) {
	tests := []struct {
		name        string
		gpuConfig   *GpuConfig
		expected    *GpuConfig
		expectedErr string
	}{
		{
			name:        "nil GpuConfig",
			gpuConfig:   nil,
			expectedErr: "config is 'nil'",
		},
		{
			name:      "empty GpuConfig",
			gpuConfig: &GpuConfig{},
			expected: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: TimeSlicingStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: DefaultTimeSlice,
					},
				},
			},
		},
		{
			name: "empty GpuConfig with SpacePartitioning",
			gpuConfig: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: SpacePartitioningStrategy,
				},
			},
			expected: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: SpacePartitioningStrategy,
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: 1,
					},
				},
			},
		},
		{
			name: "full GpuConfig",
			gpuConfig: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: SpacePartitioningStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: ShortTimeSlice,
					},
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: 5,
					},
				},
			},
			expected: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: SpacePartitioningStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: ShortTimeSlice,
					},
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: 5,
					},
				},
			},
		},
		{
			name:      "default GpuConfig",
			gpuConfig: DefaultGpuConfig(),
			expected:  DefaultGpuConfig(), // default should be fully normalized already
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.gpuConfig.Normalize()
			if test.expectedErr != "" {
				if err == nil {
					t.Errorf("expected error %q, but got no error", test.expectedErr)
				} else if err.Error() != test.expectedErr {
					t.Errorf("expected error %q, but got %v", test.expectedErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected Normalize to succeed, got error %v", err)
				}
			}
			if diff := cmp.Diff(test.gpuConfig, test.expected); diff != "" {
				t.Error("expected configs to be equal:\n", diff)
			}
		})
	}
}
