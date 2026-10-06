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

package plumbing

import (
	"context"
	"errors"
)

// SandboxHandler is the bridge between NRI pod-sandbox events and the Attacher.
// It is decoupled from the NRI API types; the NRI plugin in
// cmd/kube-ovn-dra-kubeletplugin/nri.go adapts NRI's *api.PodSandbox into these
// plain calls.
//
// NRI is enabled by default in containerd 2.x, so no runtime patching is
// required.
type SandboxHandler struct {
	store    *NICStore
	attacher Attacher
}

// NewSandboxHandler wires a NICStore to an Attacher.
func NewSandboxHandler(store *NICStore, attacher Attacher) *SandboxHandler {
	return &SandboxHandler{store: store, attacher: attacher}
}

// kubevirtVirtLauncherLabel is the well-known label KubeVirt sets on every
// virt-launcher pod (verified against a live v1.9.0-rc.0 cluster:
// metadata.labels["kubevirt.io"] == "virt-launcher"). Its presence is how
// OnRunPodSandbox decides whether Attach must wrap the veth in a bridge+tap
// (see Spec.KubeVirtVMI) instead of handing the veth to the pod directly.
const kubevirtVirtLauncherLabel = "virt-launcher"

// OnRunPodSandbox attaches the NICs of podUID to its sandbox, filling in the
// now-known netns path. Called from the NRI RunPodSandbox hook, and from
// Synchronize for running sandboxes; Attach is idempotent. labels are the pod's labels from the NRI event, used only to detect a
// KubeVirt virt-launcher pod. On the first attach error, the NICs attached so
// far are detached again and the error is returned, so the caller can fail the
// sandbox: a half-networked pod is worse than a failed one.
func (h *SandboxHandler) OnRunPodSandbox(ctx context.Context, podUID, containerID, netnsPath string, labels map[string]string) error {
	for _, spec := range h.sandboxSpecs(podUID, containerID, netnsPath, labels) {
		if err := h.attacher.Attach(ctx, spec); err != nil {
			errs := []error{err}
			for _, attached := range h.store.TakeAttached(podUID) {
				if detachErr := h.attacher.Detach(ctx, attached); detachErr != nil {
					errs = append(errs, detachErr)
				}
			}
			return errors.Join(errs...)
		}
		h.store.MarkAttached(podUID, spec)
	}
	return nil
}

// OnSynchronizeSandbox attaches the NICs of a sandbox that is already running,
// e.g. after a plugin restart that missed its RunPodSandbox. Called from the
// NRI Synchronize hook. Unlike OnRunPodSandbox it tries every NIC and detaches
// nothing on failure: the pod is running, and its other NICs work.
func (h *SandboxHandler) OnSynchronizeSandbox(ctx context.Context, podUID, containerID, netnsPath string, labels map[string]string) error {
	var errs []error
	for _, spec := range h.sandboxSpecs(podUID, containerID, netnsPath, labels) {
		if err := h.attacher.Attach(ctx, spec); err != nil {
			errs = append(errs, err)
			continue
		}
		h.store.MarkAttached(podUID, spec)
	}
	return errors.Join(errs...)
}

// sandboxSpecs returns the NICs of a pod with the fields filled in that are
// only known once its sandbox exists.
func (h *SandboxHandler) sandboxSpecs(podUID, containerID, netnsPath string, labels map[string]string) []Spec {
	specs := h.store.Specs(podUID)
	isVMI := labels[kubevirtLabelKey] == kubevirtVirtLauncherLabel
	for i := range specs {
		specs[i].ContainerID = containerID
		specs[i].NetnsPath = netnsPath
		specs[i].KubeVirtVMI = isVMI
	}
	return specs
}

// kubevirtLabelKey is the label key checked against kubevirtVirtLauncherLabel.
const kubevirtLabelKey = "kubevirt.io"

// OnStopPodSandbox detaches every NIC for the pod. Called from the NRI
// StopPodSandbox hook. It only tears down the datapath; kube-ovn-controller
// releases the addresses.
func (h *SandboxHandler) OnStopPodSandbox(ctx context.Context, podUID, podName, podNamespace string) error {
	var errs []error
	for _, spec := range h.store.TakeAttached(podUID) {
		if err := h.attacher.Detach(ctx, spec); err != nil {
			errs = append(errs, err)
		}
	}
	// The attached Specs only live in memory; after a plugin restart the NICs of
	// pods attached before it are found through the OVS port's external_ids.
	// Leftover ports would keep claiming the same OVN port (a keep-vm-ip VM's
	// port outlives its pods) and steal its binding from the next pod.
	if err := h.attacher.DetachPodPorts(ctx, podName, podNamespace); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
