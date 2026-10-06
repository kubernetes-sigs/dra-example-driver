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

package nicprepare

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

// TestWithMask covers combining the bare IP kube-ovn annotates with the subnet
// CIDR mask into the CIDR form the plumbing layer needs.
func TestWithMask(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		cidr     string
		expected string
	}{
		{"bare ip + /24", "172.23.0.5", "172.23.0.0/24", "172.23.0.5/24"},
		{"bare ip + /16", "10.0.1.5", "10.0.0.0/16", "10.0.1.5/16"},
		{"ip already has mask", "172.23.0.5/24", "172.23.0.0/24", "172.23.0.5/24"},
		{"empty ip unchanged", "", "172.23.0.0/24", ""},
		{"unparseable cidr falls back to bare ip", "1.2.3.4", "not-a-cidr", "1.2.3.4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withMask(tt.ip, tt.cidr); got != tt.expected {
				t.Errorf("withMask(%q,%q) = %q, want %q", tt.ip, tt.cidr, got, tt.expected)
			}
		})
	}
}

func TestValidateSecondaryProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		subnet   string
		wantErr  bool
	}{
		// Default/empty provider names the pod's PRIMARY port — never valid for a secondary NIC.
		{"empty provider rejected", "", "ovn-subnet", true},
		{"default ovn provider rejected", "ovn", "ovn-subnet", true},
		// A dedicated provider (multus convention) is accepted.
		{"dedicated overlay provider ok", "ovn-subnet.default.ovn", "ovn-subnet", false},
		{"vlan provider ok", "external.vlan100-subnet.ovn", "vlan100-subnet", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecondaryProvider(tt.provider, tt.subnet)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSecondaryProvider(%q,%q) err=%v, wantErr=%v", tt.provider, tt.subnet, err, tt.wantErr)
			}
		})
	}
}

func TestRequestIPAM(t *testing.T) {
	const provider = "blue.default.ovn"
	device := resourceapi.Device{
		Name: "subnet-blue",
		Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			"nic.kubeovn.io/subnetName": {StringValue: ptr.To("blue")},
			"nic.kubeovn.io/provider":   {StringValue: ptr.To(provider)},
		},
	}
	claim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "default"},
		Status: resourceapi.ResourceClaimStatus{
			ReservedFor: []resourceapi.ResourceClaimConsumerReference{{Resource: "pods", Name: "virt-launcher-vm1-abcde", UID: "uid-1"}},
		},
	}
	result := &resourceapi.DeviceRequestAllocationResult{Device: device.Name}

	tests := []struct {
		name        string
		annotations map[string]string
		wantPort    string
		wantIP      string
		wantGateway string
	}{
		{
			name: "port named after the pod",
			annotations: map[string]string{
				provider + ".kubernetes.io/allocated":   "true",
				provider + ".kubernetes.io/ip_address":  "10.10.0.5",
				provider + ".kubernetes.io/mac_address": "00:00:00:aa:bb:cc",
				provider + ".kubernetes.io/cidr":        "10.10.0.0/24",
				provider + ".kubernetes.io/gateway":     "10.10.0.1",
			},
			wantPort:    "virt-launcher-vm1-abcde.default." + provider,
			wantIP:      "10.10.0.5/24",
			wantGateway: "10.10.0.1",
		},
		{
			name: "keep-vm-ip port named after the vm, first family of dual-stack",
			annotations: map[string]string{
				provider + ".kubernetes.io/allocated":      "true",
				provider + ".kubernetes.io/ip_address":     "10.10.0.5,fd00::5",
				provider + ".kubernetes.io/mac_address":    "00:00:00:aa:bb:cc",
				provider + ".kubernetes.io/cidr":           "10.10.0.0/24,fd00::/120",
				provider + ".kubernetes.io/gateway":        "10.10.0.1,fd00::1",
				provider + ".kubernetes.io/virtualmachine": "vm1",
			},
			wantPort:    "vm1.default." + provider,
			wantIP:      "10.10.0.5/24",
			wantGateway: "10.10.0.1",
		},
		{
			// A second NIC on the same provider makes kube-ovn key both by interface name.
			name: "nic sharing its provider is keyed by interface name",
			annotations: map[string]string{
				provider + ".net1.kubernetes.io/allocated":   "true",
				provider + ".net1.kubernetes.io/ip_address":  "10.10.0.9",
				provider + ".net1.kubernetes.io/mac_address": "00:00:00:aa:bb:dd",
				provider + ".net1.kubernetes.io/cidr":        "10.10.0.0/24",
				provider + ".net1.kubernetes.io/gateway":     "10.10.0.1",
				provider + ".net2.kubernetes.io/allocated":   "true",
				provider + ".net2.kubernetes.io/ip_address":  "10.10.0.10",
				provider + ".net2.kubernetes.io/mac_address": "00:00:00:aa:bb:ee",
			},
			wantPort:    "virt-launcher-vm1-abcde.default." + provider + ".net1",
			wantIP:      "10.10.0.9/24",
			wantGateway: "10.10.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name: "virt-launcher-vm1-abcde", Namespace: "default", UID: "uid-1", Annotations: tt.annotations,
			}})
			cfg, err := RequestIPAM(context.Background(), client, claim, result, device, "net1")
			if err != nil {
				t.Fatalf("RequestIPAM() error = %v", err)
			}
			if cfg.LogicalSwitchPort != tt.wantPort {
				t.Errorf("LogicalSwitchPort = %q, want %q", cfg.LogicalSwitchPort, tt.wantPort)
			}
			if cfg.IP != tt.wantIP {
				t.Errorf("IP = %q, want %q", cfg.IP, tt.wantIP)
			}
			if cfg.Gateway != tt.wantGateway {
				t.Errorf("Gateway = %q, want %q", cfg.Gateway, tt.wantGateway)
			}
			if cfg.PodName != "virt-launcher-vm1-abcde" || cfg.PodUID != "uid-1" {
				t.Errorf("pod = %s/%s, want virt-launcher-vm1-abcde/uid-1", cfg.PodName, cfg.PodUID)
			}
		})
	}

	t.Run("fails without a subnetName attribute", func(t *testing.T) {
		dev := device.DeepCopy()
		delete(dev.Attributes, "nic.kubeovn.io/subnetName")
		client := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "virt-launcher-vm1-abcde", Namespace: "default", UID: "uid-1"}})
		if _, err := RequestIPAM(context.Background(), client, claim, result, *dev, "net1"); err == nil {
			t.Fatal("expected an error for a device without subnetName")
		}
	})

	t.Run("rejects the default provider without waiting", func(t *testing.T) {
		dev := device.DeepCopy()
		dev.Attributes["nic.kubeovn.io/provider"] = resourceapi.DeviceAttribute{StringValue: ptr.To("ovn")}
		client := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "virt-launcher-vm1-abcde", Namespace: "default", UID: "uid-1"}})
		if _, err := RequestIPAM(context.Background(), client, claim, result, *dev, "net1"); err == nil {
			t.Fatal("expected an error for the default provider")
		}
	})
}

func TestDualStackAddresses(t *testing.T) {
	tests := []struct {
		name             string
		ips, cidrs, gws  string
		wantIPs          []string
		wantCIDR, wantGw string
	}{
		{"ipv4", "10.10.0.5", "10.10.0.0/24", "10.10.0.1", []string{"10.10.0.5/24"}, "10.10.0.0/24", "10.10.0.1"},
		{"ipv6", "fd00::5", "fd00::/120", "fd00::1", []string{"fd00::5/120"}, "fd00::/120", "fd00::1"},
		{"dual-stack", "10.10.0.5,fd00::5", "10.10.0.0/24,fd00::/120", "10.10.0.1,fd00::1", []string{"10.10.0.5/24", "fd00::5/120"}, "10.10.0.0/24", "10.10.0.1"},
		{"dual-stack, ipv6 listed first", "fd00::5,10.10.0.5", "fd00::/120,10.10.0.0/24", "fd00::1,10.10.0.1", []string{"10.10.0.5/24", "fd00::5/120"}, "10.10.0.0/24", "10.10.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ips, cidr, gw := dualStackAddresses(tt.ips, tt.cidrs, tt.gws)
			if strings.Join(ips, ",") != strings.Join(tt.wantIPs, ",") || cidr != tt.wantCIDR || gw != tt.wantGw {
				t.Errorf("dualStackAddresses() = %v, %q, %q; want %v, %q, %q", ips, cidr, gw, tt.wantIPs, tt.wantCIDR, tt.wantGw)
			}
		})
	}
}

func TestPortName(t *testing.T) {
	tests := []struct {
		name, owner, namespace, provider, expected string
	}{
		{"default provider", "mypod", "default", "ovn", "mypod.default"},
		{"empty provider", "mypod", "default", "", "mypod.default"},
		{"dedicated provider", "vm1", "default", "blue.default.ovn", "vm1.default.blue.default.ovn"},
		{"provider keyed by interface name", "vm1", "default", "blue.default.ovn.net2", "vm1.default.blue.default.ovn.net2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := portName(tt.owner, tt.namespace, tt.provider); got != tt.expected {
				t.Errorf("portName(%q,%q,%q) = %q, want %q", tt.owner, tt.namespace, tt.provider, got, tt.expected)
			}
		})
	}
}

func TestLookupPodForClaim(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "default", UID: "uid-1"}}
	claimFor := func(refs ...resourceapi.ResourceClaimConsumerReference) *resourceapi.ResourceClaim {
		return &resourceapi.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "default"},
			Status:     resourceapi.ResourceClaimStatus{ReservedFor: refs},
		}
	}
	podRef := resourceapi.ResourceClaimConsumerReference{Resource: "pods", Name: "pod1", UID: "uid-1"}

	tests := []struct {
		name    string
		claim   *resourceapi.ResourceClaim
		wantErr bool
	}{
		{"reserved for the pod", claimFor(podRef), false},
		{"skips other consumers", claimFor(resourceapi.ResourceClaimConsumerReference{APIGroup: "example.com", Resource: "things", UID: "x"}, podRef), false},
		{"not reserved", claimFor(), true},
		{"pod gone", claimFor(resourceapi.ResourceClaimConsumerReference{Resource: "pods", Name: "pod2", UID: "uid-2"}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, ns, uid, err := lookupPodForClaim(context.Background(), fake.NewSimpleClientset(pod), tt.claim)
			if (err != nil) != tt.wantErr {
				t.Fatalf("lookupPodForClaim() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (name != "pod1" || ns != "default" || uid != "uid-1") {
				t.Errorf("lookupPodForClaim() = %s/%s %s, want default/pod1 uid-1", ns, name, uid)
			}
		})
	}
}
