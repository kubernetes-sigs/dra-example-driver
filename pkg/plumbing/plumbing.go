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

// Package plumbing attaches a DRA-allocated kube-ovn NIC to a pod's network
// namespace without Multus or a CNI call.
//
// kube-ovn-controller allocates the NIC's address and creates its logical
// switch port (see pkg/nicprepare). This package performs the node-local part
// kube-ovn-cni does for a Multus attachment: a veth pair into the pod netns,
// addressing, and the host end on br-int with external_ids:iface-id set to the
// logical switch port, so ovn-controller binds the port to this chassis.
//
// # Timing
//
// The allocation is resolved in PrepareResourceClaims, before the pod sandbox
// exists. The attach can only run once the sandbox netns is created, so it is
// driven from an NRI hook (RunPodSandbox). The two phases are bridged by a
// PendingStore (see store.go): Prepare registers a Spec keyed by pod UID; the
// NRI hook drains it and calls Attach.
//
//	PrepareResourceClaims ──> wait for kube-ovn ──> PendingStore.Add(podUID, Spec)
//	NRI RunPodSandbox      ──> PendingStore.Take(podUID) ──> Attacher.Attach
//	NRI StopPodSandbox     ──> PendingStore.TakeAttached(podUID) ──> Attacher.Detach
package plumbing

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
)

// ErrNotImplemented is returned by the datapath helpers on platforms other
// than Linux.
var ErrNotImplemented = errors.New("plumbing: not implemented yet")

// Datapath constants mirrored from kube-ovn (keep in sync with KUBE_OVN_VERSION).
const (
	// integrationBridge is the OVN integration bridge all NIC ports go to.
	integrationBridge = "br-int"
	// cniVendor is the external_ids:vendor value kube-ovn stamps on ports.
	cniVendor = "kube-ovn"
	// ownerExternalID marks the OVS ports this driver creates, so they can be
	// found and removed without in-memory state.
	ownerExternalID = "kube-ovn-dra-driver"
	ownerName       = "nic"
)

// Spec is everything the attach step needs for ONE secondary NIC. It is built
// in PrepareResourceClaims from the DRA device attributes + the kube-ovn IPAM
// result, then consumed later by the NRI sandbox hook.
type Spec struct {
	// --- identity (used as the PendingStore key and for OVS external_ids) ---

	// PodUID / PodName / PodNamespace identify the target pod.
	PodUID       string
	PodName      string
	PodNamespace string

	// --- where to plumb ---

	// NetnsPath is the pod sandbox network namespace path. EMPTY at IPAM time;
	// filled in by the NRI hook from the RunPodSandbox event before Attach.
	NetnsPath string
	// IfaceName is the interface name inside the pod netns (e.g. "net1").
	IfaceName string

	// --- IPAM result (from kube-ovn-controller, see pkg/nicprepare) ---

	// IP is the allocated address in CIDR notation (e.g. "172.23.0.5/24"). For
	// dual-stack NICs it is the IPv4 address; VMIs only get this one, by DHCPv4.
	IP string
	// ExtraIPs are the NIC's addresses of the other IP family, in CIDR notation.
	// They are configured on pod interfaces only.
	ExtraIPs []string
	// MAC is the allocated hardware address (e.g. "00:11:22:33:44:55").
	MAC string
	// Gateway is the subnet gateway. Used for per-NIC routes ONLY — the default
	// route belongs to eth0 and must never be touched (this is a secondary NIC).
	Gateway string
	// Routes are extra routes to install via this interface (CIDR strings).
	// The subnet CIDR itself is added from IP; Gateway is not made default.
	Routes []string
	// MTU for the pod-side interface; 0 means inherit the bridge/default.
	MTU int

	// ContainerID is the sandbox container ID (from the NRI event). Used to
	// derive the host/pod veth names exactly as kube-ovn does — see vethNames.
	ContainerID string

	// --- binding ---

	// Provider is the kube-ovn subnet provider of the NIC.
	Provider string
	// IfaceID is the external_ids:iface-id of the host port. It MUST equal the
	// logical switch port name kube-ovn-controller created for this NIC, or
	// OVN will not bind the port. Underlay subnets work the same way: their
	// logical switch reaches the VLAN through a localnet port.
	IfaceID string

	// KubeVirtVMI marks that this pod is a KubeVirt virt-launcher pod (detected
	// from the "kubevirt.io: virt-launcher" pod label in the NRI RunPodSandbox
	// event — see cmd/kube-ovn-dra-kubeletplugin/nri.go). A plain pod can
	// consume the veth directly; a VM's QEMU process cannot — QEMU's tap netdev
	// backend needs an actual tun/tap-driver device (TUNSETIFF), and a veth is
	// not one. When true, Attach additionally creates a Linux bridge + a real
	// tap device inside the pod netns, enslaves both the tap and the existing
	// veth to it, and renames the veth out of the way so the tap ends up with
	// the IfaceName the KubeVirt network-binding-plugin sidecar
	// (kube-ovn-network-binding-plugin) already expects as its domain XML
	// target — no change needed on that side.
	KubeVirtVMI bool
}

// Validate checks that the fields required for an attach are present.
func (s *Spec) Validate() error {
	switch {
	case s.NetnsPath == "":
		return errors.New("plumbing: Spec.NetnsPath is empty (sandbox not resolved yet)")
	case s.IfaceName == "":
		return errors.New("plumbing: Spec.IfaceName is empty")
	case s.IP == "":
		return errors.New("plumbing: Spec.IP is empty")
	case s.ContainerID == "":
		return errors.New("plumbing: Spec.ContainerID is empty (cannot derive veth names)")
	case s.IfaceID == "":
		return errors.New("plumbing: Spec.IfaceID is empty, OVN would not bind the port")
	}
	return nil
}

// Attacher creates and tears down the pod-side datapath for one NIC.
type Attacher interface {
	// Attach creates the veth pair, moves the pod end into Spec.NetnsPath,
	// configures it from the IPAM result, and wires the host end to OVS.
	// Must be idempotent: a retried Attach for an already-plumbed iface is a
	// no-op, not an error.
	Attach(ctx context.Context, spec Spec) error

	// Detach removes the OVS port and veth for one NIC. Idempotent: detaching
	// something already gone returns nil.
	Detach(ctx context.Context, spec Spec) error

	// DetachPodPorts removes every OVS port (and its host veth) this driver
	// created for the pod, found by external_ids instead of a Spec.
	DetachPodPorts(ctx context.Context, podName, podNamespace string) error
}

// ovsAttacher is the kube-ovn / OVS implementation of Attacher.
type ovsAttacher struct {
	// dhcpServers tracks the per-VMI single-client DHCP servers started by
	// ensureVMIDHCPServer, keyed by bridge name (globally unique — see
	// shortHashName). This is in-process state, unlike everything else in
	// this package: it does not survive a driver restart, only a running
	// process's lifetime, matching KubeVirt's own per-launcher-process DHCP
	// server (this driver plays that same role for VMI secondary NICs, just
	// for potentially many pods in one process instead of one per pod).
	dhcpMu      sync.Mutex
	dhcpServers map[string]dhcpServeCloser
}

// NewOVSAttacher returns the default OVS-backed Attacher.
func NewOVSAttacher() Attacher {
	return &ovsAttacher{dhcpServers: make(map[string]dhcpServeCloser)}
}

// Attach is the high-level recipe; see the private helpers below. The ordering
// matters: build the link, enter the netns,
// configure addressing, THEN hand the host end to OVS so OVN can bind it.
func (a *ovsAttacher) Attach(ctx context.Context, spec Spec) error {
	if err := spec.Validate(); err != nil {
		return err
	}

	hostVeth, podVeth, err := a.vethNames(spec)
	if err != nil {
		return err
	}

	// 1. Create the veth pair on the host side.
	if err := a.createVethPair(ctx, hostVeth, podVeth, spec.MTU); err != nil {
		return err
	}

	// 2. Move the pod end into the sandbox netns and rename it to IfaceName.
	if err := a.moveIntoNetns(ctx, podVeth, spec.NetnsPath, spec.IfaceName); err != nil {
		_ = a.cleanupHostVeth(ctx, hostVeth) // best-effort
		return err
	}

	// 3. Configure addressing inside the pod netns (IP/MAC/MTU + per-NIC
	//    routes). MUST NOT replace the default route.
	if err := a.configurePodIface(ctx, spec); err != nil {
		_ = a.cleanupHostVeth(ctx, hostVeth)
		return err
	}

	// 4. Attach the host end to br-int and stamp external_ids so OVN binds the
	//    logical switch port to this chassis.
	if err := a.attachToOVS(ctx, hostVeth, spec); err != nil {
		_ = a.cleanupHostVeth(ctx, hostVeth)
		return err
	}

	// 5. KubeVirt VMIs need a real tap device, not a veth, for QEMU to attach
	//    to. Wrap the veth in a bridge+tap, freeing IfaceName for the tap.
	if spec.KubeVirtVMI {
		if err := a.wireVMIBridge(ctx, spec); err != nil {
			_ = a.detachFromOVS(ctx, hostVeth, spec)
			_ = a.cleanupHostVeth(ctx, hostVeth)
			return err
		}
	}

	return nil
}

// Detach is the symmetric teardown: drop the bridge/tap (if any), the OVS
// port, then the veth.
func (a *ovsAttacher) Detach(ctx context.Context, spec Spec) error {
	if spec.KubeVirtVMI {
		if err := a.unwireVMIBridge(ctx, spec); err != nil {
			return err
		}
	}

	hostVeth, _, err := a.vethNames(spec)
	if err != nil {
		return err
	}
	// Deleting the OVS port first stops OVN from re-binding while we tear down.
	if err := a.detachFromOVS(ctx, hostVeth, spec); err != nil {
		return err
	}
	return a.cleanupHostVeth(ctx, hostVeth)
}

// ---------------------------------------------------------------------------
// Datapath helpers
// ---------------------------------------------------------------------------

// vethNames derives the host/pod veth endpoint names. Originally this mirrored
// kube-ovn's generateNicName (pkg/daemon/ovs_linux.go) —
// containerID[:12-len(iface)]+"_"+iface+"_h"/"_c" — on the assumption that
// kube-ovn's control plane needed the host veth's OS-level name to match its
// own convention. That assumption was wrong: OVN binds the Logical Switch
// Port purely off external_ids:iface-id (set by attachToOVS from
// Spec.IfaceID), never by parsing veth names —
// confirmed against kube-ovn's own pkg/controller and pkg/ovs, which only ever
// query OVS by external-ids:iface-id. The veth's kernel name is a host-local
// implementation detail, not a control-plane contract.
//
// That old scheme was also a real bug: when IfaceName is long (this driver's
// CRD-supplied names like "nicdda1950d" run 11 chars), only
// 12-len(iface)=1 character of the containerID survives truncation. Two
// unrelated sandboxes whose container IDs merely start with the same hex
// digit (1-in-16 odds) then compute IDENTICAL host/pod veth names. Whichever
// Attach runs second finds the host end already present (see createVethPair's
// idempotency check), skips creating its own pair, and then fails in
// moveIntoNetns looking for a "pod" peer that was already moved into a
// DIFFERENT pod's netns — reproduced live as "look up pod veth ...: Link not
// found" during a kube-ovn-dra-vmi redeploy.
//
// Hashing the full (containerID, iface) pair instead gives 32 bits of
// collision resistance regardless of iface name length, at the same
// IFNAMSIZ-safe fixed length shortHashName already provides for the
// bridge/tap names below. "vh"/"vc" prefixes keep host/pod visually paired
// under the same hash suffix while staying distinct from those ("kvb"/"kvv").
func (a *ovsAttacher) vethNames(spec Spec) (host, pod string, err error) {
	if spec.ContainerID == "" {
		return "", "", errors.New("plumbing: Spec.ContainerID is empty (cannot derive veth names)")
	}
	key := spec.ContainerID + "/" + spec.IfaceName
	return shortHashName("vh", key), shortHashName("vc", key), nil
}

// shortHashName derives a fixed-length, IFNAMSIZ-safe interface name from key,
// regardless of key's own length. Used for the bridge/tap devices
// wireVMIBridge adds, and (see vethNames above) for the veth pair itself.
func shortHashName(prefix, key string) string {
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s%x", prefix, sum[:4])[:len(prefix)+8]
}

// bridgeNameFor and vethRenameFor derive the bridge and the "moved aside"
// veth name wireVMIBridge uses for a given IfaceName. Deterministic from
// IfaceName alone so Detach's unwireVMIBridge needs no extra stored state.
func bridgeNameFor(ifaceName string) string { return shortHashName("kvb", ifaceName) }
func vethRenameFor(ifaceName string) string { return shortHashName("kvv", ifaceName) }

// kubevirtQemuUID is the uid/gid virt-launcher's compute and hook-sidecar
// containers run as in modern (non-root-by-default) KubeVirt — verified
// against a live v1.9.0-rc.0 cluster's pod spec
// (securityContext.runAsUser/runAsGroup on both the "compute" and
// "hook-sidecar-*" containers). The tap device wireVMIBridge creates must be
// owned by this uid/gid or QEMU (running as this user) cannot open it.
const kubevirtQemuUID = 107

// The per-step datapath helpers (createVethPair, moveIntoNetns,
// configurePodIface, attachToOVS, detachFromOVS, cleanupHostVeth) are
// platform-specific and live in plumbing_linux.go (real netlink/OVS
// implementation) and plumbing_nolinux.go (ErrNotImplemented stubs).
