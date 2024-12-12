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

func TestGpuSharingGetTimeSlicingConfig(t *testing.T) {
	tests := []struct {
		name        string
		gpuSharing  *GpuSharing
		expected    *TimeSlicingConfig
		expectedErr string
	}{
		{
			name:        "nil GpuSharing",
			gpuSharing:  nil,
			expectedErr: "no sharing set to get config from",
		},
		{
			name: "strategy is not TimeSlicing",
			gpuSharing: &GpuSharing{
				Strategy: "not" + TimeSlicingStrategy,
			},
			expectedErr: "strategy is not set to 'TimeSlicing'",
		},
		{
			name: "non-nil SpacePartitioningConfig",
			gpuSharing: &GpuSharing{
				Strategy:                TimeSlicingStrategy,
				SpacePartitioningConfig: &SpacePartitioningConfig{},
			},
			expectedErr: "cannot use SpacePartitioningConfig with the 'TimeSlicing' strategy",
		},
		{
			name: "valid TimeSlicingConfig",
			gpuSharing: &GpuSharing{
				Strategy: TimeSlicingStrategy,
				TimeSlicingConfig: &TimeSlicingConfig{
					Interval: LongTimeSlice,
				},
			},
			expected: &TimeSlicingConfig{
				Interval: LongTimeSlice,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			timeSlicing, err := test.gpuSharing.GetTimeSlicingConfig()
			if test.expectedErr != "" {
				if err == nil {
					t.Errorf("expected error %q, but got no error", test.expectedErr)
				} else if err.Error() != test.expectedErr {
					t.Errorf("expected error %q, but got %v", test.expectedErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected GetTimeSlicingConfig to succeed, got error %v", err)
				}
			}
			if diff := cmp.Diff(timeSlicing, test.expected); diff != "" {
				t.Error("expected time slicing configs to be equal:\n", diff)
			}
		})
	}

}
func TestGpuSharingGetSpacePartitioningConfig(t *testing.T) {
	tests := []struct {
		name        string
		gpuSharing  *GpuSharing
		expected    *SpacePartitioningConfig
		expectedErr string
	}{
		{
			name:        "nil GpuSharing",
			gpuSharing:  nil,
			expectedErr: "no sharing set to get config from",
		},
		{
			name: "strategy is not SpacePartitioning",
			gpuSharing: &GpuSharing{
				Strategy: "not" + SpacePartitioningStrategy,
			},
			expectedErr: "strategy is not set to 'SpacePartitioning'",
		},
		{
			name: "non-nil TimeSlicingConfig",
			gpuSharing: &GpuSharing{
				Strategy:          SpacePartitioningStrategy,
				TimeSlicingConfig: &TimeSlicingConfig{},
			},
			expectedErr: "cannot use TimeSlicingConfig with the 'SpacePartitioning' strategy",
		},
		{
			name: "valid SpacePartitioningConfig",
			gpuSharing: &GpuSharing{
				Strategy: SpacePartitioningStrategy,
				SpacePartitioningConfig: &SpacePartitioningConfig{
					PartitionCount: 5,
				},
			},
			expected: &SpacePartitioningConfig{
				PartitionCount: 5,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spacePartitioning, err := test.gpuSharing.GetSpacePartitioningConfig()
			if test.expectedErr != "" {
				if err == nil {
					t.Errorf("expected error %q, but got no error", test.expectedErr)
				} else if err.Error() != test.expectedErr {
					t.Errorf("expected error %q, but got %v", test.expectedErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected GetSpacePartitioningConfig to succeed, got error %v", err)
				}
			}
			if diff := cmp.Diff(spacePartitioning, test.expected); diff != "" {
				t.Error("expected space partitioning configs to be equal:\n", diff)
			}
		})
	}
}
