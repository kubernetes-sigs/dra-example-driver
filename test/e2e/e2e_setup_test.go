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
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

var rootDir, currentDir, demoManifestsDir string
var clientset *kubernetes.Clientset
var dynamicClient dynamic.Interface
var restMapper meta.RESTMapper

func init() {
	currentDir, _ = os.Getwd()
	rootDir = filepath.Join(filepath.Dir(currentDir), "..")
	// command line flag for demo manifests directory
	flag.StringVar(&demoManifestsDir, "demo-manifests-dir", filepath.Join(rootDir, "demo"), "Directory containing demo YAML manifests")
}

const (
	checkPodLogsTimeout  = "30s"
	checkPodLogsInterval = "1s"
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

	// Create a Kubernetes clientset
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	Expect(err).NotTo(HaveOccurred())

	clientset, err = kubernetes.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred())

	dynamicClient, err = dynamic.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred())

	// Create a RESTMapper to properly map GVK to GVR
	groupResources, err := restmapper.GetAPIGroupResources(clientset.Discovery())
	Expect(err).NotTo(HaveOccurred())
	restMapper = restmapper.NewDiscoveryRESTMapper(groupResources)

	// Check if the webhook is ready
	// Even after verifying that the Pod is Ready and the expected Endpoints resource
	// exists with the Pod's IP, the webhook still seems to have "connection refused"
	// issues, so retry here until we can ensure it's available before the real tests start.
	By("Ensuring the webhook is ready")
	verifyWebhook(ctx)
})

func verifyWebhook(ctx context.Context) {
	GinkgoHelper()
	fmt.Fprintln(GinkgoWriter, "Waiting for webhook to be available")

	// Create a simple ResourceClaim to test webhook availability
	testClaim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webhook-test",
			Namespace: "default",
		},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{
					{
						Name: "gpu",
						Exactly: &resourceapi.ExactDeviceRequest{
							DeviceClassName: "gpu.example.com",
						},
					},
				},
			},
		},
	}

	// Wait for webhook to be available by trying to create the ResourceClaim with dry-run mode
	Eventually(func() error {
		_, err := clientset.ResourceV1().ResourceClaims("default").Create(
			ctx,
			testClaim,
			metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}},
		)
		if err != nil {
			return fmt.Errorf("webhook not ready: %w", err)
		}
		return nil
	}, "30s", "1s").WithContext(ctx).Should(Succeed())
}

// deployManifest creates resources from a manifest file and returns a cleanup function.
func deployManifest(ctx context.Context, manifestFile string) func() {
	GinkgoHelper()
	absPath := filepath.Join(demoManifestsDir, manifestFile)
	createManifest(ctx, dynamicClient, absPath)
	return func() {
		deleteManifest(context.Background(), dynamicClient, absPath)
	}
}

// parseManifests reads a YAML file and returns a slice of unstructured objects
func parseManifests(manifestPath string) ([]*unstructured.Unstructured, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file %s: %w", manifestPath, err)
	}

	var objects []*unstructured.Unstructured
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)

	for {
		var obj unstructured.Unstructured
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode object from %s: %w", manifestPath, err)
		}
		if len(obj.Object) == 0 {
			continue
		}

		// Set default namespace for namespaced resources if not specified
		gvk := obj.GroupVersionKind()
		namespace := obj.GetNamespace()
		if namespace == "" && (gvk.Kind == "ResourceClaim" || gvk.Kind == "ResourceClaimTemplate") {
			obj.SetNamespace("default")
		}

		objects = append(objects, &obj)
	}

	return objects, nil
}

// getGVRForObject returns the GroupVersionResource for an unstructured object
func getGVRForObject(obj *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	gvk := obj.GroupVersionKind()
	
	// Use RESTMapper to get the correct resource name
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to get REST mapping for %v: %w", gvk, err)
	}
	
	return mapping.Resource, nil
}

// createObjects creates a list of unstructured objects using the dynamic client
func createObjects(ctx context.Context, dynamicClient dynamic.Interface, objects []*unstructured.Unstructured, dryRun bool) error {
	GinkgoHelper()
	for _, obj := range objects {
		gvr, err := getGVRForObject(obj)
		if err != nil {
			return fmt.Errorf("failed to get GVR for object %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
		
		namespace := obj.GetNamespace()

		createOptions := metav1.CreateOptions{}
		if dryRun {
			createOptions.DryRun = []string{metav1.DryRunAll}
		}

		if namespace != "" {
			_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, obj, createOptions)
		} else {
			_, err = dynamicClient.Resource(gvr).Create(ctx, obj, createOptions)
		}

		if err != nil {
			if dryRun {
				return err
			}
			Expect(err).NotTo(HaveOccurred())
		}
	}
	return nil
}

// deleteObjects deletes a list of unstructured objects using the dynamic client
func deleteObjects(ctx context.Context, dynamicClient dynamic.Interface, objects []*unstructured.Unstructured) {
	GinkgoHelper()
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	for _, obj := range objects {
		gvr, err := getGVRForObject(obj)
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "Warning: Failed to get GVR for object %s/%s: %v\n",
				obj.GetNamespace(), obj.GetName(), err)
			continue
		}
		
		namespace := obj.GetNamespace()
		gvk := obj.GroupVersionKind()

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

// createManifest creates resources from a manifest file
func createManifest(ctx context.Context, dynamicClient dynamic.Interface, manifestPath string) {
	GinkgoHelper()
	objects, err := parseManifests(manifestPath)
	Expect(err).NotTo(HaveOccurred())

	err = createObjects(ctx, dynamicClient, objects, false)
	Expect(err).NotTo(HaveOccurred())
}

// deleteManifest deletes resources from a manifest file
func deleteManifest(ctx context.Context, dynamicClient dynamic.Interface, manifestPath string) {
	GinkgoHelper()
	objects, err := parseManifests(manifestPath)
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "Warning: %v\n", err)
		return
	}

	deleteObjects(ctx, dynamicClient, objects)
}

// createManifestWithDryRun creates objects from a manifest with dry-run mode
func createManifestWithDryRun(ctx context.Context, dynamicClient dynamic.Interface, manifestPath string) error {
	GinkgoHelper()
	objects, err := parseManifests(manifestPath)
	if err != nil {
		return err
	}
	return createObjects(ctx, dynamicClient, objects, true)
}

func checkPodsReadyAndRunning(namespace string, pods []string, expectedPodCount int) {
	GinkgoHelper()
	for _, podName := range pods {
		Eventually(func(g Gomega) {
			pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Failed to get pod %s/%s", namespace, podName)
			ready := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					ready = true
					break
				}
			}
			g.Expect(ready).To(BeTrue(),
				"Pod %s/%s is not Ready (phase: %s, conditions: %v)",
				namespace, podName, pod.Status.Phase, pod.Status.Conditions)
		}, "120s", "5s").Should(Succeed())
	}
	// check if the pods are in Running state
	podList, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	runningPodCount := 0
	for _, pod := range podList.Items {
		if pod.Status.Phase == "Running" {
			runningPodCount++
		}
	}
	Expect(runningPodCount).To(Equal(expectedPodCount),
		"Expected %d running pods in namespace %s, got %d", expectedPodCount, namespace, runningPodCount)
}

// getGPUsFromPodLogs retrieves pod logs and extracts GPU device information
func getGPUsFromPodLogs(namespace, pod, container string) ([]string, string) {
	GinkgoHelper()
	req := clientset.CoreV1().Pods(namespace).GetLogs(pod, &v1.PodLogOptions{
		Container: container,
	})
	podLogs, err := req.Stream(context.TODO())
	Expect(err).NotTo(HaveOccurred())
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	Expect(err).NotTo(HaveOccurred())
	logs := buf.String()

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

func isGPUAlreadySeen(observedGPUs map[string]string, gpu string) bool {
	_, alreadySeen := observedGPUs[gpu]
	return alreadySeen
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
// and tracks them in observedGPUs to ensure no GPU is claimed twice within a test
func verifyGPUAllocation(namespace, podName, containerName string, expectedGPUCount int, observedGPUs map[string]string) {
	GinkgoHelper()
	var gpus []string
	Eventually(func(g Gomega) {
		// Get pod logs and extract GPUs
		gpus, _ = getGPUsFromPodLogs(namespace, podName, containerName)
		verifyGPUCount(g, gpus, expectedGPUCount, namespace, podName, containerName)

		// Verify each GPU is unclaimed
		for _, gpu := range gpus {
			claimNewGPU(g, observedGPUs, gpu, namespace, podName, containerName)
		}
	}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
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
	}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
}

// claimNewGPU verifies that a GPU is unclaimed and adds it to observedGPUs
func claimNewGPU(g Gomega, observedGPUs map[string]string, gpu, namespace, podName, containerName string) {
	GinkgoHelper()
	g.Expect(isGPUAlreadySeen(observedGPUs, gpu)).To(Equal(false),
		fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed by %s",
			namespace, podName, containerName, gpu, observedGPUs[gpu]))
	observedGPUs[gpu] = namespace + "/" + podName
	fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s\n", namespace, podName, containerName, gpu)
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
		fmt.Sprintf("Pod %s/%s, container %s should claim the same GPU as previous, but got %s instead of %s",
			namespace, podName, containerName, gpu, expectedGPU))
	fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s\n", namespace, podName, containerName, gpu)
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
