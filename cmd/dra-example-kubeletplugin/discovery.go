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
	"math/rand"
	"os"
	"strconv"
	"strings"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	semver "github.com/Masterminds/semver/v3"
	"github.com/google/uuid"
)

func enumerateAllPossibleDevices(numGPUs int, deviceAttributes []string) (AllocatableDevices, error) {
	seed := os.Getenv("NODE_NAME")
	uuids := generateUUIDs(seed, numGPUs)

	// Parse additional device attributes from the flag
	additionalAttributes, err := parseDeviceAttributes(deviceAttributes)
	if err != nil {
		return nil, fmt.Errorf("error parsing device attributes: %w", err)
	}

	alldevices := make(AllocatableDevices)
	for i, uuid := range uuids {
		// Start with default attributes
		attributes := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
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
		}

		// Add additional attributes from the flag
		for key, value := range additionalAttributes {
			attributes[key] = value
		}

		device := resourceapi.Device{
			Name:       fmt.Sprintf("gpu-%d", i),
			Attributes: attributes,
			Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
				"memory": {
					Value: resource.MustParse("80Gi"),
				},
			},
		}
		alldevices[device.Name] = device
	}
	return alldevices, nil
}

// parseDeviceAttributes parses a comma-separated string of key=value pairs
// and returns a map of device attributes with automatic type detection.
// Supported value types:
// - int: integer values (e.g., "count=5")
// - bool: boolean values (e.g., "enabled=true", "disabled=false")
// - version: semantic version values (e.g., "driver_version=1.2.3")
// - string: any other value (e.g., "productName=NVIDIA GeForce RTX 5090", "architecture=Blackwell")
func parseDeviceAttributes(deviceAttributes []string) (map[resourceapi.QualifiedName]resourceapi.DeviceAttribute, error) {
	attributes := make(map[resourceapi.QualifiedName]resourceapi.DeviceAttribute)

	if len(deviceAttributes) == 0 {
		return attributes, nil
	}

	for _, pair := range deviceAttributes {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid device attribute format: %s (expected key=value)", pair)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == "" {
			return nil, fmt.Errorf("device attribute key cannot be empty")
		}

		// Detect value type and create appropriate DeviceAttribute
		attr, err := createDeviceAttribute(value)
		if err != nil {
			return nil, fmt.Errorf("invalid value for attribute %s: %w", key, err)
		}

		attributes[resourceapi.QualifiedName(key)] = attr
	}

	return attributes, nil
}

// createDeviceAttribute creates a DeviceAttribute with the appropriate value type
// based on the input string. It tries to detect the type in this order:
// 1. bool (true/false)
// 2. int (integer)
// 3. version (semantic version pattern)
// 4. string (default)
func createDeviceAttribute(value string) (resourceapi.DeviceAttribute, error) {
	// Check for boolean values
	if value == "true" {
		return resourceapi.DeviceAttribute{
			BoolValue: ptr.To(true),
		}, nil
	}
	if value == "false" {
		return resourceapi.DeviceAttribute{
			BoolValue: ptr.To(false),
		}, nil
	}

	// Check for integer values
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		return resourceapi.DeviceAttribute{
			IntValue: ptr.To(intVal),
		}, nil
	}

	// Check for semantic version pattern (basic check for x.y.z format)
	if isSemanticVersion(value) {
		return resourceapi.DeviceAttribute{
			VersionValue: ptr.To(value),
		}, nil
	}

	// Default to string value
	// Validate string length (max 64 characters as per API spec)
	if len(value) > 64 {
		return resourceapi.DeviceAttribute{}, fmt.Errorf("string value too long (max 64 characters): %s", value)
	}

	return resourceapi.DeviceAttribute{
		StringValue: ptr.To(value),
	}, nil
}

// isSemanticVersion checks whether the string is a valid semantic version per https://semver.org/.
// It accepts versions like 1.2.3, 1.0.0-beta.1, and allows build metadata like +exp.sha.
func isSemanticVersion(value string) bool {
	// Enforce strict SemVer (MAJOR.MINOR.PATCH) per semver.org
	_, err := semver.StrictNewVersion(value)
	return err == nil
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
