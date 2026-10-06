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

package annotation

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testProvider = "blue.default.ovn"
	testPod      = "pod1"
	testNS       = "default"
)

func allocatedAnnotations(key string) map[string]string {
	return map[string]string{
		key + ".kubernetes.io/allocated":      "true",
		key + ".kubernetes.io/ip_address":     "10.10.0.5",
		key + ".kubernetes.io/mac_address":    "00:00:00:aa:bb:cc",
		key + ".kubernetes.io/cidr":           "10.10.0.0/24",
		key + ".kubernetes.io/gateway":        "10.10.0.1",
		key + ".kubernetes.io/logical_switch": "blue",
	}
}

func TestExtractAllocation(t *testing.T) {
	withVM := allocatedAnnotations(testProvider)
	withVM[testProvider+".kubernetes.io/virtualmachine"] = "vm1"
	notAllocated := allocatedAnnotations(testProvider)
	notAllocated[testProvider+".kubernetes.io/allocated"] = "false"
	noMAC := allocatedAnnotations(testProvider)
	delete(noMAC, testProvider+".kubernetes.io/mac_address")

	tests := []struct {
		name        string
		annotations map[string]string
		want        *AllocatedNIC
	}{
		{"no annotations", nil, nil},
		{"other provider", allocatedAnnotations("red.default.ovn"), nil},
		{"not allocated yet", notAllocated, nil},
		{"allocated without mac", noMAC, nil},
		{
			"allocated", allocatedAnnotations(testProvider),
			&AllocatedNIC{IP: "10.10.0.5", MAC: "00:00:00:aa:bb:cc", CIDR: "10.10.0.0/24", Gateway: "10.10.0.1", SubnetName: "blue", Provider: testProvider},
		},
		{
			"port named after a vm", withVM,
			&AllocatedNIC{IP: "10.10.0.5", MAC: "00:00:00:aa:bb:cc", CIDR: "10.10.0.0/24", Gateway: "10.10.0.1", SubnetName: "blue", VMName: "vm1", Provider: testProvider},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations}}
			got, ok := extractAllocation(pod, testProvider)
			if ok != (tt.want != nil) {
				t.Fatalf("extractAllocation() ok = %v, want %v", ok, tt.want != nil)
			}
			if tt.want != nil && *got != *tt.want {
				t.Errorf("extractAllocation() = %+v, want %+v", *got, *tt.want)
			}
		})
	}
}

func TestWaitForAllocation(t *testing.T) {
	t.Run("returns the first provider key found", func(t *testing.T) {
		key := testProvider + ".net2"
		client := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: testPod, Namespace: testNS, Annotations: allocatedAnnotations(key),
		}})
		got, err := WaitForAllocation(context.Background(), client, testNS, testPod, key, testProvider)
		if err != nil {
			t.Fatalf("WaitForAllocation() error = %v", err)
		}
		if got.Provider != key {
			t.Errorf("Provider = %q, want %q", got.Provider, key)
		}
	})

	t.Run("times out while not allocated", func(t *testing.T) {
		client := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: testPod, Namespace: testNS}})
		ctx, cancel := context.WithTimeout(context.Background(), 2*waitInterval)
		defer cancel()
		if _, err := WaitForAllocation(ctx, client, testNS, testPod, testProvider); err == nil {
			t.Fatal("expected a timeout error")
		}
	})

	t.Run("fails when the pod is gone", func(t *testing.T) {
		start := time.Now()
		if _, err := WaitForAllocation(context.Background(), fake.NewSimpleClientset(), testNS, testPod, testProvider); err == nil {
			t.Fatal("expected an error for a missing pod")
		}
		if time.Since(start) > waitTimeout/2 {
			t.Error("a missing pod should fail fast instead of waiting for the timeout")
		}
	})
}
