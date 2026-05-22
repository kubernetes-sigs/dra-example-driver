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
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	gpuv1alpha1 "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
)

var _ = Describe("Test GPU allocation", func() {
	It("should allocate 1 distinct GPU per pod", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{
			GPUDeviceStatus: true,
		})
		namespace := "basic-resourceclaimtemplate"
		pods := []string{"pod0", "pod1"}
		containerName := "ctr0"
		expectedGPUCount := 1

		deployManifest(ctx, namespace, "basic-resourceclaimtemplate.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)

		observedGPUs := make(map[string]string)
		for _, podName := range pods {
			verifyGPUAllocation(ctx, namespace, podName, containerName, expectedGPUCount, observedGPUs)
			verifyResourceClaimDeviceStatus(ctx, namespace, podName)
		}
	})

	It("should allocate 2 distinct GPUs to a single container", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{})
		namespace := "basic-multiple-requests"
		pods := []string{"pod0"}
		containerName := "ctr0"
		expectedGPUCount := 2

		deployManifest(ctx, namespace, "basic-multiple-requests.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)

		observedGPUs := make(map[string]string)
		verifyGPUAllocation(ctx, namespace, pods[0], containerName, expectedGPUCount, observedGPUs)
	})

	It("should share 1 GPU between containers with TimeSlicing default interval", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{})
		namespace := "basic-shared-claim-across-containers"
		pods := []string{"pod0"}

		deployManifest(ctx, namespace, "basic-shared-claim-across-containers.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)

		verifySharedGPUGroup(ctx, namespace, sharingGroup{
			members: []podContainer{
				{pod: "pod0", container: "ctr0"},
				{pod: "pod0", container: "ctr1"},
			},
			expectedStrategy:  string(gpuv1alpha1.TimeSlicingStrategy),
			expectedProperty:  "TIMESLICE_INTERVAL",
			expectedPropValue: string(gpuv1alpha1.DefaultTimeSlice),
		})
	})

	It("should share 1 GPU between pods with TimeSlicing default interval", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{})
		namespace := "basic-shared-claim-across-pods"
		pods := []string{"pod0", "pod1"}

		deployManifest(ctx, namespace, "basic-shared-claim-across-pods.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)

		verifySharedGPUGroup(ctx, namespace, sharingGroup{
			members: []podContainer{
				{pod: "pod0", container: "ctr0"},
				{pod: "pod1", container: "ctr0"},
			},
			expectedStrategy:  string(gpuv1alpha1.TimeSlicingStrategy),
			expectedProperty:  "TIMESLICE_INTERVAL",
			expectedPropValue: string(gpuv1alpha1.DefaultTimeSlice),
		})
	})

	It("should share GPUs with TimeSlicing and SpacePartitioning", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{})
		namespace := "basic-resourceclaim-opaque-config"
		pods := []string{"pod0"}

		deployManifest(ctx, namespace, "basic-resourceclaim-opaque-config.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)

		verifySharedGPUGroup(ctx, namespace, sharingGroup{
			members: []podContainer{
				{pod: "pod0", container: "ts-ctr0"},
				{pod: "pod0", container: "ts-ctr1"},
			},
			expectedStrategy:  string(gpuv1alpha1.TimeSlicingStrategy),
			expectedProperty:  "TIMESLICE_INTERVAL",
			expectedPropValue: string(gpuv1alpha1.LongTimeSlice),
		})

		verifySharedGPUGroup(ctx, namespace, sharingGroup{
			members: []podContainer{
				{pod: "pod0", container: "sp-ctr0"},
				{pod: "pod0", container: "sp-ctr1"},
			},
			expectedStrategy:  string(gpuv1alpha1.SpacePartitioningStrategy),
			expectedProperty:  "PARTITION_COUNT",
			expectedPropValue: "10",
		})
	})

	It("should share 1 GPU between init container and regular container", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{})
		namespace := "initcontainer-shared-gpu"
		pods := []string{"pod0"}

		deployManifest(ctx, namespace, "initcontainer-shared-gpu.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)

		verifySharedGPUGroup(ctx, namespace, sharingGroup{
			members: []podContainer{
				{pod: "pod0", container: "init0"},
				{pod: "pod0", container: "ctr0"},
			},
			expectedStrategy:  string(gpuv1alpha1.TimeSlicingStrategy),
			expectedProperty:  "TIMESLICE_INTERVAL",
			expectedPropValue: string(gpuv1alpha1.DefaultTimeSlice),
		})
	})

	It("should have DRA_ADMIN_ACCESS set to true", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{})
		namespace := "admin-access"
		pods := []string{"pod0"}
		containerName := "ctr0"

		deployManifest(ctx, namespace, "admin-access.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)
		verifyDRAAdminAccess(ctx, namespace, pods[0], containerName, "true")
	})

	It("should allocate 1 GPU per pod for extended resource requests", func(ctx SpecContext) {
		// Each parallel test must advertise its DeviceClass under a unique
		// extended resource name so KEP-5004 reservations don't collide.
		drv := installDriver(ctx, DriverConfig{
			ExtendedResourceName: "example.com/gpu-ext-resource",
		})
		namespace := "extended-resource-request"
		pods := []string{"pod0", "pod1"}
		containerName := "ctr0"
		expectedGPUCount := 1
		expectedResourceNames := map[string]string{
			"pod0": "deviceclass.resource.kubernetes.io/" + drv.DriverName,
			"pod1": drv.ExtendedResourceName,
		}

		deployManifest(ctx, namespace, "extended-resource-request.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)

		observedGPUs := make(map[string]string)
		for _, podName := range pods {
			verifyGPUAllocation(ctx, namespace, podName, containerName, expectedGPUCount, observedGPUs)
			verifyExtendedResourceClaimStatus(ctx, namespace, podName, containerName, expectedResourceNames[podName])
		}
	})

	It("should allocate 1 GPU selected using CEL expression", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{})
		namespace := "cel-selector"
		pods := []string{"pod0"}
		containerName := "ctr0"
		expectedGPUCount := 1

		deployManifest(ctx, namespace, "cel-selector.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)

		observedGPUs := make(map[string]string)
		verifyGPUAllocation(ctx, namespace, pods[0], containerName, expectedGPUCount, observedGPUs)
	})

	It("Should share 1 GPU among the Pods in each of 2 PodGroups", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{})
		namespace := "podgroup-resourceclaimtemplate"
		containerName := "ctr0"
		expectedGPUCount := 1

		deployManifest(ctx, namespace, "podgroup-resourceclaimtemplate.yaml", drv)

		Eventually(ctx, func(g Gomega, ctx context.Context) {
			deployments, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			for _, deployment := range deployments.Items {
				g.Expect(deployment.Status.ObservedGeneration).To(Equal(deployment.Generation))
				g.Expect(deployment.Status.Replicas).To(Equal(*deployment.Spec.Replicas))
			}
		}, "30s", "1s").Should(Succeed())

		pods := map[string][]corev1.Pod{} // PodGroup name -> Pods
		podList, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		var podNames []string
		for _, pod := range podList.Items {
			pg := *pod.Spec.SchedulingGroup.PodGroupName
			pods[pg] = append(pods[pg], pod)
			podNames = append(podNames, pod.Name)
		}

		checkPodsReadyAndRunning(ctx, namespace, podNames)

		observedGPUs := make(map[string]string)
		for _, groupPods := range pods {
			for _, pod := range groupPods {
				verifyGPUAllocation(ctx, namespace, pod.Name, containerName, expectedGPUCount, observedGPUs)
				break // only check one Pod for each PodGroup
			}
		}

		for _, groupPods := range pods {
			var members []podContainer
			for _, pod := range groupPods {
				members = append(members, podContainer{pod: pod.Name, container: containerName})
			}
			verifySharedGPUGroup(ctx, namespace, sharingGroup{
				members:           members,
				expectedStrategy:  string(gpuv1alpha1.TimeSlicingStrategy),
				expectedProperty:  "TIMESLICE_INTERVAL",
				expectedPropValue: string(gpuv1alpha1.DefaultTimeSlice),
			})
		}
	})

	It("should allocate partition devices from shared GPU counters", func(ctx SpecContext) {
		drv := installDriver(ctx, DriverConfig{
			ExtraValues: map[string]string{
				"kubeletPlugin.gpuPartitions": "4",
			},
		})
		namespace := "partitionable-devices"
		pods := []string{"pod0"}
		containerName := "ctr0"
		expectedGPUCount := 2

		deployManifest(ctx, namespace, "partitionable-devices.yaml", drv)
		checkPodsReadyAndRunning(ctx, namespace, pods)

		observedGPUs := make(map[string]string)
		verifyGPUAllocation(ctx, namespace, pods[0], containerName, expectedGPUCount, observedGPUs)
	})

	// Webhook tests share one driver pinned to "gpu.example.com" so their
	// static testdata stays valid; Ordered+Serial avoids concurrent upgrades.
	Context("Webhooks", Ordered, Serial, func() {
		BeforeAll(func(ctx SpecContext) {
			installDriver(ctx, DriverConfig{
				DriverName:     defaultDeviceClassName,
				WebhookEnabled: true,
			})
		})

		// invalidGpuConfig marshals to an opaque GpuConfig parameter with an
		// unknown time-slice interval value that the webhook should reject.
		invalidGpuConfig := func() runtime.RawExtension {
			cfg := gpuv1alpha1.GpuConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gpuv1alpha1.SchemeGroupVersion.String(),
					Kind:       gpuv1alpha1.GpuConfigKind,
				},
				Sharing: &gpuv1alpha1.GpuSharing{
					Strategy: gpuv1alpha1.TimeSlicingStrategy,
					TimeSlicingConfig: &gpuv1alpha1.TimeSlicingConfig{
						Interval: "InvalidInterval",
					},
				},
			}
			raw, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())
			return runtime.RawExtension{Raw: raw}
		}

		It("should reject invalid v1 ResourceClaim", func(ctx SpecContext) {
			claim := &resourcev1.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "webhook-test", Namespace: "default"},
				Spec: resourcev1.ResourceClaimSpec{
					Devices: resourcev1.DeviceClaim{
						Requests: []resourcev1.DeviceRequest{{
							Name:    "ts-gpu",
							Exactly: &resourcev1.ExactDeviceRequest{DeviceClassName: defaultDeviceClassName},
						}},
						Config: []resourcev1.DeviceClaimConfiguration{{
							Requests: []string{"ts-gpu"},
							DeviceConfiguration: resourcev1.DeviceConfiguration{
								Opaque: &resourcev1.OpaqueDeviceConfiguration{
									Driver:     defaultDeviceClassName,
									Parameters: invalidGpuConfig(),
								},
							},
						}},
					},
				},
			}
			_, err := clientset.ResourceV1().ResourceClaims(claim.Namespace).Create(
				ctx, claim, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
			Expect(err).To(HaveOccurred(), "Expected webhook to reject invalid v1 ResourceClaim")
			Expect(err.Error()).To(ContainSubstring("unknown time-slice interval"))
		})

		It("should reject invalid v1beta1 ResourceClaim", func(ctx SpecContext) {
			claim := &resourcev1beta1.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "webhook-test-v1beta1", Namespace: "default"},
				Spec: resourcev1beta1.ResourceClaimSpec{
					Devices: resourcev1beta1.DeviceClaim{
						Requests: []resourcev1beta1.DeviceRequest{{
							Name:            "ts-gpu",
							DeviceClassName: defaultDeviceClassName,
						}},
						Config: []resourcev1beta1.DeviceClaimConfiguration{{
							Requests: []string{"ts-gpu"},
							DeviceConfiguration: resourcev1beta1.DeviceConfiguration{
								Opaque: &resourcev1beta1.OpaqueDeviceConfiguration{
									Driver:     defaultDeviceClassName,
									Parameters: invalidGpuConfig(),
								},
							},
						}},
					},
				},
			}
			_, err := clientset.ResourceV1beta1().ResourceClaims(claim.Namespace).Create(
				ctx, claim, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
			Expect(err).To(HaveOccurred(), "Expected webhook to reject invalid v1beta1 ResourceClaim")
			Expect(err.Error()).To(ContainSubstring("unknown time-slice interval"))
		})

		It("should reject invalid v1 ResourceClaimTemplate", func(ctx SpecContext) {
			template := &resourcev1.ResourceClaimTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "webhook-test", Namespace: "default"},
				Spec: resourcev1.ResourceClaimTemplateSpec{
					Spec: resourcev1.ResourceClaimSpec{
						Devices: resourcev1.DeviceClaim{
							Requests: []resourcev1.DeviceRequest{{
								Name:    "ts-gpu",
								Exactly: &resourcev1.ExactDeviceRequest{DeviceClassName: defaultDeviceClassName},
							}},
							Config: []resourcev1.DeviceClaimConfiguration{{
								Requests: []string{"ts-gpu"},
								DeviceConfiguration: resourcev1.DeviceConfiguration{
									Opaque: &resourcev1.OpaqueDeviceConfiguration{
										Driver:     defaultDeviceClassName,
										Parameters: invalidGpuConfig(),
									},
								},
							}},
						},
					},
				},
			}
			_, err := clientset.ResourceV1().ResourceClaimTemplates(template.Namespace).Create(
				ctx, template, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
			Expect(err).To(HaveOccurred(), "Expected webhook to reject invalid v1 ResourceClaimTemplate")
			Expect(err.Error()).To(ContainSubstring("unknown time-slice interval"))
		})
	})
})
