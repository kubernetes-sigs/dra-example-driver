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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
