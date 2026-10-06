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
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Fixtures, relative to the repository root. They share subnets with the base
// demo, so each can be applied and removed independently.
const (
	// twoNICRelPath is the 2-NIC scaling example: net1 on vlan100-subnet
	// (VLAN underlay) and net2 on ovn-subnet (OVN overlay).
	twoNICRelPath = "demo/nic-example/examples/2nic.yaml"
	// sharedSubnetRelPath is two pods with one NIC each on ovn-subnet.
	sharedSubnetRelPath = "demo/nic-example/examples/shared-subnet.yaml"
	// sameSubnetRelPath is one pod with two NICs on ovn-subnet.
	sameSubnetRelPath = "demo/nic-example/examples/same-subnet-2nic.yaml"
)

// Names from the demo fixtures. The underlay gateway is the FRR gateway on
// eth1.100 (vlan100-subnet's spec.gateway).
const (
	twoNICPod         = "nic-demo-2"
	sameSubnetPod     = "same-subnet-2nic"
	underlaySubnet    = "vlan100-subnet"
	overlaySubnet     = "ovn-subnet"
	underlayGatewayIP = "172.23.0.253"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func kubectl(args ...string) (string, error) {
	cmd := exec.Command("kubectl", args...)
	cmd.Dir = repoRoot()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// useFixture applies a fixture for the current spec and removes it afterwards.
// It also removes leftovers of an earlier run first.
func useFixture(relPath string) {
	fixture := filepath.Join(repoRoot(), relPath)
	_, _ = kubectl("delete", "-f", fixture, "--ignore-not-found", "--wait=true")
	DeferCleanup(func() {
		_, _ = kubectl("delete", "-f", fixture, "--ignore-not-found", "--wait=true")
	})
	out, err := kubectl("apply", "-f", fixture)
	Expect(err).NotTo(HaveOccurred(), "kubectl apply: %s", out)
}

func waitRunning(pods ...string) {
	for _, pod := range pods {
		Eventually(func() (string, error) {
			return kubectl("get", "pod", pod, "-o", "jsonpath={.status.phase}")
		}, defaultTimeout, pollInterval).Should(Equal("Running"), "pod %s", pod)
	}
}

func subnetProvider(ctx context.Context, subnet string) string {
	obj, err := dynClient.Resource(subnetGVR).Get(ctx, subnet, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "get subnet %s", subnet)
	provider, _, _ := unstructured.NestedString(obj.Object, "spec", "provider")
	Expect(provider).NotTo(BeEmpty(), "subnet %s has no provider", subnet)
	return provider
}

// expectNIC checks that kube-ovn-controller allocated the NIC keyed by key and
// that the driver configured that address on iface in the pod. It returns the
// address.
func expectNIC(ctx context.Context, pod, iface, key string) string {
	p, err := clientset.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(p.Annotations).To(HaveKeyWithValue(key+".kubernetes.io/allocated", "true"), "pod %s NIC %s", pod, key)
	ip := p.Annotations[key+".kubernetes.io/ip_address"]
	Expect(ip).NotTo(BeEmpty(), "pod %s NIC %s has no address", pod, key)

	Eventually(func() (string, error) {
		return kubectl("exec", pod, "--", "ip", "-o", "addr", "show", "dev", iface)
	}, defaultTimeout, pollInterval).Should(ContainSubstring(ip+"/"), "pod %s %s should carry %s", pod, iface, ip)
	return ip
}

// ipCRExists reports whether kube-ovn's ip CR for the pod's NIC keyed by key
// exists.
func ipCRExists(ctx context.Context, pod, key string) bool {
	_, err := dynClient.Resource(ipGVR).Get(ctx, fmt.Sprintf("%s.%s.%s", pod, namespace, key), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false
	}
	Expect(err).NotTo(HaveOccurred())
	return true
}

var _ = Describe("kube-ovn NIC DRA driver", func() {
	ctx := context.Background()

	It("publishes NIC devices in ResourceSlices", func() {
		slices, err := clientset.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		var devices int
		var sawSubnetAttr bool
		for _, slice := range slices.Items {
			if slice.Spec.Driver != driverName {
				continue
			}
			for _, dev := range slice.Spec.Devices {
				devices++
				if _, ok := dev.Attributes[resourcev1.QualifiedName("nic.kubeovn.io/subnetName")]; ok {
					sawSubnetAttr = true
				}
			}
		}
		Expect(devices).To(BeNumerically(">", 0), "expected at least one %s device in a ResourceSlice", driverName)
		Expect(sawSubnetAttr).To(BeTrue(), "expected a device with the nic.kubeovn.io/subnetName attribute")
	})

	It("plugs the NICs kube-ovn-controller allocates and releases them with the pod", func() {
		useFixture(twoNICRelPath)
		waitRunning(twoNICPod)
		underlay, overlay := subnetProvider(ctx, underlaySubnet), subnetProvider(ctx, overlaySubnet)

		By("each NIC carrying the address kube-ovn-controller allocated")
		expectNIC(ctx, twoNICPod, "net1", underlay)
		expectNIC(ctx, twoNICPod, "net2", overlay)
		Expect(ipCRExists(ctx, twoNICPod, underlay)).To(BeTrue())
		Expect(ipCRExists(ctx, twoNICPod, overlay)).To(BeTrue())

		By("kube-ovn-controller releasing the addresses when the pod is deleted")
		out, err := kubectl("delete", "-f", filepath.Join(repoRoot(), twoNICRelPath), "--wait=true")
		Expect(err).NotTo(HaveOccurred(), "kubectl delete: %s", out)
		Eventually(func() bool {
			return ipCRExists(ctx, twoNICPod, underlay) || ipCRExists(ctx, twoNICPod, overlay)
		}, defaultTimeout, pollInterval).Should(BeFalse())
	})

	It("keys NICs of one pod on one subnet by interface name", func() {
		useFixture(sameSubnetRelPath)
		waitRunning(sameSubnetPod)
		provider := subnetProvider(ctx, overlaySubnet)

		ip1 := expectNIC(ctx, sameSubnetPod, "net1", provider+".net1")
		ip2 := expectNIC(ctx, sameSubnetPod, "net2", provider+".net2")
		Expect(ip1).NotTo(Equal(ip2))
		Expect(ipCRExists(ctx, sameSubnetPod, provider+".net1")).To(BeTrue())
		Expect(ipCRExists(ctx, sameSubnetPod, provider+".net2")).To(BeTrue())
	})

	It("connects pods that share a subnet over the overlay", func() {
		useFixture(sharedSubnetRelPath)
		waitRunning("shared-a", "shared-b")
		provider := subnetProvider(ctx, overlaySubnet)

		ipA := expectNIC(ctx, "shared-a", "net1", provider)
		ipB := expectNIC(ctx, "shared-b", "net1", provider)
		Expect(ipA).NotTo(Equal(ipB))

		By("pinging shared-b's NIC from shared-a")
		Eventually(func() (string, error) {
			return kubectl("exec", "shared-a", "--", "ping", "-c", "3", "-W", "2", "-I", "net1", ipB)
		}, defaultTimeout, pollInterval).Should(ContainSubstring(" 0% packet loss"))
	})

	Context("VLAN underlay datapath", Label("containerlab"), func() {
		BeforeEach(func() {
			if !containerlabEnabled() {
				Skip("set E2E_CONTAINERLAB=1 (and run `make clab-deploy`) to exercise the VLAN underlay")
			}
		})

		It("reaches the external VLAN gateway over the underlay NIC", func() {
			useFixture(twoNICRelPath)
			waitRunning(twoNICPod)

			By("pinging the FRR gateway " + underlayGatewayIP + " from the underlay NIC")
			Eventually(func() (string, error) {
				out, err := kubectl("exec", twoNICPod, "--", "ping", "-c", "3", "-W", "2", "-I", "net1", underlayGatewayIP)
				return strings.TrimSpace(out), err
			}, defaultTimeout, pollInterval).Should(ContainSubstring(" 0% packet loss"))
		})
	})
})
