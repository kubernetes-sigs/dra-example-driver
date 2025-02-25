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

// Package kubeclient a series of k8s function
package kubeclient

import (
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"

	"huawei.com/npu-exporter/v5/common-utils/hwlog"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"sigs.k8s.io/dra-example-driver/pkg/common"
)

// TryUpdatePodAnnotation is to try updating pod annotation
func (ki *ClientK8s) TryUpdatePodAnnotation(pod *v1.Pod, annotation map[string]string) error {
	if pod == nil {
		return fmt.Errorf("invalid pod")
	}
	for i := 0; i < common.RetryUpdateCount; i++ {
		podNew, err := ki.GetPod(pod)
		if err != nil || podNew == nil {
			hwlog.RunLog.Errorf("query pod info failed. %#v", err)
			continue
		}
		if podNew.Annotations == nil {
			return fmt.Errorf("invalid pod Annotations")
		}
		for k, v := range annotation {
			podNew.Annotations[k] = v
		}

		if _, err = ki.UpdatePod(podNew); err == nil {
			return nil
		}
		hwlog.RunLog.Warnf("update pod annotation failed, times: %d, error is %#v", i+1, err)
		time.Sleep(time.Second)
	}
	return fmt.Errorf("update pod annotation failed, exceeded max number of retries")
}

func (ki *ClientK8s) isConfigMapChanged(cm *v1.ConfigMap) bool {
	cmData, err := ki.GetConfigMap()
	if err != nil {
		hwlog.RunLog.Infof("get device info configmap failed, error is: %#v", err)
		return true
	}
	return !reflect.DeepEqual(cmData, cm)
}

func (ki *ClientK8s) createOrUpdateConfigMap(cm *v1.ConfigMap) (*v1.ConfigMap, error) {
	newCM, err := ki.CreateConfigMap(cm)
	if err != nil {
		if !ki.IsCMExist(err) {
			return nil, fmt.Errorf("unable to create configmap, %#v", err)
		}
		// To reduce the cm write operations
		if !ki.isConfigMapChanged(cm) {
			hwlog.RunLog.Info("configmap not changed, no need update")
			return cm, nil
		}
		if newCM, err = ki.UpdateConfigMap(cm); err != nil {
			return nil, fmt.Errorf("unable to update ConfigMap, %#v", err)
		}
	}
	return newCM, nil
}

// IsCMExist judge cm is exist
func (ki *ClientK8s) IsCMExist(err error) bool {
	return errors.IsAlreadyExists(err)
}

// WriteDeviceInfoDataIntoCM write deviceinfo into config map
func (ki *ClientK8s) WriteDeviceInfoDataIntoCM(deviceInfo map[string]string) (*v1.ConfigMap, error) {
	var nodeDeviceData = common.NodeDeviceInfoCache{
		DeviceInfo: common.NodeDeviceInfo{
			DeviceList: deviceInfo,
			UpdateTime: time.Now().Unix(),
		},
	}
	nodeDeviceData.CheckCode = common.MakeDataHash(nodeDeviceData.DeviceInfo)

	var data []byte
	if data = common.MarshalData(nodeDeviceData); len(data) == 0 {
		return nil, fmt.Errorf("marshal nodeDeviceData failed")
	}
	deviceInfoCM := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ki.DeviceInfoName,
			Namespace: common.DeviceInfoCMNameSpace,
		},
		Data: map[string]string{common.DeviceInfoCMDataKey: string(data)},
	}

	hwlog.RunLog.Debugf("write device info cache into cm: %s/%s.", deviceInfoCM.Namespace, deviceInfoCM.Name)
	return ki.createOrUpdateConfigMap(deviceInfoCM)
}

// AnnotationReset reset annotation and device info
func (ki *ClientK8s) AnnotationReset() error {
	curNode, err := ki.GetNode()
	if err != nil {
		hwlog.RunLog.Errorf("failed to get node, nodeName: %s, err: %#v", ki.NodeName, err)
		return err
	}
	if curNode == nil {
		hwlog.RunLog.Error("invalid node")
		return fmt.Errorf("invalid node")
	}
	newNode := curNode.DeepCopy()
	ki.resetNodeAnnotations(newNode)
	ki.ResetDeviceInfo()
	for i := 0; i < common.RetryUpdateCount; i++ {
		if _, _, err = ki.PatchNodeState(curNode, newNode); err == nil {
			hwlog.RunLog.Infof("reset annotation success")
			return nil
		}
		hwlog.RunLog.Errorf("failed to patch volcano npu resource, times:%d", i+1)
		time.Sleep(time.Second)
		continue
	}
	hwlog.RunLog.Errorf("failed to patch volcano npu resource: %#v", err)
	return err
}

// GetPodsUsedNpu get npu by status
func (ki *ClientK8s) GetPodsUsedNpu(devType string) sets.String {
	podList, err := ki.GetActivePodList()
	if err != nil {
		hwlog.RunLog.Errorf("get pod list failed, err: %#v", err)
		return sets.String{}
	}
	var useNpu []string
	for _, pod := range podList {
		annotationTag := fmt.Sprintf("%s%s", common.ResourceNamePrefix, devType)
		tmpNpu, ok := pod.Annotations[annotationTag]
		if !ok || len(tmpNpu) == 0 || len(tmpNpu) > common.PodAnnotationMaxMemory {
			continue
		}
		tmpNpuList := strings.Split(tmpNpu, common.CommaSepDev)
		if len(tmpNpuList) == 0 || len(tmpNpuList) > common.MaxDevicesNum {
			hwlog.RunLog.Warnf("invalid annotation, len is %d", len(tmpNpu))
			continue
		}
		useNpu = append(useNpu, tmpNpuList...)
		hwlog.RunLog.Debugf("pod Name: %s, getNPUByStatus vol : %#v", pod.Name, tmpNpu)
	}
	hwlog.RunLog.Debugf("nodeName: %s, useNpus: %#v", ki.NodeName, useNpu)
	return sets.NewString(useNpu...)
}

// GetNodeServerID Get Node Server ID
func (ki *ClientK8s) GetNodeServerID() (string, error) {
	node, err := ki.GetNode()
	if err != nil {
		return "", err
	}
	if len(node.Status.Addresses) > common.MaxPodLimit {
		hwlog.RunLog.Error("the number of node status in exceeds the upper limit")
		return "", fmt.Errorf("the number of node status in exceeds the upper limit")
	}
	var serverID string
	for _, addresses := range node.Status.Addresses {
		if addresses.Type == v1.NodeInternalIP && net.ParseIP(addresses.Address) != nil {
			serverID = addresses.Address
			break
		}
	}
	return serverID, nil
}
