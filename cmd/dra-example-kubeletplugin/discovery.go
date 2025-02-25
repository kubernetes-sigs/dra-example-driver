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
	"fmt"
	"math/rand"
	"os"
	"strings"

	"huawei.com/npu-exporter/v5/devmanager"
	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/google/uuid"
)

func enumerateAllPossibleDevices() (AllocatableDevices, error) {
	manager, err := devmanager.NewHwDevManager()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize NPU manager: %v", err)
	}
	allInfo, err := manager.GetNPUs()
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate NPUs: %v", err)
	}

	// 获取环境变量，指定可见设备
	visibleDevices := os.Getenv("ASCEND_VISIBLE_DEVICES")
	var selectedIDs []string
	if visibleDevices != "" {
		selectedIDs = strings.Split(visibleDevices, ",")
	}

	alldevices := make(AllocatableDevices)
	for _, dev := range allInfo.AllDevs {
		deviceName := fmt.Sprintf("npu-%d", dev.LogicID)
		if len(selectedIDs) > 0 && !contains(selectedIDs, fmt.Sprint(dev.LogicID)) {
			continue
		}

		device := resourceapi.Device{
			Name: deviceName,
			Basic: &resourceapi.BasicDevice{
				Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
					"index": {IntValue: ptr.To(int64(dev.LogicID))},
					"uuid":  {StringValue: ptr.To(dev.DeviceName)},
					"model": {StringValue: ptr.To(dev.DevType)},
				},
				Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
					"memory": {Value: resource.MustParse("32Gi")},
				},
			},
		}
		alldevices[device.Name] = device
	}
	return alldevices, nil
}

func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func generateUUIDs(seed string, count int) []string {
	rand := rand.New(rand.NewSource(hash(seed)))

	uuids := make([]string, count)
	for i := 0; i < count; i++ {
		charset := make([]byte, 16)
		rand.Read(charset)
		uuid, _ := uuid.FromBytes(charset)
		uuids[i] = "gpu-" + uuid.String()
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
