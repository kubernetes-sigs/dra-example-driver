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

package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/deviceattribute"
)

func TestParsePCIeRoots(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		assert.Nil(t, ParsePCIeRoots(""))
	})

	t.Run("single root", func(t *testing.T) {
		assert.Equal(t, []string{"pci0000:00"}, ParsePCIeRoots("pci0000:00"))
	})

	t.Run("comma separated with spaces", func(t *testing.T) {
		assert.Equal(t, []string{"pci0000:00", "pci0000:80"}, ParsePCIeRoots("pci0000:00, pci0000:80"))
	})

	t.Run("skips empty parts", func(t *testing.T) {
		assert.Equal(t, []string{"pci0000:00", "pci0000:80"}, ParsePCIeRoots("pci0000:00,,pci0000:80"))
	})
}

func TestResolvePCIeRoots(t *testing.T) {
	t.Run("returns configured roots", func(t *testing.T) {
		configured := []string{"pci0000:40", "pci0000:c0"}
		assert.Equal(t, configured, ResolvePCIeRoots(configured))
	})

	t.Run("returns defaults when empty", func(t *testing.T) {
		assert.Equal(t, DefaultPCIeRoots, ResolvePCIeRoots(nil))
		assert.Equal(t, DefaultPCIeRoots, ResolvePCIeRoots([]string{}))
	})
}

func TestTopologyAttributesForDeviceIndex(t *testing.T) {
	roots := []string{"pci0000:00", "pci0000:80"}

	t.Run("nil when no roots", func(t *testing.T) {
		assert.Nil(t, TopologyAttributesForDeviceIndex(0, nil))
	})

	t.Run("round robin roots", func(t *testing.T) {
		attrs0 := TopologyAttributesForDeviceIndex(0, roots)
		require.NotNil(t, attrs0)
		assert.Equal(t, roots[0], *attrs0[deviceattribute.StandardDeviceAttributePCIeRoot].StringValue)

		attrs1 := TopologyAttributesForDeviceIndex(1, roots)
		require.NotNil(t, attrs1)
		assert.Equal(t, roots[1], *attrs1[deviceattribute.StandardDeviceAttributePCIeRoot].StringValue)

		attrs2 := TopologyAttributesForDeviceIndex(2, roots)
		require.NotNil(t, attrs2)
		assert.Equal(t, roots[0], *attrs2[deviceattribute.StandardDeviceAttributePCIeRoot].StringValue)
	})
}

func TestTopologyAttributesForDeviceIndex_ResourceAPIKeys(t *testing.T) {
	roots := []string{"pci0000:00"}
	attrs := TopologyAttributesForDeviceIndex(0, roots)
	require.Contains(t, attrs, resourceapi.QualifiedName(deviceattribute.StandardDeviceAttributePCIeRoot))
}
