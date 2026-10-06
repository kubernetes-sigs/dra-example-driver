//go:build linux

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
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	dhcp "github.com/krolaw/dhcp4"
	dhcpconn "github.com/krolaw/dhcp4/conn"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

// createVethPair creates the veth pair on the host and brings the host side up.
// Idempotent: if the host end already exists (retry), it is reused.
func (a *ovsAttacher) createVethPair(ctx context.Context, host, pod string, mtu int) error {
	if _, err := netlink.LinkByName(host); err == nil {
		return nil // already created on a previous attempt
	}

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: host},
		PeerName:  pod,
	}
	if mtu > 0 {
		veth.MTU = mtu
	}
	if err := netlink.LinkAdd(veth); err != nil {
		return fmt.Errorf("create veth %s<->%s: %w", host, pod, err)
	}

	hostLink, err := netlink.LinkByName(host)
	if err != nil {
		return fmt.Errorf("look up host veth %s: %w", host, err)
	}
	if mtu > 0 {
		if peerLink, err := netlink.LinkByName(pod); err == nil {
			_ = netlink.LinkSetMTU(peerLink, mtu)
		}
	}
	if err := netlink.LinkSetUp(hostLink); err != nil {
		return fmt.Errorf("set host veth %s up: %w", host, err)
	}
	return nil
}

// moveIntoNetns moves the pod-side veth (named pod) into the sandbox netns and
// renames it to ifaceName. The link is left DOWN so configurePodIface can set
// its MAC before bringing it up.
//
// Idempotent: if ifaceName already exists inside the target netns, the move+
// rename already happened on a previous (partially-failed) Attach for this
// same Spec — createVethPair's own idempotency check would otherwise
// short-circuit ("host still exists") on a retry, while this function
// unconditionally looked up "pod" in the CALLER's netns and failed, since
// that peer no longer lives there once it's been moved+renamed away. That
// contradicted Attach's documented idempotency guarantee and reproduced
// live as "look up pod veth ...: Link not found" on a retried Attach whose
// earlier attempt got past this step but failed in a later one (e.g.
// attachToOVS or wireVMIBridge).
func (a *ovsAttacher) moveIntoNetns(ctx context.Context, pod, netnsPath, ifaceName string) error {
	ns, err := netns.GetFromPath(netnsPath)
	if err != nil {
		return fmt.Errorf("open netns %s: %w", netnsPath, err)
	}
	defer ns.Close()

	if h, err := netlink.NewHandleAt(ns); err == nil {
		_, alreadyMoved := h.LinkByName(ifaceName)
		h.Close()
		if alreadyMoved == nil {
			return nil // already moved and renamed on a previous attempt
		}
	}

	link, err := netlink.LinkByName(pod)
	if err != nil {
		return fmt.Errorf("look up pod veth %s: %w", pod, err)
	}
	if err := netlink.LinkSetNsFd(link, int(ns)); err != nil {
		return fmt.Errorf("move %s into netns %s: %w", pod, netnsPath, err)
	}

	h, err := netlink.NewHandleAt(ns)
	if err != nil {
		return fmt.Errorf("netlink handle for netns %s: %w", netnsPath, err)
	}
	defer h.Close()

	inner, err := h.LinkByName(pod)
	if err != nil {
		return fmt.Errorf("look up moved veth %s in netns: %w", pod, err)
	}
	// Rename requires the link to be down.
	if err := h.LinkSetDown(inner); err != nil {
		return fmt.Errorf("set %s down before rename: %w", pod, err)
	}
	if err := h.LinkSetName(inner, ifaceName); err != nil {
		return fmt.Errorf("rename %s -> %s in netns: %w", pod, ifaceName, err)
	}
	return nil
}

// configurePodIface sets MAC, MTU, the IP/CIDR and per-NIC routes inside the
// pod netns, then brings the interface up. It MUST NOT install a default route
// — eth0 owns the default; this is a secondary NIC.
func (a *ovsAttacher) configurePodIface(ctx context.Context, spec Spec) error {
	ns, err := netns.GetFromPath(spec.NetnsPath)
	if err != nil {
		return fmt.Errorf("open netns %s: %w", spec.NetnsPath, err)
	}
	defer ns.Close()

	h, err := netlink.NewHandleAt(ns)
	if err != nil {
		return fmt.Errorf("netlink handle for netns %s: %w", spec.NetnsPath, err)
	}
	defer h.Close()

	link, err := h.LinkByName(spec.IfaceName)
	if err != nil {
		return fmt.Errorf("look up %s in netns: %w", spec.IfaceName, err)
	}

	// MAC must be set while the link is down (moveIntoNetns left it down).
	if spec.MAC != "" {
		mac, err := net.ParseMAC(spec.MAC)
		if err != nil {
			return fmt.Errorf("parse MAC %q: %w", spec.MAC, err)
		}
		if err := h.LinkSetHardwareAddr(link, mac); err != nil {
			return fmt.Errorf("set MAC on %s: %w", spec.IfaceName, err)
		}
	}
	if spec.MTU > 0 {
		if err := h.LinkSetMTU(link, spec.MTU); err != nil {
			return fmt.Errorf("set MTU on %s: %w", spec.IfaceName, err)
		}
	}

	addresses := []string{spec.IP}
	if !spec.KubeVirtVMI {
		addresses = append(addresses, spec.ExtraIPs...)
	}
	for _, ip := range addresses {
		addr, err := netlink.ParseAddr(ip)
		if err != nil {
			return fmt.Errorf("parse IP %q: %w", ip, err)
		}
		if addr.IP.To4() == nil {
			// The address is unique in its subnet; skip duplicate address detection
			// so it is usable right away, as kube-ovn-cni does for pod interfaces.
			addr.Flags |= unix.IFA_F_NODAD
		}
		if err := h.AddrAdd(link, addr); err != nil && !isExists(err) {
			return fmt.Errorf("add address %s to %s: %w", ip, spec.IfaceName, err)
		}
	}

	if err := h.LinkSetUp(link); err != nil {
		return fmt.Errorf("set %s up: %w", spec.IfaceName, err)
	}

	// Per-NIC routes only. Reject default routes defensively.
	for _, r := range spec.Routes {
		_, dst, err := net.ParseCIDR(r)
		if err != nil {
			return fmt.Errorf("parse route %q: %w", r, err)
		}
		if ones, _ := dst.Mask.Size(); ones == 0 {
			return fmt.Errorf("refusing to install default route %q on secondary NIC %s", r, spec.IfaceName)
		}
		route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: dst}
		if spec.Gateway != "" {
			route.Gw = net.ParseIP(spec.Gateway)
		}
		if err := h.RouteAdd(route); err != nil && !isExists(err) {
			return fmt.Errorf("add route %s on %s: %w", r, spec.IfaceName, err)
		}
	}
	return nil
}

// attachToOVS adds the host veth to br-int with external_ids:iface-id set to
// the logical switch port, so OVN binds the port to this chassis.
func (a *ovsAttacher) attachToOVS(ctx context.Context, hostVeth string, spec Spec) error {
	ipNoMask := strings.Join(ipsWithoutMask(append([]string{spec.IP}, spec.ExtraIPs...)), ",")
	return ovsVsctl(ctx,
		"--may-exist", "add-port", integrationBridge, hostVeth,
		"--", "set", "interface", hostVeth,
		"external_ids:iface-id="+spec.IfaceID,
		"external_ids:vendor="+cniVendor,
		"external_ids:"+ownerExternalID+"="+ownerName,
		"external_ids:pod_name="+spec.PodName,
		"external_ids:pod_namespace="+spec.PodNamespace,
		"external_ids:ip="+ipNoMask,
		"external_ids:pod_netns="+spec.NetnsPath,
	)
}

// detachFromOVS removes the host veth's port from br-int.
func (a *ovsAttacher) detachFromOVS(ctx context.Context, hostVeth string, _ Spec) error {
	return ovsVsctl(ctx, "--if-exists", "del-port", integrationBridge, hostVeth)
}

// DetachPodPorts removes the OVS ports this driver created for the pod, found
// by the owner and pod external_ids, and deletes their host veths.
func (a *ovsAttacher) DetachPodPorts(ctx context.Context, podName, podNamespace string) error {
	out, err := exec.CommandContext(ctx, "ovs-vsctl", "--bare", "--columns=name", "find", "interface",
		"external_ids:"+ownerExternalID+"="+ownerName,
		"external_ids:pod_name="+podName,
		"external_ids:pod_namespace="+podNamespace,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("find ovs ports of pod %s/%s: %w: %s", podNamespace, podName, err, strings.TrimSpace(string(out)))
	}
	var errs []error
	for _, name := range strings.Fields(string(out)) {
		if err := ovsVsctl(ctx, "--if-exists", "del-port", name); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := a.cleanupHostVeth(ctx, name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// cleanupHostVeth deletes the host-side veth, which removes its pod-side peer.
// Idempotent: a missing link is not an error.
func (a *ovsAttacher) cleanupHostVeth(ctx context.Context, hostVeth string) error {
	link, err := netlink.LinkByName(hostVeth)
	if err != nil {
		return nil // already gone
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete host veth %s: %w", hostVeth, err)
	}
	return nil
}

// wireVMIBridge wraps the already-plumbed veth (currently named
// spec.IfaceName) in a Linux bridge + a real tap device, inside the pod
// netns: QEMU's tap netdev backend attaches via TUNSETIFF, which only works
// against an actual tun/tap-driver device, not a veth. The original veth is
// renamed to vethRenameFor(spec.IfaceName) and enslaved to a new bridge
// (bridgeNameFor); a new tap device is created, enslaved to the same bridge,
// and — critically — named spec.IfaceName, so it ends up being the device the
// KubeVirt network-binding-plugin sidecar's domain XML already targets, with
// no change needed on that side.
//
// Idempotent: if spec.IfaceName already refers to a tuntap device (a retried
// call after a previous success), this is a no-op.
func (a *ovsAttacher) wireVMIBridge(ctx context.Context, spec Spec) error {
	ns, err := netns.GetFromPath(spec.NetnsPath)
	if err != nil {
		return fmt.Errorf("open netns %s: %w", spec.NetnsPath, err)
	}
	defer ns.Close()

	h, err := netlink.NewHandleAt(ns)
	if err != nil {
		return fmt.Errorf("netlink handle for netns %s: %w", spec.NetnsPath, err)
	}
	defer h.Close()

	existing, err := h.LinkByName(spec.IfaceName)
	if err != nil {
		return fmt.Errorf("look up %s to wrap in a bridge: %w", spec.IfaceName, err)
	}
	bridgeName := bridgeNameFor(spec.IfaceName)
	if existing.Type() == "tuntap" {
		// Already wired by a previous Attach. The DHCP server, however, is
		// per-process (not per-netns) state: a driver restart loses it even
		// though the bridge/tap survive, so it must still be (re-)ensured.
		return a.ensureVMIDHCPServer(ctx, spec, bridgeName)
	}

	vethName := vethRenameFor(spec.IfaceName)
	if err := h.LinkSetDown(existing); err != nil {
		return fmt.Errorf("set %s down before rename: %w", spec.IfaceName, err)
	}
	if err := h.LinkSetName(existing, vethName); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", spec.IfaceName, vethName, err)
	}

	bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}}
	if err := h.LinkAdd(bridge); err != nil && !isExists(err) {
		return fmt.Errorf("create bridge %s: %w", bridgeName, err)
	}
	bridgeLink, err := h.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("look up bridge %s: %w", bridgeName, err)
	}

	// Pin the bridge's own MAC via an explicit SETLINK *before* any port is
	// enslaved. A freshly created, portless bridge's address isn't
	// administratively fixed (addr_assign_type stays NET_ADDR_PERM/random),
	// so the kernel keeps dynamically re-adopting the lowest MAC among its
	// *current* ports on every enslave (br_stp_recalculate_bridge_id) —
	// confirmed live: after we forced the tap's MAC to spec.MAC and enslaved
	// it second, the bridge silently drifted to that value while the veth
	// (which had already copied the bridge's *pre-drift* address) kept the
	// stale one, so bridge and veth ended up with different MACs despite
	// the copy below. Re-issuing the same address here via SETLINK marks it
	// NET_ADDR_SET, which the kernel treats as administratively fixed and
	// no longer overrides once ports are added.
	bridgeMAC := bridgeLink.Attrs().HardwareAddr
	if err := h.LinkSetHardwareAddr(bridgeLink, bridgeMAC); err != nil {
		return fmt.Errorf("pin bridge %s MAC: %w", bridgeName, err)
	}

	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{Name: spec.IfaceName},
		Mode:      netlink.TUNTAP_MODE_TAP,
		Flags:     netlink.TUNTAP_NO_PI | netlink.TUNTAP_VNET_HDR | netlink.TUNTAP_ONE_QUEUE,
		Owner:     kubevirtQemuUID,
		Group:     kubevirtQemuUID,
	}
	if err := addTuntapInNetns(ns, tap); err != nil {
		return fmt.Errorf("create tap %s: %w", spec.IfaceName, err)
	}
	tapLink, err := h.LinkByName(spec.IfaceName)
	if err != nil {
		return fmt.Errorf("look up tap %s: %w", spec.IfaceName, err)
	}

	// The tap's MAC is what the KubeVirt sidecar puts on the domain's virtio-net
	// interface (it has no other way to learn kube-ovn's IPAM-assigned MAC).
	// It MUST equal spec.MAC: kube-ovn's OVN logical switch port enforces port
	// security keyed on this exact MAC (+ IP), so any other MAC — including
	// libvirt's own auto-generated fallback if none is set at all — gets its
	// traffic silently dropped at the switch. Tuntap creation's TUNSETIFF
	// ioctl does not itself honor LinkAttrs.HardwareAddr, so this is a
	// separate, explicit SETLINK call.
	if spec.MAC != "" {
		mac, err := net.ParseMAC(spec.MAC)
		if err != nil {
			return fmt.Errorf("parse MAC %q for tap %s: %w", spec.MAC, spec.IfaceName, err)
		}
		if err := h.LinkSetHardwareAddr(tapLink, mac); err != nil {
			return fmt.Errorf("set MAC %s on tap %s: %w", spec.MAC, spec.IfaceName, err)
		}
	}

	// The veth still carries spec.MAC from the earlier configurePodIface step
	// (which runs identically for the plain-pod case, where the veth IS the
	// pod's own interface and must have it). Now that the tap carries that
	// same MAC instead, the veth needs a different one.
	//
	// This mirrors KubeVirt's own bridge binding exactly (netpod.go's
	// bridgeBindingSpec: podIface.CopyMacFrom = bridgeIface.Name, plus
	// LinuxStack.PortLearning = false), not an independent design: the veth's
	// MAC becomes a copy of the bridge's own (auto-assigned) MAC, and FDB
	// learning is disabled on the veth port. Empirically, our first attempt
	// — a distinct-but-unique MAC via vethMACFor, default learning left on —
	// eliminated the MAC collision (confirmed via `bridge fdb show`) but the
	// OVN DHCP reply still never crossed from the veth port to the tap port.
	// Deploying a side-by-side comparison VMI wired via Multus + KubeVirt's
	// real bridge binding on the identical OVN subnet got DHCP instantly,
	// and reading netpod.go showed this exact MAC+learning combination is
	// the difference — so we now reproduce it verbatim instead of a
	// from-scratch equivalent.
	if err := h.LinkSetHardwareAddr(existing, bridgeMAC); err != nil {
		return fmt.Errorf("copy bridge MAC onto %s: %w", vethName, err)
	}

	if err := h.LinkSetMaster(existing, bridgeLink); err != nil {
		return fmt.Errorf("enslave %s to bridge %s: %w", vethName, bridgeName, err)
	}
	if err := h.LinkSetMaster(tapLink, bridgeLink); err != nil {
		return fmt.Errorf("enslave tap %s to bridge %s: %w", spec.IfaceName, bridgeName, err)
	}

	if err := h.LinkSetUp(existing); err != nil {
		return fmt.Errorf("set %s up: %w", vethName, err)
	}
	if err := h.LinkSetUp(tapLink); err != nil {
		return fmt.Errorf("set tap %s up: %w", spec.IfaceName, err)
	}
	if err := h.LinkSetUp(bridgeLink); err != nil {
		return fmt.Errorf("set bridge %s up: %w", bridgeName, err)
	}

	// Must come after enslavement + up: PortLearning is a bridge-port
	// attribute (IFLA_BRPORT_LEARNING), invalid until the link is a slave.
	if err := h.LinkSetLearning(existing, false); err != nil {
		return fmt.Errorf("disable FDB learning on %s: %w", vethName, err)
	}

	if spec.MAC != "" {
		mac, err := net.ParseMAC(spec.MAC)
		if err != nil {
			return fmt.Errorf("parse MAC %q for tap %s: %w", spec.MAC, spec.IfaceName, err)
		}

		// Setting a bridge port's own kernel hwaddr (done above, before
		// enslavement) makes the kernel auto-register it as a "self,
		// permanent" bridge FDB entry — visible as `bridge fdb show`
		// reporting BOTH "self" and "permanent" for (spec.MAC, tap). That
		// combination isn't a harmless label: the kernel's bridge RX path
		// treats a frame whose destination matches a LOCAL (self) FDB entry
		// as addressed to the bridge device's own stack (br_pass_frame_up),
		// not as something to forward out that port. For a real host-side
		// port with an IP this is correct (deliver locally); for a
		// QEMU-facing tap it is not — the guest is a forwarding
		// destination, not a local listener — so every unicast frame
		// arriving on any OTHER port for this exact MAC (kube-ovn's OVN
		// DHCP/ARP replies, or any real traffic OVN ever sends back to this
		// VM) gets silently swallowed instead of reaching the tap, while
		// the SAME MAC's broadcast/DHCP traffic still worked because our
		// own DHCP server binds directly to the bridge device and never
		// needs port-to-port forwarding at all. Confirmed empirically: a
		// side-by-side Multus-attached VMI (using KubeVirt's own real
		// bridge binding, which deliberately never sets a MacAddress on its
		// tap — see netpod.go's bridgeBindingSpec) could ping its OVN
		// gateway with 0% loss; reproducing the failure live by setting a
		// tap's own kernel MAC to match its guest's traffic MAC, then
		// fixing it in place with exactly the command below, took that
		// same ping from 100% loss to 0%.
		//
		// `bridge fdb replace <spec.MAC> dev <tap> master static` re-registers
		// the identical (MAC, port) pair as a plain forwarding entry instead,
		// without the kernel's internal self/local bit. We still deliberately
		// keep the tap's own kernel hwaddr equal to spec.MAC (unlike
		// KubeVirt) rather than leaving it unset: the sidecar has no other
		// channel to learn kube-ovn's IPAM-assigned MAC for a DRA network
		// (no Multus network-status annotation exists to read it from), so
		// it discovers it via a netlink lookup of this exact device by
		// name. This keeps a real forwarding entry AND that discovery path
		// working simultaneously.
		//
		// MUST run after tap's enslavement to bridgeLink above: `master`
		// requires the port to already have a bridge master — attempting
		// this beforehand fails with "RTNETLINK answers: Operation not
		// supported" (confirmed live: an earlier revision called this right
		// after setting the tap's MAC, before LinkSetMaster, and every
		// Attach failed outright with that exact error).
		//
		// Shells out to the `bridge` CLI (iproute2) rather than constructing
		// the equivalent RTM_NEWNEIGH via netlink.Neigh{Family: AF_BRIDGE,
		// Flags: NTF_MASTER, State: NUD_NOARP} + Handle.NeighSet: that raw
		// netlink form reliably fails with EOPNOTSUPP ("operation not
		// supported") in this environment even with correct ordering, while
		// `bridge fdb replace` with the same intent succeeds. Matches the
		// existing ovsVsctl precedent in this file for shelling out to a
		// trusted, imported CLI tool instead of hand-rolling its netlink
		// protocol.
		if err := bridgeFdbReplaceStatic(ctx, ns, mac, tapLink.Attrs().Name); err != nil {
			return fmt.Errorf("replace auto-created self FDB entry for %s on tap %s with a forwarding one: %w", spec.MAC, spec.IfaceName, err)
		}
	}

	return a.ensureVMIDHCPServer(ctx, spec, bridgeName)
}

// ensureVMIDHCPServer starts the per-VMI single-client DHCP server bound to
// bridgeName, unless one is already running for it. See dhcp.go for why this
// exists instead of relying on kube-ovn's own OVN-side DHCP responder.
//
// Idempotent via a.dhcpServers, keyed by spec.NetnsPath — NOT by bridgeName.
// bridgeName is shortHashName(spec.IfaceName), which is deterministic from
// the VMI's *network name* alone (the sidecar recomputes the exact same
// hash every time from that name — that's the whole point of the naming
// contract), so it is IDENTICAL across every separate pod instance that
// ever attaches that same VMI network, e.g. every reboot/recreate of a
// VMI. Confirmed live: recreating a VLAN-subnet VMI whose earlier
// incarnation's DHCP server was still marked "running" in this map from
// before found the map key already present, skipped starting a fresh
// listener for the NEW pod's actual (different) netns/bridge, and left
// nothing bound to :67 in it at all — the guest never got a lease.
// spec.NetnsPath is a fresh, genuinely unique path per pod sandbox
// instance, so keying on it means a stale/leaked entry from an
// abnormally-torn-down previous pod (Detach not reliably called: a crash,
// force-delete, or driver restart between Attach and Detach) can never
// block a new pod's own server from starting, regardless of whether that
// network name was ever used before.
func (a *ovsAttacher) ensureVMIDHCPServer(ctx context.Context, spec Spec, bridgeName string) error {
	a.dhcpMu.Lock()
	_, running := a.dhcpServers[spec.NetnsPath]
	a.dhcpMu.Unlock()
	if running {
		return nil
	}

	ip, ipNet, err := net.ParseCIDR(spec.IP)
	if err != nil {
		return fmt.Errorf("parse Spec.IP %q for DHCP server: %w", spec.IP, err)
	}
	if ip.To4() == nil {
		// The guest learns its address by DHCPv4 only; IPv6 needs RA/DHCPv6.
		klog.FromContext(ctx).Info("No DHCPv4 server for a VMI NIC with an IPv6 address", "iface", spec.IfaceName, "ip", spec.IP)
		return nil
	}
	mac, err := net.ParseMAC(spec.MAC)
	if err != nil {
		return fmt.Errorf("parse Spec.MAC %q for DHCP server: %w", spec.MAC, err)
	}
	opts, err := vmiDHCPOptions(spec, ipNet.Mask)
	if err != nil {
		return err
	}

	listener, err := newNetnsUDP4FilterListener(ctx, spec.NetnsPath, bridgeName, ":67")
	if err != nil {
		return fmt.Errorf("start DHCP listener on %s: %w", bridgeName, err)
	}

	handler := &singleClientDHCPHandler{
		serverIP:  fakeDHCPServerIP,
		clientIP:  ip.To4(),
		clientMAC: mac,
		options:   opts,
	}

	a.dhcpMu.Lock()
	a.dhcpServers[spec.NetnsPath] = listener
	a.dhcpMu.Unlock()

	logger := klog.FromContext(ctx)
	go func() {
		// dhcp.Serve returns once stopVMIDHCPServer closes the listener
		// during Detach — that is the expected, silent shutdown path, not
		// a real error, but log it in case it's actually a fault.
		err := dhcp.Serve(listener, handler)
		logger.V(4).Info("DHCP server stopped", "bridge", bridgeName, "err", err)
	}()
	return nil
}

// stopVMIDHCPServer closes the listener started by ensureVMIDHCPServer, if
// any, which makes its dhcp.Serve goroutine return. Idempotent: nothing
// running is not an error. Keyed by netnsPath — see ensureVMIDHCPServer.
func (a *ovsAttacher) stopVMIDHCPServer(netnsPath string) error {
	a.dhcpMu.Lock()
	listener, running := a.dhcpServers[netnsPath]
	delete(a.dhcpServers, netnsPath)
	a.dhcpMu.Unlock()
	if !running {
		return nil
	}
	return listener.Close()
}

// newNetnsUDP4FilterListener creates an interface-filtered UDP4 listener (see
// github.com/krolaw/dhcp4/conn) inside netnsPath. Like addTuntapInNetns
// below, net.ListenPacket and net.InterfaceByName are scoped to the CALLING
// THREAD's current netns, not to any netlink handle, so this needs the same
// LockOSThread + netns.Set dance.
//
// Binds with SO_REUSEADDR — not optional here. KubeVirt's own virt-launcher
// process starts an identical filtered listener on ":67" in this SAME pod
// netns for the PRIMARY (bridge-bound) interface, and its own
// pkg/network/dhcp/server explicitly sets SO_REUSEADDR for exactly this
// reason (multiple such listeners coexisting in one netns, one per bridge).
// Without it here too, virt-launcher's own bind failed outright with
// "address already in use" and crashed the whole VMI — confirmed live.
func newNetnsUDP4FilterListener(ctx context.Context, netnsPath, ifaceName, laddr string) (dhcpServeCloser, error) {
	ns, err := netns.GetFromPath(netnsPath)
	if err != nil {
		return nil, fmt.Errorf("open netns %s: %w", netnsPath, err)
	}
	defer ns.Close()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNs, err := netns.Get()
	if err != nil {
		return nil, fmt.Errorf("get current netns: %w", err)
	}
	defer func() {
		_ = netns.Set(origNs)
		origNs.Close()
	}()

	if err := netns.Set(ns); err != nil {
		return nil, fmt.Errorf("switch to target netns: %w", err)
	}

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("look up %s: %w", ifaceName, err)
	}
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				opErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	l, err := lc.ListenPacket(ctx, "udp4", laddr)
	if err != nil {
		return nil, err
	}
	p := ipv4.NewPacketConn(l)
	if err := p.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		_ = l.Close()
		return nil, err
	}
	return dhcpconn.NewServeIf(iface.Index, p), nil
}

// addTuntapInNetns creates a tuntap device inside ns. This CANNOT go through a
// netns-scoped netlink.Handle the way every other link operation in this file
// does: vishvananda/netlink's Tuntap LinkAdd doesn't use rtnetlink at all — it
// opens /dev/net/tun and does a TUNSETIFF ioctl, which the kernel scopes to
// the CALLING THREAD's current network namespace, not to any netlink socket.
// A Handle from NewHandleAt(ns) only namespaces the rtnetlink socket, so
// calling h.LinkAdd on a Tuntap silently creates the device in the driver
// process's own (host) netns instead of ns — confirmed live: the tap "existed"
// but a lookup via the ns-scoped Handle immediately after returned "Link not
// found". The fix is to actually switch this goroutine's OS thread into ns
// for the duration of the ioctl.
func addTuntapInNetns(ns netns.NsHandle, tap *netlink.Tuntap) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNs, err := netns.Get()
	if err != nil {
		return fmt.Errorf("get current netns: %w", err)
	}
	defer func() {
		_ = netns.Set(origNs)
		origNs.Close()
	}()

	if err := netns.Set(ns); err != nil {
		return fmt.Errorf("switch to target netns: %w", err)
	}

	if err := netlink.LinkAdd(tap); err != nil && !isExists(err) {
		return err
	}
	return nil
}

// bridgeFdbReplaceStatic runs `bridge fdb replace <mac> dev <ifaceName>
// master static` inside ns. Like addTuntapInNetns above, this needs the
// LockOSThread + netns.Set dance: exec.Command forks from the CALLING OS
// THREAD's current netns (via clone()), not from any netlink handle's
// namespace, so without switching first the child would run — and therefore
// operate on the wrong bridge/tap — in the driver's own host netns.
func bridgeFdbReplaceStatic(ctx context.Context, ns netns.NsHandle, mac net.HardwareAddr, ifaceName string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNs, err := netns.Get()
	if err != nil {
		return fmt.Errorf("get current netns: %w", err)
	}
	defer func() {
		_ = netns.Set(origNs)
		origNs.Close()
	}()

	if err := netns.Set(ns); err != nil {
		return fmt.Errorf("switch to target netns: %w", err)
	}

	out, err := exec.CommandContext(ctx, "bridge", "fdb", "replace", mac.String(), "dev", ifaceName, "master", "static").CombinedOutput()
	if err != nil {
		return fmt.Errorf("bridge fdb replace: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// unwireVMIBridge removes the tap + bridge wireVMIBridge added. The
// underlying veth pair itself is left for Detach's existing cleanupHostVeth
// (keyed by the host-side name, unaffected by the pod-side rename) to remove.
// Idempotent: a missing netns or missing devices are not errors.
func (a *ovsAttacher) unwireVMIBridge(ctx context.Context, spec Spec) error {
	bridgeName := bridgeNameFor(spec.IfaceName)
	// Process-local state (see ovsAttacher.dhcpServers): stop it regardless of
	// whether the netns below still resolves, or the goroutine leaks.
	if err := a.stopVMIDHCPServer(spec.NetnsPath); err != nil {
		return fmt.Errorf("stop DHCP server for netns %s: %w", spec.NetnsPath, err)
	}

	ns, err := netns.GetFromPath(spec.NetnsPath)
	if err != nil {
		return nil // netns already gone; nothing left to clean up inside it
	}
	defer ns.Close()

	h, err := netlink.NewHandleAt(ns)
	if err != nil {
		return fmt.Errorf("netlink handle for netns %s: %w", spec.NetnsPath, err)
	}
	defer h.Close()

	if tapLink, err := h.LinkByName(spec.IfaceName); err == nil && tapLink.Type() == "tuntap" {
		if err := h.LinkDel(tapLink); err != nil {
			return fmt.Errorf("delete tap %s: %w", spec.IfaceName, err)
		}
	}

	if bridgeLink, err := h.LinkByName(bridgeName); err == nil {
		if err := h.LinkDel(bridgeLink); err != nil {
			return fmt.Errorf("delete bridge %s: %w", bridgeName, err)
		}
	}

	return nil
}

// ovsVsctl runs `ovs-vsctl <args>` on the host OVS database.
func ovsVsctl(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "ovs-vsctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ovs-vsctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ipsWithoutMask strips the CIDR suffix of every non-empty address.
func ipsWithoutMask(cidrs []string) []string {
	ips := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		if cidr != "" {
			ips = append(ips, ipWithoutMask(cidr))
		}
	}
	return ips
}

// ipWithoutMask strips the CIDR suffix, e.g. "172.23.0.5/24" -> "172.23.0.5".
func ipWithoutMask(cidr string) string {
	if i := strings.IndexByte(cidr, '/'); i >= 0 {
		return cidr[:i]
	}
	return cidr
}

// isExists reports whether err indicates the object already exists (EEXIST),
// so repeated AddrAdd/RouteAdd on retry are not treated as failures.
func isExists(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "file exists")
}
