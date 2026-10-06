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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"
)

func TestPreparedDevicesGetDevices(t *testing.T) {
	if got := PreparedDevices(nil).GetDevices(); got != nil {
		t.Errorf("GetDevices() of nil = %v, want nil", got)
	}
	devices := PreparedDevices{
		{Device: drapbv1.Device{DeviceName: "dev1"}},
		{Device: drapbv1.Device{DeviceName: "dev2"}},
	}.GetDevices()
	if len(devices) != 2 || devices[0].DeviceName != "dev1" || devices[1].DeviceName != "dev2" {
		t.Errorf("GetDevices() = %v, want dev1 and dev2 in order", devices)
	}
}

func TestNoopConfigHandler(t *testing.T) {
	var h NoopConfigHandler
	if edits, err := h.ApplyConfig(nil, nil); err != nil || edits != nil {
		t.Errorf("ApplyConfig(nil) = %v, %v; want no edits and no error", edits, err)
	}
	if _, err := h.ApplyConfig(&runtime.Unknown{}, nil); err == nil {
		t.Error("ApplyConfig(config) must reject configuration")
	}
	if err := h.Validate(&runtime.Unknown{}); err == nil {
		t.Error("Validate() must reject configuration")
	}
	sb := h.SchemeBuilder()
	if err := sb.AddToScheme(runtime.NewScheme()); err != nil {
		t.Errorf("SchemeBuilder().AddToScheme() error = %v", err)
	}
}
