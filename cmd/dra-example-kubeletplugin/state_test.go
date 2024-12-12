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

package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"
)

func TestPreparedDevicesGetDevices(t *testing.T) {
	tests := []struct {
		name            string
		preparedDevices PreparedDevices
		expected        []*drapbv1.Device
	}{
		{
			name:            "nil PreparedDevices",
			preparedDevices: nil,
			expected:        nil,
		},
		{
			name: "several PreparedDevices",
			preparedDevices: PreparedDevices{
				{Device: drapbv1.Device{DeviceName: "dev1"}},
				{Device: drapbv1.Device{DeviceName: "dev2"}},
				{Device: drapbv1.Device{DeviceName: "dev3"}},
			},
			expected: []*drapbv1.Device{
				{DeviceName: "dev1"},
				{DeviceName: "dev2"},
				{DeviceName: "dev3"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			devices := test.preparedDevices.GetDevices()
			if diff := cmp.Diff(devices, test.expected); diff != "" {
				t.Error("expected devices to be equal:\n", diff)
			}
		})
	}
}
