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
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/google/uuid"
)

func enumerateAllPossibleDevices(numGPUs int) (AllocatableDevices, error) {
	// seed := os.Getenv("NODE_NAME")
	// numGPUs = 1
	// uuids := generateUUIDs(seed, numGPUs)
	usbdevices := FindPICOs()
	log.Println("devices", usbdevices)

	alldevices := make(AllocatableDevices)
	for i, usbdevice := range usbdevices {
		device := resourceapi.Device{
			Name: fmt.Sprintf("gpu-%d", i),
			Basic: &resourceapi.BasicDevice{
				Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
					"index": {
						IntValue: ptr.To(int64(i)),
					},
					"serial": {
						StringValue: ptr.To(usbdevice.Serial),
					},
					"vendor": {
						StringValue: ptr.To(fmt.Sprintf("0x%04X", uint16(usbdevice.Vendor))),
					},
					"product": {
						StringValue: ptr.To(strconv.Itoa(int(usbdevice.Product))),
					},
					"buspath": {
						StringValue: ptr.To(usbdevice.BusPath),
					},
					"model": {
						StringValue: ptr.To("Rasberry Pi Pico"),
					},
					// "driverVersion": {
					// 	VersionValue: ptr.To("1.0.0"),
					// },
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

type usbDevice struct {
	// Vendor is the USB Vendor ID of the device.
	Vendor USBID `json:"vendor"`
	// Product is the USB Product ID of the device.
	Product USBID `json:"product"`
	// Bus is the physical USB bus this device is located at.
	Bus uint16 `json:"bus"`
	// BusDevice is the location of the device on the Bus.
	BusDevice uint16 `json:"busdev"`
	// Serial is the serial number of the device.
	Serial  string `json:"serial"`
	BusPath string `json:"buspath"`
}
type USBID uint16

func convertUint16(data []byte) (uint64, error) {
	dataStr := strings.ReplaceAll(string(data), "\n", "")
	re, err := strconv.ParseUint(dataStr, 16, 16)
	return re, err
}
func FindPICOs() []usbDevice {
	devices := []usbDevice{}

	// USB VPUs

	usbdevices, _ := filepath.Glob("/sys/bus/usb/devices/*/idVendor")
	// fs.ReadFile(usbdevices, "idVendor")
	log.Println(usbdevices)
	for _, path := range usbdevices {
		vendorid, _ := os.ReadFile(path)
		if strings.Contains(string(vendorid), "2e8a") {

			// || strings.Contains(string(vendorid), "13fe") {
			productid, _ := os.ReadFile(filepath.Dir(path) + "/idProduct")
			if strings.Contains(string(productid), "0005") {
				// || strings.Contains(string(productid), "4300") {
				serial, _ := os.ReadFile(filepath.Dir(path) + "/serial")
				log.Println("Serial", string(serial))
				busnum, _ := os.ReadFile(filepath.Dir(path) + "/busnum")
				buint, _ := convertUint16(busnum)
				log.Println("Busnum", buint)
				devnum, _ := os.ReadFile(filepath.Dir(path) + "/devnum")
				duint, _ := convertUint16(devnum)
				log.Println("Devnum", duint)
				buspath := fmt.Sprintf("/dev/bus/usb/%03x/%03x", buint, duint)
				log.Printf("/dev/bus/usb/%03x/%03x", buint, duint)
				// make(&[]usbDevices)
				usbDevices := usbDevice{
					Vendor:    USBID(buint),
					Product:   USBID(duint),
					Bus:       uint16(buint),
					BusDevice: uint16(duint),
					Serial:    string(serial),
					BusPath:   buspath,
				}
				log.Println(usbDevices)
				devices = append(devices, usbDevices)
				// devices = append(devices, string(serial))
			}
		}
	}
	return devices
}
