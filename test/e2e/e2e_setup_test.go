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
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

var rootDir, currentDir, demoManifestsDir string
var observedGPUs map[string]string
var demoFiles = []string{"gpu-test1", "gpu-test2", "gpu-test3", "gpu-test7", "gpu-test4", "gpu-test5", "gpu-test6", "gpu-test8"}
var clientset *kubernetes.Clientset
var dynamicClient dynamic.Interface

func init() {
	currentDir, _ = os.Getwd()
	rootDir = filepath.Join(filepath.Dir(currentDir), "..")
	observedGPUs = make(map[string]string)
	// command line flag for demo manifests directory
	flag.StringVar(&demoManifestsDir, "demo-manifests-dir", filepath.Join(rootDir, "demo"), "Directory containing demo YAML manifests")
}

const (
	timeout = "30s"
	interval = "1s"
)
func TestE2e(t *testing.T) {
	flag.Parse()
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	suiteConfig, _ := GinkgoConfiguration()
	if suiteConfig.ParallelTotal > 1 {
		Fail("tests cannot be run in parallel")
	}
	// Manually append bin paths if they are missing
	path := os.Getenv("PATH")
	home, _ := os.UserHomeDir()
	goBin := home + "/go/bin"

	if !strings.Contains(path, goBin) {
		os.Setenv("PATH", path+":"+goBin+":/usr/local/bin")
	}

	// Create a Kubernetes clientset
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	Expect(err).NotTo(HaveOccurred())

	clientset, err = kubernetes.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred())

	dynamicClient, err = dynamic.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred())

	// Check if the nodes are up
	By("Verifying if the nodes are up")
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	var nodeNames []string
	for _, node := range nodes.Items {
		nodeNames = append(nodeNames, node.Name)
	}
	Expect(nodeNames).To(SatisfyAll(
    	ContainElement(ContainSubstring("control-plane")),
    	ContainElement(ContainSubstring("worker")),
	))

	// Check if the worker node is in Ready state
	By("Waiting for the node to move to Ready state")
	nodeName := "dra-example-driver-cluster-worker"
	Eventually(func(g Gomega) {
    	// Get the latest state of the node
		node, err := clientset.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		// Check if the node is ready
		isReady := false
		for _, cond := range node.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				isReady = true
				break
			}
		}
    	g.Expect(isReady).To(BeTrue(), "Expected node %s to be Ready", nodeName)
	}, "120s", "2s").Should(Succeed())
	
	// Check if the webhook is ready
	By("Ensuring the webhook is ready")
	manifestPath := filepath.Join(currentDir, "testdata", "webhooks", "resourceclaim.yaml")
	verifyWebhook(ctx, dynamicClient, manifestPath)

	// Deploy all the test files
	By("Deploying all the GPU test files")
	for _, file := range demoFiles {
		absPath := filepath.Join(demoManifestsDir, file+".yaml")
		createOrDeleteManifest(ctx, dynamicClient, absPath, "create")
	}
})

func verifyWebhook(ctx context.Context, dynamicClient dynamic.Interface, manifestPath string) {
	GinkgoHelper()
	fmt.Fprintln(GinkgoWriter, "Waiting for webhook to be available")
	
	data, err := os.ReadFile(manifestPath)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to read manifest file: %s", manifestPath))
	
	// Parse the YAML into an unstructured object
	var obj unstructured.Unstructured
	err = yaml.Unmarshal(data, &obj)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to unmarshal manifest: %s", manifestPath))
	resourceClaim := schema.GroupVersionResource{
		Group:    "resource.k8s.io",
		Version:  "v1",
		Resource: "resourceclaims",
	}
	namespace := obj.GetNamespace()
	if namespace == "" { namespace = "default" }
	// Wait for webhook to be available by trying to create the ResourceClaim with dry-run mode
	Eventually(func() error {
		_, err := dynamicClient.Resource(resourceClaim).Namespace(namespace).Create(
			ctx,
			&obj,
			metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}},
		)
		if err != nil {
			return fmt.Errorf("webhook not ready: %w", err)
		}
		return nil
	}, "15s", interval).WithContext(ctx).Should(Succeed())
}

func createOrDeleteManifest(ctx context.Context, dynamicClient dynamic.Interface, manifestPath string, operation string) {
	GinkgoHelper()
	data, err := os.ReadFile(manifestPath)
	if operation == "delete" && err != nil {
		fmt.Fprintf(GinkgoWriter, "Warning: Failed to read manifest file %s: %v\n", manifestPath, err)
		return
	}
	Expect(err).NotTo(HaveOccurred())
	
	// Split YAML documents
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		var obj unstructured.Unstructured
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			if operation == "delete" {
				fmt.Fprintf(GinkgoWriter, "Warning: Failed to decode object from %s: %v\n", manifestPath, err)
				continue
			}
			Expect(err).NotTo(HaveOccurred())
		}
		if len(obj.Object) == 0 {
			continue
		}
		
		gvk := obj.GroupVersionKind()
		gvr := schema.GroupVersionResource{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Resource: strings.ToLower(gvk.Kind) + "s",
		}
		namespace := obj.GetNamespace()
		if operation == "create" {
			if namespace != "" {
				_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, &obj, metav1.CreateOptions{})
			} else {
				_, err = dynamicClient.Resource(gvr).Create(ctx, &obj, metav1.CreateOptions{})
			}
			Expect(err).NotTo(HaveOccurred())
		} else {
			deletePolicy := metav1.DeletePropagationForeground
			deleteOptions := metav1.DeleteOptions{
				PropagationPolicy: &deletePolicy,
			}
			if namespace != "" {
				err = dynamicClient.Resource(gvr).Namespace(namespace).Delete(ctx, obj.GetName(), deleteOptions)
			} else {
				err = dynamicClient.Resource(gvr).Delete(ctx, obj.GetName(), deleteOptions)
			}
			// Ignore not found errors
			if err != nil && !strings.Contains(err.Error(), "not found") {
				fmt.Fprintf(GinkgoWriter, "Warning: Failed to delete %s/%s in namespace %s: %v\n",
					gvk.Kind, obj.GetName(), namespace, err)
			}
		}
	}
}

func checkPodsReadyAndRunning(namespace string, pods []string, expectedPodCount int) {
	GinkgoHelper()
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

// getGPUsFromPodLogs retrieves pod logs and extracts GPU device information
func getGPUsFromPodLogs(namespace, pod, container string) ([]string, string) {
	GinkgoHelper()
	cmd := exec.Command("kubectl", "logs", "-n", namespace, pod, "-c", container)
	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(session, "10s").Should(gexec.Exit(0))

	logs := string(session.Out.Contents())
	
	re := regexp.MustCompile(`(?m)^declare -x GPU_DEVICE_[0-9]+="(.+)"$`)
	matches := re.FindAllStringSubmatch(logs, -1)

	var gpus []string
	for _, m := range matches {
		if len(m) > 1 {
			gpus = append(gpus, m[1])
		}
	}
	return gpus, logs
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

// verifyGPUAllocation checks that a pod/container has the expected number of GPUs
// and tracks them in observedGPUs to ensure no GPU is claimed twice
func verifyGPUAllocation(namespace, podName, containerName string, expectedGPUCount int) {
	GinkgoHelper()
	var gpus []string
	Eventually(func(g Gomega) {
		// Get pod logs and extract GPUs
		gpus, _ = getGPUsFromPodLogs(namespace, podName, containerName)
		verifyGPUCount(g, gpus, expectedGPUCount, namespace, podName, containerName)
		
		// Verify each GPU is unclaimed
		for _, gpu := range gpus {
			claimNewGPU(g, gpu, namespace, podName, containerName)
		}
	}, timeout, interval).Should(Succeed())
}

// verifyDRAAdminAccess verifies that DRA_ADMIN_ACCESS is set to the expected value
func verifyDRAAdminAccess(namespace, podName, containerName, expectedValue string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		_, logs := getGPUsFromPodLogs(namespace, podName, containerName)
		draAdminAccess := extractGPUProperty(logs, "", "DRA_ADMIN_ACCESS")
		g.Expect(draAdminAccess).To(Equal(expectedValue),
			fmt.Sprintf("Expected Pod %s/%s, container %s to have DRA_ADMIN_ACCESS=%s, but got %s",
				namespace, podName, containerName, expectedValue, draAdminAccess))
		fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s has DRA_ADMIN_ACCESS=%s\n", namespace, podName, containerName, draAdminAccess)
	}, timeout, interval).Should(Succeed())
}
// claimNewGPU verifies that a GPU is unclaimed and adds it to observedGPUs
func claimNewGPU(g Gomega, gpu, namespace, podName, containerName string) {
	GinkgoHelper()
	g.Expect(isGPUAlreadySeen(gpu)).To(Equal(false), 
		fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed", 
			namespace, podName, containerName, gpu))
	observedGPUs[gpu] = namespace + "/" + podName
	fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, podName, containerName, gpu)
}
// verifyGPUCount verifies that a container has the expected number of GPUs
func verifyGPUCount(g Gomega, gpus []string, expectedGPUCount int, namespace, podName, containerName string) {
	GinkgoHelper()
	g.Expect(gpus).To(HaveLen(expectedGPUCount),
		fmt.Sprintf("Expected Pod %s/%s, container %s to have %d GPUs, but got %d: %v",
			namespace, podName, containerName, expectedGPUCount, len(gpus), gpus))
}

// verifySharedGPU verifies that a container reuses the same GPU as expected
func verifySharedGPU(g Gomega, gpu, expectedGPU, namespace, podName, containerName string) {
	GinkgoHelper()
	g.Expect(gpu).To(Equal(expectedGPU), 
		fmt.Sprintf("Pod %s/%s, container %s should claim the same GPU as %s, but did not", 
			namespace, podName, containerName, observedGPUs[expectedGPU]))
	fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s", namespace, podName, containerName, gpu)
}


// verifyGPUProperties verifies GPU sharing strategy and an optional additional property
func verifyGPUProperties(g Gomega, logs, namespace, podName, containerName string, gpus []string, expectedSharingStrategy, expectedProperty, expectedPropertyValue string) {
	GinkgoHelper()
	sharingStrategy := extractGPUProperty(logs, getGPUID(gpus[0]), "SHARING_STRATEGY")
	g.Expect(sharingStrategy).To(Equal(expectedSharingStrategy),
		fmt.Sprintf("Expected Pod %s/%s, container %s to have sharing strategy %s, got %s",
			namespace, podName, containerName, expectedSharingStrategy, sharingStrategy))
	
	if expectedProperty != "" {
		propertyValue := extractGPUProperty(logs, getGPUID(gpus[0]), expectedProperty)
		g.Expect(propertyValue).To(Equal(expectedPropertyValue),
			fmt.Sprintf("Expected Pod %s/%s, container %s to have %s=%s, got %s",
				namespace, podName, containerName, expectedProperty, expectedPropertyValue, propertyValue))
	}
}

var _ = AfterSuite(func(ctx SpecContext) {
	// Pod deletion should be fast (less than the default grace period of 30s)
	// see https://github.com/kubernetes/kubernetes/issues/127188 for details
	for _, file := range demoFiles {
		absPath := filepath.Join(demoManifestsDir, file+".yaml")
		Eventually(func() error {
			createOrDeleteManifest(ctx, dynamicClient, absPath, "delete")
			return nil
		}, "25s", interval).Should(Succeed(), fmt.Sprintf("Failed to delete resources in %s within 25s", file))
	}
})

