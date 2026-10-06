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
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func TestIPsWithoutMask(t *testing.T) {
	got := strings.Join(ipsWithoutMask([]string{"10.10.0.5/24", "", "fd00::5/120"}), ",")
	if got != "10.10.0.5,fd00::5" {
		t.Errorf("ipsWithoutMask() joined = %q, want %q", got, "10.10.0.5,fd00::5")
	}
}

// newTestNetns locks the test goroutine to its OS thread, moves the thread into
// a fresh network namespace that plays the host, and creates a named namespace
// that plays the pod sandbox. It returns the pod netns path. The datapath tests
// need root and skip otherwise.
func newTestNetns(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root to create network namespaces")
	}
	runtime.LockOSThread()
	orig, err := netns.Get()
	if err != nil {
		t.Fatalf("get current netns: %v", err)
	}
	name := fmt.Sprintf("dra-%s-%d", strings.ToLower(t.Name()), os.Getpid())
	podNS, err := netns.NewNamed(name)
	if err != nil {
		t.Fatalf("create pod netns: %v", err)
	}
	hostNS, err := netns.New()
	if err != nil {
		t.Fatalf("create host netns: %v", err)
	}
	t.Cleanup(func() {
		_ = netns.Set(orig)
		_ = orig.Close()
		_ = hostNS.Close()
		_ = podNS.Close()
		_ = netns.DeleteNamed(name)
		runtime.UnlockOSThread()
	})
	return "/var/run/netns/" + name
}

func podHandle(t *testing.T, path string) *netlink.Handle {
	t.Helper()
	ns, err := netns.GetFromPath(path)
	if err != nil {
		t.Fatalf("open netns %s: %v", path, err)
	}
	defer ns.Close()
	h, err := netlink.NewHandleAt(ns)
	if err != nil {
		t.Fatalf("netlink handle for %s: %v", path, err)
	}
	t.Cleanup(h.Close)
	return h
}

// plumbPodIface runs the netns part of Attach (everything except OVS) twice, to
// cover idempotency, and returns the host veth name.
func plumbPodIface(t *testing.T, a *ovsAttacher, spec Spec) string {
	t.Helper()
	ctx := context.Background()
	host, pod, err := a.vethNames(spec)
	if err != nil {
		t.Fatalf("vethNames() error = %v", err)
	}
	for range 2 {
		if err := a.createVethPair(ctx, host, pod, spec.MTU); err != nil {
			t.Fatalf("createVethPair() error = %v", err)
		}
		if err := a.moveIntoNetns(ctx, pod, spec.NetnsPath, spec.IfaceName); err != nil {
			t.Fatalf("moveIntoNetns() error = %v", err)
		}
		if err := a.configurePodIface(ctx, spec); err != nil {
			t.Fatalf("configurePodIface() error = %v", err)
		}
	}
	return host
}

func TestDatapathPodNIC(t *testing.T) {
	spec := Spec{
		NetnsPath:   newTestNetns(t),
		IfaceName:   "net1",
		IP:          "10.10.0.5/24",
		ExtraIPs:    []string{"fd00::5/120"},
		MAC:         "00:00:00:aa:bb:cc",
		Gateway:     "10.10.0.1",
		Routes:      []string{"192.168.0.0/16"},
		MTU:         1400,
		ContainerID: "0123456789abcdef",
		IfaceID:     "pod1.default.blue.default.ovn",
	}
	a := &ovsAttacher{dhcpServers: make(map[string]dhcpServeCloser)}
	host := plumbPodIface(t, a, spec)

	h := podHandle(t, spec.NetnsPath)
	link, err := h.LinkByName("net1")
	if err != nil {
		t.Fatalf("net1 not in the pod netns: %v", err)
	}
	if got := link.Attrs().HardwareAddr.String(); got != spec.MAC {
		t.Errorf("MAC = %s, want %s", got, spec.MAC)
	}
	if got := link.Attrs().MTU; got != spec.MTU {
		t.Errorf("MTU = %d, want %d", got, spec.MTU)
	}
	if link.Attrs().Flags&net.FlagUp == 0 {
		t.Error("net1 is not up")
	}
	addrs, err := h.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		t.Fatalf("list addresses: %v", err)
	}
	var got []string
	for _, addr := range addrs {
		if addr.IP.IsGlobalUnicast() {
			got = append(got, addr.IPNet.String())
		}
	}
	if strings.Join(got, ",") != "10.10.0.5/24,fd00::5/120" {
		t.Errorf("addresses = %v, want 10.10.0.5/24 and fd00::5/120", got)
	}
	routes, err := h.RouteList(link, netlink.FAMILY_V4)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	var viaGateway, defaultRoute bool
	for _, r := range routes {
		if r.Dst == nil {
			defaultRoute = true
		} else if r.Dst.String() == "192.168.0.0/16" && r.Gw.Equal(net.ParseIP(spec.Gateway)) {
			viaGateway = true
		}
	}
	if !viaGateway || defaultRoute {
		t.Errorf("routes = %v, want 192.168.0.0/16 via %s and no default route", routes, spec.Gateway)
	}

	if err := a.cleanupHostVeth(context.Background(), host); err != nil {
		t.Fatalf("cleanupHostVeth() error = %v", err)
	}
	if _, err := h.LinkByName("net1"); err == nil {
		t.Error("net1 still exists after the host veth was deleted")
	}
	if err := a.cleanupHostVeth(context.Background(), host); err != nil {
		t.Errorf("second cleanupHostVeth() error = %v", err)
	}
}

func TestDatapathVMINIC(t *testing.T) {
	spec := Spec{
		NetnsPath:   newTestNetns(t),
		IfaceName:   "net1",
		IP:          "10.10.0.5/24",
		ExtraIPs:    []string{"fd00::5/120"},
		MAC:         "00:00:00:aa:bb:cc",
		Gateway:     "10.10.0.1",
		ContainerID: "0123456789abcdef",
		IfaceID:     "vm1.default.blue.default.ovn",
		KubeVirtVMI: true,
	}
	a := &ovsAttacher{dhcpServers: make(map[string]dhcpServeCloser)}
	host := plumbPodIface(t, a, spec)
	ctx := context.Background()
	for range 2 {
		if err := a.wireVMIBridge(ctx, spec); err != nil {
			t.Fatalf("wireVMIBridge() error = %v", err)
		}
	}
	// A repeated attach of the wired NIC, e.g. from NRI Synchronize after a
	// plugin restart, must leave the tap alone.
	h := podHandle(t, spec.NetnsPath)
	tapBefore, err := h.LinkByName("net1")
	if err != nil {
		t.Fatalf("tap missing: %v", err)
	}
	plumbPodIface(t, a, spec)
	if err := a.wireVMIBridge(ctx, spec); err != nil {
		t.Fatalf("wireVMIBridge() after a repeated attach error = %v", err)
	}
	tapAfter, err := h.LinkByName("net1")
	if err != nil {
		t.Fatalf("tap missing after a repeated attach: %v", err)
	}
	if tapAfter.Attrs().HardwareAddr.String() != tapBefore.Attrs().HardwareAddr.String() {
		t.Errorf("repeated attach changed the tap MAC from %s to %s", tapBefore.Attrs().HardwareAddr, tapAfter.Attrs().HardwareAddr)
	}
	if addrs, _ := h.AddrList(tapAfter, netlink.FAMILY_ALL); len(addrs) != 0 {
		for _, addr := range addrs {
			if addr.IP.IsGlobalUnicast() {
				t.Errorf("repeated attach configured %s on the tap", addr.IPNet)
			}
		}
	}

	tap, err := h.LinkByName("net1")
	if err != nil || tap.Type() != "tuntap" {
		t.Fatalf("net1 = %v (%v), want the tap device", tap, err)
	}
	bridge, err := h.LinkByName(bridgeNameFor("net1"))
	if err != nil {
		t.Fatalf("bridge missing: %v", err)
	}
	veth, err := h.LinkByName(vethRenameFor("net1"))
	if err != nil {
		t.Fatalf("renamed veth missing: %v", err)
	}
	for _, port := range []netlink.Link{tap, veth} {
		if port.Attrs().MasterIndex != bridge.Attrs().Index {
			t.Errorf("%s is not enslaved to the bridge", port.Attrs().Name)
		}
	}
	addrs, err := h.AddrList(veth, netlink.FAMILY_V6)
	if err != nil {
		t.Fatalf("list addresses: %v", err)
	}
	for _, addr := range addrs {
		if addr.IP.IsGlobalUnicast() {
			t.Errorf("VMI NIC got the extra address %s, the guest only gets its IPv4 address by DHCP", addr.IPNet)
		}
	}
	a.dhcpMu.Lock()
	_, serving := a.dhcpServers[spec.NetnsPath]
	a.dhcpMu.Unlock()
	if !serving {
		t.Error("no DHCP server started for the VMI NIC")
	}

	for range 2 {
		if err := a.unwireVMIBridge(ctx, spec); err != nil {
			t.Fatalf("unwireVMIBridge() error = %v", err)
		}
	}
	if _, err := h.LinkByName("net1"); err == nil {
		t.Error("tap still exists after unwireVMIBridge")
	}
	if _, err := h.LinkByName(bridgeNameFor("net1")); err == nil {
		t.Error("bridge still exists after unwireVMIBridge")
	}
	a.dhcpMu.Lock()
	_, serving = a.dhcpServers[spec.NetnsPath]
	a.dhcpMu.Unlock()
	if serving {
		t.Error("DHCP server still registered after unwireVMIBridge")
	}
	if err := a.cleanupHostVeth(ctx, host); err != nil {
		t.Fatalf("cleanupHostVeth() error = %v", err)
	}
}
