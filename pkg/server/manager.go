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

// Package server holds the implementation of registration to kubelet, k8s pod resource interface.
package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"huawei.com/npu-exporter/v5/devmanager"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/dra-example-driver/pkg/common"
	"sigs.k8s.io/dra-example-driver/pkg/device"
	"sigs.k8s.io/dra-example-driver/pkg/kubeclient"
)

// HwDevManager manages huawei device devices.
type HwDevManager struct {
	groupDevice map[string][]*common.NpuDevice
	ServerMap   map[string]InterfaceServer
	AllInfo     common.NpuAllInfo
	manager     device.DevManager
	RunMode     string
}

// NewHwDevManager function is used to new a dev manager.
func NewHwDevManager(devM devmanager.DeviceInterface) *HwDevManager {
	var hdm HwDevManager
	if err := hdm.setAscendManager(devM); err != nil {

		return nil
	}
	if err := hdm.setAllDeviceAndType(); err != nil {

		return nil
	}
	if err := hdm.initPluginServer(); err != nil {

		return nil
	}
	return &hdm
}

func (hdm *HwDevManager) setAscendManager(dmgr devmanager.DeviceInterface) error {
	devType := dmgr.GetDevType()
	switch devType {
	case common.Ascend310, common.Ascend310B:
		if !common.ParamOption.PresetVDevice {
			return fmt.Errorf("only 310p and 910 support dynamic virtual instance")
		}
		hdm.RunMode = common.Ascend310
		hdm.manager = device.NewHwAscend310Manager()
	case common.Ascend910, common.Ascend910B:
		hdm.RunMode = common.Ascend910
		hdm.manager = device.NewHwAscend910Manager()
	case common.Ascend310P:
		hdm.RunMode = common.Ascend310P
		hdm.manager = device.NewHwAscend310PManager()
	default:

		return fmt.Errorf("an unsupported device type")
	}
	common.ParamOption.RealCardType = devType
	hdm.manager.SetDmgr(dmgr)
	productTypes, err := hdm.manager.GetDmgr().GetAllProductType()
	if err != nil {
		return err
	}
	common.ParamOption.ProductTypes = productTypes
	if err = common.CheckCardUsageMode(common.ParamOption.Use310PMixedInsert, productTypes); err != nil {
		return err
	}
	return hdm.UpdateServerType()
}

// UpdateServerType update server type, like Ascend910-32
func (hdm *HwDevManager) UpdateServerType() error {
	if common.ParamOption.BuildScene == common.EdgeScene {
		return nil
	}
	kubeClient, err := kubeclient.NewClientK8s()
	if err != nil {

		return err
	}
	hdm.manager.SetKubeClient(kubeClient)

	aiCoreCount, err := hdm.manager.GetChipAiCoreCount()
	if err != nil {

		return err
	}
	common.ParamOption.AiCoreCount = aiCoreCount
	return hdm.updateNodeServerType(aiCoreCount)

}

func (hdm *HwDevManager) updateNodeServerType(aiCoreCount int32) error {
	oldNode, err := hdm.manager.GetKubeClient().GetNode()
	if err != nil {

		return err
	}
	if oldNode == nil {

		return fmt.Errorf("invalid node")
	}
	if _, ok := oldNode.Labels[common.ServerTypeLabelKey]; ok {
		return nil
	}
	newNode := oldNode.DeepCopy()
	newNode.Labels[common.ServerTypeLabelKey] = common.ParamOption.RealCardType +
		common.MiddelLine + strconv.Itoa(int(aiCoreCount))
	for i := 0; i < common.RetryUpdateCount; i++ {
		if _, _, err = hdm.manager.GetKubeClient().PatchNodeState(oldNode, newNode); err == nil {

			return nil
		}

		time.Sleep(time.Second)
	}
	return fmt.Errorf("update server type to node label failed")
}

func (hdm *HwDevManager) setAllDeviceAndType() error {
	var err error
	if hdm.AllInfo, err = hdm.manager.GetNPUs(); err != nil {
		return err
	}
	if len(hdm.AllInfo.AllDevTypes) == 0 {
		return fmt.Errorf("no devices type found")
	}
	return nil
}

func (hdm *HwDevManager) initPluginServer() error {
	hdm.ServerMap = make(map[string]InterfaceServer, len(hdm.AllInfo.AllDevTypes))
	hdm.groupDevice = device.ClassifyDevices(hdm.AllInfo.AllDevs, hdm.AllInfo.AllDevTypes)
	defaultDevices, err := common.GetDefaultDevices(common.ParamOption.GetFdFlag)
	if err != nil {

		return err
	}
	if !common.ParamOption.PresetVDevice {
		hdm.ServerMap[common.AiCoreResourceName] = NewPluginServer(common.AiCoreResourceName,
			hdm.AllInfo.AICoreDevs, defaultDevices, hdm.manager)
		return nil
	}
	for _, deviceType := range hdm.AllInfo.AllDevTypes {
		hdm.ServerMap[deviceType] = NewPluginServer(deviceType, hdm.groupDevice[deviceType], defaultDevices,
			hdm.manager)
	}
	return nil
}

// GetNPUs will set device default health, actually, it should be based on the last status if exist
func (hdm *HwDevManager) updateDeviceHealth(curAllDevs []common.NpuDevice) {
	lastAllDevs := make(map[string]int, len(hdm.AllInfo.AllDevs))
	for index, dev := range hdm.AllInfo.AllDevs {
		lastAllDevs[dev.DeviceName] = index
	}
	for i, dev := range curAllDevs {
		if index, exist := lastAllDevs[dev.DeviceName]; exist && index < len(hdm.AllInfo.AllDevs) {
			curAllDevs[i].Health = hdm.AllInfo.AllDevs[index].Health
			curAllDevs[i].NetworkHealth = hdm.AllInfo.AllDevs[index].NetworkHealth
		}
	}
}

func (hdm *HwDevManager) updateDevice() error {
	if common.ParamOption.PresetVDevice {
		return nil
	}
	element, exist := hdm.ServerMap[common.AiCoreResourceName]
	if !exist {
		return fmt.Errorf("not found %s plugin server", common.AiCoreResourceName)
	}
	pluginServer, ok := element.(*PluginServer)
	if !ok {
		return fmt.Errorf("serverMap convert %s failed", common.AiCoreResourceName)
	}
	err := pluginServer.DestroyNotUsedVNPU()
	if err != nil {
		return err
	}
	allInfo, err := hdm.manager.GetNPUs()
	if err != nil {
		return err
	}
	if err := hdm.manager.CheckDeviceTypeLabel(); err != nil {

	}
	hdm.updateDeviceHealth(allInfo.AllDevs)
	hdm.groupDevice = device.ClassifyDevices(allInfo.AllDevs, allInfo.AllDevTypes)
	hdm.AllInfo = allInfo
	return nil
}

func (hdm *HwDevManager) pluginNotify(classifyDev []*common.NpuDevice, devType string) {
	serverMap, ok := hdm.ServerMap[devType]
	if !ok {

		return
	}
	pluginServer, ok := serverMap.(*PluginServer)
	if !ok {

		return
	}
	if !pluginServer.Notify(classifyDev) {

	}
}

func (hdm *HwDevManager) notifyToK8s() {
	isDevStateChange := hdm.manager.IsDeviceStatusChange(hdm.groupDevice, hdm.AllInfo.AICoreDevs, hdm.RunMode)
	for devType, isChanged := range isDevStateChange {
		if !isChanged {
			continue
		}
		if !common.ParamOption.PresetVDevice {
			hdm.pluginNotify(hdm.AllInfo.AICoreDevs, common.AiCoreResourceName)
			return
		}
		hdm.pluginNotify(hdm.groupDevice[devType], devType)
	}
}

func (hdm *HwDevManager) chipHotReset() {
	if hdm.RunMode == common.Ascend910 {

		return
	}
	if common.ParamOption.HotReset != common.HotResetInfer {

		return
	}
	for devType, devices := range hdm.groupDevice {
		if common.IsVirtualDev(devType) || len(devices) == 0 {
			continue
		}
		if common.IsContainAtlas300IDuo() {
			continue
		}
	}
}

func (hdm *HwDevManager) isDuoCardChipHealthy(deviceChip []*common.NpuDevice) bool {
	return true
}

func (hdm *HwDevManager) useVolcanoNotify() {
	if common.ParamOption.BuildScene == common.EdgeScene {
		return
	}
	if hdm.manager.GetKubeClient() == nil {

		return
	}
	common.DpStartReset.Do(func() {
		if err := hdm.manager.GetKubeClient().AnnotationReset(); err != nil {

		}
	})
	if err := hdm.updatePodAnnotation(); err != nil {

	}
	if !common.ParamOption.UseVolcanoType {
		return
	}
	hdm.manager.DoWithVolcanoListAndWatch(hdm.groupDevice)
}

// SignCatch stop system sign catch
func (hdm *HwDevManager) SignCatch(cancel context.CancelFunc) {
	osSignChan := common.NewSignWatcher(syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL)
	if osSignChan == nil {

		return
	}
	select {
	case _, signEnd := <-osSignChan:
		if signEnd == false {

			return
		}

		cancel()
		hdm.stopAllSever()
		hdm.manager.GetDmgr().ShutDown()
	}
}

func (hdm *HwDevManager) stopAllSever() {
	for deviceType := range hdm.ServerMap {

		hdm.ServerMap[deviceType].Stop()
	}

}

func (hdm *HwDevManager) setRestartForAll() {
	for deviceType := range hdm.ServerMap {
		hdm.ServerMap[deviceType].SetRestartFlag(true)
	}
}

func (hdm *HwDevManager) startAllServer(socketWatcher *common.FileWatch) bool {
	success := true
	for _, serverInterface := range hdm.ServerMap {
		if !serverInterface.GetRestartFlag() {
			continue
		}
		if err := serverInterface.Start(socketWatcher); err != nil {
			success = false
		} else {
			serverInterface.SetRestartFlag(false)
		}
	}
	return success
}

func (hdm *HwDevManager) handleDeleteEvent(deleteFile string) {
	for deviceType := range hdm.ServerMap {
		candidateSocketFilename := fmt.Sprintf("%s.sock", deviceType)
		if candidateSocketFilename == deleteFile {

		}
	}
}

func (hdm *HwDevManager) updatePodAnnotation() error {
	serverID, err := hdm.manager.GetKubeClient().GetNodeServerID()
	if err != nil {
		return fmt.Errorf("get node server id failed: %#v", err)
	}
	if !common.ParamOption.PresetVDevice {
		return hdm.updateSpecTypePodAnnotation(common.AiCoreResourceName, serverID)
	}
	for _, devType := range hdm.AllInfo.AllDevTypes {
		// for 310P vnpu no need update
		if common.IsVirtualDev(devType) && !strings.HasPrefix(devType, common.Ascend910) {
			continue
		}
		if err := hdm.updateSpecTypePodAnnotation(devType, serverID); err != nil {

		}
	}
	return nil
}

func (hdm *HwDevManager) updateSpecTypePodAnnotation(deviceType, serverID string) error {
	element, exist := hdm.ServerMap[deviceType]
	if !exist {
		return fmt.Errorf("not found %s plugin server", deviceType)
	}
	pluginServer, ok := element.(*PluginServer)
	if !ok {
		return fmt.Errorf("serverMap convert %s failed", deviceType)
	}
	podList, err := hdm.manager.GetKubeClient().GetActivePodList()
	if err != nil {
		return err
	}
	podDeviceInfo, err := pluginServer.GetKltAndRealAllocateDev(podList)
	if err != nil {
		return err
	}
	for _, deviceInfo := range podDeviceInfo {

		_, existDeviceKey := deviceInfo.Pod.Annotations[common.Pod910DeviceKey]
		_, existRealAlloc := deviceInfo.Pod.Annotations[common.ResourceNamePrefix+common.PodRealAlloc]
		if existDeviceKey || existRealAlloc {
			continue
		}
		if len(deviceInfo.KltDevice) == 0 || len(deviceInfo.RealDevice) == 0 {
			continue
		}

		if err := hdm.manager.AddPodAnnotation(&deviceInfo.Pod, deviceInfo.KltDevice, deviceInfo.RealDevice,
			deviceType, serverID); err != nil {
		} else {

		}
	}
	return nil
}

func (hdm *HwDevManager) hotReset(device *common.NpuDevice) {
	var isResetExec = false
	if err := wait.PollImmediate(time.Second, time.Minute, func() (bool, error) {
		if err := hdm.execResetChip(device.LogicID, &isResetExec); err != nil {

			return false, err
		}
		bootState, err := hdm.manager.GetDmgr().GetDeviceBootStatus(device.LogicID)
		if err != nil {

			return false, err
		}
		if bootState != common.BootStartFinish {

			return false, nil
		}
		return true, nil
	}); err != nil {

		return
	}

}
func (hdm *HwDevManager) execResetChip(logicID int32, isResetExec *bool) error {
	if *isResetExec {
		return nil
	}
	cardID, deviceID, err := hdm.manager.GetDmgr().GetCardIDDeviceID(logicID)
	if err != nil {

		return err
	}
	if common.IsContainAtlas300IDuo() {
		deviceID = 0
	}

	if err := hdm.manager.GetDmgr().SetDeviceReset(cardID, deviceID); err != nil {

		return err
	}
	*isResetExec = true

	return nil
}
