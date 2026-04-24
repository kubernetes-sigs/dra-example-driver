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
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gpuv1alpha1 "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
)

var _ = Describe("Test GPU allocation", func() {
	Context("Two pods, one container each, one GPU per container", func() {
		namespace := "two-pods-one-gpu-each"
		pods := []string{"pod0", "pod1"}
		containerName := "ctr0"
		expectedGPUCount := 1

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
		})
		It("should have exactly 1 unclaimed GPU per container", func() {
			for _, podName := range pods {
				verifyGPUAllocation(namespace, podName, containerName, expectedGPUCount)
			}
		})
	})
	Context("One pod, one container with two GPUs", func() {
		namespace := "one-pod-two-gpus"
		pods := []string{"pod0"}
		containerName := "ctr0"
		expectedGPUCount := 2

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
		})
		It("should have exactly 2 unclaimed GPUs", func() {
			verifyGPUAllocation(namespace, pods[0], containerName, expectedGPUCount)
		})
	})
	Context("One pod, two containers sharing one GPU with TimeSlicing and Default interval", func() {
		namespace := "shared-gpu-across-containers"
		pods := []string{"pod0"}
		containerNames := []string{"ctr0", "ctr1"}
		expectedGPUCount := 1
		expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
		expectedTimeSliceInterval := string(gpuv1alpha1.DefaultTimeSlice)

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
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
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})
	Context("Two pods sharing a global ResourceClaim with TimeSlicing and Default interval", func() {
		namespace := "shared-global-claim"
		pods := []string{"pod0", "pod1"}
		containers := []string{"ctr0"}
		expectedGPUCount := 1
		expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
		expectedTimeSliceInterval := string(gpuv1alpha1.DefaultTimeSlice)

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
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
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})
	Context("GPU sharing strategies: TimeSlicing with Long interval and SpacePartitioning", func() {
		namespace := "gpu-sharing-strategies"
		pods := []string{"pod0"}
		tsContainers := []string{"ts-ctr0", "ts-ctr1"}
		spContainers := []string{"sp-ctr0", "sp-ctr1"}
		expectedGPUCount := 1
		expectedTSSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
		expectedSPSharingStrategy := string(gpuv1alpha1.SpacePartitioningStrategy)
		expectedPartitionCount := "10"
		expectedTimeSliceInterval := string(gpuv1alpha1.LongTimeSlice)

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
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
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
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
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})
	Context("InitContainer and container sharing one GPU with TimeSlicing and Default interval", func() {
		namespace := "initcontainer-shared-gpu"
		pods := []string{"pod0"}
		containerNames := []string{"init0", "ctr0"}
		expectedGPUCount := 1
		expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
		expectedTimeSliceInterval := string(gpuv1alpha1.DefaultTimeSlice)

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
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
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})
	Context("DRA AdminAccess set to true", func() {
		namespace := "admin-access"
		pods := []string{"pod0"}
		containerName := "ctr0"

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
		})
		It("should have DRA_ADMIN_ACCESS set to true", func() {
			verifyDRAAdminAccess(namespace, pods[0], containerName, "true")
		})
	})
	Context("CEL expression selector for single GPU", func() {
		namespace := "cel-selector"
		pods := []string{"pod0"}
		containerNames := []string{"ctr0"}
		expectedGPUCount := 1

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
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

				err := createManifestWithDryRun(ctx, dynamicClient, manifestPath)
				fmt.Fprintf(GinkgoWriter, "Error from create: %v\n", err)
				Expect(err).To(HaveOccurred(),
					"Expected webhook to reject %s, but it was accepted", testCase.fileName)
				Expect(err.Error()).To(ContainSubstring("unknown time-slice interval"),
					"Webhook did not reject %s invalid GpuConfig with the expected message. Got error: %v", testCase.name, err)
			})
		}
	})

})
