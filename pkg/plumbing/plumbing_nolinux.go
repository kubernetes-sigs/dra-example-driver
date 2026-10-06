//go:build !linux

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

// The kube-ovn datapath (veth + netns + OVS) is Linux-only. On other platforms
// the helpers compile but return ErrNotImplemented so the package still builds
// (e.g. for editor tooling or cross-compilation of non-node binaries).

package plumbing

import "context"

func (a *ovsAttacher) createVethPair(ctx context.Context, host, pod string, mtu int) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) moveIntoNetns(ctx context.Context, pod, netnsPath, ifaceName string) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) configurePodIface(ctx context.Context, spec Spec) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) attachToOVS(ctx context.Context, hostVeth string, spec Spec) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) detachFromOVS(ctx context.Context, hostVeth string, spec Spec) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) cleanupHostVeth(ctx context.Context, hostVeth string) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) wireVMIBridge(ctx context.Context, spec Spec) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) unwireVMIBridge(ctx context.Context, spec Spec) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) ensureVMIDHCPServer(ctx context.Context, spec Spec, bridgeName string) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) stopVMIDHCPServer(bridgeName string) error {
	return ErrNotImplemented
}

func (a *ovsAttacher) DetachPodPorts(ctx context.Context, podName, podNamespace string) error {
	return ErrNotImplemented
}
