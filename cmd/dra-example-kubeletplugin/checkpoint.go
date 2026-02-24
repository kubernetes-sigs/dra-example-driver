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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/dra-example-driver/internal/profiles"
)

type PreparedClaims map[string]profiles.PreparedDevices

type Checkpoint struct {
	V1 *CheckpointV1 `json:"v1,omitempty"`
}

type CheckpointV1 struct {
	PreparedClaims PreparedClaims `json:"preparedClaims,omitempty"`
}

func newCheckpoint() *Checkpoint {
	pc := &Checkpoint{
		V1: &CheckpointV1{},
	}
	return pc
}

func readCheckpoint(path string) (*Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	checkpoint := new(Checkpoint)
	err = json.Unmarshal(data, checkpoint)
	if err != nil {
		return nil, fmt.Errorf("unmarshal json from %s: %w", path, err)
	}
	return checkpoint, nil
}

func writeCheckpoint(path string, checkpoint *Checkpoint) (err error) {
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "tmp-checkpoint-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	defer func() {
		if err1 := tmp.Close(); err1 != nil && err == nil {
			err = fmt.Errorf("close temp file: %w", err1)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write to temp file %s: %w", tmp.Name(), err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmp.Name(), path, err)
	}
	return nil
}

func (cp *Checkpoint) GetPreparedDevices(claimUID string) profiles.PreparedDevices {
	if cp.V1 == nil {
		return nil
	}
	if devices, ok := cp.V1.PreparedClaims[claimUID]; ok {
		return devices
	}
	return nil
}

func (cp *Checkpoint) AddPreparedDevices(claimUID string, pds profiles.PreparedDevices) {
	if cp.V1 == nil {
		return
	}

	if cp.V1.PreparedClaims == nil {
		cp.V1.PreparedClaims = make(PreparedClaims)
	}

	cp.V1.PreparedClaims[claimUID] = pds
}

func (cp *Checkpoint) RemovePreparedDevices(claimUID string) {
	if cp.V1 == nil {
		return
	}

	delete(cp.V1.PreparedClaims, claimUID)
}
