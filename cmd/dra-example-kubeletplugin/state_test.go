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
	"context"
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

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

// TestUnprepareReturnsErrorOnUnreadableCheckpoint verifies that Unprepare returns an error
// (rather than panicking) when checkpoint.json exists but cannot be decoded.
// The fix returns the error immediately without touching the checkpoint, letting
// the kubelet retry once the operator has corrected the file.
func TestUnprepareReturnsErrorOnUnreadableCheckpoint(t *testing.T) {
	const (
		nodeName   = "test-node"
		driverName = "cpu.example.com"
		claimUID   = "some-claim-uid"
	)

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

	// The plugin directory must exist before writing files (NewDeviceState does
	// not create it; that is done by RunPlugin at startup).
	pluginDir := filepath.Join(tmpDir, driverName)
	require.NoError(t, os.MkdirAll(pluginDir, 0750))

	// create a CDI spec file for the claim, simulating a claim that was fully
	// prepared before the checkpoint was corrupted. After Unprepare returns an
	// error the file must still be present — nothing should have been released.
	cdiSpecPath := filepath.Join(tmpDir, "k8s.cpu.example.com-cpu-"+claimUID+"-0.yaml")
	require.NoError(t, os.WriteFile(cdiSpecPath, []byte("placeholder"), 0600))

	// Write garbage bytes to the checkpoint file so that readCheckpoint returns
	// (nil, err) — the exact precondition that triggered the nil dereference.
	corruptData := []byte("THIS IS NOT VALID CHECKPOINT JSON")
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, DriverPluginCheckpointFile),
		corruptData,
		0600,
	))

	// 1. Unprepare must return an error, not panic.
	err = state.Unprepare(types.UID(claimUID))
	require.Error(t, err, "Unprepare must return an error on an unreadable checkpoint")
	assert.Contains(t, err.Error(), "unable to read checkpoint")

	// 2. The corrupt file must be left exactly as it was — the driver must not
	// overwrite it, preserving evidence and avoiding silent data loss.
	data, readErr := os.ReadFile(state.checkpointPath)
	require.NoError(t, readErr)
	assert.Equal(t, corruptData, data, "corrupt checkpoint file must not be modified by Unprepare")

	// 3. The CDI spec file for the claim must still be present — Unprepare
	// must not release anything when it cannot read the checkpoint.
	_, statErr := os.Stat(cdiSpecPath)
	assert.NoError(t, statErr, "CDI spec file must still be present after Unprepare returns an error")
}

func TestPrepareRestoredClaimRecreatesMissingClaimSpec(t *testing.T) {
	const (
		nodeName   = "test-node"
		driverName = "cpu.example.com"
	)

	root := t.TempDir()
	state := newTestCPUDeviceState(t, root, nodeName, driverName)
	claim := testCPUClaim(driverName, nodeName)

	prepared, err := state.Prepare(context.Background(), claim)
	require.NoError(t, err)
	require.NotEmpty(t, prepared)
	assertClaimSpecResolvesPreparedDevices(t, state, claim.UID, prepared)

	require.NoError(t, os.Remove(claimSpecPath(state, claim.UID)))

	restartedState := newTestCPUDeviceState(t, root, nodeName, driverName)
	restored, err := restartedState.Prepare(context.Background(), claim)
	require.NoError(t, err)
	require.NotEmpty(t, restored)

	assert.Equal(t, prepared.GetDevices(), restored.GetDevices())
	assertClaimSpecResolvesPreparedDevices(t, restartedState, claim.UID, restored)

	checkpoint, err := readCheckpoint(restartedState.checkpointPath, restartedState.checkpointDecoder)
	require.NoError(t, err)
	require.Len(t, checkpoint.PreparedClaims, 1)
	assert.Equal(t, claim.UID, checkpoint.PreparedClaims[0].UID)
}

func TestPrepareRestoredClaimIsIdempotentWhenClaimSpecExists(t *testing.T) {
	const (
		nodeName   = "test-node"
		driverName = "cpu.example.com"
	)

	root := t.TempDir()
	state := newTestCPUDeviceState(t, root, nodeName, driverName)
	claim := testCPUClaim(driverName, nodeName)

	prepared, err := state.Prepare(context.Background(), claim)
	require.NoError(t, err)
	require.NotEmpty(t, prepared)
	assertClaimSpecResolvesPreparedDevices(t, state, claim.UID, prepared)

	restartedState := newTestCPUDeviceState(t, root, nodeName, driverName)
	restored, err := restartedState.Prepare(context.Background(), claim)
	require.NoError(t, err)
	require.NotEmpty(t, restored)

	assert.Equal(t, prepared.GetDevices(), restored.GetDevices())
	assertClaimSpecResolvesPreparedDevices(t, restartedState, claim.UID, restored)
}

func TestPrepareRestoredClaimFailsWhenClaimSpecCannotBeRecreated(t *testing.T) {
	const (
		nodeName   = "test-node"
		driverName = "cpu.example.com"
	)

	root := t.TempDir()
	state := newTestCPUDeviceState(t, root, nodeName, driverName)
	claim := testCPUClaim(driverName, nodeName)

	_, err := state.Prepare(context.Background(), claim)
	require.NoError(t, err)

	require.NoError(t, os.Remove(claimSpecPath(state, claim.UID)))

	restartedState := newTestCPUDeviceState(t, root, nodeName, driverName)
	require.NoError(t, os.Mkdir(claimSpecPath(restartedState, claim.UID), 0750))

	restored, err := restartedState.Prepare(context.Background(), claim)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to recreate CDI spec file for claim from checkpoint")
	assert.Nil(t, restored)

	checkpoint, readErr := readCheckpoint(restartedState.checkpointPath, restartedState.checkpointDecoder)
	require.NoError(t, readErr)
	require.Len(t, checkpoint.PreparedClaims, 1)
	assert.Equal(t, claim.UID, checkpoint.PreparedClaims[0].UID)
}

func newTestCPUDeviceState(t *testing.T, root, nodeName, driverName string) *DeviceState {
	t.Helper()

	flags := &Flags{
		cdiRoot:                     root,
		driverName:                  driverName,
		profile:                     "cpu",
		nodeName:                    nodeName,
		cpuNUMANodes:                1,
		cpusPerNUMANode:             4,
		kubeletPluginsDirectoryPath: root,
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, driverName), 0750))

	state, err := NewDeviceState(&Config{
		flags:   flags,
		profile: cpu.NewProfile(nodeName, driverName, flags.cpuNUMANodes, flags.cpusPerNUMANode),
	})
	require.NoError(t, err)
	return state
}

func testCPUClaim(driverName, nodeName string) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "claim-uid",
			Name:      "claim",
			Namespace: "default",
		},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: &resourceapi.AllocationResult{
				Devices: resourceapi.DeviceAllocationResult{
					Results: []resourceapi.DeviceRequestAllocationResult{
						{
							Request: "cpus",
							Driver:  driverName,
							Pool:    nodeName,
							Device:  "numa-0",
						},
					},
				},
			},
		},
	}
}

func claimSpecPath(state *DeviceState, claimUID types.UID) string {
	specName := cdiapi.GenerateTransientSpecName(state.cdi.vendor(), state.cdi.class, string(claimUID))
	return filepath.Join(filepath.Dir(state.checkpointPath), "..", specName+".yaml")
}

func assertClaimSpecResolvesPreparedDevices(t *testing.T, state *DeviceState, claimUID types.UID, prepared PreparedDevices) {
	t.Helper()

	specBytes, err := os.ReadFile(claimSpecPath(state, claimUID))
	require.NoError(t, err)

	var spec cdispec.Spec
	require.NoError(t, yaml.Unmarshal(specBytes, &spec))
	require.Equal(t, state.cdi.kind(), spec.Kind)

	specDevices := map[string]cdispec.Device{}
	for _, device := range spec.Devices {
		specDevices[device.Name] = device
	}

	resolved := 0
	for _, device := range prepared {
		for _, cdiDeviceID := range device.CdiDeviceIds {
			_, _, deviceName, err := cdiparser.ParseQualifiedName(cdiDeviceID)
			require.NoError(t, err)
			if deviceName == cdiCommonDeviceName {
				continue
			}
			assert.Contains(t, specDevices, deviceName)
			resolved++
		}
	}
	assert.Greater(t, resolved, 0, "expected at least one claim-specific CDI device ID")
}

func TestMergeDeviceStatusPreservesConditions(t *testing.T) {
	cond := metav1.Condition{Type: "BindingConditions", Status: metav1.ConditionTrue, Reason: "Ready"}
	existing := []resourceapi.AllocatedDeviceStatus{
		{Driver: "gpu.example.com", Pool: "node-a", Device: "gpu-0", Conditions: []metav1.Condition{cond}},
	}
	updates := []resourceapi.AllocatedDeviceStatus{
		{Driver: "gpu.example.com", Pool: "node-a", Device: "gpu-0", Data: &runtime.RawExtension{Raw: []byte(`{"uuid":"x"}`)}},
		{Driver: "gpu.example.com", Pool: "node-a", Device: "gpu-1", Data: &runtime.RawExtension{Raw: []byte(`{"uuid":"y"}`)}},
	}

	merged := mergeDeviceStatus(existing, updates)

	require.Len(t, merged, 2)
	assert.Equal(t, "gpu-0", merged[0].Device)
	assert.Equal(t, []metav1.Condition{cond}, merged[0].Conditions, "existing conditions must be preserved")
	assert.Equal(t, updates[0].Data, merged[0].Data)
	assert.Equal(t, updates[1], merged[1], "unmatched entries are appended")
	assert.Len(t, existing[0].Conditions, 1, "input must not be mutated")
}

func TestMergeDeviceStatusDistinguishesShareID(t *testing.T) {
	shareA, shareB := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	existing := []resourceapi.AllocatedDeviceStatus{
		{Driver: "gpu.example.com", Pool: "node-a", Device: "gpu-0", ShareID: &shareA, Conditions: []metav1.Condition{{Type: "BindingConditions", Status: metav1.ConditionTrue}}},
	}
	updates := []resourceapi.AllocatedDeviceStatus{
		{Driver: "gpu.example.com", Pool: "node-a", Device: "gpu-0", ShareID: &shareB, Data: &runtime.RawExtension{Raw: []byte(`{}`)}},
	}

	merged := mergeDeviceStatus(existing, updates)

	require.Len(t, merged, 2)
	assert.Equal(t, &shareA, merged[0].ShareID)
	assert.Len(t, merged[0].Conditions, 1)
	assert.Equal(t, &shareB, merged[1].ShareID)
}
