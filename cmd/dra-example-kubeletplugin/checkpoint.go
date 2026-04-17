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
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	checkpointapi "sigs.k8s.io/dra-example-driver/internal/api/checkpoint"
)

func readCheckpoint(path string, decoder runtime.Decoder) (*checkpointapi.Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	checkpoint := new(checkpointapi.Checkpoint)
	_, _, err = decoder.Decode(data, ptr.To(checkpointapi.SchemeGroupVersion.WithKind("Checkpoint")), checkpoint)
	if err != nil {
		return nil, fmt.Errorf("unmarshal json from %s: %w", path, err)
	}
	return checkpoint, nil
}

func writeCheckpoint(path string, encoder runtime.Encoder, checkpoint *checkpointapi.Checkpoint) (err error) {
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
	if err := encoder.Encode(checkpoint, tmp); err != nil {
		return fmt.Errorf("encode to temp file %s: %w", tmp.Name(), err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmp.Name(), path, err)
	}
	return nil
}
