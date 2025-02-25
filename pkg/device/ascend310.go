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

// Package device a series of device function.
package device

import (
	"fmt"

	"sigs.k8s.io/dra-example-driver/pkg/common"
)

// HwAscend310Manager manages huawei Ascend310 devices.
type HwAscend310Manager struct {
	AscendTools
}

// NewHwAscend310Manager used to create ascend 310 manager
func NewHwAscend310Manager() *HwAscend310Manager {
	name := common.Ascend310
	if common.ParamOption.GetFdFlag {
		name = common.AscendfdPrefix
	}
	return &HwAscend310Manager{
		AscendTools: AscendTools{
			name:         name,
			unHealthyKey: common.HuaweiUnHealthAscend310,
			devCount:     common.MaxCardNum * common.MaxDevNumInCard,
		},
	}
}

// GetNPUs Discovers all HUAWEI Ascend310 devices by call devmanager interface
func (hnm *HwAscend310Manager) GetNPUs() (common.NpuAllInfo, error) {
	devNum, devList, err := hnm.dmgr.GetDeviceList()
	if err != nil {
		return common.NpuAllInfo{}, err
	}
	if devNum > hnm.devCount {
		return common.NpuAllInfo{}, fmt.Errorf("invalid device num: %d", devNum)
	}
	var allDevices []common.NpuDevice
	var allDeviceTypes []string
	for i := int32(0); i < devNum; i++ {
		davinCiDev, err := hnm.getDavinCiDev(devList[i])
		if err != nil {
			return common.NpuAllInfo{}, err
		}
		deviceName := fmt.Sprintf("%s-%d", hnm.name, davinCiDev.PhyID)
		device := hnm.assembleNpuDeviceStruct(hnm.name, deviceName, davinCiDev)
		allDevices = append(allDevices, device)
	}
	allDeviceTypes = append(allDeviceTypes, hnm.name)
	return common.NpuAllInfo{AllDevs: allDevices, AllDevTypes: allDeviceTypes}, nil
}

// DoWithVolcanoListAndWatch ascend310 watch device
func (hnm *HwAscend310Manager) DoWithVolcanoListAndWatch(classifyDevs map[string][]*common.NpuDevice) {
	devStatusSet := hnm.getDevStatesDevSet(classifyDevs)
	if err := hnm.UpdateNodeDeviceInfo(devStatusSet, hnm.updateDeviceInfo); err != nil {
	}
}

func (hnm *HwAscend310Manager) updateDeviceInfo(_, newDeviceInfo map[string]string,
	devStatusSet common.DevStatusSet) error {
	if newDeviceInfo == nil {
		return fmt.Errorf("invalid new device info")
	}
	newDeviceInfo[common.HuaweiAscend310] = common.ToString(devStatusSet.FreeHealthyDevice[hnm.name],
		common.CommaSepDev)
	newDeviceInfo[hnm.unHealthyKey] = common.ToString(devStatusSet.UnHealthyDevice, common.CommaSepDev)
	return nil
}
