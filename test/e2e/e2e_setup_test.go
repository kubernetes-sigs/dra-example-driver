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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

var rootDir, demoManifestsDir string
var clientset *kubernetes.Clientset
var dynamicClient dynamic.Interface
var restMapper meta.RESTMapper

// driverPodSelector finds kubelet plugin Pods within an installed driver's release namespace.
const driverPodSelector = "app.kubernetes.io/component=kubeletplugin"

// defaultGPUDeviceClassName is the driver name baked into demo manifests
// and webhook testdata; deployManifest substitutes it for the per-test
// driver name, and the webhook tests pin their installed driver to it so
// their static testdata stays valid.
const defaultGPUDeviceClassName = "gpu.example.com"

// defaultCPUDeviceClassName is the driver name baked into demo manifests
// using the cpu profile; deployManifest substitutes it for the per-test
// driver name.
const defaultCPUDeviceClassName = "cpu.example.com"

// defaultDeviceClassNames are the driver names baked into demo manifests as
// placeholders; deployManifest substitutes each for the per-test driver name.
var defaultDeviceClassNames = []string{defaultGPUDeviceClassName, defaultCPUDeviceClassName}

// defaultExtendedResourceName is the extended resource name baked into demo
// manifests; deployManifest substitutes it when ExtendedResourceName is set.
const defaultExtendedResourceName = "example.com/gpu"

func init() {
	cwd, _ := os.Getwd()
	rootDir = filepath.Join(filepath.Dir(cwd), "..")
	// command line flag for demo manifests directory
	flag.StringVar(&demoManifestsDir, "demo-manifests-dir", filepath.Join(rootDir, "demo"), "Directory containing demo YAML manifests")
}

const (
	checkPodLogsTimeout  = "30s"
	checkPodLogsInterval = "1s"
)

var (
	gpuDeviceRegexp = regexp.MustCompile(`(?m)^declare -x GPU_DEVICE_[A-Z0-9_]+="(gpu-.+)"$`)
	gpuIDRegexp     = regexp.MustCompile(`^gpu-(.+)$`)
)

func TestE2e(t *testing.T) {
	flag.Parse()
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
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
})

// deployManifest reads a demo manifest file, substitutes per-test driver
// identifiers, creates the resulting objects, and registers cleanup and
// failure diagnostics via DeferCleanup. Substitution rules:
//   - any name in defaultDeviceClassNames -> drv.DriverName (always applied)
//   - "example.com/gpu" -> drv.ExtendedResourceName (only when set)
func deployManifest(ctx context.Context, namespace, manifestFile string, drv installedDriver) {
	GinkgoHelper()
	absPath := filepath.Join(demoManifestsDir, manifestFile)
	raw, err := os.ReadFile(absPath)
	Expect(err).NotTo(HaveOccurred(), "Failed to read manifest %s", absPath)

	transformed := substituteDriverIdentifiers(string(raw), drv)
	objects, err := parseManifests(transformed)
	Expect(err).NotTo(HaveOccurred(), "Failed to parse manifest %s", absPath)

	err = createObjects(ctx, dynamicClient, objects, false)
	Expect(err).NotTo(HaveOccurred(), "Failed to create objects from %s", absPath)

	// DeferCleanup is LIFO: register cleanup first, then diagnostics second.
	// On teardown, diagnostics run first (while pods exist), then cleanup deletes them.
	DeferCleanup(func(ctx context.Context) {
		deleteObjects(ctx, dynamicClient, objects)
	}, NodeTimeout(30*time.Second))
	DeferCleanup(dumpDiagnosticsOnFailure, namespace, NodeTimeout(15*time.Second))
}

// substituteDriverIdentifiers replaces the demo manifest driver identifiers
// with the per-test driver name and (when set) extended resource name.
func substituteDriverIdentifiers(raw string, drv installedDriver) string {
	GinkgoHelper()
	out := raw
	for _, placeholder := range defaultDeviceClassNames {
		out = strings.ReplaceAll(out, placeholder, drv.DriverName)
	}
	if drv.ExtendedResourceName != "" {
		out = strings.ReplaceAll(out, defaultExtendedResourceName, drv.ExtendedResourceName)
	}
	// Guard against demo manifests that drift from convention.
	for _, placeholder := range defaultDeviceClassNames {
		Expect(out).NotTo(ContainSubstring(placeholder),
			"Manifest still references %q after substitution; deployManifest must replace every occurrence",
			placeholder)
	}
	return out
}

// dumpDiagnosticsOnFailure collects pod status and events for the workload
// namespace when a test has failed. Driver-pod logs are dumped separately by
// dumpDriverDiagnostics.
func dumpDiagnosticsOnFailure(ctx context.Context, namespace string) {
	if !CurrentSpecReport().Failed() {
		return
	}

	fmt.Fprintf(GinkgoWriter, "\n=== Failure diagnostics for namespace %s ===\n", namespace)

	// Pod status
	podList, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "Failed to list pods: %v\n", err)
	} else {
		for _, pod := range podList.Items {
			fmt.Fprintf(GinkgoWriter, "Pod %s: phase=%s conditions=%v\n",
				pod.Name, pod.Status.Phase, pod.Status.Conditions)
		}
	}

	// Events
	events, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "Failed to list events: %v\n", err)
	} else {
		for _, e := range events.Items {
			fmt.Fprintf(GinkgoWriter, "Event %s/%s: %s %s\n",
				e.InvolvedObject.Kind, e.InvolvedObject.Name, e.Reason, e.Message)
		}
	}
}

// parseManifests decodes a YAML document (multiple objects allowed, separated
// by ---) into unstructured objects. Takes a string so callers can transform
// (e.g., driver-name substitution) before parsing.
func parseManifests(data string) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured
	decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(data), 4096)

	for {
		var obj unstructured.Unstructured
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode object: %w", err)
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

// getGVRForObject returns the GroupVersionResource for an unstructured object.
func getGVRForObject(obj *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	gvk := obj.GroupVersionKind()

	// Use RESTMapper to get the correct resource name
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to get REST mapping for %v: %w", gvk, err)
	}

	return mapping.Resource, nil
}

// createObjects creates a list of unstructured objects using the dynamic client.
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
// and waits for them to be fully removed.
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

		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "Warning: Failed to delete %s/%s in namespace %s: %v\n",
				gvk.Kind, obj.GetName(), namespace, err)
		}
	}

	// Wait for all objects to be fully removed
	for _, obj := range objects {
		gvr, err := getGVRForObject(obj)
		if err != nil {
			continue
		}
		namespace := obj.GetNamespace()
		name := obj.GetName()

		Eventually(func() bool {
			var err error
			if namespace != "" {
				_, err = dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
			} else {
				_, err = dynamicClient.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
			}
			return apierrors.IsNotFound(err)
		}).WithContext(ctx).WithTimeout(30*time.Second).WithPolling(1*time.Second).Should(BeTrue(),
			"Timed out waiting for %s/%s to be deleted", obj.GroupVersionKind().Kind, name)
	}
}

func checkPodsReadyAndRunning(ctx context.Context, namespace string, pods []string) {
	GinkgoHelper()
	// check if the pods are Ready and Running
	for _, podName := range pods {
		Eventually(func(g Gomega) {
			pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(),
				"Failed to get pod %s/%s", namespace, podName)
			g.Expect(pod.Status.Phase).To(Equal(v1.PodRunning),
				"Pod %s/%s has phase %s, expected Running (conditions: %v)",
				namespace, podName, pod.Status.Phase, pod.Status.Conditions)
			ready := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
					ready = true
					break
				}
			}
			g.Expect(ready).To(BeTrue(),
				"Pod %s/%s is Running but not Ready (conditions: %v)",
				namespace, podName, pod.Status.Conditions)
		}, "120s", "5s").Should(Succeed())
	}
}

// getGPUsFromPodLogs retrieves pod logs and extracts GPU device information.
// Returns errors via g so callers inside Eventually can retry on transient failures.
func getGPUsFromPodLogs(ctx context.Context, g Gomega, namespace, pod, container string) ([]string, string) {
	GinkgoHelper()
	req := clientset.CoreV1().Pods(namespace).GetLogs(pod, &v1.PodLogOptions{
		Container: container,
	})
	podLogs, err := req.Stream(ctx)
	g.Expect(err).NotTo(HaveOccurred(),
		"Failed to stream logs for pod %s/%s, container %s", namespace, pod, container)
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	g.Expect(err).NotTo(HaveOccurred(),
		"Failed to read logs for pod %s/%s, container %s", namespace, pod, container)
	logs := buf.String()

	matches := gpuDeviceRegexp.FindAllStringSubmatch(logs, -1)

	var gpus []string
	for _, m := range matches {
		if len(m) > 1 {
			gpus = append(gpus, m[1])
		}
	}
	return gpus, logs
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
	matches := gpuIDRegexp.FindAllStringSubmatch(gpu, -1)
	if len(matches) > 0 && len(matches[0]) > 1 {
		return strings.ToUpper(strings.ReplaceAll(matches[0][1], "-", "_"))
	}
	return ""
}

// verifyGPUAllocation checks that a pod/container has the expected number of GPUs
// and tracks them in observedGPUs to ensure no GPU is claimed twice within a test.
func verifyGPUAllocation(ctx context.Context, namespace, podName, containerName string, expectedGPUCount int, observedGPUs map[string]string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		// Get pod logs and extract GPUs
		gpus, _ := getGPUsFromPodLogs(ctx, g, namespace, podName, containerName)
		verifyGPUCount(g, gpus, expectedGPUCount, namespace, podName, containerName)

		// Verify each GPU is unclaimed
		for _, gpu := range gpus {
			claimNewGPU(g, observedGPUs, gpu, namespace, podName, containerName)
		}
	}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
}

// verifyDRAAdminAccess verifies that DRA_ADMIN_ACCESS is set to the expected value.
func verifyDRAAdminAccess(ctx context.Context, namespace, podName, containerName, expectedValue string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		_, logs := getGPUsFromPodLogs(ctx, g, namespace, podName, containerName)
		draAdminAccess := extractGPUProperty(logs, "", "DRA_ADMIN_ACCESS")
		g.Expect(draAdminAccess).To(Equal(expectedValue),
			fmt.Sprintf("Expected Pod %s/%s, container %s to have DRA_ADMIN_ACCESS=%s, but got %s",
				namespace, podName, containerName, expectedValue, draAdminAccess))
		fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s has DRA_ADMIN_ACCESS=%s\n",
			namespace, podName, containerName, draAdminAccess)
	}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
}

// verifyExtendedResourceClaimStatus verifies that the pod's
// extendedResourceClaimStatus has a request mapping for the given container
// that references the expected extended-resource name.
func verifyExtendedResourceClaimStatus(ctx context.Context, namespace, podName, containerName, expectedResourceName string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred(),
			"Failed to get pod %s/%s", namespace, podName)
		g.Expect(pod.Status.ExtendedResourceClaimStatus).NotTo(BeNil(),
			"Pod %s/%s has no extendedResourceClaimStatus", namespace, podName)
		g.Expect(pod.Status.ExtendedResourceClaimStatus.ResourceClaimName).NotTo(BeEmpty(),
			"Pod %s/%s extendedResourceClaimStatus has empty resourceClaimName", namespace, podName)

		var matched *v1.ContainerExtendedResourceRequest
		for i := range pod.Status.ExtendedResourceClaimStatus.RequestMappings {
			m := &pod.Status.ExtendedResourceClaimStatus.RequestMappings[i]
			if m.ContainerName == containerName && m.ResourceName == expectedResourceName {
				matched = m
				break
			}
		}
		g.Expect(matched).NotTo(BeNil(),
			"Pod %s/%s extendedResourceClaimStatus has no requestMapping for container %s with resourceName %s; got: %+v",
			namespace, podName, containerName, expectedResourceName,
			pod.Status.ExtendedResourceClaimStatus.RequestMappings)
		g.Expect(matched.RequestName).NotTo(BeEmpty(),
			"Pod %s/%s extendedResourceClaimStatus requestMapping for container %s has empty requestName",
			namespace, podName, containerName)
		fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s has extendedResourceClaimStatus mapping %s -> %s\n",
			namespace, podName, containerName, expectedResourceName, matched.RequestName)
	}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
}

// verifyNodeAllocatableResourceClaimStatus verifies that the pod's
// nodeAllocatableResourceClaimStatuses contains the expected per-resource
// quantities for the (template-generated) ResourceClaim of podLocalClaimName.
func verifyNodeAllocatableResourceClaimStatus(ctx context.Context, namespace, podName, podLocalClaimName, containerName string, expectedResources v1.ResourceList) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred(),
			"Failed to get pod %s/%s", namespace, podName)

		rcsIdx := slices.IndexFunc(pod.Status.ResourceClaimStatuses, func(s v1.PodResourceClaimStatus) bool {
			return s.Name == podLocalClaimName && s.ResourceClaimName != nil
		})
		g.Expect(rcsIdx).NotTo(Equal(-1),
			"Pod %s/%s has no resourceClaimStatuses entry for pod-local claim %q; status: %+v",
			namespace, podName, podLocalClaimName, pod.Status.ResourceClaimStatuses)
		generatedClaimName := *pod.Status.ResourceClaimStatuses[rcsIdx].ResourceClaimName

		statusIdx := slices.IndexFunc(pod.Status.NodeAllocatableResourceClaimStatuses, func(s v1.NodeAllocatableResourceClaimStatus) bool {
			return s.ResourceClaimName == generatedClaimName
		})
		g.Expect(statusIdx).NotTo(Equal(-1),
			"Pod %s/%s has no nodeAllocatableResourceClaimStatuses entry for claim %q; status: %+v",
			namespace, podName, generatedClaimName, pod.Status.NodeAllocatableResourceClaimStatuses)
		matched := &pod.Status.NodeAllocatableResourceClaimStatuses[statusIdx]

		g.Expect(matched.Containers).To(ContainElement(containerName),
			"Pod %s/%s nodeAllocatableResourceClaimStatuses entry for claim %q does not list container %q (got %v)",
			namespace, podName, generatedClaimName, containerName, matched.Containers)

		for resourceName, expected := range expectedResources {
			actual, ok := matched.Resources[resourceName]
			g.Expect(ok).To(BeTrue(),
				"Pod %s/%s nodeAllocatableResourceClaimStatuses entry for claim %q has no %s resource: %+v",
				namespace, podName, generatedClaimName, resourceName, matched.Resources)
			g.Expect(actual.Cmp(expected)).To(BeZero(),
				"Pod %s/%s claim %q resource %s: got %s, want %s",
				namespace, podName, generatedClaimName, resourceName, actual.String(), expected.String())
		}

		fmt.Fprintf(GinkgoWriter, "Pod %s/%s claim %q has nodeAllocatableResourceClaimStatus %+v for container %s\n",
			namespace, podName, generatedClaimName, matched.Resources, containerName)
	}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
}

// deviceShare is one allocation result's device name and ShareID.
type deviceShare struct {
	device  string
	shareID string
}

// getClaimDeviceShares returns the device name and ShareID for every allocation
// result of the (template-generated) ResourceClaim referenced by
// podLocalClaimName. ShareID is the empty string when an allocation has none.
// It retries until the claim exists and is allocated.
func getClaimDeviceShares(ctx context.Context, namespace, podName, podLocalClaimName string) []deviceShare {
	GinkgoHelper()
	var shares []deviceShare
	Eventually(func(g Gomega) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred(), "Failed to get pod %s/%s", namespace, podName)

		rcsIdx := slices.IndexFunc(pod.Status.ResourceClaimStatuses, func(s v1.PodResourceClaimStatus) bool {
			return s.Name == podLocalClaimName && s.ResourceClaimName != nil
		})
		g.Expect(rcsIdx).NotTo(Equal(-1),
			"Pod %s/%s has no resourceClaimStatuses entry for pod-local claim %q; status: %+v",
			namespace, podName, podLocalClaimName, pod.Status.ResourceClaimStatuses)
		claimName := *pod.Status.ResourceClaimStatuses[rcsIdx].ResourceClaimName

		claim, err := clientset.ResourceV1().ResourceClaims(namespace).Get(ctx, claimName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred(), "Failed to get ResourceClaim %s/%s", namespace, claimName)
		g.Expect(claim.Status.Allocation).NotTo(BeNil(),
			"ResourceClaim %s/%s is not yet allocated", namespace, claimName)
		g.Expect(claim.Status.Allocation.Devices.Results).NotTo(BeEmpty(),
			"ResourceClaim %s/%s has no allocated devices", namespace, claimName)

		results := claim.Status.Allocation.Devices.Results
		shares = make([]deviceShare, 0, len(results))
		for _, result := range results {
			share := deviceShare{device: result.Device}
			if result.ShareID != nil {
				share.shareID = string(*result.ShareID)
			}
			shares = append(shares, share)
		}
	}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
	return shares
}

// expectedMapping describes the asserted shape of a single
// NodeAllocatableResourceMapping. Exactly one of AllocationMultiplier and
// CapacityKey should be set; whichever is set is what the helper asserts.
type expectedMapping struct {
	AllocationMultiplier *resource.Quantity
	CapacityKey          *resourceapi.QualifiedName
}

// waitForResourceSlicesWithNodeAllocatableMappings waits until every device
// published by driverName carries NodeAllocatableResourceMappings matching
// expected. Closes the race between driver install and ResourceSlice publication.
func waitForResourceSlicesWithNodeAllocatableMappings(ctx context.Context, driverName string, expected map[v1.ResourceName]expectedMapping) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		slices, err := clientset.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{
			FieldSelector: "spec.driver=" + driverName,
		})
		g.Expect(err).NotTo(HaveOccurred(), "failed to list ResourceSlices")
		g.Expect(slices.Items).NotTo(BeEmpty(),
			"no ResourceSlices yet published for driver %q", driverName)

		var matched int
		for _, slice := range slices.Items {
			for _, device := range slice.Spec.Devices {
				mappings := device.NodeAllocatableResourceMappings
				g.Expect(mappings).NotTo(BeNil(),
					"device %q in slice %q has no NodeAllocatableResourceMappings", device.Name, slice.Name)

				for resourceName, exp := range expected {
					m, ok := mappings[resourceName]
					g.Expect(ok).To(BeTrue(), "device %q missing %s mapping", device.Name, resourceName)
					switch {
					case exp.AllocationMultiplier != nil:
						g.Expect(m.AllocationMultiplier).NotTo(BeNil(), "device %q %s mapping has no AllocationMultiplier", device.Name, resourceName)
						g.Expect(m.AllocationMultiplier.Cmp(*exp.AllocationMultiplier)).To(BeZero(),
							"device %q %s multiplier = %s, want %s", device.Name, resourceName, m.AllocationMultiplier.String(), exp.AllocationMultiplier.String())
					case exp.CapacityKey != nil:
						g.Expect(m.CapacityKey).NotTo(BeNil(), "device %q %s mapping has no CapacityKey", device.Name, resourceName)
						g.Expect(*m.CapacityKey).To(Equal(*exp.CapacityKey),
							"device %q %s capacityKey = %q, want %q", device.Name, resourceName, *m.CapacityKey, *exp.CapacityKey)
					default:
						Fail(fmt.Sprintf("expectedMapping for %s must set either AllocationMultiplier or CapacityKey", resourceName))
					}
				}
				matched++
			}
		}
		g.Expect(matched).To(BeNumerically(">", 0),
			"no devices for driver %q advertise NodeAllocatableResourceMappings yet", driverName)
	}, "60s", "2s").Should(Succeed())
}

// claimNewGPU verifies that a GPU is unclaimed and adds it to observedGPUs.
func claimNewGPU(g Gomega, observedGPUs map[string]string, gpu, namespace, podName, containerName string) {
	GinkgoHelper()
	claimedBy, alreadySeen := observedGPUs[gpu]
	g.Expect(alreadySeen).To(BeFalse(),
		fmt.Sprintf("Pod %s/%s, container %s should have a new GPU but claimed %s which is already claimed by %s",
			namespace, podName, containerName, gpu, claimedBy))
	observedGPUs[gpu] = namespace + "/" + podName
	fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s claimed %s\n",
		namespace, podName, containerName, gpu)
}

// verifyGPUCount verifies that a container has the expected number of GPUs.
func verifyGPUCount(g Gomega, gpus []string, expectedGPUCount int, namespace, podName, containerName string) {
	GinkgoHelper()
	g.Expect(gpus).To(HaveLen(expectedGPUCount),
		fmt.Sprintf("Expected Pod %s/%s, container %s to have %d GPUs, but got %d: %v",
			namespace, podName, containerName, expectedGPUCount, len(gpus), gpus))
}

// verifyGPUProperties verifies GPU sharing strategy and an optional additional property.
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

// podContainer identifies a specific container in a specific pod.
type podContainer struct {
	pod       string
	container string
}

// sharingGroup describes a set of pod/containers that should all share the same GPU,
// along with the expected sharing properties.
type sharingGroup struct {
	// members lists the pod/container pairs that should all see the same GPU.
	members []podContainer
	// expectedStrategy is the expected GPU sharing strategy (e.g. "TimeSlicing", "SpacePartitioning").
	expectedStrategy string
	// expectedProperty is the name of the GPU property to verify (e.g. "TIMESLICE_INTERVAL", "PARTITION_COUNT").
	expectedProperty string
	// expectedPropValue is the expected value of expectedProperty (e.g. "Default", "Long", "10").
	expectedPropValue string
}

// verifySharedGPUGroup verifies that all members of a sharing group see the same GPU
// and that the GPU has the expected sharing properties.
func verifySharedGPUGroup(ctx context.Context, namespace string, group sharingGroup) {
	GinkgoHelper()
	var firstGPU string
	for i, member := range group.members {
		Eventually(func(g Gomega) {
			gpus, logs := getGPUsFromPodLogs(ctx, g, namespace, member.pod, member.container)
			verifyGPUCount(g, gpus, 1, namespace, member.pod, member.container)
			if i == 0 {
				firstGPU = gpus[0]
				fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s has GPU %s (first in group)\n",
					namespace, member.pod, member.container, firstGPU)
			} else {
				g.Expect(gpus[0]).To(Equal(firstGPU),
					fmt.Sprintf("Pod %s/%s, container %s should claim the same GPU as previous, but got %s instead of %s",
						namespace, member.pod, member.container, gpus[0], firstGPU))
				fmt.Fprintf(GinkgoWriter, "Pod %s/%s, container %s shares GPU %s\n",
					namespace, member.pod, member.container, gpus[0])
			}
			verifyGPUProperties(g, logs, namespace, member.pod, member.container, gpus,
				group.expectedStrategy, group.expectedProperty, group.expectedPropValue)
		}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
	}
}

// verifyResourceClaimDeviceStatus verifies that the ResourceClaim(s) associated
// with the given pod expose per-device data.
func verifyResourceClaimDeviceStatus(ctx context.Context, namespace, podName string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred(),
			"Failed to get pod %s/%s", namespace, podName)
		g.Expect(pod.Status.ResourceClaimStatuses).NotTo(BeEmpty(),
			"Pod %s/%s has no ResourceClaimStatuses", namespace, podName)

		seenClaimWithData := false
		for _, rcs := range pod.Status.ResourceClaimStatuses {
			g.Expect(rcs.ResourceClaimName).NotTo(BeNil(),
				"Pod %s/%s ResourceClaimStatus %q has no resourceClaimName", namespace, podName, rcs.Name)
			claim, err := clientset.ResourceV1().ResourceClaims(namespace).Get(ctx, *rcs.ResourceClaimName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(),
				"Failed to get ResourceClaim %s/%s", namespace, *rcs.ResourceClaimName)
			g.Expect(claim.Status.Devices).NotTo(BeEmpty(),
				"ResourceClaim %s/%s has no status.devices entries", namespace, *rcs.ResourceClaimName)

			for _, d := range claim.Status.Devices {
				g.Expect(d.Data).NotTo(BeNil(),
					"ResourceClaim %s/%s device %s has no status.data", namespace, *rcs.ResourceClaimName, d.Device)
				var data map[string]resourceapi.DeviceAttribute
				g.Expect(json.Unmarshal(d.Data.Raw, &data)).To(Succeed(),
					"ResourceClaim %s/%s device %s has unparseable status.data", namespace, *rcs.ResourceClaimName, d.Device)

				g.Expect(data).To(HaveKey("uuid"),
					"ResourceClaim %s/%s device %s status.data is missing uuid: %v", namespace, *rcs.ResourceClaimName, d.Device, data)
				g.Expect(data["uuid"].StringValue).NotTo(BeNil())
				g.Expect(*data["uuid"].StringValue).NotTo(BeEmpty())

				g.Expect(data).To(HaveKey("model"),
					"ResourceClaim %s/%s device %s status.data is missing model: %v", namespace, *rcs.ResourceClaimName, d.Device, data)
				g.Expect(data["model"].StringValue).NotTo(BeNil())
				g.Expect(*data["model"].StringValue).NotTo(BeEmpty())

				g.Expect(data).To(HaveKey("driverVersion"),
					"ResourceClaim %s/%s device %s status.data is missing driverVersion: %v", namespace, *rcs.ResourceClaimName, d.Device, data)
				g.Expect(data["driverVersion"].VersionValue).NotTo(BeNil())
				g.Expect(*data["driverVersion"].VersionValue).NotTo(BeEmpty())

				fmt.Fprintf(GinkgoWriter, "ResourceClaim %s/%s device %s has status.data: uuid=%s model=%s driverVersion=%s\n",
					namespace, *rcs.ResourceClaimName, d.Device,
					*data["uuid"].StringValue, *data["model"].StringValue, *data["driverVersion"].VersionValue)
				seenClaimWithData = true
			}
		}
		g.Expect(seenClaimWithData).To(BeTrue(),
			"Pod %s/%s: no ResourceClaim observed with non-empty status.devices[].data", namespace, podName)
	}, checkPodLogsTimeout, checkPodLogsInterval).Should(Succeed())
}

// verifyResourceSliceBindingConditions verifies that all devices published in
// ResourceSlices for the given driver have bindingConditions set.
func verifyResourceSliceBindingConditions(ctx context.Context, driverName string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		slices, err := clientset.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{
			FieldSelector: "spec.driver=" + driverName,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(slices.Items).NotTo(BeEmpty(), "No ResourceSlices found")

		for _, slice := range slices.Items {
			for _, device := range slice.Spec.Devices {
				g.Expect(device.BindingConditions).NotTo(BeEmpty(),
					"Device %s in ResourceSlice %s: bindingConditions should not be empty", device.Name, slice.Name)
				fmt.Fprintf(GinkgoWriter, "Device %s in ResourceSlice %s: bindingConditions=%v\n",
					device.Name, slice.Name, device.BindingConditions)
			}
		}
	}, "30s", "2s").Should(Succeed())
}
