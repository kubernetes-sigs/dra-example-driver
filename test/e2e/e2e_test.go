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
	"k8s.io/dynamic-resource-allocation/api/metadata"
	gpuv1alpha1 "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
)

var _ = Describe("Test GPU allocation", func() {
	Context("GPU Test 1- Two pods, one container each, one GPU per container", func() {
		namespace := "gpu-test1"
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
	Context("GPU Test 2- One pod, one container with two GPUs", func() {
		namespace := "gpu-test2"
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
	Context("GPU Test 3- One pod, two containers and one GPU having TimeSlicing sharing strategy and Default TimeSlice interval", func() {
		namespace := "gpu-test3"
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
	Context("GPU Test 4- Two pods, one container each, one GPU having TimeSlicing sharing strategy and Default TimeSlice interval", func() {
		namespace := "gpu-test4"
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
	Context("GPU Test 5- One pod, four containers, two shared GPUs having TimeSlicing & SpacePartitioning sharing strategy and Long TimeSlice interval", func() {
		namespace := "gpu-test5"
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
	Context("GPU Test 6- One pod, one init container, one container, one GPU having TimeSlicing sharing strategy and Default TimeSlice interval", func() {
		namespace := "gpu-test6"
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
	Context("GPU Test 7- Test DRAAdminAccess set to true", func() {
		namespace := "gpu-test7"
		pods := []string{"pod0"}
		containerName := "ctr0"

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
		})
		It("should have DRA_ADMIN_ACCESS set to true", func() {
			verifyDRAAdminAccess(namespace, pods[0], containerName, "true")
		})
	})
	Context("GPU Test 8- One pod, one container with single gpu selected using cel expression", func() {
		namespace := "gpu-test8"
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
	Context("GPU Test 9- One pod, one container, verify in-container device metadata for ResourceClaimTemplate", func() {
		namespace := "gpu-test9"
		pods := []string{"pod0"}
		containerName := "ctr0"
		expectedGPUCount := 1

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
		})
		It("should have exactly 1 GPU with valid device metadata", func() {
			var gpus []string
			Eventually(func(g Gomega) {
				By("checking that there is exactly 1 GPU")
				gpus, _ = getGPUsFromPodLogs(namespace, pods[0], containerName)
				verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
				claimNewGPU(g, gpus[0], namespace, pods[0], containerName)
			}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())

			metadataPath := metadata.ResourceClaimTemplateFilePath("gpu.example.com", "gpu", "gpu")
			By("checking that the device metadata file exists and is valid")
			dm, err := readDeviceMetadata(namespace, pods[0], containerName, metadataPath)
			Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("Expected to read metadata file at %s", metadataPath))
			fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s has metadata file at: %s\n",
				namespace, pods[0], containerName, metadataPath)
			Expect(dm.PodClaimName).ToNot(BeEmpty(), "Expected PodClaimName in metadata to not be empty")
			Expect(dm.Requests).To(HaveLen(1)))
		})
	})
	Context("GPU Test 10- One pod, one container, verify in-container device metadata for ResourceClaim", func() {
		namespace := "gpu-test10"
		pods := []string{"pod0"}
		containerName := "ctr0"
		expectedGPUCount := 1

		It("should have all the pods ready and running", func() {
			checkPodsReadyAndRunning(namespace, pods, len(pods))
		})
		It("should have exactly 1 GPU with valid device metadata", func() {
			var gpus []string
			Eventually(func(g Gomega) {
				By("checking that there is exactly 1 GPU")
				gpus, _ = getGPUsFromPodLogs(namespace, pods[0], containerName)
				verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
				claimNewGPU(g, gpus[0], namespace, pods[0], containerName)
			}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())

			metadataPath := metadata.ResourceClaimFilePath("gpu.example.com", "single-gpu", "gpu")
			By("checking that the device metadata file exists and is valid")
			dm, err := readDeviceMetadata(namespace, pods[0], containerName, metadataPath)
			Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("Expected to read metadata file at %s", metadataPath))
			fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s has metadata file at: %s\n",
				namespace, pods[0], containerName, metadataPath)
			Expect(dm.Requests).To(HaveLen(1))
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
