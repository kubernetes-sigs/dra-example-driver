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

// Package annotation reads the kube-ovn IPAM result of a DRA NIC from the pod
// annotations kube-ovn-controller writes.
//
// kube-ovn keys every per-network annotation by a provider key, using the
// template "<provider>.kubernetes.io/<field>" (see kube-ovn pkg/util/const.go
// *AnnotationTemplate). For a DRA NIC the key is the subnet's spec.provider,
// or "<provider>.<interfaceName>" when several NICs of the pod share the
// provider, the same rule kube-ovn uses for repeated Multus attachments.
//
// kube-ovn-controller writes ip_address, mac_address, cidr, gateway and
// logical_switch, and finally allocated="true". The driver waits for
// allocated="true", then reads the result.
package annotation

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	coreclientset "k8s.io/client-go/kubernetes"
)

const (
	// kube-ovn per-provider annotation templates ("%s" = provider key). Mirrors
	// kube-ovn pkg/util/const.go *AnnotationTemplate constants.
	tmplLogicalSwitch = "%s.kubernetes.io/logical_switch"
	tmplIPAddress     = "%s.kubernetes.io/ip_address"
	tmplMACAddress    = "%s.kubernetes.io/mac_address"
	tmplCIDR          = "%s.kubernetes.io/cidr"
	tmplGateway       = "%s.kubernetes.io/gateway"
	// allocated="true" is the readiness signal kube-ovn-controller sets once
	// IPAM is done for the provider.
	tmplAllocated = "%s.kubernetes.io/allocated"
	// virtualmachine is set by kube-ovn-controller only when it names the pod's ports after
	// the owning KubeVirt VM (keep-vm-ip) instead of after the pod.
	tmplVM = "%s.kubernetes.io/virtualmachine"

	// waitTimeout is the maximum time we wait for kube-ovn to respond.
	waitTimeout  = 30 * time.Second
	waitInterval = 500 * time.Millisecond
)

// AllocatedNIC holds the IPAM result written back by kube-ovn-controller.
type AllocatedNIC struct {
	// IP is the allocated address as kube-ovn writes it: bare, without a mask
	// (e.g. "10.0.1.5"), comma-separated for dual-stack.
	IP string
	// MAC is the allocated MAC address (e.g. "00:11:22:33:44:55").
	MAC string
	// CIDR is the subnet CIDR block (e.g. "10.0.1.0/24").
	CIDR string
	// Gateway is the gateway for the subnet (e.g. "10.0.1.1").
	Gateway string
	// SubnetName is the kube-ovn Subnet used.
	SubnetName string
	// VMName is the KubeVirt VM kube-ovn named the port after, empty when the
	// port is named after the pod.
	VMName string
	// Provider is the annotation key prefix kube-ovn used for this NIC.
	Provider string
}

// WaitForAllocation polls the pod until kube-ovn has written back the IPAM
// result annotations for one of the providers (the first one found wins), or
// until the context deadline is reached.
func WaitForAllocation(ctx context.Context, client coreclientset.Interface, namespace, podName string, providers ...string) (*AllocatedNIC, error) {
	ctx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()

	var result *AllocatedNIC
	err := wait.PollUntilContextTimeout(ctx, waitInterval, waitTimeout, true, func(ctx context.Context) (bool, error) {
		pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, provider := range providers {
			if nic, ok := extractAllocation(pod, provider); ok {
				result = nic
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for kube-ovn IPAM allocation for providers %q on pod %s/%s: %w", providers, namespace, podName, err)
	}
	return result, nil
}

// extractAllocation reads the kube-ovn response annotations from a pod for one
// provider. It gates on allocated="true" — the signal kube-ovn-controller sets
// when IPAM is complete — then returns the result. Returns (nil, false) until
// then.
func extractAllocation(pod *corev1.Pod, provider string) (*AllocatedNIC, bool) {
	ann := pod.Annotations
	if ann == nil {
		return nil, false
	}
	if ann[fmt.Sprintf(tmplAllocated, provider)] != "true" {
		return nil, false
	}
	ip := ann[fmt.Sprintf(tmplIPAddress, provider)]
	mac := ann[fmt.Sprintf(tmplMACAddress, provider)]
	if ip == "" || mac == "" {
		return nil, false
	}
	return &AllocatedNIC{
		IP:         ip,
		MAC:        mac,
		CIDR:       ann[fmt.Sprintf(tmplCIDR, provider)],
		Gateway:    ann[fmt.Sprintf(tmplGateway, provider)],
		SubnetName: ann[fmt.Sprintf(tmplLogicalSwitch, provider)],
		VMName:     ann[fmt.Sprintf(tmplVM, provider)],
		Provider:   provider,
	}, true
}
