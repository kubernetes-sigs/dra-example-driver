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

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"

	"sigs.k8s.io/dra-example-driver/internal/profiles"
)

func TestReadWriteCheckpointRoundtrip(t *testing.T) {
	tests := map[string]struct {
		checkpoint *Checkpoint
	}{
		"new checkpoint": {
			checkpoint: newCheckpoint(),
		},
		"populated checkpoint": {
			checkpoint: &Checkpoint{
				V1: &CheckpointV1{
					PreparedClaims{
						"uid": profiles.PreparedDevices{
							{
								Device: drapbv1.Device{
									RequestNames: []string{"req"},
									PoolName:     "pool",
									DeviceName:   "dev",
									CdiDeviceIds: []string{"id"},
								},
								ContainerEdits: &cdi.ContainerEdits{
									ContainerEdits: &specs.ContainerEdits{
										Env: []string{"KEY=value"},
									},
								},
								AdminAccess: true,
							},
						},
					},
				},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, DriverPluginCheckpointFile)

			checkpoint, err := readCheckpoint(path)
			assert.Nil(t, checkpoint)
			assert.ErrorIs(t, err, fs.ErrNotExist)

			checkpoint = test.checkpoint
			err = writeCheckpoint(path, checkpoint)
			require.NoError(t, err)

			read, err := readCheckpoint(path)
			require.NoError(t, err)
			assert.Equal(t, test.checkpoint, read)
		})
	}

	// The checkpoint format used to contain a checksum. This test ensures that
	// checkpoints written in the old format can still be read.
	t.Run("old checkpoint format", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DriverPluginCheckpointFile)

		old := `{
			"checksum": 1,
			"v1": {
				"preparedClaims": {
					"uid": []
				}
			}
		}`

		err := os.WriteFile(path, []byte(old), 0o600)
		require.NoError(t, err)

		checkpoint, err := readCheckpoint(path)
		assert.NoError(t, err)
		assert.NotNil(t, checkpoint.V1.PreparedClaims)
	})
}
