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
	"fmt"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("Test GPU allocation", func() {
	Context("GPU Test 1- Two pods, one container each, one GPU per container", func() {
		namespace := "gpu-test1"
		pods := []string{"pod0", "pod1"}
		containerName := "ctr0"
		expectedPodCount := 2
		expectedGPUCount := 1

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, expectedPodCount)
		})
		It("should have exactly 1 unclaimed GPU per container", func() {
			for _, podName := range pods {
				verifyGPUAllocation(namespace, podName, containerName, expectedGPUCount)
			}
		})
	})
	Context("GPU Test 2- One pod, one container with two GPUs", func() {
		namespace := "gpu-test2"
		pods := []string{"pod0"}
		containerName := "ctr0"
		expectedPodCount := 1
		expectedGPUCount := 2

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, expectedPodCount)
		})
		It("should have exactly 2 unclaimed GPUs", func() {
			verifyGPUAllocation(namespace, pods[0], containerName, expectedGPUCount)
		})
	})
	Context("GPU Test 3- One pod, two containers and one GPU having TimeSlicing sharing strategy and Default TimeSlice interval", func() {
		namespace := "gpu-test3"
		pods := []string{"pod0"}
		containerNames := []string{"ctr0", "ctr1"}
		expectedPodCount := 1
		expectedGPUCount := 1
		expectedSharingStrategy := "TimeSlicing"
		expectedTimeSliceInterval := "Default"

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, expectedPodCount)
		})
		It("should have 1 GPU shared in time with default timeslice interval for each container", func() {
			var gpus []string
			var logs string
			var gpuCtr0, gpuCtr1 string
			for _, containerName := range containerNames {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs = getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
					if containerName == "ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuCtr0 = gpus[0]
						claimNewGPU(g, gpus[0], namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containerName))
						gpuCtr1 = gpus[0]
						verifySharedGPU(g, gpuCtr1, gpuCtr0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, timeout, interval).Should(Succeed())
			}
		})
	})
	Context("GPU Test 4- Two pods, one container each, one GPU having TimeSlicing sharing strategy and Default TimeSlice interval", func() {
		namespace := "gpu-test4"
		pods := []string{"pod0", "pod1"}
		containers := []string{"ctr0"}
		expectedPodCount := 2
		expectedGPUCount := 1
		expectedSharingStrategy := "TimeSlicing"
		expectedTimeSliceInterval := "Default"

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, expectedPodCount)
		})
		It("should have 1 GPU shared in time with default timeslice interval for each pod", func() {
			var gpus []string
			var logs string
			var gpuPod0Ctr0, gpuPod1Ctr0 string
			for _, podName := range pods {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs = getGPUsFromPodLogs(namespace, podName, containers[0])
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, podName, containers[0])
					if podName == "pod0" && containers[0] == "ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containers[0]))
						gpuPod0Ctr0 = gpus[0]
						claimNewGPU(g, gpuPod0Ctr0, namespace, podName, containers[0])
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containers[0]))
						gpuPod1Ctr0 = gpus[0]
						verifySharedGPU(g, gpuPod1Ctr0, gpuPod0Ctr0, namespace, podName, containers[0])
					}
					verifyGPUProperties(g, logs, namespace, podName, containers[0], gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, timeout, interval).Should(Succeed())
			}
		})
	})
	Context("GPU Test 5- One pod, four containers, two shared GPUs having TimeSlicing & SpacePartitioning sharing strategy and Long TimeSlice interval", func() {
		namespace := "gpu-test5"
		pods := []string{"pod0"}
		tsContainers := []string{"ts-ctr0", "ts-ctr1"}
		spContainers := []string{"sp-ctr0", "sp-ctr1"}
		expectedPodCount := 1
		expectedGPUCount := 1
		expectedTSSharingStrategy := "TimeSlicing"
		expectedSPSharingStrategy := "SpacePartitioning"
		expectedPartitionCount := "10"
		expectedTimeSliceInterval := "Long"

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, expectedPodCount)
		})
		It("should have exactly 1 GPU shared in time with long timeslice interval for each container", func() {
			var gpus []string
			var logs string
			var gpuTsCtr0, gpuTsCtr1 string
			for _, containerName := range tsContainers {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs = getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
					if containerName == "ts-ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuTsCtr0 = gpus[0]
						claimNewGPU(g, gpuTsCtr0, namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containerName))
						gpuTsCtr1 = gpus[0]
						verifySharedGPU(g, gpuTsCtr1, gpuTsCtr0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedTSSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, timeout, interval).Should(Succeed())
			}
		})
		It("should have exactly 1 GPU shared in space for each container", func() {
			var gpus []string
			var logs string
			var gpuSpCtr0, gpuSpCtr1 string
			for _, containerName := range spContainers {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs = getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
					if containerName == "sp-ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuSpCtr0 = gpus[0]
						claimNewGPU(g, gpuSpCtr0, namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containerName))
						gpuSpCtr1 = gpus[0]
						verifySharedGPU(g, gpuSpCtr1, gpuSpCtr0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSPSharingStrategy, "PARTITION_COUNT", expectedPartitionCount)
				}, timeout, interval).Should(Succeed())
			}
		})
	})
	Context("GPU Test 6- One pod, one init container, one container, one GPU having TimeSlicing sharing strategy and Default TimeSlice interval", func() {
		namespace := "gpu-test6"
		pods := []string{"pod0"}
		containerNames := []string{"init0", "ctr0"}
		expectedPodCount := 1
		expectedGPUCount := 1
		expectedSharingStrategy := "TimeSlicing"
		expectedTimeSliceInterval := "Default"

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, expectedPodCount)
		})
		It("should have exactly 1 unclaimed GPU shared in time with default timeslice interval", func() {
			var gpus []string
			var logs string
			var gpuInit0, gpuCtr0 string
			for _, containerName := range containerNames {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs = getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
					if containerName == "init0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuInit0 = gpus[0]
						claimNewGPU(g, gpuInit0, namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containerName))
						gpuCtr0 = gpus[0]
						verifySharedGPU(g, gpuCtr0, gpuInit0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, timeout, interval).Should(Succeed())
			}
		})
	})
	Context("GPU Test 7- Test DRAAdminAccess set to true", func() {
		namespace := "gpu-test7"
		pods := []string{"pod0"}
		containerName := "ctr0"
		expectedPodCount := 1

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, expectedPodCount)
		})
		It("should have DRA_ADMIN_ACCESS set to true", func() {
			verifyDRAAdminAccess(namespace, pods[0], containerName, "true")
		})
	})
	Context("GPU Test 8- One pod, one container with single gpu selected using cel expression", func() {
		namespace := "gpu-test8"
		pods := []string{"pod0"}
		containerNames := []string{"ctr0"}
		expectedPodCount := 1
		expectedGPUCount := 1

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, expectedPodCount)
		})
		It("should have exactly 1 unclaimed GPU selected using cel expression", func() {
			verifyGPUAllocation(namespace, pods[0], containerNames[0], expectedGPUCount)
		})
	})
	Context("Webhooks", func() {
		tests := []struct {
			name     string
			fileName string
		}{
			{name: "v1 ResourceClaim", fileName: "invalid_rc_v1.yaml"},
			{name: "v1beta1 ResourceClaim", fileName: "invalid_rc_v1beta1.yaml"},
			{name: "v1 ResourceClaimTemplate", fileName: "invalid_rc_template.yaml"},
		}

		for _, testCase := range tests {
			It("should reject invalid "+testCase.name, func(ctx SpecContext) {
				manifestPath := filepath.Join(currentDir, "testdata", "webhooks", testCase.fileName)

				cmd := exec.CommandContext(ctx, "kubectl", "create", "--dry-run=server", "-f", manifestPath)
				webhookResponse, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(webhookResponse).Should(gexec.Exit())

				Expect(webhookResponse.ExitCode()).NotTo(Equal(0),
					"Expected webhook to reject %s, but it was accepted", testCase.fileName)
				Expect(webhookResponse.Err).To(gbytes.Say("unknown time-slice interval"),
					"Webhook did not reject %s invalid GpuConfig with the expected message", testCase.name)
			})
		}
	})

})
