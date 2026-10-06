# Architecture: kube-ovn NIC DRA Driver

This document describes the design of a Dynamic Resource Allocation (DRA) driver
that bridges Kubernetes DRA (KEP-3063) with the kube-ovn CNI to provide
scheduler-managed secondary network interfaces for pods.

It is intended as a reference implementation for the intersection of:
- [kubernetes-sigs/multi-network-api](https://github.com/kubernetes-sigs/multi-network-api)
- [KEP-4815 DRA Partitionable Devices](https://github.com/kubernetes/enhancements/issues/4815)
- [kube-ovn](https://github.com/kubeovn/kube-ovn) subnet-backed IPAM

---

## Problem Statement

Kubernetes today has no first-class API for attaching a pod to a secondary network
with scheduler-visible resource allocation. The existing solutions (Multus + NAD,
kube-ovn annotations) work but share a common gap:

- **No scheduler visibility**: secondary NIC assignment happens in the CNI plugin,
  after the pod is already scheduled. The scheduler cannot consider NIC availability.
- **No hotplug**: secondary NICs are wired at pod start, not dynamically after.
- **No declarative request**: the network is requested through an annotation
  and a NetworkAttachmentDefinition instead of a typed, schedulable resource.

DRA solves the scheduler visibility and request problems. This driver shows how
to back DRA ResourceSlices with real kube-ovn subnets, let kube-ovn allocate
from the claims, and plumb the interfaces using NRI.

---

## Key Design Constraint: Two-Phase Plumbing

The central constraint driving the architecture is:

| Phase | Trigger | Constraints |
|---|---|---|
| **Phase 1** — resolve the allocation | `NodePrepareResources` (kubelet → driver gRPC) | Slow OK, but the pod netns does **not exist yet** |
| **Phase 2** — plumbing | NRI `RunPodSandbox` hook | Pod netns **exists**, but the NRI request timeout is short (2 s by default) |

The veth cannot be created in `NodePrepareResources`, because the pod network
namespace does not exist yet. Waiting for kube-ovn-controller in
`RunPodSandbox` would risk the NRI deadline. So phase 1 waits for the
allocation and phase 2 only does local netlink and OVS work.

Phase 1 does not allocate anything itself: kube-ovn-controller, started with
`--enable-dra-nic`, allocates the address and creates the logical switch port
as soon as the scheduler has bound the pod, the same way it does for a Multus
attachment. The driver waits for the result.

---

## Architecture Overview

```
┌────────────────────────────────────────────────────────────────────────┐
│                       Kubernetes control plane                         │
│                                                                        │
│  kube-scheduler: reads ResourceSlices, allocates devices to claims,    │
│                  binds the pod                                         │
│                                                                        │
│  kube-ovn-controller (--enable-dra-nic): watches claims and slices,    │
│    device → subnet → provider, IPAM, logical switch port, ip CR,       │
│    pod annotations <provider key>.kubernetes.io/*                      │
└────────────────────────────────────────────────────────────────────────┘
                                   │ pod annotations
┌──────────────────────────────────▼─────────────────────────────────────┐
│                           Node                                         │
│                                                                        │
│  kubelet ── NodePrepareResources ──► driver.go ──► state.go            │
│                                       nicprepare.RequestIPAM()         │
│                                         wait for allocated=true        │
│                                       PendingStore.Add(podUID, Spec)   │
│                                                                        │
│  containerd ── NRI RunPodSandbox ──► nri.go ──► SandboxHandler         │
│                                       PendingStore.Take(podUID)        │
│                                       Attacher.Attach() per NIC:       │
│                                         veth + netns config +          │
│                                         br-int port (iface-id = LSP)   │
│                                         (+ bridge/tap/DHCP for VMs)    │
│                                                                        │
│  containerd ── NRI StopPodSandbox ─► SandboxHandler                    │
│                                       Attacher.Detach() per NIC +      │
│                                       DetachPodPorts() sweep           │
│                                                                        │
│  ovn-controller: binds the port on iface-id, installs the flows        │
└────────────────────────────────────────────────────────────────────────┘
```

---

## Component Map

### `cmd/kube-ovn-dra-kubeletplugin/driver.go`

Implements the kubelet plugin interface of
`k8s.io/dynamic-resource-allocation/kubeletplugin`: publishes the
ResourceSlices and forwards `PrepareResourceClaims` /
`UnprepareResourceClaims` to the device state. It also starts the NRI plugin.

### `cmd/kube-ovn-dra-kubeletplugin/state.go`

- **`NewDeviceState`** — takes the devices the NIC profile enumerated, the CDI
  handler and the checkpoint manager.
- **`Prepare`** — decodes the opaque `NicConfig`s (claim configs take
  precedence over class configs), resolves every NIC of the claim
  concurrently with `nicprepare.RequestIPAM`, adds one `plumbing.Spec` per NIC
  to the `PendingStore` and writes the CDI spec and checkpoint. A repeated
  call for a checkpointed claim returns the stored devices.
- **`Unprepare`** — removes the CDI spec and the checkpoint entry. Nothing is
  released: kube-ovn-controller owns the address and port.

### `cmd/kube-ovn-dra-kubeletplugin/nri.go`

The NRI plugin (`dra-nic`, index `90`, so it runs after the primary CNI has set
up `eth0`). It extracts the network namespace path from the sandbox and calls
the `plumbing.SandboxHandler`. An attach failure fails the sandbox, so kubelet
retries instead of running a pod with missing NICs.

### `internal/profiles/nic/`

Lists kube-ovn `Subnet`s (and their `Vlan`s) at startup and publishes each as
a device; see the mapping below. `ApplyConfig` adds the CDI environment
variables `KUBE_OVN_NIC_IFACE_<device>` and `KUBE_OVN_NIC_SUBNET_<device>`.

### `pkg/nicprepare/` and `pkg/annotation/`

```
RequestIPAM(ctx, client, claim, result, device, ifaceName) → NicDeviceConfig
  1. Resolve the pod from claim.Status.ReservedFor
  2. Read subnetName and provider from the device; reject the default provider
  3. Wait (up to 30 s) for <provider>.<ifaceName>.kubernetes.io/allocated or
     <provider>.kubernetes.io/allocated = "true" and read ip_address,
     mac_address, cidr, gateway and virtualmachine
  4. Port name = <pod or VM>.<namespace>.<provider key>; addresses in CIDR
     notation, IPv4 first
```

### `pkg/plumbing/`

- **`SandboxHandler`** — drains the `PendingStore` on `RunPodSandbox`, rolls
  back on partial failure, records attached NICs and detaches them on
  `StopPodSandbox`, followed by a sweep of all OVS ports owned by the pod.
- **`ovsAttacher`** (`plumbing_linux.go`) — veth pair, move and rename into the
  netns, MAC/MTU/addresses/routes, `ovs-vsctl add-port br-int` with
  `external_ids`, and for KubeVirt pods the bridge, tap and DHCP server
  (`dhcp.go`). `plumbing_nolinux.go` keeps the package cross-compilable.

---

## ResourceSlice → kube-ovn Subnet Mapping

Each kube-ovn `Subnet` CRD becomes a `Device` in the driver's `ResourceSlice`:

```yaml
# kube-ovn Subnet CRD
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: ovn-subnet
spec:
  cidrBlock: 10.200.0.0/24
  protocol: IPv4
  vpc: ovn-cluster

  provider: ovn-subnet.default.ovn

# Becomes this Device in ResourceSlice
name: subnet-ovn-subnet
allowMultipleAllocations: true
attributes:
  nic.kubeovn.io/subnetName:  "ovn-subnet"
  nic.kubeovn.io/subnetType:  "ovn"
  nic.kubeovn.io/provider:    "ovn-subnet.default.ovn"
  nic.kubeovn.io/vpc:         "ovn-cluster"
```

VLAN subnets also get `subnetType: "vlan"`, `vlanId` and `providerNetwork`
from their `Vlan`. kube-ovn-controller only needs `subnetName`.

Users select devices via CEL in their `ResourceClaim`:

```yaml
requests:
  - name: nic0
    exactly:
      deviceClassName: nic.kubeovn.io
      selectors:
        - cel:
            expression: >
              device.attributes['nic.kubeovn.io'].subnetName == 'ovn-subnet'
```

---

## IPAM Model: Flat, CNI-Owned

The driver does **not** split subnets per node. The kube-ovn OVN logical switch
spans all nodes — any pod on any node can receive any IP from the subnet's CIDR.
The scheduler selects a node; kube-ovn-controller then allocates an IP from the
flat pool once the pod is bound.

This is the **flat multi-network model**: one `/24` across all nodes, no sub-CIDR
allocation per node. The OVN fabric handles inter-node forwarding transparently.

This contrasts with the default Kubernetes pod CIDR model where each node owns a
sub-CIDR (e.g. `/27` per node from a `/16` cluster CIDR).

```
Flat model (this driver):
  Node A: pod gets 10.200.0.5  ─┐
  Node A: pod gets 10.200.0.6  ─┤── all from 10.200.0.0/24
  Node B: pod gets 10.200.0.7  ─┘   kube-ovn OVN handles routing

Sub-CIDR model (default NodeIpam):
  Node A: owns 10.244.0.0/27  → pods .1-.30
  Node B: owns 10.244.0.32/27 → pods .33-.62
```

---

## NRI Integration

NRI (Node Resource Interface) is the containerd plugin API used to intercept
container lifecycle events. The driver registers as an NRI plugin that handles:

- **`RunPodSandbox`** — the sandbox network namespace exists and its path is
  in the event. Phase 2 plumbing.
- **`StopPodSandbox`** — the sandbox stops. Triggers the detach.

NRI is enabled by default in containerd 2.x. The plugin registers as
`dra-nic` with index `90`. The NRI socket directory `/var/run/nri` and the
host netns directory are mounted into the plugin pod; the netns mount uses
`HostToContainer` propagation, because the runtime creates the netns files
after the plugin has started.

---

## What This Driver Does NOT Do

- **No IPAM of its own**: kube-ovn-controller allocates and releases.
- **No IP pool accounting in the scheduler**: subnet devices allow multiple
  allocations without a capacity, so the scheduler does not see pool
  exhaustion. Consumable capacity could change that (see below).
- **No NetworkPolicy or Services on secondary interfaces**: out of scope,
  tracked by `multi-network-api`.
- **No hot-plug**: NICs are attached when the sandbox starts.

---

## Relation to Upstream Proposals

### kubernetes-sigs/multi-network-api

`multi-network-api` defines the `Network` CRD (identity layer) and explicitly
defers interface attachment to DRA:

> *"We do not list basic use cases that just add network interfaces to a pod,
> since those are currently handled by Dynamic Resource Allocation."*

This driver implements exactly that DRA-based attachment layer. A future
integration would:
1. Watch `Network` objects
2. Map each `Network` to a kube-ovn `Subnet`
3. Publish the subnet as a `ResourceSlice` device
4. Accept `ResourceClaim` selectors referencing the `Network` name

### Consumable capacity / partitionable devices

Today any number of claims can take the same subnet device. With consumable
capacity (or KEP-4815 partitionable devices), the IP pool itself would become a
finite device capacity:

```
ResourceSlice device: subnet-ovn-subnet
  pool capacity: 254 IPs
  partition shape: 1 IP per claim
```

The scheduler would track pool exhaustion and refuse to schedule pods when the
subnet is full — something the current driver cannot express.

---

## Demo Setup

The full demo uses:

- **kind** (v1.35+) — local multi-node cluster, no default CNI
- **Containerlab** — injects `eth1` into kind node containers to simulate
  a VLAN-capable physical NIC for kube-ovn ProviderNetwork, plus an FRR gateway
- **kube-ovn** with `--enable-dra-nic` — installed via Helm from the
  `KUBE_OVN_VERSION` branch, provides OVN overlay + VLAN subnets
- **This driver** — DRA kubelet plugin + NRI plugin
- **Multus** (optional) — only for the DRA vs Multus benchmark

```bash
make kind-demo          # full end-to-end setup
make kind-delete        # teardown
```

See [`nic-driver.md`](nic-driver.md),
[`demo/kind/kind-no-cni.yaml`](../demo/kind/kind-no-cni.yaml) and
[`demo/containerlab/vlan-topology.yaml`](../demo/containerlab/vlan-topology.yaml).
