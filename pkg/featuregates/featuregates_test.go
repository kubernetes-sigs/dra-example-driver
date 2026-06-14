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
	"testing"

	"github.com/stretchr/testify/assert"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/component-base/featuregate"
)

// test-only feature gates used to exercise the feature gate machinery without
// coupling tests to production gate defaults.
const (
	testAlpha featuregate.Feature = "TestAlpha"
	testBeta  featuregate.Feature = "TestBeta"
	testGA    featuregate.Feature = "TestGA"
)

var testFeatureGates = map[featuregate.Feature]featuregate.VersionedSpecs{
	testAlpha: {
		{
			Default:    false,
			PreRelease: featuregate.Alpha,
			Version:    version.MajorMinor(0, 1),
		},
	},
	testBeta: {
		{
			Default:    true,
			PreRelease: featuregate.Beta,
			Version:    version.MajorMinor(0, 1),
		},
	},
	testGA: {
		{
			Default:    true,
			PreRelease: featuregate.GA,
			Version:    version.MajorMinor(0, 2),
		},
	},
}

func newTestFeatureGates(v *version.Version) featuregate.MutableVersionedFeatureGate {
	fg := featuregate.NewVersionedFeatureGate(v)
	utilruntime.Must(fg.AddVersioned(testFeatureGates))
	return fg
}

func withFeatureGates(t *testing.T, enabled map[string]bool) {
	t.Helper()
	withCustomFeatureGates(t, newFeatureGates(version.MajorMinor(1, 36)), enabled)
}

func withTestFeatureGates(t *testing.T, enabled map[string]bool) {
	t.Helper()
	withCustomFeatureGates(t, newTestFeatureGates(version.MajorMinor(1, 36)), enabled)
}

func withCustomFeatureGates(t *testing.T, fg featuregate.MutableVersionedFeatureGate, enabled map[string]bool) {
	t.Helper()
	if len(enabled) > 0 {
		utilruntime.Must(fg.SetFromMap(enabled))
	}
	featureGates = fg
	t.Cleanup(func() {
		featureGates = nil
		featureGatesOnce = sync.Once{}
	})
}

func TestTestFeatureGateDefaults(t *testing.T) {
	fg := newTestFeatureGates(version.MajorMinor(1, 36))

	assert.False(t, fg.Enabled(testAlpha))
	assert.True(t, fg.Enabled(testBeta))
	assert.True(t, fg.Enabled(testGA))
}

func TestEnabledWithTestFeatures(t *testing.T) {
	withTestFeatureGates(t, map[string]bool{
		string(testAlpha): true,
		string(testBeta):  false,
	})

	assert.True(t, Enabled(testAlpha))
	assert.False(t, Enabled(testBeta))
	assert.True(t, Enabled(testGA))
}

func TestDeviceProfile_DefaultGPU(t *testing.T) {
	withFeatureGates(t, nil)
	assert.Equal(t, "gpu", DeviceProfile())
}

func TestDeviceProfile_VFIOGPUEnabledGate(t *testing.T) {
	withFeatureGates(t, map[string]bool{string(VFIOGPUEnabled): true})
	assert.Equal(t, "vfio-gpu", DeviceProfile())
}
