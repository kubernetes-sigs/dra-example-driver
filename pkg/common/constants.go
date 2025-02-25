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

const (
	// Component component name
	Component = "device-plugin"
	// MaxBackups log file max backup
	MaxBackups = 30
	// MaxAge the log file last time
	MaxAge = 7

	// KubeEnvMaxLength k8s env name max length
	KubeEnvMaxLength = 230
	// PodNameMaxLength pod name max length
	PodNameMaxLength = 253
	// PodNameSpaceMaxLength pod name space max length
	PodNameSpaceMaxLength = 63
	// MaxPodLimit max pod num
	MaxPodLimit = 10000
	// MaxContainerLimit max container num
	MaxContainerLimit = 300000
	// RetryUpdateCount is max number of retry resource update
	RetryUpdateCount = 3
	// MaxDeviceNameLen max length of device name, like "Ascend310P-4c.3cpu-100-0"
	MaxDeviceNameLen = 50
	// MaxGRPCRecvMsgSize 4MB
	MaxGRPCRecvMsgSize = 4 * 1024 * 1024
	// MaxGRPCConcurrentStreams limit on the number of concurrent streams to each ServerTransport.
	MaxGRPCConcurrentStreams = 64
	// MaxConcurrentLimit limit over listener
	MaxConcurrentLimit = 64
	// MaxIPConnectionLimit limit over ip
	MaxIPConnectionLimit = 64
	// CacheSize cache for ip
	CacheSize = 128
	// MaxVirtualDeviceNum max num of virtual device
	MaxVirtualDeviceNum = 1024
	// CMDataMaxMemory configMap max data size 1MB
	CMDataMaxMemory = 1024 * 1024
	// PodAnnotationMaxMemory pod annotation max data size 1MB
	PodAnnotationMaxMemory = 1024 * 1024

	// DeviceInfoCMNameSpace namespace of device info configmap
	DeviceInfoCMNameSpace = "kube-system"
	// DeviceInfoCMNamePrefix device info configmap name prefix
	DeviceInfoCMNamePrefix = "mindx-dl-deviceinfo-"
	// DeviceInfoCMDataKey device info configmap data key
	DeviceInfoCMDataKey = "DeviceInfoCfg"

	runtimeEnvNum = 3
	// ascendVisibleDevicesEnv visible devices env
	ascendVisibleDevicesEnv = "ASCEND_VISIBLE_DEVICES"
	// ascendRuntimeOptionsEnv virtual runtime option env
	ascendRuntimeOptionsEnv = "ASCEND_RUNTIME_OPTIONS"
	// ascendAllowLinkEnv a500a2 need mount softlink
	ascendAllowLinkEnv = "ASCEND_ALLOW_LINK"
	// PodPredicateTime pod predicate time
	PodPredicateTime = "predicate-time"
	// Pod2kl pod annotation key, means kubelet allocate device
	Pod2kl = "kltDev"
	// PodRealAlloc pod annotation key, means pod real mount device
	PodRealAlloc = "AscendReal"
	// Pod910DeviceKey pod annotation key, for generate 910 hccl rank table
	Pod910DeviceKey = "ascend.kubectl.kubernetes.io/ascend-910-configuration"

	// PodResourceSeverKey for pod resource key
	PodResourceSeverKey = "podResource"
	// VirtualDev Virtual device tag
	VirtualDev = "VIRTUAL"
	// PhyDeviceLen like Ascend910-0 split length is 2
	PhyDeviceLen = 2
	// VirDeviceLen like Ascend910-2c-100-1 split length is 4
	VirDeviceLen = 4
	// MaxDevicesNum max device num
	MaxDevicesNum = 64
	// MaxCardNum max card num
	MaxCardNum = 64
	// MaxDevNumInCard max device num in card
	MaxDevNumInCard = 4
	// MaxRequestVirtualDeviceNum max request device num
	MaxRequestVirtualDeviceNum = 1
	// LabelDeviceLen like Ascend910-0 split length is 2
	LabelDeviceLen = 2
	// DefaultDeviceIP device ip address
	DefaultDeviceIP = "127.0.0.1"
	// NormalState health state
	NormalState = uint32(0)
	// GeneralAlarm health state
	GeneralAlarm = uint32(1)

	// SocketChmod socket file mode
	SocketChmod = 0600
	// RunMode310 for 310 chip
	RunMode310 = "ascend310"
	// RunMode910 for 910 chip
	RunMode910 = "ascend910"
	// RunMode310P for 310P chip
	RunMode310P = "ascend310P"

	// Interval interval time
	Interval = 1
	// Timeout time
	Timeout = 10
	// BaseDec base
	BaseDec = 10
	// BitSize base size
	BitSize = 64
	// BitSize32 base size 32
	BitSize32 = 32
	// SleepTime The unit is seconds
	SleepTime = 5
)

const (
	// ResourceNamePrefix prefix
	ResourceNamePrefix = "huawei.com/"
	// Ascend310P 310p
	Ascend310P = "Ascend310P"
	// Ascend310PV 310P-V
	Ascend310PV = Ascend310P + "-V"
	// Ascend310PVPro 310P-VPro
	Ascend310PVPro = Ascend310P + "-VPro"
	// Ascend310PIPro 310P-IPro
	Ascend310PIPro = Ascend310P + "-IPro"
	// Ascend310Pc1 Ascend310P 1 core
	Ascend310Pc1 = Ascend310P + "-" + Core1
	// Ascend310Pc2 Ascend310P 2 core
	Ascend310Pc2 = Ascend310P + "-" + Core2
	// Ascend310Pc4 Ascend310P 4 core
	Ascend310Pc4 = Ascend310P + "-" + Core4
	// Ascend310Pc4Cpu3 Ascend310P 4core 3cpu
	Ascend310Pc4Cpu3 = Ascend310P + "-" + Core4Cpu3
	// Ascend310Pc2Cpu1 Ascend310P 2core 1cpu
	Ascend310Pc2Cpu1 = Ascend310P + "-" + Core2Cpu1
	// Ascend310Pc4Cpu4Dvpp Ascend310P 4core 4cpu dvpp
	Ascend310Pc4Cpu4Dvpp = Ascend310P + "-" + Core4Cpu4Dvpp
	// Ascend310Pc4Cpu3Ndvpp Ascend310P 4core 3cpu ndvpp
	Ascend310Pc4Cpu3Ndvpp = Ascend310P + "-" + Core4Cpu3Ndvpp
	// HuaweiAscend310P with prefix
	HuaweiAscend310P = ResourceNamePrefix + Ascend310P

	// Ascend910 910
	Ascend910 = "Ascend910"
	// Ascend910c2  Ascend910 2core
	Ascend910c2 = Ascend910 + "-" + Core2
	// Ascend910c4 Ascend910 4core
	Ascend910c4 = Ascend910 + "-" + Core4
	// Ascend910c8 Ascend910 8core
	Ascend910c8 = Ascend910 + "-" + Core8
	// Ascend910c16 Ascend910 16core
	Ascend910c16 = Ascend910 + "-" + Core16
	// HuaweiAscend910 with prefix
	HuaweiAscend910 = ResourceNamePrefix + Ascend910

	// Ascend310 310
	Ascend310 = "Ascend310"
	// Ascend310B 310B chip
	Ascend310B = "Ascend310B"
	// HuaweiAscend310 with prefix
	HuaweiAscend310 = ResourceNamePrefix + Ascend310
	// AscendfdPrefix use in fd
	AscendfdPrefix = "davinci-mini"

	// Ascend910B ascend 1980B(910B) chip
	Ascend910B = "Ascend910B"

	// HuaweiNetworkUnHealthAscend910 910 network unhealthy
	HuaweiNetworkUnHealthAscend910 = ResourceNamePrefix + "Ascend910-NetworkUnhealthy"
	// HuaweiUnHealthAscend910 unhealthy
	HuaweiUnHealthAscend910 = ResourceNamePrefix + Ascend910 + "-Unhealthy"
	// HuaweiUnHealthAscend310P 310p unhealthy
	HuaweiUnHealthAscend310P = ResourceNamePrefix + Ascend310P + "-Unhealthy"
	// HuaweiUnHealthAscend310 310 unhealthy
	HuaweiUnHealthAscend310 = ResourceNamePrefix + Ascend310 + "-Unhealthy"
	// HuaweiNetworkRecoverAscend910 910 network recover
	HuaweiNetworkRecoverAscend910 = ResourceNamePrefix + Ascend910 + "-NetworkRecover"
	// HuaweiRecoverAscend910 910 recover
	HuaweiRecoverAscend910 = ResourceNamePrefix + Ascend910 + "-Recover"

	// AiCoreResourceName resource name for virtual device
	AiCoreResourceName = "npu-core"

	// Core1 1 core
	Core1 = "1c"
	// Core2 2 core
	Core2 = "2c"
	// Core4 4 core
	Core4 = "4c"
	// Core8 8 core
	Core8 = "8c"
	// Core16 16 core
	Core16 = "16c"
	// Core4Cpu3 4core 3cpu
	Core4Cpu3 = "4c.3cpu"
	// Core2Cpu1 2core 1cpu
	Core2Cpu1 = "2c.1cpu"
	// Core4Cpu4Dvpp 4core 4cpu dvpp
	Core4Cpu4Dvpp = "4c.4cpu.dvpp"
	// Core4Cpu3Ndvpp 4core 3cpu ndvpp
	Core4Cpu3Ndvpp = "4c.3cpu.ndvpp"

	// Vir01 template name vir01
	Vir01 = "vir01"
	// Vir02 template name vir02
	Vir02 = "vir02"
	// Vir04 template name vir04
	Vir04 = "vir04"
	// Vir08 template name vir08
	Vir08 = "vir08"
	// Vir16 template name vir16
	Vir16 = "vir16"
	// Vir04C3 template name vir04_3c
	Vir04C3 = "vir04_3c"
	// Vir02C1 template name vir02_1c
	Vir02C1 = "vir02_1c"
	// Vir04C4Dvpp template name vir04_4c_dvpp
	Vir04C4Dvpp = "vir04_4c_dvpp"
	// Vir04C3Ndvpp template name vir04_3c_ndvpp
	Vir04C3Ndvpp = "vir04_3c_ndvpp"

	// AnnotationVNPUInfoSplitLen length of pod annotation for allocate vnpu info
	AnnotationVNPUInfoSplitLen = 2

	// MaxAICoreNum max ai core num
	MaxAICoreNum = 32
	// MinAICoreNum min ai core num
	MinAICoreNum = 8
	// DefaultIDForCreateVNPU default id for creating vnpu
	DefaultIDForCreateVNPU = 0xFFFFFFFF

	// ServerTypeLabelKey the node label key of server type
	ServerTypeLabelKey = "servertype"
	// ServerTypeInfoMinLen the min len of server type split data
	ServerTypeInfoMinLen = 2
	// VGroupAndDevLen a list only contain virtual group and device
	VGroupAndDevLen = 2
)

const (
	// HiAIHDCDevice hisi_hdc
	HiAIHDCDevice = "/dev/hisi_hdc"
	// HiAIManagerDevice davinci_manager
	HiAIManagerDevice = "/dev/davinci_manager"
	// HiAISVMDevice devmm_svm
	HiAISVMDevice = "/dev/devmm_svm"
	// HiAi200RCSVM0 svm0
	HiAi200RCSVM0 = "/dev/svm0"
	// HiAi200RCLog log_drv
	HiAi200RCLog = "/dev/log_drv"
	// HiAi200RCEventSched event_sched
	HiAi200RCEventSched = "/dev/event_sched"
	// HiAi200RCUpgrade upgrade
	HiAi200RCUpgrade = "/dev/upgrade"
	// HiAi200RCHiDvpp hi_dvpp
	HiAi200RCHiDvpp = "/dev/hi_dvpp"
	// HiAi200RCMemoryBandwidth memory_bandwidth
	HiAi200RCMemoryBandwidth = "/dev/memory_bandwidth"
	// HiAi200RCTsAisle ts_aisle
	HiAi200RCTsAisle = "/dev/ts_aisle"
)

const (
	// Atlas200ISoc 200 soc env
	Atlas200ISoc = "Atlas 200I SoC A1"
	// Atlas200ISocXSMEM is xsmem_dev
	Atlas200ISocXSMEM = "/dev/xsmem_dev"
	// Atlas200ISocSYS is sys
	Atlas200ISocSYS = "/dev/sys"
	// Atlas200ISocVDEC is vdec
	Atlas200ISocVDEC = "/dev/vdec"
	// Atlas200ISocVPC is vpc
	Atlas200ISocVPC = "/dev/vpc"
	// Atlas200ISocSpiSmbus is spi_smbus
	Atlas200ISocSpiSmbus = "/dev/spi_smbus"
	// Atlas200ISocUserConfig is user_config
	Atlas200ISocUserConfig = "/dev/user_config"
)

const (
	// Atlas310BDvppCmdlist is dvpp_cmdlist
	Atlas310BDvppCmdlist = "/dev/dvpp_cmdlist"
	// Atlas310BPngd is pngd
	Atlas310BPngd = "/dev/pngd"
	// Atlas310BVenc is venc
	Atlas310BVenc = "/dev/venc"
)

const (
	// RootUID is root user id
	RootUID = 0
	// RootGID is root group id
	RootGID = 0

	// DotSepDev if the separator between devices on labels
	DotSepDev = "."

	// CommaSepDev if the separator between devices on annotation
	CommaSepDev = ","
	// MiddelLine if the separator between devices for split id
	MiddelLine = "-"
	// UnderLine the separator between ids
	UnderLine = "_"

	// NoNPUResource means allocated some devices that don't exist
	NoNPUResource = "NoNPUResource"
	// NPUSegmentFailed means create vnpu device failed
	NPUSegmentFailed = "NPUSegmentFailed"
	// CenterScene deploy the device-plugin component on the central side
	CenterScene = "center"
	// EdgeScene deploy the device-plugin component on the edge side
	EdgeScene = "edge"
)

// Special scene for invoking the dcmi interface
const (
	DeviceNotSupport = 8255
	// DefaultAiCoreNum set a default value of aicore number
	DefaultAiCoreNum = 1
)

const (
	// Atlas300IDuo for hot reset function, sync chip healthy state
	Atlas300IDuo = "Atlas 300I Duo"
	// HotResetClose not using chip hot reset function
	HotResetClose = -1
	// HotResetInfer using infer chip hot reset
	HotResetInfer = 0
	// HotResetTrain using train chip hot reset
	HotResetTrain = 1
	// BootStartFinish chip hot reset finish
	BootStartFinish = 16
)
