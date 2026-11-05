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
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	checkpointapi "sigs.k8s.io/dra-example-driver/internal/api/checkpoint"
)

func TestReadWriteCheckpointRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DriverPluginCheckpointFile)

	decoder, encoder, err := checkpointSerializer()
	if err != nil {
		t.Fatal("failed to initialize checkpoint serializer:", err)
	}

	checkpoint, err := readCheckpoint(path, decoder)
	assert.NoError(t, err)
	assert.Equal(t, new(checkpointapi.Checkpoint), checkpoint)

	// "prepare" some claims
	updatedCheckpoint := &checkpointapi.Checkpoint{
		PreparedClaims: []checkpointapi.PreparedClaim{
			{UID: types.UID("123")},
			{UID: types.UID("456")},
		},
	}
	err = writeCheckpoint(path, encoder, updatedCheckpoint)
	require.NoError(t, err)

	checkpoint, err = readCheckpoint(path, decoder)
	require.NoError(t, err)
	assert.Equal(t, updatedCheckpoint, checkpoint)

	// "unprepare" those claims
	updatedCheckpoint = &checkpointapi.Checkpoint{
		PreparedClaims: nil,
	}
	err = writeCheckpoint(path, encoder, updatedCheckpoint)
	require.NoError(t, err)

	checkpoint, err = readCheckpoint(path, decoder)
	require.NoError(t, err)
	assert.Equal(t, updatedCheckpoint, checkpoint)
}

type failingEncoder struct{}

func (failingEncoder) Encode(runtime.Object, io.Writer) error {
	return errors.New("no space left on device")
}
func (failingEncoder) Identifier() runtime.Identifier { return "failing" }

func TestWriteCheckpointRemovesTempFileOnFailure(t *testing.T) {
	_, encoder, err := checkpointSerializer()
	require.NoError(t, err)

	tests := map[string]struct {
		setup   func(t *testing.T, path string)
		encoder runtime.Encoder
	}{
		"encode fails": {
			encoder: failingEncoder{},
		},
		"rename fails": {
			// A non-empty directory at path makes the rename fail.
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.MkdirAll(filepath.Join(path, "keep"), 0o755))
			},
			encoder: encoder,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, DriverPluginCheckpointFile)
			if test.setup != nil {
				test.setup(t, path)
			}

			err := writeCheckpoint(path, test.encoder, &checkpointapi.Checkpoint{})
			require.Error(t, err)

			leftovers, err := filepath.Glob(filepath.Join(dir, "tmp-checkpoint-*"))
			require.NoError(t, err)
			assert.Empty(t, leftovers, "temp files left in the plugin directory")
		})
	}
}
