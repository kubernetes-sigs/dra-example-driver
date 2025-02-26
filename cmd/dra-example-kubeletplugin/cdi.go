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
	"os"
	"strings"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

const (
	cdiVendor           = "k8s." + DriverName
	cdiClass            = "npu"
	cdiKind             = cdiVendor + "/" + cdiClass
	cdiCommonDeviceName = "common"
)

type CDIHandler struct {
	cache *cdiapi.Cache
}

func NewCDIHandler(config *Config) (*CDIHandler, error) {
	cache, err := cdiapi.NewCache(
		cdiapi.WithSpecDirs(config.flags.cdiRoot),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create a new CDI cache: %w", err)
	}
	handler := &CDIHandler{
		cache: cache,
	}

	return handler, nil
}

// CreateCommonSpecFile 生成通用的 CDI spec，用于设置节点相关环境变量
func (cdi *CDIHandler) CreateCommonSpecFile() error {
	spec := &cdispec.Spec{
		Kind: cdiKind,
		Devices: []cdispec.Device{
			{
				Name: cdiCommonDeviceName,
				ContainerEdits: cdispec.ContainerEdits{
					Env: []string{
						fmt.Sprintf("KUBERNETES_NODE_NAME=%s", os.Getenv("NODE_NAME")),
						fmt.Sprintf("DRA_RESOURCE_DRIVER_NAME=%s", DriverName),
					},
				},
			},
		},
	}

	minVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	spec.Version = minVersion

	specName, err := cdiapi.GenerateNameForTransientSpec(spec, cdiCommonDeviceName)
	if err != nil {
		return fmt.Errorf("failed to generate Spec name: %w", err)
	}

	return cdi.cache.WriteSpec(spec, specName)
}

// CreateClaimSpecFile 为给定 claim 创建临时 CDI spec 文件
// 修改点：将所有分配到的设备名称聚合后，以环境变量 ASCEND_VISIBLE_DEVICES 注入到 Pod 中
func (cdi *CDIHandler) CreateClaimSpecFile(claimUID string, devices PreparedDevices) error {
	var deviceNames []string
	for _, device := range devices {
		deviceNames = append(deviceNames, device.DeviceName)
	}
	visibleDevices := strings.Join(deviceNames, ",")

	specName := cdiapi.GenerateTransientSpecName(cdiVendor, cdiClass, claimUID)

	spec := &cdispec.Spec{
		Kind: cdiKind,
		Devices: []cdispec.Device{
			{
				// 使用 claimUID 作为 CDI 设备名称
				Name: claimUID,
				ContainerEdits: cdispec.ContainerEdits{
					Env: []string{
						// 注入环境变量，告知 Pod 哪些 NPU 设备应该暴露
						fmt.Sprintf("ASCEND_VISIBLE_DEVICES=%s", visibleDevices),
					},
				},
			},
		},
	}

	minVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	spec.Version = minVersion

	return cdi.cache.WriteSpec(spec, specName)
}

// DeleteClaimSpecFile 删除指定 claim 的 CDI spec 文件
func (cdi *CDIHandler) DeleteClaimSpecFile(claimUID string) error {
	specName := cdiapi.GenerateTransientSpecName(cdiVendor, cdiClass, claimUID)
	return cdi.cache.RemoveSpec(specName)
}

// GetClaimDevices 返回当前 claim 对应的 CDI 设备名称列表
func (cdi *CDIHandler) GetClaimDevices(claimUID string, devices []string) []string {
	return []string{
		cdiparser.QualifiedName(cdiVendor, cdiClass, cdiCommonDeviceName),
		cdiparser.QualifiedName(cdiVendor, cdiClass, claimUID),
	}
}
