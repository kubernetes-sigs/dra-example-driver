//go:build e2e

/*
 * Copyright The Kubernetes Authors.
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

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
)

// Install each profile with its shipped driver name, which is longer than the
// generated test names. Serial prevents collisions with tests that install the
// same DeviceClass.
var _ = DescribeTable("Default driver names should allocate devices", Serial,
	func(ctx SpecContext, driverName string, extraValues map[string]string) {
		drv := installDriver(ctx, DriverConfig{
			DriverName:  driverName,
			ExtraValues: extraValues,
		})

		By("allocating a device to a Pod")
		claim := createDeviceClaim(ctx, drv, "default-name")
		startPodWithClaim(ctx, drv, "default-name", claim.Name)

		By("checking driver health across multiple liveness probes")
		verifyDriverRemainsHealthy(ctx, drv)
	},
	Entry("gpu", defaultGPUDeviceClassName, map[string]string{"deviceProfile": "gpu"}),
	Entry("cpu", defaultCPUDeviceClassName, map[string]string{
		"deviceProfile":                     "cpu",
		"kubeletPlugin.cpu.cpusPerNUMANode": "1",
	}),
	Entry("net", defaultNETDeviceClassName, map[string]string{"deviceProfile": "net"}),
)
