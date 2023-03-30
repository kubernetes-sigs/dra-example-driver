/*
 * Copyright 2023 The Kubernetes Authors.
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
	"math/rand"
	"os"

	"github.com/google/uuid"
)

func enumerateAllPossibleDevices() (AllocatableDevices, error) {
	numGPUs := 8
	seed := os.Getenv("NODE_NAME")
	uuids := generateUUIDs(seed, numGPUs)

	alldevices := make(AllocatableDevices)
	for _, uuid := range uuids {
		deviceInfo := &AllocatableDeviceInfo{
			GpuInfo: &GpuInfo{
				uuid:  uuid,
				model: "LATEST-GPU-MODEL",
			},
		}
		alldevices[uuid] = deviceInfo
	}
	return alldevices, nil
}

func generateUUIDs(seed string, count int) []string {
	rand := rand.New(rand.NewSource(hash(seed)))

	uuids := make([]string, count)
	for i := 0; i < count; i++ {
		charset := make([]byte, 16)
		rand.Read(charset)
		uuid, _ := uuid.FromBytes(charset)
		uuids[i] = "GPU-" + uuid.String()
	}

	return uuids
}

func hash(s string) int64 {
	h := int64(0)
	for _, c := range s {
		h = 31*h + int64(c)
	}
	return h
}
