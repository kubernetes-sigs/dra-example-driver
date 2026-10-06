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

// Package e2e contains end-to-end tests for the kube-ovn NIC DRA driver.
//
// The suite assumes a running cluster with the driver and kube-ovn (with
// --enable-dra-nic) already deployed — bring one up with `make setup-e2e`, which
// drives everything through the Makefile's kind-*/kube-ovn targets so all
// cluster/kube-ovn Helm values stay sourced from a single place. Run with
// `make test-e2e`; tear down with `make teardown-e2e`.
//
// Specs are labelled:
//   - default specs run on a plain kind cluster (no containerlab).
//   - specs labelled "containerlab" exercise the VLAN underlay datapath and need
//     `make clab-deploy` (sudo + containerlab); they are skipped unless
//     E2E_CONTAINERLAB=1.
package e2e

import (
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// driverName must match the DeviceClass / ResourceSlice driver the chart
	// registers (Helm driverName default; see deployments/helm/...).
	driverName = "nic.kubeovn.io"
	namespace  = "default"

	defaultTimeout = 4 * time.Minute
	pollInterval   = 5 * time.Second
)

var (
	ipGVR     = schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "ips"}
	subnetGVR = schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "subnets"}
)

var (
	clientset kubernetes.Interface
	dynClient dynamic.Interface
)

// containerlabEnabled reports whether the VLAN-underlay (containerlab) specs
// should run. They need the host-side clab topology, so they are opt-in.
func containerlabEnabled() bool {
	return os.Getenv("E2E_CONTAINERLAB") == "1"
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "kube-ovn NIC DRA e2e suite")
}

var _ = BeforeSuite(func() {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = clientcmd.RecommendedHomeFile
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred(), "load kubeconfig %q", kubeconfig)

	clientset, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	dynClient, err = dynamic.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
})
