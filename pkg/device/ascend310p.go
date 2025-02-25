/* Copyright(C) 2022. Huawei Technologies Co.,Ltd. All rights reserved.
   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

// Package device a series of device function
package device

import (
	"fmt"

	"huawei.com/npu-exporter/v5/common-utils/hwlog"

	"sigs.k8s.io/dra-example-driver/pkg/common"
)

// HwAscend310PManager manages huawei Ascend310P devices.
type HwAscend310PManager struct {
	AscendTools
}

// NewHwAscend310PManager used to create ascend 310P manager
func NewHwAscend310PManager() *HwAscend310PManager {
	return &HwAscend310PManager{
		AscendTools: AscendTools{
			name:         common.Ascend310P,
			unHealthyKey: common.HuaweiUnHealthAscend310P,
			devCount:     common.MaxDevicesNum,
		},
	}
}

// GetNPUs Discovers all HUAWEI Ascend310P devices by call devmanager interface
func (hnm *HwAscend310PManager) GetNPUs() (common.NpuAllInfo, error) {
	devNum, devList, err := hnm.dmgr.GetDeviceList()
	if err != nil {
		return common.NpuAllInfo{}, err
	}
	if devNum > hnm.devCount {
		return common.NpuAllInfo{}, fmt.Errorf("invalid device num: %d", devNum)
	}
	var allDevices []common.NpuDevice
	var aiCoreDevices []*common.NpuDevice
	var allDeviceTypes []string
	for i := int32(0); i < devNum; i++ {
		davinCiDev, err := hnm.getDavinCiDev(devList[i])
		if err != nil {
			return common.NpuAllInfo{}, err
		}
		if common.ParamOption.Use310PMixedInsert {
			if err = hnm.assemble310PMixedPhyDevices(davinCiDev, &allDevices, &allDeviceTypes); err != nil {
				hwlog.RunLog.Errorf("assemble 310P mixed phy devices failed: %#v", err)
			}
			continue
		}
		vDevInfos, err := hnm.getVirtualDevice(devList[i])
		if err != nil {
			hwlog.RunLog.Errorf("The virtual device is considered not exist, please check the error: %#v", err)
		}
		if vDevInfos.TotalResource.VDevNum > common.MaxVirtualDeviceNum {
			return common.NpuAllInfo{}, fmt.Errorf("invalid virtual device count")
		}
		if !common.ParamOption.PresetVDevice {
			common.FakeAiCoreDevice(davinCiDev, &aiCoreDevices)
		}
		if vDevInfos.TotalResource.VDevNum == 0 {
			hnm.assemblePhyDevices(davinCiDev, &allDevices, &allDeviceTypes)
			continue
		}
		hnm.assembleVirtualDevices(davinCiDev, vDevInfos, &allDevices, &allDeviceTypes)
	}
	allDeviceTypes = hnm.removeDuplicate(&allDeviceTypes)
	return common.NpuAllInfo{AllDevs: allDevices, AICoreDevs: aiCoreDevices, AllDevTypes: allDeviceTypes}, nil
}

// DoWithVolcanoListAndWatch ascend310P affinity scheduling
func (hnm *HwAscend310PManager) DoWithVolcanoListAndWatch(classifyDevs map[string][]*common.NpuDevice) {
	devStatusSet := hnm.getDevStatesDevSet(classifyDevs)
	if err := hnm.UpdateNodeDeviceInfo(devStatusSet, hnm.updateDeviceInfo); err != nil {
		hwlog.RunLog.Errorf("update device info failed, err: %#v", err)
	}
}

func (hnm *HwAscend310PManager) updateDeviceInfo(_, newDevInfo map[string]string,
	devStatusSet common.DevStatusSet) error {
	if newDevInfo == nil {
		return fmt.Errorf("invalid new device info")
	}
	newDevInfo[common.HuaweiAscend310P] = common.ToString(devStatusSet.FreeHealthyDevice[hnm.name],
		common.CommaSepDev)
	newDevInfo[hnm.unHealthyKey] = common.ToString(devStatusSet.UnHealthyDevice, common.CommaSepDev)
	return nil
}
