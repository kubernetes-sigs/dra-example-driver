//go:build e2e

/*
 * Copyright 2026 The Kubernetes Authors.
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

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var rootDir, currentDir string
var observedGPUs map[string]string
var demoFiles = []string{"gpu-test1", "gpu-test2", "gpu-test3", "gpu-test7", "gpu-test4", "gpu-test5", "gpu-test6"}

func init() {
	currentDir, _ = os.Getwd()
	rootDir = filepath.Join(filepath.Dir(currentDir), "..")
	observedGPUs = make(map[string]string)
}
func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2e Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	// Manually append bin paths if they are missing
	path := os.Getenv("PATH")
	home, _ := os.UserHomeDir()
	goBin := home + "/go/bin"

	if !strings.Contains(path, goBin) {
		os.Setenv("PATH", path+":"+goBin+":/usr/local/bin")
	}

	By("Verifying the cluster")
	// Verify the cluster
	cmdGetClusters := exec.Command("kind", "get", "clusters")
	clusterResponse, err := gexec.Start(cmdGetClusters, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(clusterResponse, "10s").Should(gexec.Exit(0))
	Expect(string(clusterResponse.Out.Contents())).To(ContainSubstring("dra-example-driver-cluster"))

	By("Verifying if the nodes are up")
	// Check if the nodes are up
	cmdGetNodes := exec.Command("kubectl", "get", "nodes")
	nodeResponse, err := gexec.Start(cmdGetNodes, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(nodeResponse, "10s").Should(gexec.Exit(0))
	Expect(string(nodeResponse.Out.Contents())).To(And(ContainSubstring("control-plane"), ContainSubstring("worker")))

	By("Waiting for the node to move to Ready state")
	// Check if the worker node is in Ready state
	cmdWaitForNode := exec.Command("kubectl", "wait", "--for=condition=Ready", "nodes/dra-example-driver-cluster-worker", "--timeout=120s")
	waitResponse, err := gexec.Start(cmdWaitForNode, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(waitResponse, "10s").Should(gexec.Exit(0))
	Expect(string(waitResponse.Out.Contents())).To(ContainSubstring("condition met"))

	By("Ensuring the webhook is ready")
	Eventually(func() error {
		return verifyWebhook(ctx)
	}, "15s", "1s").WithContext(ctx).Should(Succeed())

	By("Deploying all the GPU test files")
	// Deploy all the test files
	for _, file := range demoFiles {
		absPath := filepath.Join(rootDir, "demo", file+".yaml")
		createPod := exec.Command("kubectl", "create", "-f", absPath)
		createPodResponse, err := gexec.Start(createPod, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())
		Eventually(createPodResponse, "10s").Should(gexec.Exit(0))
	}
})

var _ = Describe("Test GPU allocation", func() {
	Context("GPU Test 1- Two pods, one container each, one GPU per container", func() {
		namespace := "gpu-test1"
		pods := []string{"pod0", "pod1"}
		containerName := "ctr0"
		expectedPodCount := 2
		expectedGPUCount := 1

		It("should have all the pods ready and running", func() {
			checkPodStatus(namespace, pods, expectedPodCount)
		})
		It("should have exactly 1 unclaimed GPU per container", func() {
			for _, podName := range pods {
				var gpus []string
				Eventually(func(g Gomega) {
					// Get pod logs
					logs := getPodLogs(namespace, podName, containerName)
					gpus = getGPUsFromLogs(logs)
					g.Expect(len(gpus)).To(Equal(expectedGPUCount),
						fmt.Sprintf("Expected Pod %s/%s, container %s to have %d GPU, but got %d: %v",
							namespace, podName, containerName, expectedGPUCount, len(gpus), gpus))
					gpuPodCtr := gpus[0]
					g.Expect(isGPUAlreadySeen(gpuPodCtr)).To(Equal(false), fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed", namespace, podName, containerName, gpuPodCtr))
					observedGPUs[gpuPodCtr] = namespace + "/" + podName
					fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, podName, containerName, gpuPodCtr)
				}, "30s", "2s").Should(Succeed())
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
			checkPodStatus(namespace, pods, expectedPodCount)
		})
		It("should have exactly 2 unclaimed GPUs", func() {
			var gpus []string
			Eventually(func(g Gomega) {
				logs := getPodLogs(namespace, pods[0], containerName)
				gpus = getGPUsFromLogs(logs)
				g.Expect(len(gpus)).To(Equal(expectedGPUCount),
					fmt.Sprintf("Expected Pod %s/%s, container %s to have %d GPUs, but got %d: %v",
						namespace, pods[0], containerName, expectedGPUCount, len(gpus), gpus))
				for _, gpu := range gpus {
					g.Expect(isGPUAlreadySeen(gpu)).To(Equal(false), fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed", namespace, pods[0], containerName, gpu))
					observedGPUs[gpu] = namespace + "/" + pods[0]
					fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, pods[0], containerName, gpu)
				}
			}, "30s", "2s").Should(Succeed())

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
			checkPodStatus(namespace, pods, expectedPodCount)
		})
		It("should have 1 GPU shared in time with default timeslice interval for each container", func() {
			var gpus []string
			var gpuPod0Ctr0, gpuPod0Ctr1 string
			for _, containerName := range containerNames {
				Eventually(func(g Gomega) {
					logs := getPodLogs(namespace, pods[0], containerName)
					gpus = getGPUsFromLogs(logs)
					g.Expect(len(gpus)).To(Equal(expectedGPUCount),
						fmt.Sprintf("Expected Pod %s/%s, container %s to have %d GPUs, but got %d: %v",
							namespace, pods[0], containerName, expectedGPUCount, len(gpus), gpus))
					if containerName == "ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuPod0Ctr0 = gpus[0]
						g.Expect(isGPUAlreadySeen(gpuPod0Ctr0)).To(Equal(false), fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed", namespace, pods[0], containerName, gpuPod0Ctr0))
						observedGPUs[gpuPod0Ctr0] = namespace + "/" + pods[0]
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, pods[0], containerName, gpuPod0Ctr0)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containerName))
						gpuPod0Ctr1 = gpus[0]
						g.Expect(gpuPod0Ctr1).To(Equal(gpuPod0Ctr0), fmt.Sprintf("Pod %s/%s, container %s should claim the same GPU as Pod %s, container ctr0, but did not", namespace, pods[0], containerName, observedGPUs[gpuPod0Ctr0]))
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, pods[0], containerName, gpuPod0Ctr1)
					}
					sharingStrategy := extractGPUProperty(logs, getGPUID(gpus[0]), "SHARING_STRATEGY")
					timeSliceInterval := extractGPUProperty(logs, getGPUID(gpus[0]), "TIMESLICE_INTERVAL")
					g.Expect(sharingStrategy).To(Equal(expectedSharingStrategy), fmt.Sprintf("Expected Pod %s/%s, container %s to have sharing strategy %s, got %s", namespace, pods[0], containerName, expectedSharingStrategy, sharingStrategy))
					g.Expect(timeSliceInterval).To(Equal(expectedTimeSliceInterval), fmt.Sprintf("Expected Pod %s/%s, container %s to have timeslice interval %s, got %s", namespace, pods[0], containerName, expectedTimeSliceInterval, timeSliceInterval))
				}, "30s", "2s").Should(Succeed())
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
			checkPodStatus(namespace, pods, expectedPodCount)
		})
		It("should have 1 GPU shared in time with default timeslice interval for each pod", func() {
			var gpus []string
			var gpuPod0Ctr0, gpuPod1Ctr0 string
			for _, podName := range pods {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					logs := getPodLogs(namespace, pods[0], containers[0])
					gpus = getGPUsFromLogs(logs)
					g.Expect(len(gpus)).To(Equal(expectedGPUCount),
						fmt.Sprintf("Expected Pod %s/%s, container %s to have %d GPUs, but got %d: %v",
							namespace, podName, containers[0], expectedGPUCount, len(gpus), gpus))
					if podName == "pod0" && containers[0] == "ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containers[0]))
						gpuPod0Ctr0 = gpus[0]
						g.Expect(isGPUAlreadySeen(gpuPod0Ctr0)).To(Equal(false), fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed", namespace, podName, containers[0], gpuPod0Ctr0))
						observedGPUs[gpuPod0Ctr0] = namespace + "/" + podName
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, podName, containers[0], gpuPod0Ctr0)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containers[0]))
						gpuPod1Ctr0 = gpus[0]
						g.Expect(gpuPod1Ctr0).To(Equal(gpuPod0Ctr0), fmt.Sprintf("Pod %s/%s, container %s should claim the same GPU as Pod %s, container ctr0, but did not", namespace, podName, containers[0], observedGPUs[gpuPod0Ctr0]))
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, podName, containers[0], gpuPod1Ctr0)
					}
					sharingStrategy := extractGPUProperty(logs, getGPUID(gpus[0]), "SHARING_STRATEGY")
					timeSliceInterval := extractGPUProperty(logs, getGPUID(gpus[0]), "TIMESLICE_INTERVAL")
					g.Expect(sharingStrategy).To(Equal(expectedSharingStrategy), fmt.Sprintf("Expected Pod %s/%s, container %s to have sharing strategy %s, got %s", namespace, podName, containers[0], expectedSharingStrategy, sharingStrategy))
					g.Expect(timeSliceInterval).To(Equal(expectedTimeSliceInterval), fmt.Sprintf("Expected Pod %s/%s, container %s to have timeslice interval %s, got %s", namespace, podName, containers[0], expectedTimeSliceInterval, timeSliceInterval))
				}, "30s", "2s").Should(Succeed())
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
			checkPodStatus(namespace, pods, expectedPodCount)
		})
		It("should have exactly 1 GPU shared in time with long timeslice interval for each container", func() {
			var gpus []string
			var gpuPod0Ctr0, gpuPod0Ctr1 string
			for _, containerName := range tsContainers {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					logs := getPodLogs(namespace, pods[0], containerName)
					gpus = getGPUsFromLogs(logs)
					g.Expect(len(gpus)).To(Equal(expectedGPUCount),
						fmt.Sprintf("Expected Pod %s/%s, container %s to have %d GPUs, but got %d: %v",
							namespace, pods[0], containerName, expectedGPUCount, len(gpus), gpus))
					if containerName == "ts-ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuPod0Ctr0 = gpus[0]
						g.Expect(isGPUAlreadySeen(gpuPod0Ctr0)).To(Equal(false), fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed", namespace, pods[0], containerName, gpuPod0Ctr0))
						observedGPUs[gpuPod0Ctr0] = namespace + "/" + pods[0]
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, pods[0], containerName, gpuPod0Ctr0)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containerName))
						gpuPod0Ctr1 = gpus[0]
						g.Expect(gpuPod0Ctr1).To(Equal(gpuPod0Ctr0), fmt.Sprintf("Pod %s/%s, container %s should claim the same GPU as Pod %s, container ctr0, but did not", namespace, pods[0], containerName, observedGPUs[gpuPod0Ctr0]))
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, pods[0], containerName, gpuPod0Ctr1)
					}
					sharingStrategy := extractGPUProperty(logs, getGPUID(gpus[0]), "SHARING_STRATEGY")
					timeSliceInterval := extractGPUProperty(logs, getGPUID(gpus[0]), "TIMESLICE_INTERVAL")
					g.Expect(sharingStrategy).To(Equal(expectedTSSharingStrategy), fmt.Sprintf("Expected Pod %s/%s, container %s to have sharing strategy %s, got %s", namespace, pods[0], containerName, expectedTSSharingStrategy, sharingStrategy))
					g.Expect(timeSliceInterval).To(Equal(expectedTimeSliceInterval), fmt.Sprintf("Expected Pod %s/%s, container %s to have timeslice interval %s, got %s", namespace, pods[0], containerName, expectedTimeSliceInterval, timeSliceInterval))
				}, "30s", "2s").Should(Succeed())
			}
		})
		It("should have exactly 1 GPU shared in space for each container", func() {
			var gpus []string
			var gpuPod0Ctr0, gpuPod0Ctr1 string
			for _, containerName := range spContainers {
				Eventually(func(g Gomega) {
					logs := getPodLogs(namespace, pods[0], containerName)
					gpus = getGPUsFromLogs(logs)
					g.Expect(len(gpus)).To(Equal(expectedGPUCount),
						fmt.Sprintf("Expected Pod %s/%s, container %s to have %d GPUs, but got %d: %v",
							namespace, pods[0], containerName, expectedGPUCount, len(gpus), gpus))
					if containerName == "sp-ctr0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuPod0Ctr0 = gpus[0]
						g.Expect(isGPUAlreadySeen(gpuPod0Ctr0)).To(Equal(false), fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed", namespace, pods[0], containerName, gpuPod0Ctr0))
						observedGPUs[gpuPod0Ctr0] = namespace + "/" + pods[0]
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, pods[0], containerName, gpuPod0Ctr0)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containerName))
						gpuPod0Ctr1 = gpus[0]
						g.Expect(gpuPod0Ctr1).To(Equal(gpuPod0Ctr0), fmt.Sprintf("Pod %s/%s, container %s should claim the same GPU as Pod %s, container ctr0, but did not", namespace, pods[0], containerName, observedGPUs[gpuPod0Ctr0]))
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, pods[0], containerName, gpuPod0Ctr1)
					}
					sharingStrategy := extractGPUProperty(logs, getGPUID(gpus[0]), "SHARING_STRATEGY")
					partitionCount := extractGPUProperty(logs, getGPUID(gpus[0]), "PARTITION_COUNT")
					g.Expect(sharingStrategy).To(Equal(expectedSPSharingStrategy), fmt.Sprintf("Expected Pod %s/%s, container %s to have sharing strategy %s, got %s", namespace, pods[0], containerName, expectedSPSharingStrategy, sharingStrategy))
					g.Expect(partitionCount).To(Equal(expectedPartitionCount), fmt.Sprintf("Expected Pod %s/%s, container %s to have partition count %s, got %s", namespace, pods[0], containerName, expectedPartitionCount, partitionCount))
				}, "30s", "2s").Should(Succeed())
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
			checkPodStatus(namespace, pods, expectedPodCount)
		})
		It("should have exactly 1 unclaimed GPU shared in time with default timeslice interval", func() {
			var gpus []string
			var gpuPod0Init0, gpuPod0Ctr0 string
			for _, containerName := range containerNames {
				Eventually(func(g Gomega) {
					By("checking that there is exactly 1 GPU")
					logs := getPodLogs(namespace, pods[0], containerName)
					gpus = getGPUsFromLogs(logs)
					g.Expect(len(gpus)).To(Equal(expectedGPUCount),
						fmt.Sprintf("Expected Pod %s/%s, container %s to have %d GPUs, but got %d: %v",
							namespace, pods[0], containerName, expectedGPUCount, len(gpus), gpus))
					if containerName == "init0" {
						By(fmt.Sprintf("checking that the GPU is unclaimed for %s", containerName))
						gpuPod0Init0 = gpus[0]
						g.Expect(isGPUAlreadySeen(gpuPod0Init0)).To(Equal(false), fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed", namespace, pods[0], containerName, gpuPod0Init0))
						observedGPUs[gpuPod0Init0] = namespace + "/" + pods[0]
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, pods[0], containerName, gpuPod0Init0)
					} else {
						By(fmt.Sprintf("checking that container %s claims the same GPU as of previous", containerName))
						gpuPod0Ctr0 = gpus[0]
						g.Expect(gpuPod0Ctr0).To(Equal(gpuPod0Init0), fmt.Sprintf("Pod %s/%s, container %s should claim the same GPU as Pod %s, container ctr0, but did not", namespace, pods[0], containerName, observedGPUs[gpuPod0Init0]))
						fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, pods[0], containerName, gpuPod0Ctr0)
					}
					sharingStrategy := extractGPUProperty(logs, getGPUID(gpus[0]), "SHARING_STRATEGY")
					timeSliceInterval := extractGPUProperty(logs, getGPUID(gpus[0]), "TIMESLICE_INTERVAL")
					g.Expect(sharingStrategy).To(Equal(expectedSharingStrategy), fmt.Sprintf("Expected Pod %s/%s, container %s to have sharing strategy %s, got %s", namespace, pods[0], containerName, expectedSharingStrategy, sharingStrategy))
					g.Expect(timeSliceInterval).To(Equal(expectedTimeSliceInterval), fmt.Sprintf("Expected Pod %s/%s, container %s to have timeslice interval %s, got %s", namespace, pods[0], containerName, expectedTimeSliceInterval, timeSliceInterval))
				}, "30s", "2s").Should(Succeed())
			}
		})
	})
	Context("GPU Test 7- Test DRAAdminAccess set to true", func() {
		namespace := "gpu-test7"
		pods := []string{"pod0"}
		containerName := "ctr0"
		expectedPodCount := 1

		It("should have all the pods ready and running", func() {
			checkPodStatus(namespace, pods, expectedPodCount)
		})
		It("should have DRA_ADMIN_ACCESS set to true", func() {
			Eventually(func(g Gomega) {
				logs := getPodLogs(namespace, pods[0], containerName)
				draAdminAccess := extractGPUProperty(logs, "", "DRA_ADMIN_ACCESS")
				g.Expect(draAdminAccess).To(Equal("true"), fmt.Sprintf("Expected Pod %s/%s, container %s to have DRA_ADMIN_ACCESS=true, but got %s",
					namespace, pods[0], containerName, draAdminAccess))
				fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s has admin access", namespace, pods[0], containerName)
			}).Should(Succeed())

		})
	})
	Context("Webhooks", func() {
		tests := []struct {
			name     string
			fileName string
		}{
			{name: "v1 ResourceClaim", fileName: "webhook_invalid_v1.yaml"},
			{name: "v1beta1 ResourceClaim", fileName: "webhook_invalid_v1beta1.yaml"},
			{name: "v1 ResourceClaimTemplate", fileName: "webhook_invalid_rc_template.yaml"},
		}

		for _, testCase := range tests {
			It("should reject invalid "+testCase.name, func(ctx SpecContext) {
				manifestPath := filepath.Join(currentDir, "webhooks", testCase.fileName)

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

func verifyWebhook(ctx context.Context) error {
	fmt.Fprintln(GinkgoWriter, "Waiting for webhook to be available")
	manifestFile := filepath.Join(currentDir, "webhooks", "webhook.yaml")
	cmd := exec.CommandContext(ctx, "kubectl", "create", "--dry-run=server", "-f", manifestFile)

	// Run returns an error if kubectl fails
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("webhook not ready: %w", err)
	}
	return nil
}

func checkPodStatus(namespace string, pods []string, expectedPodCount int) {
	// check if the pods are Ready
	for _, podName := range pods {
		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace, "-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`)
			session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())
			Eventually(session, "10s").Should(gexec.Exit(0))
			return strings.TrimSpace(string(session.Out.Contents()))
		}, "120s", "5s").Should(Equal("True"))
	}
	// check if the pods are in Running state
	cmd := exec.Command("kubectl", "get", "pods", "-n", namespace)
	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(session, "10s").Should(gexec.Exit(0))
	runningPodCount := strings.Count(string(session.Out.Contents()), "Running")
	Expect(runningPodCount).To(Equal(expectedPodCount))
}

func getPodLogs(namespace, pod, container string) string {
	cmd := exec.Command("kubectl", "logs", "-n", namespace, pod, "-c", container)
	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(session, "10s").Should(gexec.Exit(0))

	logs := string(session.Out.Contents())
	return logs
}
func getGPUsFromLogs(logs string) []string {
	re := regexp.MustCompile(`(?m)^declare -x GPU_DEVICE_[0-9]+="(.+)"$`)
	matches := re.FindAllStringSubmatch(logs, -1)

	var gpus []string
	for _, m := range matches {
		if len(m) > 1 {
			gpus = append(gpus, m[1])
		}
	}
	return gpus
}

func isGPUAlreadySeen(gpu string) bool {
	if _, alreadySeen := observedGPUs[gpu]; alreadySeen {
		return true
	}
	return false
}

func extractGPUProperty(logs string, id string, property string) string {
	var pattern string
	if property == "DRA_ADMIN_ACCESS" {
		pattern = fmt.Sprintf(`(?m)^declare -x %s="(.+)"$`, property)
	} else {
		pattern = fmt.Sprintf(`(?m)^declare -x GPU_DEVICE_%s_%s="(.+)"$`, id, property)
	}
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(logs, -1)

	if len(matches) > 0 && len(matches[0]) > 1 {
		return matches[0][1]
	}
	return ""
}

func getGPUID(gpu string) string {
	re := regexp.MustCompile(`^gpu-([0-9]+)$`)
	matches := re.FindAllStringSubmatch(gpu, -1)
	if len(matches) > 0 && len(matches[0]) > 1 {
		return matches[0][1]
	}
	return ""
}

var _ = AfterSuite(func() {
	// Pod deletion should be fast (less than the default grace period of 30s)
	// see https://github.com/kubernetes/kubernetes/issues/127188 for details
	for _, file := range demoFiles {
		absPath := filepath.Join(rootDir, "demo", file+".yaml")
		deletePod := exec.Command("kubectl", "delete", "-f", absPath)
		deletePodResponse, err := gexec.Start(deletePod, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())
		Eventually(deletePodResponse, "25s", "1s").Should(gexec.Exit(0), fmt.Sprintf("Failed to delete resource in %s within 25s", file))
	}
})
