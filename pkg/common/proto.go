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

// Package common a series of common function
package common

import (
	"sync"

	"github.com/fsnotify/fsnotify"
	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	// ParamOption for option
	ParamOption Option
	// DpStartReset for reset configmap
	DpStartReset sync.Once
)

// NodeDeviceInfoCache record node NPU device information. Will be solidified into cm.
type NodeDeviceInfoCache struct {
	DeviceInfo NodeDeviceInfo
	CheckCode  string
}

// NodeDeviceInfo record node NPU device information. Will be solidified into cm.
type NodeDeviceInfo struct {
	DeviceList map[string]string
	UpdateTime int64
}

// DeviceHealth health status of device
type DeviceHealth struct {
	Health        string
	NetworkHealth string
}

// NpuAllInfo all npu infos
type NpuAllInfo struct {
	AllDevTypes []string
	AllDevs     []NpuDevice
	AICoreDevs  []*NpuDevice
}

// NpuDevice npu device description
type NpuDevice struct {
	DevType       string
	DeviceName    string
	Health        string
	NetworkHealth string
	IP            string
	LogicID       int32
	PhyID         int32
	CardID        int32
}

// DavinCiDev davinci device
type DavinCiDev struct {
	LogicID int32
	PhyID   int32
	CardID  int32
}

// Device id for Instcance
type Device struct { // Device
	DeviceID string `json:"device_id"` // device id
	DeviceIP string `json:"device_ip"` // device ip
}

// Instance is for annotation
type Instance struct { // Instance
	PodName  string   `json:"pod_name"`  // pod Name
	ServerID string   `json:"server_id"` // serverdId
	Devices  []Device `json:"devices"`   // dev
}

// Option option
type Option struct {
	GetFdFlag          bool     // to describe FdFlag
	UseAscendDocker    bool     // UseAscendDocker to chose docker type
	UseVolcanoType     bool     // use volcano mode
	AutoStowingDevs    bool     // auto stowing fixes devices or not
	PresetVDevice      bool     // preset virtual device
	Use310PMixedInsert bool     // chose 310P mixed insert mode
	ListAndWatchPeriod int      // set listening device state period
	HotReset           int      // unhealthy chip hot reset
	AiCoreCount        int32    // found by dcmi interface
	BuildScene         string   // build scene judge device-plugin start scene
	ProductTypes       []string // all product types
	RealCardType       string   // real card type
}

// GetAllDeviceInfoTypeList Get All Device Info Type List
func GetAllDeviceInfoTypeList() map[string]struct{} {
	return map[string]struct{}{HuaweiUnHealthAscend910: {}, HuaweiNetworkUnHealthAscend910: {},
		ResourceNamePrefix + Ascend910: {}, ResourceNamePrefix + Ascend910c2: {},
		ResourceNamePrefix + Ascend910c4: {}, ResourceNamePrefix + Ascend910c8: {},
		ResourceNamePrefix + Ascend910c16: {}, ResourceNamePrefix + Ascend310: {},
		ResourceNamePrefix + Ascend310P: {}, ResourceNamePrefix + Ascend310Pc1: {},
		ResourceNamePrefix + Ascend310Pc2: {}, ResourceNamePrefix + Ascend310Pc4: {},
		ResourceNamePrefix + Ascend310Pc2Cpu1: {}, ResourceNamePrefix + Ascend310Pc4Cpu3: {},
		ResourceNamePrefix + Ascend310Pc4Cpu3Ndvpp: {}, ResourceNamePrefix + Ascend310Pc4Cpu4Dvpp: {},
		HuaweiUnHealthAscend310P: {}, HuaweiUnHealthAscend310: {}, ResourceNamePrefix + AiCoreResourceName: {}}
}

// FileWatch is used to watch sock file
type FileWatch struct {
	FileWatcher *fsnotify.Watcher
}

// DevStatusSet contain different states devices
type DevStatusSet struct {
	UnHealthyDevice    sets.String
	NetUnHealthyDevice sets.String
	FreeHealthyDevice  map[string]sets.String
}

// Get310PProductType get 310P product type
func Get310PProductType() map[string]string {
	return map[string]string{
		"Atlas 300V Pro": Ascend310PVPro,
		"Atlas 300V":     Ascend310PV,
		"Atlas 300I Pro": Ascend310PIPro,
	}
}
