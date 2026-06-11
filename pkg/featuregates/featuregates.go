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

package featuregates

import (
	"sync"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/component-base/featuregate"
	logsapi "k8s.io/component-base/logs/api/v1"
)

// featureGateEmulationVersion must use Kubernetes-style versions (major.minor
// matching the vendored k8s.io/* release), not driver SemVer. Driver-local
// gates use 0.x Version fields and remain visible because 0.x < 1.y.
var featureGateEmulationVersion = version.MajorMinor(1, 36)

const (
	// VFIOGPUEnabled enables PCI passthrough device discovery and preparation via
	// the vfio-gpu profile. When enabled, the driver selects that profile
	// instead of the default gpu profile.
	VFIOGPUEnabled featuregate.Feature = "VFIOGPUEnabled"
)

var defaultFeatureGates = map[featuregate.Feature]featuregate.VersionedSpecs{
	VFIOGPUEnabled: {
		{
			Default:    false,
			PreRelease: featuregate.Alpha,
			Version:    version.MajorMinor(0, 1),
		},
	},
}

var (
	featureGatesOnce sync.Once
	featureGates     featuregate.MutableVersionedFeatureGate
)

// FeatureGates returns the process-wide feature gate set, including logging
// gates from component-base and driver-specific gates.
func FeatureGates() featuregate.MutableVersionedFeatureGate {
	if featureGates == nil {
		featureGatesOnce.Do(func() {
			featureGates = newFeatureGates(featureGateEmulationVersion)
		})
	}
	return featureGates
}

func newFeatureGates(v *version.Version) featuregate.MutableVersionedFeatureGate {
	fg := featuregate.NewVersionedFeatureGate(v)
	utilruntime.Must(logsapi.AddFeatureGates(fg))
	utilruntime.Must(fg.AddVersioned(defaultFeatureGates))
	utilruntime.Must(fg.SetFromMap(map[string]bool{
		string(logsapi.ContextualLogging): true,
	}))
	return fg
}

// Enabled reports whether feature is enabled on the process-wide gate set.
func Enabled(feature featuregate.Feature) bool {
	return FeatureGates().Enabled(feature)
}

const (
	profileGPU     = "gpu"
	profileVFIOGPU = "vfio-gpu"
)

// DeviceProfile returns the active device profile. VFIOGPUEnabled selects vfio-gpu;
// otherwise the default gpu profile is used.
func DeviceProfile() string {
	if Enabled(VFIOGPUEnabled) {
		return profileVFIOGPU
	}
	return profileGPU
}
