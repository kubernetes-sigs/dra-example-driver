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
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"

	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/google/uuid"
)

func enumerateAllPossibleDevices(numGPUs int) (AllocatableDevices, error) {
	seed := os.Getenv("NODE_NAME")
	uuids := generateUUIDs(seed, numGPUs)

	alldevices := make(AllocatableDevices)
	for i, uuid := range uuids {
		device := resourceapi.Device{
			Name: fmt.Sprintf("gpu-%d", i),
			Basic: &resourceapi.BasicDevice{
				Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
					"index": {
						IntValue: ptr.To(int64(i)),
					},
					"uuid": {
						StringValue: ptr.To(uuid),
					},
					"model": {
						StringValue: ptr.To("LATEST-GPU-MODEL"),
					},
					"driverVersion": {
						VersionValue: ptr.To("1.0.0"),
					},
				},
				Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
					"memory": {
						Value: resource.MustParse("80Gi"),
					},
				},
			},
		}
		alldevices[device.Name] = device
	}
	return alldevices, nil
}

func generateUUIDs(seed string, count int) []string {
	rand := rand.New(rand.NewSource(hash(seed)))

	uuids := make([]string, count)
	for i := 0; i < count; i++ {
		charset := make([]byte, 16)
		rand.Read(charset)
		uuid, _ := uuid.FromBytes(charset)
		uuids[i] = "gpu-" + uuid.String()
	}

	return uuids
}

func hash(s string) int64 {
	h := int64(0)
	for _, c := range s {
		h = 31*h + int64(c)
	}
	return h
}

// HostHardwareInfo represents hardware information available to admin access.
type HostHardwareInfo struct {
	CPUInfo     string
	MemInfo     string
	KernelInfo  string
	SystemInfo  string
	NetworkInfo string
	StorageInfo string
}

// GetHostHardwareInfo gathers hardware information from the host system.
func GetHostHardwareInfo() (*HostHardwareInfo, error) {
	info := &HostHardwareInfo{}

	// Get CPU information
	if cpuInfo, err := readFile("/proc/cpuinfo"); err == nil {
		info.CPUInfo = extractCPUModel(cpuInfo)
	} else {
		info.CPUInfo = fmt.Sprintf("CPU Info unavailable: %v", err)
	}

	// Get memory information
	if memInfo, err := readFile("/proc/meminfo"); err == nil {
		info.MemInfo = extractMemInfo(memInfo)
	} else {
		info.MemInfo = fmt.Sprintf("Memory Info unavailable: %v", err)
	}

	// Get kernel information
	if kernelInfo, err := readFile("/proc/version"); err == nil {
		info.KernelInfo = strings.TrimSpace(kernelInfo)
	} else {
		info.KernelInfo = fmt.Sprintf("Kernel Info unavailable: %v", err)
	}

	// Get system information (architecture, OS)
	info.SystemInfo = fmt.Sprintf("GOOS: %s, GOARCH: %s", runtime.GOOS, runtime.GOARCH)

	// Get network information
	if netInfo, err := getNetworkInfo(); err == nil {
		info.NetworkInfo = netInfo
	} else {
		info.NetworkInfo = fmt.Sprintf("Network Info unavailable: %v", err)
	}

	// Get storage information
	if storageInfo, err := getStorageInfo(); err == nil {
		info.StorageInfo = storageInfo
	} else {
		info.StorageInfo = fmt.Sprintf("Storage Info unavailable: %v", err)
	}

	return info, nil
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func extractCPUModel(cpuInfo string) string {
	scanner := bufio.NewScanner(strings.NewReader(cpuInfo))
	cpuCount := 0
	var modelName string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "processor") {
			cpuCount++
		}
		if strings.HasPrefix(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				modelName = strings.TrimSpace(parts[1])
			}
		}
	}

	if modelName != "" {
		return fmt.Sprintf("%d x %s", cpuCount, modelName)
	}
	return fmt.Sprintf("%d CPU cores", cpuCount)
}

func extractMemInfo(memInfo string) string {
	scanner := bufio.NewScanner(strings.NewReader(memInfo))
	var totalMem, availableMem string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				totalMem = parts[1] + "kB"
			}
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				availableMem = parts[1] + "kB"
			}
		}
	}

	if totalMem != "" && availableMem != "" {
		return fmt.Sprintf("Total: %s, Available: %s", totalMem, availableMem)
	}
	return "Memory information parsing failed"
}

func getNetworkInfo() (string, error) {
	// Read network interfaces
	interfaces, err := readFile("/proc/net/dev")
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(strings.NewReader(interfaces))
	var interfaceNames []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, ":") && !strings.Contains(line, "Inter-|") && !strings.Contains(line, "face |") {
			parts := strings.Split(line, ":")
			if len(parts) > 0 {
				ifName := strings.TrimSpace(parts[0])
				if ifName != "lo" { // Skip loopback
					interfaceNames = append(interfaceNames, ifName)
				}
			}
		}
	}

	return fmt.Sprintf("Network Interfaces: %s", strings.Join(interfaceNames, ", ")), nil
}

func getStorageInfo() (string, error) {
	// Read mounted filesystems
	mounts, err := readFile("/proc/mounts")
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(strings.NewReader(mounts))
	var rootFS string
	var mountCount int

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			mountPoint := fields[1]
			fsType := fields[2]

			if mountPoint == "/" {
				rootFS = fmt.Sprintf("Root FS: %s (%s)", fields[0], fsType)
			}

			// Count real filesystems (not virtual ones)
			if !strings.HasPrefix(fields[0], "/proc") &&
				!strings.HasPrefix(fields[0], "/sys") &&
				!strings.HasPrefix(fields[0], "/dev/pts") &&
				fsType != "tmpfs" && fsType != "devtmpfs" &&
				fsType != "cgroup" && fsType != "cgroup2" {
				mountCount++
			}
		}
	}

	if rootFS != "" {
		return fmt.Sprintf("%s, Total mounts: %d", rootFS, mountCount), nil
	}
	return fmt.Sprintf("Storage mounts: %d", mountCount), nil
}
