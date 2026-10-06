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
	"testing"
)

type recordingAttacher struct {
	attached []Spec
	detached []Spec
	swept    []string
	// failIface makes Attach fail for that interface name.
	failIface string
}

func (r *recordingAttacher) Attach(_ context.Context, spec Spec) error {
	if spec.IfaceName == r.failIface {
		return errors.New("attach failed")
	}
	r.attached = append(r.attached, spec)
	return nil
}

func (r *recordingAttacher) Detach(_ context.Context, spec Spec) error {
	r.detached = append(r.detached, spec)
	return nil
}

func (r *recordingAttacher) DetachPodPorts(_ context.Context, podName, podNamespace string) error {
	r.swept = append(r.swept, podNamespace+"/"+podName)
	return nil
}

func TestSandboxHandlerDetachesAttachedNICs(t *testing.T) {
	store := NewNICStore()
	attacher := &recordingAttacher{}
	h := NewSandboxHandler(store, attacher)

	store.Add(Spec{PodUID: "uid-1", ClaimUID: "claim", IfaceName: "net1"})
	store.Add(Spec{PodUID: "uid-1", ClaimUID: "claim", IfaceName: "net2"})
	labels := map[string]string{kubevirtLabelKey: kubevirtVirtLauncherLabel}
	if err := h.OnRunPodSandbox(context.Background(), "uid-1", "cid", "/var/run/netns/x", labels); err != nil {
		t.Fatalf("OnRunPodSandbox() error = %v", err)
	}
	if err := h.OnStopPodSandbox(context.Background(), "uid-1", "virt-launcher-vm1", "default"); err != nil {
		t.Fatalf("OnStopPodSandbox() error = %v", err)
	}

	if len(attacher.detached) != 2 {
		t.Fatalf("detached %d NICs, want 2", len(attacher.detached))
	}
	for _, spec := range attacher.detached {
		if spec.ContainerID != "cid" || spec.NetnsPath != "/var/run/netns/x" || !spec.KubeVirtVMI {
			t.Errorf("detached spec %+v is missing the sandbox fields set at attach time", spec)
		}
	}
	if len(attacher.swept) != 1 || attacher.swept[0] != "default/virt-launcher-vm1" {
		t.Errorf("swept = %v, want [default/virt-launcher-vm1]", attacher.swept)
	}

	// A second stop (or a stop after a plugin restart) has no Specs left but still sweeps.
	if err := h.OnStopPodSandbox(context.Background(), "uid-1", "virt-launcher-vm1", "default"); err != nil {
		t.Fatalf("second OnStopPodSandbox() error = %v", err)
	}
	if len(attacher.detached) != 2 || len(attacher.swept) != 2 {
		t.Errorf("after second stop: detached=%d swept=%d, want 2 and 2", len(attacher.detached), len(attacher.swept))
	}
}

func TestSandboxHandlerPlainPod(t *testing.T) {
	store := NewNICStore()
	attacher := &recordingAttacher{}
	h := NewSandboxHandler(store, attacher)

	store.Add(Spec{PodUID: "uid-1", ClaimUID: "claim", IfaceName: "net1"})
	if err := h.OnRunPodSandbox(context.Background(), "uid-1", "cid", "/var/run/netns/x", map[string]string{"app": "web"}); err != nil {
		t.Fatalf("OnRunPodSandbox() error = %v", err)
	}
	if len(attacher.attached) != 1 || attacher.attached[0].KubeVirtVMI {
		t.Errorf("attached = %+v, want one NIC without the VMI wiring", attacher.attached)
	}

	// A pod without DRA NICs is a no-op.
	if err := h.OnRunPodSandbox(context.Background(), "uid-2", "cid2", "/var/run/netns/y", nil); err != nil {
		t.Fatalf("OnRunPodSandbox() for a pod without NICs error = %v", err)
	}
	if len(attacher.attached) != 1 {
		t.Errorf("attached %d NICs, want 1", len(attacher.attached))
	}
}

func TestSandboxHandlerRollsBackPartialAttach(t *testing.T) {
	store := NewNICStore()
	attacher := &recordingAttacher{failIface: "net2"}
	h := NewSandboxHandler(store, attacher)

	store.Add(Spec{PodUID: "uid-1", ClaimUID: "claim", IfaceName: "net1"})
	store.Add(Spec{PodUID: "uid-1", ClaimUID: "claim", IfaceName: "net2"})
	if err := h.OnRunPodSandbox(context.Background(), "uid-1", "cid", "/var/run/netns/x", nil); err == nil {
		t.Fatal("OnRunPodSandbox() succeeded, want the attach error")
	}
	if len(attacher.detached) != 1 || attacher.detached[0].IfaceName != "net1" {
		t.Errorf("detached = %+v, want net1 rolled back", attacher.detached)
	}
	if got := store.TakeAttached("uid-1"); len(got) != 0 {
		t.Errorf("rolled back NICs still recorded as attached: %+v", got)
	}
}

func TestSandboxHandlerReattachesRecreatedSandbox(t *testing.T) {
	store := NewNICStore()
	attacher := &recordingAttacher{}
	h := NewSandboxHandler(store, attacher)
	store.Add(Spec{PodUID: "uid-1", ClaimUID: "claim", IfaceName: "net1"})

	for _, sandbox := range []string{"sandbox-1", "sandbox-2"} {
		if err := h.OnRunPodSandbox(context.Background(), "uid-1", sandbox, "/var/run/netns/"+sandbox, nil); err != nil {
			t.Fatalf("OnRunPodSandbox(%s) error = %v", sandbox, err)
		}
		if err := h.OnStopPodSandbox(context.Background(), "uid-1", "pod", "default"); err != nil {
			t.Fatalf("OnStopPodSandbox(%s) error = %v", sandbox, err)
		}
	}
	if len(attacher.attached) != 2 || attacher.attached[1].ContainerID != "sandbox-2" {
		t.Errorf("attached = %+v, want the NIC attached to both sandboxes", attacher.attached)
	}

	store.RemoveClaim("claim")
	if err := h.OnRunPodSandbox(context.Background(), "uid-1", "sandbox-3", "/var/run/netns/x", nil); err != nil {
		t.Fatalf("OnRunPodSandbox() after unprepare error = %v", err)
	}
	if len(attacher.attached) != 2 {
		t.Errorf("an unprepared claim's NIC was attached again: %+v", attacher.attached)
	}
}

func TestSandboxHandlerSynchronizeKeepsWorkingNICs(t *testing.T) {
	store := NewNICStore()
	attacher := &recordingAttacher{failIface: "net1"}
	h := NewSandboxHandler(store, attacher)
	store.Add(Spec{PodUID: "uid-1", ClaimUID: "claim", IfaceName: "net1"})
	store.Add(Spec{PodUID: "uid-1", ClaimUID: "claim", IfaceName: "net2"})

	if err := h.OnSynchronizeSandbox(context.Background(), "uid-1", "cid", "/var/run/netns/x", nil); err == nil {
		t.Fatal("OnSynchronizeSandbox() succeeded, want the attach error of net1")
	}
	if len(attacher.attached) != 1 || attacher.attached[0].IfaceName != "net2" {
		t.Errorf("attached = %+v, want net2 attached despite net1 failing", attacher.attached)
	}
	if len(attacher.detached) != 0 {
		t.Errorf("Synchronize detached %+v from a running pod", attacher.detached)
	}
	if got := store.TakeAttached("uid-1"); len(got) != 1 {
		t.Errorf("attached records = %+v, want net2", got)
	}
}
