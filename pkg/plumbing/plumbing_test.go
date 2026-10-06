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
	"testing"
)

func TestSpecValidate(t *testing.T) {
	base := func() Spec {
		return Spec{
			PodName:      "p",
			PodNamespace: "ns",
			NetnsPath:    "/proc/1/ns/net",
			IfaceName:    "net1",
			IP:           "172.23.0.5/24",
			ContainerID:  "abcdef0123456789",
			IfaceID:      "p.ns.blue.ns.ovn",
		}
	}

	tests := []struct {
		name    string
		spec    Spec
		wantErr bool
	}{
		{"valid", base(), false},
		{"missing netns", func() Spec { s := base(); s.NetnsPath = ""; return s }(), true},
		{"missing iface", func() Spec { s := base(); s.IfaceName = ""; return s }(), true},
		{"missing ip", func() Spec { s := base(); s.IP = ""; return s }(), true},
		{"missing containerID", func() Spec { s := base(); s.ContainerID = ""; return s }(), true},
		{"missing iface-id", func() Spec { s := base(); s.IfaceID = ""; return s }(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestVethNames(t *testing.T) {
	a := &ovsAttacher{}
	host, pod, err := a.vethNames(Spec{ContainerID: "abcdef0123456789", IfaceName: "net1"})
	if err != nil {
		t.Fatalf("vethNames() unexpected error: %v", err)
	}
	wantHost := shortHashName("vh", "abcdef0123456789/net1")
	wantPod := shortHashName("vc", "abcdef0123456789/net1")
	if host != wantHost {
		t.Errorf("host = %q, want %q", host, wantHost)
	}
	if pod != wantPod {
		t.Errorf("pod = %q, want %q", pod, wantPod)
	}
	if host == pod {
		t.Errorf("host and pod names must differ, both = %q", host)
	}
	// Kernel interface names must fit IFNAMSIZ (15 chars).
	if len(host) > 15 || len(pod) > 15 {
		t.Errorf("veth name exceeds 15 chars: host=%q (%d) pod=%q (%d)", host, len(host), pod, len(pod))
	}
	// Deterministic: same input must reproduce the same names (Attach's own
	// idempotency and Detach both depend on this).
	host2, pod2, err := a.vethNames(Spec{ContainerID: "abcdef0123456789", IfaceName: "net1"})
	if err != nil || host2 != host || pod2 != pod {
		t.Errorf("vethNames() not deterministic: got (%q, %q, %v), want (%q, %q, nil)", host2, pod2, err, host, pod)
	}
}

// TestVethNamesLongIfaceName guards two historical bugs at once. First, an
// IfaceName long enough to underflow containerID[:12-len(iface)] used to
// panic with "slice bounds out of range [:-2]" instead of returning an error
// (hit live against a kube-ovn-network-binding-plugin demo using a 14-char
// IfaceName). Second, and more subtly, even a non-underflowing but long
// IfaceName (this driver's CRD-supplied names like "nicdda1950d" run 11
// chars) left only 12-len(iface) characters of the containerID to
// distinguish names, so two unrelated sandboxes whose container IDs merely
// shared a leading hex digit collided on identical veth names — reproduced
// live as "look up pod veth ...: Link not found" during a kube-ovn-dra-vmi
// redeploy. The hash-based scheme has no such underflow or entropy collapse:
// it must succeed here, produce a fixed-length name, and differ from a
// same-length-class iface name's names computed from a different key.
func TestVethNamesLongIfaceName(t *testing.T) {
	a := &ovsAttacher{}
	host, pod, err := a.vethNames(Spec{ContainerID: "abcdef0123456789", IfaceName: "poddda1950dc07"})
	if err != nil {
		t.Fatalf("vethNames() with a 14-char IfaceName: unexpected error: %v", err)
	}
	if len(host) > 15 || len(pod) > 15 {
		t.Errorf("veth name exceeds 15 chars: host=%q (%d) pod=%q (%d)", host, len(host), pod, len(pod))
	}

	// Different container IDs sharing a leading hex digit must NOT collide
	// (the actual bug: the old scheme derived names from only 1 truncated
	// character of the containerID here, since 12-len("nicdda1950d")=1).
	hostA, podA, err := a.vethNames(Spec{ContainerID: "cabc000000000000", IfaceName: "nicdda1950d"})
	if err != nil {
		t.Fatalf("vethNames() unexpected error: %v", err)
	}
	hostB, podB, err := a.vethNames(Spec{ContainerID: "cdef000000000000", IfaceName: "nicdda1950d"})
	if err != nil {
		t.Fatalf("vethNames() unexpected error: %v", err)
	}
	if hostA == hostB || podA == podB {
		t.Errorf("different container IDs sharing a leading hex digit collided: A=(%q,%q) B=(%q,%q)", hostA, podA, hostB, podB)
	}
}

func TestVethNamesEmptyContainerID(t *testing.T) {
	a := &ovsAttacher{}
	if _, _, err := a.vethNames(Spec{ContainerID: "", IfaceName: "net1"}); err == nil {
		t.Fatal("vethNames() with empty ContainerID: want error, got nil")
	}
}

func TestNICStore(t *testing.T) {
	s := NewNICStore()
	s.Add(Spec{PodUID: "uid-1", ClaimUID: "claim-a", IfaceName: "net1"})
	s.Add(Spec{PodUID: "uid-1", ClaimUID: "claim-b", IfaceName: "net2"})
	s.Add(Spec{PodUID: "uid-2", ClaimUID: "claim-c", IfaceName: "net1"})
	s.Add(Spec{PodUID: "uid-1", ClaimUID: "claim-a", IfaceName: "net1", IP: "10.0.0.5/24"})

	specs := s.Specs("uid-1")
	if len(specs) != 2 || specs[0].IP != "10.0.0.5/24" || specs[1].IfaceName != "net2" {
		t.Fatalf("Specs(uid-1) = %+v, want net1 (replaced) and net2", specs)
	}
	specs[0].NetnsPath = "/changed"
	if s.Specs("uid-1")[0].NetnsPath != "" {
		t.Error("Specs must return a copy")
	}
	if got := s.Specs("uid-1"); len(got) != 2 {
		t.Errorf("Specs must not drain the store, got %d", len(got))
	}

	s.RemoveClaim("claim-a")
	if got := s.Specs("uid-1"); len(got) != 1 || got[0].ClaimUID != "claim-b" {
		t.Errorf("after RemoveClaim(claim-a) Specs(uid-1) = %+v, want claim-b only", got)
	}
	s.RemoveClaim("claim-b")
	if got := s.Specs("uid-1"); got != nil {
		t.Errorf("after removing all claims Specs(uid-1) = %+v, want nil", got)
	}
	if got := s.Specs("uid-2"); len(got) != 1 {
		t.Errorf("Specs(uid-2) len = %d, want 1", len(got))
	}

	s.MarkAttached("uid-1", Spec{IfaceName: "net1"})
	s.MarkAttached("uid-1", Spec{IfaceName: "net1", ContainerID: "again"})
	s.MarkAttached("uid-1", Spec{IfaceName: "net2"})
	if got := s.TakeAttached("uid-1"); len(got) != 2 || got[0].ContainerID != "again" {
		t.Errorf("TakeAttached(uid-1) = %+v, want net1 (replaced) and net2", got)
	}
	if got := s.TakeAttached("uid-1"); len(got) != 0 {
		t.Errorf("TakeAttached(uid-1) after drain len = %d, want 0", len(got))
	}
}

func TestDeviceNames(t *testing.T) {
	for _, name := range []string{bridgeNameFor("net1"), vethRenameFor("net1"), shortHashName("vh", "a/very/long/key/that/exceeds/ifnamsiz")} {
		if len(name) > 15 {
			t.Errorf("%q exceeds IFNAMSIZ", name)
		}
	}
	if bridgeNameFor("net1") == bridgeNameFor("net2") || bridgeNameFor("net1") == vethRenameFor("net1") {
		t.Error("bridge and veth rename names must be distinct per interface")
	}
	if name := bridgeNameFor("net1"); name != shortHashName("kvb", "net1") {
		t.Errorf("bridgeNameFor(net1) = %q, want the deterministic hash name", name)
	}
}
