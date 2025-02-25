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
	"huawei.com/npu-exporter/v5/common-utils/hwlog"
	"sigs.k8s.io/dra-example-driver/pkg/common"
)

// Start starts the gRPC server, registers the device plugin with the Kubelet
func (ps *PluginServer) Start(socketWatcher *common.FileWatch) error {
	// clean
	ps.Stop()

	var err error

	// Start gRPC server
	if err = ps.serve(socketWatcher); err != nil {
		return err
	}

	// Registers To Kubelet.
	if err = ps.register(); err == nil {
		hwlog.RunLog.Infof("register %s to kubelet success.", ps.deviceType)
		return nil
	}
	ps.Stop()
	hwlog.RunLog.Errorf("register to kubelet failed, err: %#v", err)
	return err
}

// Stop the gRPC server
func (ps *PluginServer) Stop() {
	ps.isRunning.Store(false)

	if ps.grpcServer == nil {
		return
	}
	ps.stopListAndWatch()
	ps.grpcServer.Stop()

	return
}

// GetRestartFlag get restart flag
func (ps *PluginServer) GetRestartFlag() bool {
	return ps.restart
}

// SetRestartFlag set restart flag
func (ps *PluginServer) SetRestartFlag(flag bool) {
	ps.restart = flag
}
