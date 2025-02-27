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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/fsnotify/fsnotify"

	"k8s.io/api/core/v1"
)

// GetPattern return pattern map
func GetPattern() map[string]string {
	return map[string]string{
		"nodeName":    `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`,
		"podName":     "^[a-z0-9]+[a-z0-9\\-]*[a-z0-9]+$",
		"fullPodName": "^[a-z0-9]+([a-z0-9\\-.]*)[a-z0-9]+$",
		"vir910":      "Ascend910-(2|4|8|16)c",
		"vir310p":     "Ascend310P-(1|2|4)c",
		"ascend910":   `^Ascend910-\d+`,
	}
}

var (
	allDeviceInfoLock sync.Mutex
)

// LockAllDeviceInfo lock for device info status
func LockAllDeviceInfo() {
	allDeviceInfoLock.Lock()
}

// UnlockAllDeviceInfo unlock for device info status
func UnlockAllDeviceInfo() {
	allDeviceInfoLock.Unlock()
}

// MakeDataHash Make Data Hash
func MakeDataHash(data interface{}) string {
	var dataBuffer []byte
	if dataBuffer = MarshalData(data); len(dataBuffer) == 0 {
		return ""
	}
	h := sha256.New()
	if _, err := h.Write(dataBuffer); err != nil {

		return ""
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

// MarshalData marshal data to bytes
func MarshalData(data interface{}) []byte {
	dataBuffer, err := json.Marshal(data)
	if err != nil {

		return nil
	}
	return dataBuffer
}

// MapDeepCopy map deep copy
func MapDeepCopy(source map[string]string) map[string]string {
	dest := make(map[string]string, len(source))
	if source == nil {
		return dest
	}
	for key, value := range source {
		dest[key] = value
	}
	return dest
}

// GetPodAnnotationByDeviceType get pod annotation by device type
func GetPodAnnotationByDeviceType(pod *v1.Pod, deviceType string) (string, error) {
	if pod == nil {
		return "", fmt.Errorf("invalid pod")
	}
	annotationTag := fmt.Sprintf("%s%s", ResourceNamePrefix, deviceType)
	annotation, exist := pod.Annotations[annotationTag]
	if !exist {
		return "", fmt.Errorf("cannot find the annotation")
	}
	if len(annotation) > PodAnnotationMaxMemory {
		return "", fmt.Errorf("pod annotation size out of memory")
	}
	return annotation, nil
}

// GetDeviceFromPodAnnotation get devices from pod annotation
func GetDeviceFromPodAnnotation(pod *v1.Pod, deviceType string) ([]string, error) {
	annotation, err := GetPodAnnotationByDeviceType(pod, deviceType)
	if err != nil {
		return nil, err
	}
	return strings.Split(annotation, CommaSepDev), nil
}

func setDeviceByPathWhen200RC(defaultDevices *[]string) {
	setDeviceByPath(defaultDevices, HiAi200RCEventSched)
	setDeviceByPath(defaultDevices, HiAi200RCHiDvpp)
	setDeviceByPath(defaultDevices, HiAi200RCLog)
	setDeviceByPath(defaultDevices, HiAi200RCMemoryBandwidth)
	setDeviceByPath(defaultDevices, HiAi200RCSVM0)
	setDeviceByPath(defaultDevices, HiAi200RCTsAisle)
	setDeviceByPath(defaultDevices, HiAi200RCUpgrade)
}

func setDeviceByPath(defaultDevices *[]string, device string) {
	if _, err := os.Stat(device); err == nil {
		*defaultDevices = append(*defaultDevices, device)
	}
}

// GetDefaultDevices get default device, for allocate mount
func GetDefaultDevices(getFdFlag bool) ([]string, error) {
	// hiAIManagerDevice is required
	if _, err := os.Stat(HiAIManagerDevice); err != nil {
		return nil, err
	}
	var defaultDevices []string
	defaultDevices = append(defaultDevices, HiAIManagerDevice)

	setDeviceByPath(&defaultDevices, HiAIHDCDevice)
	setDeviceByPath(&defaultDevices, HiAISVMDevice)
	if getFdFlag {
		setDeviceByPathWhen200RC(&defaultDevices)
	}

	var productType string
	if len(ParamOption.ProductTypes) == 1 {
		productType = ParamOption.ProductTypes[0]
	}
	if productType == Atlas200ISoc {
		socDefaultDevices, err := set200SocDefaultDevices()
		if err != nil {

			return nil, err
		}
		defaultDevices = append(defaultDevices, socDefaultDevices...)
	}
	if ParamOption.RealCardType == Ascend310B {
		a310BDefaultDevices := set310BDefaultDevices()
		defaultDevices = append(defaultDevices, a310BDefaultDevices...)
	}
	return defaultDevices, nil
}

// set200SocDefaultDevices set 200 soc defaults devices
func set200SocDefaultDevices() ([]string, error) {
	var socDefaultDevices = []string{
		Atlas200ISocVPC,
		Atlas200ISocVDEC,
		Atlas200ISocSYS,
		Atlas200ISocSpiSmbus,
		Atlas200ISocUserConfig,
		HiAi200RCTsAisle,
		HiAi200RCSVM0,
		HiAi200RCLog,
		HiAi200RCMemoryBandwidth,
		HiAi200RCUpgrade,
	}
	for _, devPath := range socDefaultDevices {
		if _, err := os.Stat(devPath); err != nil {
			return nil, err
		}
	}
	var socOptionsDevices = []string{
		HiAi200RCEventSched,
		Atlas200ISocXSMEM,
	}
	for _, devPath := range socOptionsDevices {
		if _, err := os.Stat(devPath); err != nil {

			continue
		}
		socDefaultDevices = append(socDefaultDevices, devPath)
	}
	return socDefaultDevices, nil
}

func set310BDefaultDevices() []string {
	var a310BDefaultDevices = []string{
		Atlas310BDvppCmdlist,
		Atlas310BPngd,
		Atlas310BVenc,
		HiAi200RCUpgrade,
		Atlas200ISocSYS,
		HiAi200RCSVM0,
		Atlas200ISocVDEC,
		Atlas200ISocVPC,
		HiAi200RCTsAisle,
		HiAi200RCLog,
	}
	var available310BDevices []string
	for _, devPath := range a310BDefaultDevices {
		if _, err := os.Stat(devPath); err != nil {

			continue
		}
		available310BDevices = append(available310BDevices, devPath)
	}
	return available310BDevices
}

func getNPUResourceNumOfPod(pod *v1.Pod, deviceType string) int64 {
	containers := pod.Spec.Containers
	if len(containers) > MaxContainerLimit {

		return int64(0)
	}
	var total int64
	annotationTag := fmt.Sprintf("%s%s", ResourceNamePrefix, deviceType)
	for _, container := range containers {
		val, ok := container.Resources.Limits[v1.ResourceName(annotationTag)]
		if !ok {
			continue
		}
		limitsDevNum := val.Value()
		if limitsDevNum < 0 || limitsDevNum > int64(MaxDevicesNum*MaxAICoreNum) {

			return int64(0)
		}
		total += limitsDevNum
	}
	return total
}

func isAscendAssignedPod(pod *v1.Pod, deviceType string) bool {
	if IsVirtualDev(deviceType) {
		return true
	}
	annotationTag := fmt.Sprintf("%s%s", ResourceNamePrefix, deviceType)
	if _, ok := pod.ObjectMeta.Annotations[annotationTag]; !ok {

		return false
	}
	return true
}

func isShouldDeletePod(pod *v1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return true
	}
	if len(pod.Status.ContainerStatuses) > MaxContainerLimit {

		return true
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil &&
			strings.Contains(status.State.Waiting.Message, "PreStartContainer check failed") {
			return true
		}
	}
	return pod.Status.Reason == "UnexpectedAdmissionError"
}

// FilterPods get pods which meet the conditions
func FilterPods(pods []v1.Pod, deviceType string, conditionFunc func(pod *v1.Pod) bool) []v1.Pod {
	var res []v1.Pod
	for _, pod := range pods {

		if getNPUResourceNumOfPod(&pod, deviceType) == 0 || !isAscendAssignedPod(&pod,
			deviceType) || isShouldDeletePod(&pod) {
			continue
		}
		if conditionFunc != nil && !conditionFunc(&pod) {
			continue
		}
		res = append(res, pod)
	}
	return res
}

// VerifyPathAndPermission used to verify the validity of the path and permission and return resolved absolute path
func VerifyPathAndPermission(verifyPath string) (string, bool) {

	absVerifyPath, err := filepath.Abs(verifyPath)
	if err != nil {

		return "", false
	}
	pathInfo, err := os.Stat(absVerifyPath)
	if err != nil {

		return "", false
	}
	realPath, err := filepath.EvalSymlinks(absVerifyPath)
	if err != nil || absVerifyPath != realPath {

		return "", false
	}
	stat, ok := pathInfo.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != RootUID || stat.Gid != RootGID {

		return "", false
	}
	return realPath, true
}

// CheckPodNameAndSpace used to check pod name or pod namespace
func CheckPodNameAndSpace(podPara string, maxLength int) error {
	if len(podPara) > maxLength {
		return fmt.Errorf("para length %d is bigger than %d", len(podPara), maxLength)
	}
	patternMap := GetPattern()
	pattern := patternMap["podName"]
	if maxLength == PodNameMaxLength {
		pattern = patternMap["fullPodName"]
	}

	if match, err := regexp.MatchString(pattern, podPara); !match || err != nil {
		return fmt.Errorf("podPara is illegal")
	}
	return nil
}

// NewFileWatch is used to watch socket file
func NewFileWatch() (*FileWatch, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &FileWatch{FileWatcher: watcher}, nil
}

// WatchFile add file to watch
func (fw *FileWatch) WatchFile(fileName string) error {
	if _, err := os.Stat(fileName); err != nil {
		return err
	}
	return fw.FileWatcher.Add(fileName)
}

// NewSignWatcher new sign watcher
func NewSignWatcher(osSigns ...os.Signal) chan os.Signal {
	// create signs chan
	signChan := make(chan os.Signal, 1)
	for _, sign := range osSigns {
		signal.Notify(signChan, sign)
	}
	return signChan
}

// GetPodConfiguration get annotation configuration of pod
func GetPodConfiguration(phyDevMapVirtualDev map[int]int, devices map[int]string, podName,
	serverID string, deviceType string) string {
	var sortDevicesKey []int
	for deviceID := range devices {
		sortDevicesKey = append(sortDevicesKey, deviceID)
	}
	sort.Ints(sortDevicesKey)
	instance := Instance{PodName: podName, ServerID: serverID}
	for _, deviceID := range sortDevicesKey {
		if !IsVirtualDev(deviceType) {
			instance.Devices = append(instance.Devices, Device{
				DeviceID: fmt.Sprintf("%d", deviceID),
				DeviceIP: devices[deviceID],
			})
			continue
		}
		phyID, exist := phyDevMapVirtualDev[deviceID]
		if !exist {

			continue
		}
		instance.Devices = append(instance.Devices, Device{
			DeviceID: fmt.Sprintf("%d", phyID),
			DeviceIP: devices[deviceID],
		})
	}
	instanceByte, err := json.Marshal(instance)
	if err != nil {

		return ""
	}
	return string(instanceByte)
}

// CheckFileUserSameWithProcess to check whether the owner of the log file is the same as the uid
func CheckFileUserSameWithProcess(loggerPath string) bool {
	curUid := os.Getuid()
	if curUid == RootUID {
		return true
	}
	pathInfo, err := os.Lstat(loggerPath)
	if err != nil {
		path := filepath.Dir(loggerPath)
		pathInfo, err = os.Lstat(path)
		if err != nil {
			fmt.Printf("get logger path stat failed, error is %#v\n", err)
			return false
		}
	}
	stat, ok := pathInfo.Sys().(*syscall.Stat_t)
	if !ok {
		fmt.Printf("get logger file stat failed\n")
		return false
	}
	if int(stat.Uid) != curUid || int(stat.Gid) != curUid {
		fmt.Printf("check log file failed, owner not right\n")
		return false
	}
	return true
}

// IsContainAtlas300IDuo in ProductTypes list, is contain Atlas 300I Duo card
func IsContainAtlas300IDuo() bool {
	for _, product := range ParamOption.ProductTypes {
		if product == Atlas300IDuo {
			return true
		}
	}
	return false
}
