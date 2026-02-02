/*
 * Copyright 2023 The Kubernetes Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"net"
	"os"
	"runtime"
	"strings"
)

// HostHardwareInfo represents OS-agnostic host information for admin access.
type HostHardwareInfo struct {
	Hostname          string
	NodeName          string
	OS                string
	Architecture      string
	NumCPU            int
	GoVersion         string
	NetworkInterfaces string
}

// GetHostHardwareInfo gathers OS-agnostic hardware information from the host system.
func GetHostHardwareInfo() (*HostHardwareInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	// Gather network interface information
	interfaces, err := net.Interfaces()
	var interfaceNames []string
	if err == nil {
		for _, iface := range interfaces {
			// Skip loopback and down interfaces
			if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 {
				interfaceNames = append(interfaceNames, iface.Name)
			}
		}
	}
	networkInterfaces := strings.Join(interfaceNames, ",")
	if networkInterfaces == "" {
		networkInterfaces = "none"
	}

	info := &HostHardwareInfo{
		Hostname:          hostname,
		NodeName:          os.Getenv("NODE_NAME"),
		OS:                runtime.GOOS,
		Architecture:      runtime.GOARCH,
		NumCPU:            runtime.NumCPU(),
		GoVersion:         runtime.Version(),
		NetworkInterfaces: networkInterfaces,
	}

	return info, nil
}
