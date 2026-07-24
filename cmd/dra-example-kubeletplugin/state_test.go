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

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/dra-example-driver/internal/profiles/cpu"
)

var (
	testShareId = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
)

func TestPreparedDevicesGetDevices(t *testing.T) {
	tests := map[string]struct {
		preparedDevices PreparedDevices
		expected        []*drapbv1.Device
	}{
		"nil PreparedDevices": {
			preparedDevices: nil,
			expected:        nil,
		},
		"several PreparedDevices": {
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
		"preparedDevice with shareId": {
			preparedDevices: PreparedDevices{
				{Device: drapbv1.Device{DeviceName: "dev1", ShareId: &testShareId}},
				{Device: drapbv1.Device{DeviceName: "dev2", ShareId: &testShareId}},
			},
			expected: []*drapbv1.Device{
				{DeviceName: "dev1", ShareId: &testShareId},
				{DeviceName: "dev2", ShareId: &testShareId},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			devices := test.preparedDevices.GetDevices()
			assert.Equal(t, test.expected, devices)
		})
	}
}

// TestComputeDeviceConfigShareID verifies that the ShareID the scheduler
// assigns to each allocation result is carried through to the prepared device,
// which the kubelet plugin then forwards to the kubelet. This guards the
// consumable-capacity sharing path: two allocations of the same device must
// keep their distinct ShareIDs.
func TestComputeDeviceConfigShareID(t *testing.T) {
	const (
		nodeName   = "test-node"
		driverName = "cpu.example.com"
	)

	flags := &Flags{
		cdiRoot:         t.TempDir(),
		driverName:      driverName,
		profile:         "cpu",
		nodeName:        nodeName,
		cpuNUMANodes:    1,
		cpusPerNUMANode: 4,
	}
	state, err := NewDeviceState(&Config{
		flags:   flags,
		profile: cpu.NewProfile(nodeName, driverName, flags.cpuNUMANodes, flags.cpusPerNUMANode),
	})
	require.NoError(t, err)

	result := func(request string, shareID *types.UID) resourceapi.DeviceRequestAllocationResult {
		return resourceapi.DeviceRequestAllocationResult{
			Request: request,
			Driver:  driverName,
			Pool:    nodeName,
			Device:  "numa-0",
			ShareID: shareID,
		}
	}

	tests := map[string]struct {
		results          []resourceapi.DeviceRequestAllocationResult
		expectedShareIDs []*types.UID
		// expectedCDISuffixes are CDI device-ID suffixes that must each appear
		// on some prepared device, proving the ShareID is woven into the CDI
		// device name so shares of one device stay distinct.
		expectedCDISuffixes []string
	}{
		"no ShareID": {
			results:             []resourceapi.DeviceRequestAllocationResult{result("cpus", nil)},
			expectedShareIDs:    []*types.UID{nil},
			expectedCDISuffixes: []string{"claim-uid-numa-0"},
		},
		"distinct ShareIDs sharing one device": {
			results: []resourceapi.DeviceRequestAllocationResult{
				result("cpus-0", ptr.To(types.UID("share-0"))),
				result("cpus-1", ptr.To(types.UID("share-1"))),
			},
			expectedShareIDs:    []*types.UID{ptr.To(types.UID("share-0")), ptr.To(types.UID("share-1"))},
			expectedCDISuffixes: []string{"claim-uid-numa-0-share-0", "claim-uid-numa-0-share-1"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{UID: "claim-uid"},
				Status: resourceapi.ResourceClaimStatus{
					Allocation: &resourceapi.AllocationResult{
						Devices: resourceapi.DeviceAllocationResult{Results: test.results},
					},
				},
			}

			prepared, err := state.computeDeviceConfig(claim)
			require.NoError(t, err)
			require.Len(t, prepared, len(test.expectedShareIDs))

			var shareIDs []*types.UID
			var cdiDeviceIDs []string
			for _, device := range prepared {
				shareIDs = append(shareIDs, device.ShareID)
				cdiDeviceIDs = append(cdiDeviceIDs, device.CdiDeviceIds...)
			}
			assert.ElementsMatch(t, test.expectedShareIDs, shareIDs)

			for _, suffix := range test.expectedCDISuffixes {
				matched := slices.ContainsFunc(cdiDeviceIDs, func(id string) bool {
					return strings.HasSuffix(id, suffix)
				})
				assert.True(t, matched, "expected a CDI device ID ending with %q in %v", suffix, cdiDeviceIDs)
			}
		})
	}
}

// TestComputeDeviceConfigSharedDeviceContainerEdits verifies that when one
// device is allocated multiple times in a claim (consumable capacity), each
// share keeps its own container edits keyed by the share-aware device id rather
// than collapsing onto a single entry for the bare device name.
func TestComputeDeviceConfigSharedDeviceContainerEdits(t *testing.T) {
	const (
		nodeName   = "test-node"
		driverName = "cpu.example.com"
	)

	flags := &Flags{
		cdiRoot:         t.TempDir(),
		driverName:      driverName,
		profile:         "cpu",
		nodeName:        nodeName,
		cpuNUMANodes:    1,
		cpusPerNUMANode: 4,
	}
	state, err := NewDeviceState(&Config{
		flags:   flags,
		profile: cpu.NewProfile(nodeName, driverName, flags.cpuNUMANodes, flags.cpusPerNUMANode),
	})
	require.NoError(t, err)

	capacityKey := resourceapi.QualifiedName(driverName + "/cpu")
	result := func(request string, shareID types.UID, consumed string) resourceapi.DeviceRequestAllocationResult {
		return resourceapi.DeviceRequestAllocationResult{
			Request: request,
			Driver:  driverName,
			Pool:    nodeName,
			Device:  "numa-0",
			ShareID: ptr.To(shareID),
			ConsumedCapacity: map[resourceapi.QualifiedName]resource.Quantity{
				capacityKey: resource.MustParse(consumed),
			},
		}
	}

	claim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{UID: "claim-uid"},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: &resourceapi.AllocationResult{
				Devices: resourceapi.DeviceAllocationResult{Results: []resourceapi.DeviceRequestAllocationResult{
					result("cpu0", "share-0", "1"),
					result("cpu1", "share-1", "3"),
				}},
			},
		},
	}

	prepared, err := state.computeDeviceConfig(claim)
	require.NoError(t, err)
	require.Len(t, prepared, 2)

	consumedByShare := map[types.UID]string{}
	for _, device := range prepared {
		require.NotNil(t, device.ShareID)
		require.NotNil(t, device.ContainerEdits)
		require.NotNil(t, device.ContainerEdits.ContainerEdits)
		for _, env := range device.ContainerEdits.Env {
			if consumed, ok := strings.CutPrefix(env, "CPU_DEVICE_0_CONSUMED_CPU="); ok {
				consumedByShare[*device.ShareID] = consumed
			}
		}
	}
	assert.Equal(t, "1", consumedByShare["share-0"], "share-0 should keep its own consumed CPU edit")
	assert.Equal(t, "3", consumedByShare["share-1"], "share-1 should keep its own consumed CPU edit")
}

// TestUnprepareRecoversFromCorruptCheckpoint verifies that Unprepare succeeds
// without panicking when the on-disk checkpoint file is corrupt.
func TestUnprepareRecoversFromCorruptCheckpoint(t *testing.T) {
	const (
		nodeName   = "test-node"
		driverName = "cpu.example.com"
	)
	claimUID := types.UID("some-claim-uid")

	// t.TempDir() is used for both CDI files and the checkpoint directory so
	// the test stays fully self-contained and cleans up automatically.
	tmpDir := t.TempDir()

	flags := &Flags{
		cdiRoot:                     tmpDir,
		driverName:                  driverName,
		profile:                     "cpu",
		nodeName:                    nodeName,
		cpuNUMANodes:                1,
		cpusPerNUMANode:             4,
		kubeletPluginsDirectoryPath: tmpDir,
	}

	state, err := NewDeviceState(&Config{
		flags:   flags,
		profile: cpu.NewProfile(nodeName, driverName, flags.cpuNUMANodes, flags.cpusPerNUMANode),
	})
	require.NoError(t, err)

	// Pre-create the CDI spec file for the claim so we can later confirm that
	// Unprepare reaches DeleteClaimSpecFile and removes it. The spec file name
	// follows the CDI convention: <vendor>-<class>_<transientID>.yaml where
	// vendor = "k8s." + driverName.
	cdiSpecFile := filepath.Join(tmpDir, "k8s."+driverName+"-cpu_"+string(claimUID)+".yaml")
	require.NoError(t, os.WriteFile(cdiSpecFile, []byte("kind: k8s."+driverName+"/cpu\n"), 0600))

	// Write garbage bytes to the checkpoint file so that readCheckpoint returns
	// (nil, err) — the exact precondition that triggers the nil dereference.
	// The plugin directory must exist before writing (NewDeviceState does not
	// create it; that is done by RunPlugin at startup).
	pluginDir := filepath.Join(tmpDir, driverName)
	require.NoError(t, os.MkdirAll(pluginDir, 0750))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, DriverPluginCheckpointFile),
		[]byte("THIS IS NOT VALID CHECKPOINT JSON"),
		0600,
	))

	// Unprepare must succeed (not just not-panic) when the checkpoint is corrupt.
	require.NoError(t, state.Unprepare(claimUID))

	// The CDI spec file for the claim must have been removed, proving that
	// Unprepare reached DeleteClaimSpecFile rather than returning early.
	assert.NoFileExists(t, cdiSpecFile, "CDI spec file must be deleted after Unprepare succeeds")

	// The checkpoint file on disk must be valid (readable and empty).
	decoder, _, err := checkpointSerializer()
	require.NoError(t, err)
	checkpoint, err := readCheckpoint(state.checkpointPath, decoder)
	require.NoError(t, err, "checkpoint file must be valid after Unprepare recovers from corruption")
	assert.Empty(t, checkpoint.PreparedClaims, "recovered checkpoint must have no prepared claims")
}
