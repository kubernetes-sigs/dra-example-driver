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

// Package server holds the implementation of registration to kubelet, k8s device plugin interface and grpc service.
package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"huawei.com/npu-exporter/v5/common-utils/hwlog"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/dra-example-driver/pkg/common"
	"sigs.k8s.io/dra-example-driver/pkg/device"
)

func (ps *PluginServer) stopListAndWatch() {
	if ps.isRunning.Load() {
		ps.stop <- struct{}{}
	}
}

// Notify is called when device status changed, to notify ListAndWatch
func (ps *PluginServer) Notify(devices []*common.NpuDevice) bool {
	if ps == nil {
		hwlog.RunLog.Error("invalid interface receiver")
		return false
	}
	if ps.isRunning.Load() {
		ps.deepCopyDevice(devices)
		ps.reciChan <- struct{}{}
		return true
	}
	return false
}

// getUnhealthyAICore
// for example:
// aicore-0, aicore-22: Ascend310P-2c-100-0
// if Ascend310P-0 is unhealthy, aicore-0, aicore-22 is unhealthy
// Ascend310P-0 has 8 aicore, and should select 6 free aicore to be set unhealthy
func (ps *PluginServer) getUnhealthyAICore() sets.String {
	unhealthyAICore := sets.String{}
	return unhealthyAICore
}

func (ps *PluginServer) generateAllDeviceMap() map[string]string {
	vol2kltMap := make(map[string]string, 1)
	var notInVolDev []string
	allDev := sets.String{}
	klDev := sets.String{}
	ps.allocMapLock.RLock()
	ps.cachedLock.RLock()
	vol2KlDevMap := make(map[string]string, len(ps.klt2RealDevMap))
	for k, r := range ps.klt2RealDevMap {
		vol2KlDevMap[r] = k
	}
	for _, dev := range ps.cachedDevices {
		allDev.Insert(dev.DeviceName)
		d, exist := vol2KlDevMap[dev.DeviceName]
		if !exist {
			notInVolDev = append(notInVolDev, dev.DeviceName)
			continue
		}
		klDev.Insert(d)
		vol2kltMap[dev.DeviceName] = d
	}
	ps.allocMapLock.RUnlock()
	ps.cachedLock.RUnlock()
	notInKlDev := allDev.Difference(klDev).List()
	for index, d := range notInKlDev {
		if index >= len(notInVolDev) {
			hwlog.RunLog.Warnf("found volcano not using device %s in notInVolDev on local %d failed", d, index)
			continue
		}
		vol := notInVolDev[index]
		vol2kltMap[vol] = d
	}
	return vol2kltMap
}

func (ps *PluginServer) deepCopyDevice(cachedDevices []*common.NpuDevice) {
	ps.cachedLock.Lock()
	ps.cachedDevices = ps.cachedDevices[:0]
	for _, dev := range cachedDevices {
		ps.cachedDevices = append(ps.cachedDevices, common.NpuDevice{
			DeviceName: dev.DeviceName,
			Health:     dev.Health,
			PhyID:      dev.PhyID,
		})
	}
	ps.cachedLock.Unlock()
}

func (ps *PluginServer) deviceExists(id string) bool {
	ps.cachedLock.RLock()
	defer ps.cachedLock.RUnlock()
	for _, d := range ps.cachedDevices {
		if d.DeviceName == id {
			return true
		}
	}
	return false
}

func getPredicateTimeFromPodAnnotation(pod *v1.Pod) uint64 {
	assumeTimeStr, ok := pod.Annotations[common.PodPredicateTime]
	if !ok {
		hwlog.RunLog.Warnf("volcano not write timestamp, pod Name: %s", pod.Name)
		return math.MaxUint64
	}
	if len(assumeTimeStr) > common.PodAnnotationMaxMemory {
		hwlog.RunLog.Warnf("timestamp fmt invalid, pod Name: %s", pod.Name)
		return math.MaxUint64
	}
	predicateTime, err := strconv.ParseUint(assumeTimeStr, common.BaseDec, common.BitSize)
	if err != nil {
		hwlog.RunLog.Errorf("parse timestamp failed, %#v", err)
		return math.MaxUint64
	}
	return predicateTime
}

func (ps *PluginServer) getOldestPod(pods []v1.Pod) *v1.Pod {
	if len(pods) == 0 {
		return nil
	}
	oldest := pods[0]
	for _, pod := range pods {
		hwlog.RunLog.Debugf("pod %s, predicate time: %s", oldest.Name, pod.Annotations[common.PodPredicateTime])
		if getPredicateTimeFromPodAnnotation(&oldest) > getPredicateTimeFromPodAnnotation(&pod) {
			oldest = pod
		}
	}
	hwlog.RunLog.Debugf("oldest pod %#v, predicate time: %#v", oldest.Name,
		oldest.Annotations[common.PodPredicateTime])
	annotation := map[string]string{common.PodPredicateTime: strconv.FormatUint(math.MaxUint64, common.BaseDec)}
	if err := ps.manager.GetKubeClient().TryUpdatePodAnnotation(&oldest, annotation); err != nil {
		hwlog.RunLog.Errorf("update pod %s failed, err: %#v", oldest.Name, err)
		return nil
	}
	return &oldest
}

func (ps *PluginServer) updateAllocMap(realAlloc, kltAlloc []string) {
	if common.ParamOption.PresetVDevice {
		ps.updatePresetAllocMap(realAlloc, kltAlloc)
	} else {
		ps.updateDynamicAllocMap(realAlloc, kltAlloc)
	}
}

func (ps *PluginServer) updateDynamicAllocMap(realAlloc, kltAlloc []string) {
	// real device exist, delete
	if len(realAlloc) == 0 {
		hwlog.RunLog.Warn("not allocate any device")
		return
	}
	// delete klt allocate device in key
	for _, id := range kltAlloc {
		if _, exist := ps.klt2RealDevMap[id]; exist {
			delete(ps.klt2RealDevMap, id)
		}
	}
	// delete real allocate device in value
	for _, id := range realAlloc {
		for k, v := range ps.klt2RealDevMap {
			if v == id {
				delete(ps.klt2RealDevMap, k)
			}
		}
	}
	isVirtualDev := common.IsVirtualDev(realAlloc[0])
	if isVirtualDev && len(realAlloc) > 1 {
		hwlog.RunLog.Warnf("virtual device only support allocate one, %v", realAlloc)
		return
	}
	// for virtual device, N ai core : 1 real device
	// aicore-0, aicore-1 : Ascend910-2c-100-0
	if isVirtualDev {
		for _, id := range kltAlloc {
			ps.klt2RealDevMap[id] = realAlloc[0]
		}
		return
	}
	// for physical device, M ai core : N real device
	// aicore-0,..., aicore-31 : Ascend910-0
	// aicore-32,..., aicore-63 : Ascend910-1
	chipAICore := ps.manager.GetChipAICore()
	if int(chipAICore)*len(realAlloc) != len(kltAlloc) {
		hwlog.RunLog.Warnf("klt allocate core not equal real allocate %v", realAlloc)
		return
	}
	realIdx := 0
	for kltIdx, id := range kltAlloc {
		ps.klt2RealDevMap[id] = realAlloc[realIdx]
		if ((kltIdx + 1) % int(chipAICore)) == 0 {
			realIdx++
		}
	}
}

func (ps *PluginServer) updatePresetAllocMap(realAlloc, kltAlloc []string) {
	if len(realAlloc) != len(kltAlloc) {
		hwlog.RunLog.Error("number of devices of klt allocate not equal real allocate")
		return
	}
	ps.allocMapLock.Lock()
	for _, id := range kltAlloc {
		if _, exist := ps.klt2RealDevMap[id]; exist {
			delete(ps.klt2RealDevMap, id)
		}
	}
	for i, id := range kltAlloc {
		ps.klt2RealDevMap[id] = realAlloc[i]
	}
	ps.allocMapLock.Unlock()
}

// GetRealAllocateDevices is convert kubelet allocate device list to volcano allocate device list
func (ps *PluginServer) GetRealAllocateDevices(kltAllocate []string) ([]string, error) {
	if ps == nil {
		return nil, fmt.Errorf("invalid interface receiver")
	}
	ps.allocMapLock.RLock()
	defer ps.allocMapLock.RUnlock()
	realAllocate := sets.String{}
	if !common.ParamOption.UseVolcanoType {
		return kltAllocate, nil
	}
	for _, id := range kltAllocate {
		realID, exist := ps.klt2RealDevMap[id]
		if !exist {
			return nil, fmt.Errorf("cannot found real allocate device by %s", id)
		}
		realAllocate.Insert(realID)
	}
	return realAllocate.List(), nil
}

// GetKltAndRealAllocateDev get kubelet and real allocate device of pod
func (ps *PluginServer) GetKltAndRealAllocateDev(podList []v1.Pod) ([]PodDeviceInfo, error) {
	var podDeviceInfo []PodDeviceInfo
	return podDeviceInfo, nil
}

// DestroyNotUsedVNPU destroy not used virtual device
func (ps *PluginServer) DestroyNotUsedVNPU() error {
	allDevInfo, err := ps.manager.GetNPUs()
	if err != nil {
		return err
	}
	podList, err := ps.manager.GetKubeClient().GetAllPodList()
	if err != nil {
		return err
	}
	podDeviceInfo, err := ps.GetKltAndRealAllocateDev(podList.Items)
	if err != nil {
		return err
	}
	usedDevice := ps.removeVGroup(podDeviceInfo)
	var needToDestroy []string
	for _, dev := range allDevInfo.AllDevs {
		if !usedDevice.Has(dev.DeviceName) {
			needToDestroy = append(needToDestroy, dev.DeviceName)
		}
	}
	for _, dev := range needToDestroy {
		if !common.IsVirtualDev(dev) {
			continue
		}
		if err = ps.manager.DestroyVirtualDevice(dev); err == nil {
			hwlog.RunLog.Infof("destroy virtual device %s success", dev)
		} else {
			hwlog.RunLog.Infof("destroy virtual device %s failed, %v", dev, err)
		}
	}
	return nil
}

func (ps *PluginServer) removeVGroup(podDeviceInfo []PodDeviceInfo) sets.String {
	usedDevice := sets.String{}
	for _, deviceInfo := range podDeviceInfo {
		usedDevice.Insert(deviceInfo.RealDevice...)
	}
	noVGroupDevice := sets.String{}
	for dev := range usedDevice {
		vDevAndGroup := strings.Split(dev, common.UnderLine)
		if len(vDevAndGroup) == 1 || len(vDevAndGroup) == common.VGroupAndDevLen {
			noVGroupDevice.Insert(vDevAndGroup[0])
		}
	}
	return noVGroupDevice
}

func checkAnnotationAllocateValid(requestDevices []string, deviceType string, pod *v1.Pod, chipAICore int32) bool {
	if common.ParamOption.PresetVDevice {
		allocateDevice, err := common.GetDeviceFromPodAnnotation(pod, deviceType)
		if err != nil {
			return false
		}
		return len(allocateDevice) == len(requestDevices)
	}
	// for dynamic segment
	annotation, err := common.GetPodAnnotationByDeviceType(pod, deviceType)
	if err != nil {
		hwlog.RunLog.Warn(err)
		return false
	}
	deviceInfos := strings.Split(annotation, common.MiddelLine)
	// for vnpu, like huawei.com/npu-core:0-vir02
	if len(deviceInfos) > 1 {
		_, template, err := common.GetVNPUSegmentInfo(deviceInfos)
		if err != nil {
			hwlog.RunLog.Warn(err)
			return false
		}
		aiCore, err := common.GetAICore(template)
		if err != nil {
			hwlog.RunLog.Warn(err)
			return false
		}
		return len(requestDevices) == aiCore
	}
	// for physical npu, huawei.com/npu-core:0,1,2,3
	phyDevices := strings.Split(deviceInfos[0], common.CommaSepDev)
	return len(requestDevices) == len(phyDevices)*int(chipAICore)
}

// getAICoreFromPodAnnotation get ai core count from pod annotation
// Annotation
// huawei.com/npu-core:0,1,2,3
// huawei.com/npu-core:0-vir02
func (ps *PluginServer) getAICoreFromPodAnnotation(pod *v1.Pod, deviceType string) ([]string, error) {
	if err := ps.DestroyNotUsedVNPU(); err != nil {
		return nil, err
	}
	annotation, err := common.GetPodAnnotationByDeviceType(pod, deviceType)
	if err != nil {
		return nil, err
	}
	deviceInfos := strings.Split(annotation, common.MiddelLine)
	if len(deviceInfos) > 1 {
		phyID, templateName, err := common.GetVNPUSegmentInfo(deviceInfos)
		if err != nil {
			return nil, err
		}
		deviceName, err := ps.manager.CreateVirtualDevice(phyID, templateName)
		if err != nil {
			return nil, err
		}
		ps.ascendRuntimeOptions = common.VirtualDev
		// like Ascend910-2c-100-0
		return []string{deviceName}, nil
	}
	ps.ascendRuntimeOptions = ""
	var phyDevs []string
	ids := strings.Split(deviceInfos[0], common.CommaSepDev)
	for _, id := range ids {
		phyDevs = append(phyDevs, fmt.Sprintf("%s-%s", ps.manager.GetName(), id))
	}
	inValidIDList := ps.isValidRequestID(ids)
	if len(inValidIDList) != 0 {
		hwlog.RunLog.Errorf("volcano allocated id %s is invalid", inValidIDList)
		return nil, fmt.Errorf(common.NoNPUResource)
	}
	// like Ascend910-0,Ascend910-1,Ascend910-2,Ascend910-3
	return phyDevs, nil
}

func (ps *PluginServer) isValidRequestID(phyDevs []string) []string {
	var inValidIDList []string
	for _, phyID := range phyDevs {
		if ps.isValidPhyID(phyID) {
			continue
		}
		inValidIDList = append(inValidIDList, phyID)
	}
	return inValidIDList
}

func (ps *PluginServer) isValidPhyID(phyID string) bool {
	for _, cacheDev := range ps.cachedDevices {
		if phyID == strconv.Itoa(int(cacheDev.PhyID)) {
			return true
		}
	}
	return false
}

func (ps *PluginServer) doWithVolcanoSchedule(requestDevices []string) ([]string, error) {
	conditionFunc := func(pod *v1.Pod) bool {
		return checkAnnotationAllocateValid(requestDevices, ps.deviceType, pod, ps.manager.GetChipAICore())
	}
	allPods, err := ps.manager.GetKubeClient().GetActivePodList()
	if err != nil {
		return nil, err
	}
	pods := common.FilterPods(allPods, ps.deviceType, conditionFunc)
	oldestPod := ps.getOldestPod(pods)
	if oldestPod == nil {
		return nil, fmt.Errorf("not get valid pod")
	}
	var allocateDevices []string
	if !common.ParamOption.PresetVDevice {
		common.LockAllDeviceInfo()
		allocateDevices, err = ps.getAICoreFromPodAnnotation(oldestPod, ps.deviceType)
		common.UnlockAllDeviceInfo()
	} else {
		allocateDevices, err = common.GetDeviceFromPodAnnotation(oldestPod, ps.deviceType)
	}
	if err != nil {
		return nil, err
	}
	hwlog.RunLog.Infof("vol found: %#v", allocateDevices)
	ps.updateAllocMap(allocateDevices, requestDevices)
	return allocateDevices, nil
}

func (ps *PluginServer) useVolcano(requestDevices []string) ([]string, error) {
	// if virtual device, allocate by k8s
	if common.IsVirtualDev(ps.deviceType) {
		return requestDevices, nil
	}
	return ps.doWithVolcanoSchedule(requestDevices)
}

func getDevPath(id, ascendRuntimeOptions string) (string, string) {
	containerPath := fmt.Sprintf("%s%s", "/dev/davinci", id)
	hostPath := containerPath
	if ascendRuntimeOptions == common.VirtualDev {
		hostPath = fmt.Sprintf("%s%s", "/dev/vdavinci", id)
	}
	return containerPath, hostPath
}

// NewPluginServer returns an initialized PluginServer
func NewPluginServer(deviceType string, devices []*common.NpuDevice, defaultDevs []string,
	manager device.DevManager) *PluginServer {
	ps := &PluginServer{
		restart:        true,
		reciChan:       make(chan interface{}),
		deviceType:     deviceType,
		defaultDevs:    defaultDevs,
		stop:           make(chan interface{}),
		klt2RealDevMap: make(map[string]string, common.MaxDevicesNum),
		isRunning:      common.NewAtomicBool(false),
		manager:        manager,
	}
	ps.deepCopyDevice(devices)
	return ps
}
