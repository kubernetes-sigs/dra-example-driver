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

package main

import (
	"context"
	"fmt"

	"github.com/containerd/nri/pkg/api"
	"github.com/containerd/nri/pkg/stub"
	"k8s.io/klog/v2"

	"github.com/soer3n/kube-ovn-dra-driver/pkg/plumbing"
)

// nriPluginName / nriPluginIdx identify this plugin to the NRI runtime. The
// index orders plugins; a high value runs us late, after the primary CNI has
// set up eth0.
const (
	nriPluginName = "dra-nic"
	nriPluginIdx  = "90"
)

// networkNamespaceType is the OCI runtime-spec namespace type for the network
// namespace. NRI carries it verbatim in PodSandbox.Linux.Namespaces[].Type;
// the api package defines no constant for it.
const networkNamespaceType = "network"

// nriPlugin bridges NRI pod-sandbox events to the plumbing.SandboxHandler. It
// implements the stub's Synchronize / RunPodSandbox / StopPodSandbox
// interfaces (detected by reflection in stub.New).
type nriPlugin struct {
	stub    stub.Stub
	handler *plumbing.SandboxHandler
}

// startNRIPlugin constructs the plugin, connects to the NRI socket, and starts
// serving in the background.
func startNRIPlugin(ctx context.Context, handler *plumbing.SandboxHandler) (*nriPlugin, error) {
	p := &nriPlugin{handler: handler}

	s, err := stub.New(p,
		stub.WithPluginName(nriPluginName),
		stub.WithPluginIdx(nriPluginIdx),
	)
	if err != nil {
		return nil, fmt.Errorf("create NRI stub: %w", err)
	}
	p.stub = s

	// Run in the background; the stub blocks until Stop() or a fatal error.
	go func() {
		logger := klog.FromContext(ctx)
		if err := s.Run(ctx); err != nil {
			logger.Error(err, "NRI plugin exited")
		}
	}()

	return p, nil
}

// stop tears down the NRI connection.
func (p *nriPlugin) stop() {
	if p != nil && p.stub != nil {
		p.stub.Stop()
	}
}

// Synchronize is invoked when the plugin (re)connects to the runtime, with all
// existing pod sandboxes. It attaches the NICs of running pods again, which
// covers a sandbox started while the plugin was down; Attach is idempotent.
// Errors are logged only: failing Synchronize would drop the NRI connection.
func (p *nriPlugin) Synchronize(ctx context.Context, pods []*api.PodSandbox, _ []*api.Container) ([]*api.ContainerUpdate, error) {
	for _, sb := range pods {
		netnsPath := networkNamespacePath(sb)
		if netnsPath == "" {
			continue
		}
		if err := p.handler.OnSynchronizeSandbox(ctx, sb.GetUid(), sb.GetId(), netnsPath, sb.GetLabels()); err != nil {
			klog.FromContext(ctx).Error(err, "Synchronize: attach failed",
				"pod", sb.GetName(), "namespace", sb.GetNamespace(), "uid", sb.GetUid(), "netns", netnsPath)
		}
	}
	return nil, nil
}

// RunPodSandbox is invoked by the runtime when a pod sandbox is created — after
// its network namespace exists. We attach the NICs the prepare path added for
// this pod.
func (p *nriPlugin) RunPodSandbox(ctx context.Context, sb *api.PodSandbox) error {
	logger := klog.FromContext(ctx).WithValues("pod", sb.GetName(), "namespace", sb.GetNamespace(), "uid", sb.GetUid())

	netnsPath := networkNamespacePath(sb)
	if netnsPath == "" {
		// No netns reported (host-network pod, or runtime didn't populate it).
		// Nothing we can attach into; skip quietly.
		logger.V(4).Info("RunPodSandbox: no network namespace, skipping")
		return nil
	}

	if err := p.handler.OnRunPodSandbox(ctx, sb.GetUid(), sb.GetId(), netnsPath, sb.GetLabels()); err != nil {
		// Failing the sandbox makes kubelet retry; a pod with missing NICs would
		// otherwise run without ever noticing.
		logger.Error(err, "RunPodSandbox: attach failed", "netns", netnsPath)
		return err
	}
	return nil
}

// StopPodSandbox is invoked when the sandbox is torn down. We detach the NICs
// of the pod; kube-ovn-controller releases their addresses.
func (p *nriPlugin) StopPodSandbox(ctx context.Context, sb *api.PodSandbox) error {
	logger := klog.FromContext(ctx).WithValues("pod", sb.GetName(), "namespace", sb.GetNamespace(), "uid", sb.GetUid())
	if err := p.handler.OnStopPodSandbox(ctx, sb.GetUid(), sb.GetName(), sb.GetNamespace()); err != nil {
		logger.Error(err, "StopPodSandbox: detach failed")
		return err
	}
	return nil
}

// networkNamespacePath returns the pod sandbox's network namespace path, or ""
// if the sandbox has none (e.g. host networking).
func networkNamespacePath(sb *api.PodSandbox) string {
	linux := sb.GetLinux()
	if linux == nil {
		return ""
	}
	for _, ns := range linux.GetNamespaces() {
		if ns.GetType() == networkNamespaceType {
			return ns.GetPath()
		}
	}
	return ""
}
