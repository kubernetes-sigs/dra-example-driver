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
	"sync"

	"google.golang.org/grpc"
	"k8s.io/api/core/v1"
	"sigs.k8s.io/dra-example-driver/pkg/common"
	"sigs.k8s.io/dra-example-driver/pkg/device"
)

// InterfaceServer interface for object that keeps running for providing service
type InterfaceServer interface {
	Start(*common.FileWatch) error
	Stop()
	GetRestartFlag() bool
	SetRestartFlag(bool)
}

// PluginServer implements the interface of DevicePluginServer; manages the registration and lifecycle of grpc server
type PluginServer struct {
	manager              device.DevManager
	grpcServer           *grpc.Server
	isRunning            *common.AtomicBool
	cachedDevices        []common.NpuDevice
	deviceType           string
	ascendRuntimeOptions string
	defaultDevs          []string
	allocMapLock         sync.RWMutex
	cachedLock           sync.RWMutex
	reciChan             chan interface{}
	stop                 chan interface{}
	klt2RealDevMap       map[string]string
	restart              bool
}

// PodDevice define device info in pod
type PodDevice struct {
	ResourceName string
	DeviceIds    []string
}

// PodDeviceInfo define device info of pod, include kubelet allocate and real allocate device
type PodDeviceInfo struct {
	Pod        v1.Pod
	KltDevice  []string
	RealDevice []string
}
