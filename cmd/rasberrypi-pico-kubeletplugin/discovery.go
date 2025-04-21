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
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/jochenvg/go-udev"
	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/utils/ptr"
)

func initialization(e *udev.Enumerate, vendorid string) map[string]*udev.Device {
	// e.AddMatchSysattr("idVendor", "2e8a")
	// e.AddMatchSysattr("idProduct", "0005")
	// dev := udev.NewDeviceFromSubsystemSysname("usb", "3-3")
	e.AddMatchProperty("ID_VENDOR_ID", vendorid)
	// e.AddMatchProperty("SUBSYSTEM", "usb")
	// e.AddMatchIsInitialized()
	devices, _ := e.Devices()
	deviceMap := make(map[string]*udev.Device)
	fmt.Println("Devices found: ", len(devices))
	for _, d := range devices {
		key := d.PropertyValue("ID_SERIAL_SHORT")
		fmt.Println("Key: ", d.PropertyValue("ID_USB_DRIVER"))
		// fmt.Println("Key: ", d.Properties())
		// fmt.Println(d.Properties())
		if d.PropertyValue("ID_USB_VENDOR") == "MicroPython" {
			deviceMap[key] = d
			exec.Command("mpremote", "bootloader").Run()
		} else if d.PropertyValue("ID_USB_VENDOR") == "Raspberry_Pi" && d.Driver() == "usb" {
			fmt.Println("Raspberry Pi device found")
			err := exec.Command("picotool", "info", "-F").Run()
			// err := exec.Command("picotool", "load", "-x", "RPI_PICO-20241129-v1.24.1.uf2").Run()
			if err != nil {
				log.Println("Error executing command:", err)
			}
			deviceMap[key] = d
		}

		// deviceMap[key] = append(deviceMap[key], d)
	}

	// for serial, devices := range deviceMap {
	// 	for _, device := range devices {

	// 		fmt.Printf("Serial %v Device found: %v", serial, device.Properties()["ID_SERIAL_SHORT"])
	// 		fmt.Println(device.Devnode())
	// 		fmt.Println(device.Devnum())

	// 	}
	// }
	return deviceMap
}

func enumerateAllPossibleDevices(vendor_id string) (AllocatableDevices, error) {
	// seed := os.Getenv("NODE_NAME")
	// numGPUs = 1
	// uuids := generateUUIDs(seed, numGPUs)
	u := udev.Udev{} // Correct initialization
	e := u.NewEnumerate()
	deviceMap := initialization(e, vendor_id)
	for serial, device := range deviceMap {

		if device.PropertyValue("ID_USB_VENDOR") == "MicroPython" {
			fmt.Println("MicroPython device added")
			// exec.Command("mpremote", "bootloader").Run()
		}
		fmt.Printf("Serial %v Device found: %v", serial, device.Devnode())
	}
	time.Sleep(5 * time.Second)
	op, err := exec.Command("picotool", "info", "-a").Output()
	if err != nil {
		fmt.Print("Error picotool")

	}

	lines := strings.Split(string(op), "\n")
	startIndex := -1
	endIndex := len(lines)

	// Find the Device Information section
	for i, line := range lines {
		if strings.Contains(line, "Device Information") {
			startIndex = i + 1 // Skip the header line
		} else if startIndex != -1 && strings.TrimSpace(line) == "" {
			endIndex = i
			break
		}
	}

	// Parse device info into a map
	deviceInfo := make(map[string]string)
	if startIndex != -1 {
		for i := startIndex; i < endIndex; i++ {
			parts := strings.SplitN(lines[i], ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				deviceInfo[key] = value
			}
		}
	}

	// Convert to JSON
	jsonData, err := json.MarshalIndent(deviceInfo, "", "  ")
	if err != nil {
		fmt.Println("Error creating JSON:", err)
	}

	fmt.Println(string(jsonData))
	// usbdevices := FindPICOs()
	// log.Println("devices", usbdevices)
	// for serial, device := range deviceMap{

	// }
	alldevices := make(AllocatableDevices)
	index := 0
	for _, device := range deviceMap {
		device := resourceapi.Device{
			Name: fmt.Sprintf("gpu-%d", index),
			Basic: &resourceapi.BasicDevice{
				Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
					"index": {
						IntValue: ptr.To(int64(index)),
					},
					"flash_id": {
						StringValue: ptr.To(string(deviceInfo["flash id"])),
					},
					// "serial": {
					// 	StringValue: ptr.To(device.Serial),
					// },
					"vendor": {
						StringValue: ptr.To(device.PropertyValue("ID_VENDOR_ID")),
					},
					"product": {
						StringValue: ptr.To(deviceInfo["type"]),
					},
					"buspath": {
						StringValue: ptr.To(device.Devnode()),
					},
					"model": {
						StringValue: ptr.To("Rasberry Pi Pico"),
					},
					"revision": {
						StringValue: ptr.To(deviceInfo["revision"]),
					},
					"core": {
						StringValue: ptr.To("Dual ARM Cortex-M0+"),
					},
					"processor_cycle": {
						StringValue: ptr.To("133MHz"),
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
		index++
	}
	return alldevices, nil
}

// func generateUUIDs(seed string, count int) []string {
// 	rand := rand.New(rand.NewSource(hash(seed)))

// 	uuids := make([]string, count)
// 	for i := 0; i < count; i++ {
// 		charset := make([]byte, 16)
// 		rand.Read(charset)
// 		uuid, _ := uuid.FromBytes(charset)
// 		uuids[i] = "gpu-" + uuid.String()
// 	}

// 	return uuids
// }

// func hash(s string) int64 {
// 	h := int64(0)
// 	for _, c := range s {
// 		h = 31*h + int64(c)
// 	}
// 	return h
// }

// type usbDevice struct {
// 	// Vendor is the USB Vendor ID of the device.
// 	Vendor USBID `json:"vendor"`
// 	// Product is the USB Product ID of the device.
// 	Product USBID `json:"product"`
// 	// Bus is the physical USB bus this device is located at.
// 	Bus uint16 `json:"bus"`
// 	// BusDevice is the location of the device on the Bus.
// 	BusDevice uint16 `json:"busdev"`
// 	// Serial is the serial number of the device.
// 	Serial  string `json:"serial"`
// 	BusPath string `json:"buspath"`
// }
// type USBID uint16

// func convertUint16(data []byte) (uint64, error) {
// 	dataStr := strings.ReplaceAll(string(data), "\n", "")
// 	re, err := strconv.ParseUint(dataStr, 16, 16)
// 	return re, err
// }
// func FindPICOs() []usbDevice {
// 	devices := []usbDevice{}

// 	// USB VPUs

// 	usbdevices, _ := filepath.Glob("/sys/bus/usb/devices/*/idVendor")
// 	// fs.ReadFile(usbdevices, "idVendor")
// 	log.Println(usbdevices)
// 	for _, path := range usbdevices {
// 		vendorid, _ := os.ReadFile(path)
// 		if strings.Contains(string(vendorid), "2e8a") {

// 			// || strings.Contains(string(vendorid), "13fe") {
// 			productid, _ := os.ReadFile(filepath.Dir(path) + "/idProduct")
// 			if strings.Contains(string(productid), "0005") {
// 				// || strings.Contains(string(productid), "4300") {
// 				serial, _ := os.ReadFile(filepath.Dir(path) + "/serial")
// 				log.Println("Serial", string(serial))
// 				busnum, _ := os.ReadFile(filepath.Dir(path) + "/busnum")
// 				buint, _ := convertUint16(busnum)
// 				log.Println("Busnum", buint)
// 				devnum, _ := os.ReadFile(filepath.Dir(path) + "/devnum")
// 				duint, _ := convertUint16(devnum)
// 				log.Println("Devnum", duint)
// 				buspath := fmt.Sprintf("/dev/bus/usb/%03x/%03x", buint, duint)
// 				log.Printf("/dev/bus/usb/%03x/%03x", buint, duint)
// 				// make(&[]usbDevices)
// 				usbDevices := usbDevice{
// 					Vendor:    USBID(buint),
// 					Product:   USBID(duint),
// 					Bus:       uint16(buint),
// 					BusDevice: uint16(duint),
// 					Serial:    string(serial),
// 					BusPath:   buspath,
// 				}
// 				log.Println(usbDevices)
// 				devices = append(devices, usbDevices)
// 				// devices = append(devices, string(serial))
// 			}
// 		}
// 	}
// 	return devices
// }
