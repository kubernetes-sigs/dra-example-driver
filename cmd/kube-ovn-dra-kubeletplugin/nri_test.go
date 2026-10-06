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
	"testing"

	"github.com/containerd/nri/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/soer3n/kube-ovn-dra-driver/pkg/plumbing"
)

type recordingAttacher struct {
	attached []plumbing.Spec
	swept    []string
}

func (r *recordingAttacher) Attach(_ context.Context, spec plumbing.Spec) error {
	r.attached = append(r.attached, spec)
	return nil
}

func (r *recordingAttacher) Detach(context.Context, plumbing.Spec) error { return nil }

func (r *recordingAttacher) DetachPodPorts(_ context.Context, podName, podNamespace string) error {
	r.swept = append(r.swept, podNamespace+"/"+podName)
	return nil
}

func sandbox(namespaces ...*api.LinuxNamespace) *api.PodSandbox {
	return &api.PodSandbox{
		Id: "sandbox-1", Uid: testPodUID, Name: testPod, Namespace: testNS,
		Linux: &api.LinuxPodSandbox{Namespaces: namespaces},
	}
}

func TestNetworkNamespacePath(t *testing.T) {
	assert.Empty(t, networkNamespacePath(&api.PodSandbox{}))
	assert.Empty(t, networkNamespacePath(sandbox(&api.LinuxNamespace{Type: "ipc", Path: "/proc/1/ns/ipc"})))
	assert.Equal(t, "/var/run/netns/x", networkNamespacePath(sandbox(
		&api.LinuxNamespace{Type: "ipc", Path: "/proc/1/ns/ipc"},
		&api.LinuxNamespace{Type: networkNamespaceType, Path: "/var/run/netns/x"},
	)))
}

func TestNRIPluginSandboxEvents(t *testing.T) {
	store := plumbing.NewPendingStore()
	attacher := &recordingAttacher{}
	p := &nriPlugin{handler: plumbing.NewSandboxHandler(store, attacher)}
	store.Add(testPodUID, plumbing.Spec{IfaceName: "net1"})

	// A sandbox without a network namespace is skipped and keeps its NICs pending.
	require.NoError(t, p.RunPodSandbox(context.Background(), sandbox()))
	assert.Empty(t, attacher.attached)

	require.NoError(t, p.RunPodSandbox(context.Background(), sandbox(&api.LinuxNamespace{Type: networkNamespaceType, Path: "/var/run/netns/x"})))
	require.Len(t, attacher.attached, 1)
	assert.Equal(t, "sandbox-1", attacher.attached[0].ContainerID)
	assert.Equal(t, "/var/run/netns/x", attacher.attached[0].NetnsPath)

	require.NoError(t, p.StopPodSandbox(context.Background(), sandbox()))
	assert.Equal(t, []string{testNS + "/" + testPod}, attacher.swept)
}
