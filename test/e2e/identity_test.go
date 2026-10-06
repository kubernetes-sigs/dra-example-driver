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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// Serial prevents collisions with tests that install the same DeviceClass.
var _ = Describe("Default driver identity", Serial, func() {
	for _, profile := range []string{"gpu", "cpu", "net"} {
		It("should allocate and recover claims across a rolling update for "+profile, func(ctx SpecContext) {
			drv := installDriver(ctx, DriverConfig{
				UseDefaultDriverName: true,
				ExtraValues: map[string]string{
					"deviceProfile":                     profile,
					"kubeletPlugin.cpu.cpusPerNUMANode": "1",
				},
			})
			Expect(drv.DriverName).To(Equal(profile + ".dra-example-driver.sigs.k8s.io"))
			claim := createIdentityClaim(ctx, drv, "before-update")
			pod := createIdentityPod(ctx, drv, "before-update", claim.Name, "")
			allocated, err := clientset.ResourceV1().ResourceClaims(drv.Namespace).Get(ctx, claim.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(allocated.Status.Allocation).NotTo(BeNil())
			Expect(allocated.Status.Allocation.Devices.Results).NotTo(BeEmpty())
			for _, result := range allocated.Status.Allocation.Devices.Results {
				Expect(result.Driver).To(Equal(drv.DriverName))
			}

			By("rolling the driver while a workload holds an allocated claim")
			daemonSets, err := clientset.AppsV1().DaemonSets(drv.Namespace).List(ctx, metav1.ListOptions{LabelSelector: driverPodSelector})
			Expect(err).NotTo(HaveOccurred())
			Expect(daemonSets.Items).To(HaveLen(1))
			ds := daemonSets.Items[0].DeepCopy()
			if ds.Spec.Template.Annotations == nil {
				ds.Spec.Template.Annotations = map[string]string{}
			}
			ds.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339Nano)
			ds, err = clientset.AppsV1().DaemonSets(drv.Namespace).Update(ctx, ds, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				current, err := clientset.AppsV1().DaemonSets(drv.Namespace).Get(ctx, ds.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(current.Status.ObservedGeneration).To(BeNumerically(">=", ds.Generation))
				g.Expect(current.Status.DesiredNumberScheduled).To(BeNumerically(">", 0))
				g.Expect(current.Status.UpdatedNumberScheduled).To(Equal(current.Status.DesiredNumberScheduled))
				g.Expect(current.Status.NumberAvailable).To(Equal(current.Status.DesiredNumberScheduled))
				pods, err := clientset.CoreV1().Pods(drv.Namespace).List(ctx, metav1.ListOptions{LabelSelector: driverPodSelector})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pods.Items).To(HaveLen(int(current.Status.DesiredNumberScheduled)))
				for _, p := range pods.Items {
					g.Expect(p.DeletionTimestamp).To(BeNil())
					g.Expect(p.Annotations["kubectl.kubernetes.io/restartedAt"]).To(Equal(ds.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]))
				}
			}).WithContext(ctx).WithTimeout(driverInstallTimeout).WithPolling(time.Second).Should(Succeed())

			By("preparing the existing claim again through the replacement plugin")
			createIdentityPod(ctx, drv, "recovered", claim.Name, pod.Spec.NodeName)
			recovered, err := clientset.ResourceV1().ResourceClaims(drv.Namespace).Get(ctx, claim.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(recovered.Status.Allocation).To(Equal(allocated.Status.Allocation))
			checkPodsReadyAndRunning(ctx, drv.Namespace, []string{pod.Name})

			By("allocating a new claim after the update")
			fresh := createIdentityClaim(ctx, drv, "after-update")
			createIdentityPod(ctx, drv, "after-update", fresh.Name, "")
		})
	}
})

func createIdentityClaim(ctx context.Context, drv installedDriver, name string) *resourcev1.ResourceClaim {
	GinkgoHelper()
	claim, err := clientset.ResourceV1().ResourceClaims(drv.Namespace).Create(ctx, &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: resourcev1.ResourceClaimSpec{Devices: resourcev1.DeviceClaim{
			Requests: []resourcev1.DeviceRequest{{
				Name: "device", Exactly: &resourcev1.ExactDeviceRequest{DeviceClassName: drv.DriverName},
			}},
		}},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	return claim
}

func createIdentityPod(ctx context.Context, drv installedDriver, name, claim, node string) *corev1.Pod {
	GinkgoHelper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "workload", Image: "ubuntu:22.04",
				Command:   []string{"bash", "-c", "export; trap 'exit 0' TERM; sleep 9999 & wait"},
				Resources: corev1.ResourceRequirements{Claims: []corev1.ResourceClaim{{Name: "device"}}},
			}},
			ResourceClaims: []corev1.PodResourceClaim{{Name: "device", ResourceClaimName: ptr.To(claim)}},
		},
	}
	if node != "" {
		pod.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": node}
	}
	_, err := clientset.CoreV1().Pods(drv.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	checkPodsReadyAndRunning(ctx, drv.Namespace, []string{name})
	logs, err := clientset.CoreV1().Pods(drv.Namespace).GetLogs(name, &corev1.PodLogOptions{Container: "workload"}).DoRaw(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(string(logs)).To(ContainSubstring("DRA_RESOURCE_DRIVER_NAME=\"" + drv.DriverName + "\""))
	pod, err = clientset.CoreV1().Pods(drv.Namespace).Get(ctx, name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	return pod
}
