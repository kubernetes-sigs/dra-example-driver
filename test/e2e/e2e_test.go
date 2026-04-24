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
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "basic-resourceclaimtemplate.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should allocate 1 distinct GPU per pod", func() {
			namespace := "basic-resourceclaimtemplate"
			pods := []string{"pod0", "pod1"}
			containerName := "ctr0"
			expectedGPUCount := 1
			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			for _, podName := range pods {
				verifyGPUAllocation(namespace, podName, containerName, expectedGPUCount, observedGPUs)
			}
		})
	})

	Context("One pod, one container with two GPUs", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "basic-multiple-requests.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should allocate 2 distinct GPUs to a single container", func() {
			namespace := "basic-multiple-requests"
			pods := []string{"pod0"}
			containerName := "ctr0"
			expectedGPUCount := 2
			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			verifyGPUAllocation(namespace, pods[0], containerName, expectedGPUCount, observedGPUs)
		})
	})

	Context("One pod, two containers sharing one GPU with TimeSlicing and Default interval", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "basic-shared-claim-across-containers.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should share 1 GPU between containers with default timeslice interval", func() {
			namespace := "basic-shared-claim-across-containers"
			pods := []string{"pod0"}
			containerNames := []string{"ctr0", "ctr1"}
			expectedGPUCount := 1
			expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
			expectedTimeSliceInterval := string(gpuv1alpha1.DefaultTimeSlice)

			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			var gpuCtr0 string
			for _, containerName := range containerNames {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
					if containerName == "ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuCtr0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpus[0], namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU", containerName))
						verifySharedGPU(g, gpus[0], gpuCtr0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})

	Context("Two pods sharing a global ResourceClaim with TimeSlicing and Default interval", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "basic-shared-claim-across-pods.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should share 1 GPU between pods with default timeslice interval", func() {
			namespace := "basic-shared-claim-across-pods"
			pods := []string{"pod0", "pod1"}
			containerName := "ctr0"
			expectedGPUCount := 1
			expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
			expectedTimeSliceInterval := string(gpuv1alpha1.DefaultTimeSlice)

			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			var gpuPod0 string
			for _, podName := range pods {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, podName, containerName)
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, podName, containerName)
					if podName == "pod0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", podName))
						gpuPod0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpuPod0, namespace, podName, containerName)
					} else {
						By(fmt.Sprintf("checking that %s claims the same GPU", podName))
						verifySharedGPU(g, gpus[0], gpuPod0, namespace, podName, containerName)
					}
					verifyGPUProperties(g, logs, namespace, podName, containerName, gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})

	Context("GPU sharing strategies: TimeSlicing with Long interval and SpacePartitioning", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "basic-resourceclaim-opaque-config.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should share GPUs with timeslicing (long interval) between ts containers", func() {
			namespace := "basic-resourceclaim-opaque-config"
			pods := []string{"pod0"}
			tsContainers := []string{"ts-ctr0", "ts-ctr1"}
			expectedGPUCount := 1
			expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
			expectedTimeSliceInterval := string(gpuv1alpha1.LongTimeSlice)

			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			var gpuTsCtr0 string
			for _, containerName := range tsContainers {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
					if containerName == "ts-ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuTsCtr0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpuTsCtr0, namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU", containerName))
						verifySharedGPU(g, gpus[0], gpuTsCtr0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})

		It("should share GPUs with space partitioning between sp containers", func() {
			namespace := "basic-resourceclaim-opaque-config"
			pods := []string{"pod0"}
			spContainers := []string{"sp-ctr0", "sp-ctr1"}
			expectedGPUCount := 1
			expectedSharingStrategy := string(gpuv1alpha1.SpacePartitioningStrategy)
			expectedPartitionCount := "10"

			observedGPUs := make(map[string]string)
			var gpuSpCtr0 string
			for _, containerName := range spContainers {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
					if containerName == "sp-ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuSpCtr0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpuSpCtr0, namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU", containerName))
						verifySharedGPU(g, gpus[0], gpuSpCtr0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "PARTITION_COUNT", expectedPartitionCount)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})

	Context("InitContainer and container sharing one GPU with TimeSlicing and Default interval", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "initcontainer-shared-gpu.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should share 1 GPU between init container and regular container", func() {
			namespace := "initcontainer-shared-gpu"
			pods := []string{"pod0"}
			containerNames := []string{"init0", "ctr0"}
			expectedGPUCount := 1
			expectedSharingStrategy := string(gpuv1alpha1.TimeSlicingStrategy)
			expectedTimeSliceInterval := string(gpuv1alpha1.DefaultTimeSlice)

			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			var gpuInit0 string
			for _, containerName := range containerNames {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					gpus, logs := getGPUsFromPodLogs(namespace, pods[0], containerName)
					verifyGPUCount(g, gpus, expectedGPUCount, namespace, pods[0], containerName)
					if containerName == "init0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuInit0 = gpus[0]
						claimNewGPU(g, observedGPUs, gpuInit0, namespace, pods[0], containerName)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU", containerName))
						verifySharedGPU(g, gpus[0], gpuInit0, namespace, pods[0], containerName)
					}
					verifyGPUProperties(g, logs, namespace, pods[0], containerName, gpus, expectedSharingStrategy, "TIMESLICE_INTERVAL", expectedTimeSliceInterval)
				}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
			}
		})
	})

	Context("DRA AdminAccess set to true", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "admin-access.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should have DRA_ADMIN_ACCESS set to true", func() {
			namespace := "admin-access"
			pods := []string{"pod0"}
			containerName := "ctr0"
			checkPodsReadyAndRunning(namespace, pods, len(pods))
			verifyDRAAdminAccess(namespace, pods[0], containerName, "true")
		})
	})

	Context("CEL expression selector for single GPU", func() {
		var cleanup func()
		BeforeEach(func(ctx SpecContext) {
			cleanup = deployManifest(ctx, "cel-selector.yaml")
		})
		AfterEach(func() {
			cleanup()
		})

		It("should allocate 1 GPU selected using CEL expression", func() {
			namespace := "cel-selector"
			pods := []string{"pod0"}
			containerName := "ctr0"
			expectedGPUCount := 1
			checkPodsReadyAndRunning(namespace, pods, len(pods))

			observedGPUs := make(map[string]string)
			verifyGPUAllocation(namespace, pods[0], containerName, expectedGPUCount, observedGPUs)
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
