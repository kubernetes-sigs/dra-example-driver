# DRA + CNI Integration: Bridging multi-network-api with CNI-backed secondary interfaces

**Status**: Exploratory idea — NOT a committed direction  
**Author**: soer3n  
**Relates to**: [multi-network-api requirements.md](https://github.com/kubernetes-sigs/multi-network-api)

> ⚠️ **State as of 2026-10-06:** `kubernetes-sigs/multi-network-api` has a
> merged requirements document ([#3](https://github.com/kubernetes-sigs/multi-network-api/pull/3),
> January 2026) and no API types yet. Two competing API proposals are open:
>
> - **`PodNetwork`** ([#5](https://github.com/kubernetes-sigs/multi-network-api/pull/5),
>   last updated July 2026): cluster-scoped immutable CRD, `spec.provider` +
>   `spec.networkRef` to the implementation's own network CR; the driver
>   creates one per network.
> - **`PodNetworkKind`** ([#4](https://github.com/kubernetes-sigs/multi-network-api/pull/4),
>   formerly NetworkKind / NetworkClass, actively revised): cluster-scoped CRD
>   whose `spec.implementationType` (a `GroupKind`) *classifies* existing
>   implementation CRs as pod networks, with an optional cluster default
>   (`DefaultKind`).
>
> Both converge on the same DRA contract:
>
> - the driver publishes attach devices with the standard device attributes
>   `multinetwork.networking.k8s.io/podNetwork` (plus `podNetworkNamespace` for
>   namespaced networks, and `podNetworkKind` in #4), so claims select a
>   network with CEL on those attributes;
> - the driver reports the attachment in the ResourceClaim device status:
>   `status.devices[].networkData` (`interfaceName`, IPs, MAC) and a
>   `podNetwork` reference in `status.devices[].data`.
>
> This document predates both and matches neither; it is kept as an idea to
> revisit once upstream converges. The driver does not depend on
> any of this: kube-ovn-controller allocates from the claims and
> `pkg/plumbing` attaches the NICs (see [`nic-driver.md`](nic-driver.md)).

---

## Summary

`multi-network-api` intentionally defers secondary interface attachment to Dynamic
Resource Allocation (DRA), stating:

> *"We do not list basic use cases that just add network interfaces to a pod,
> since those are currently handled by Dynamic Resource Allocation."*

However, there is no reference design showing *how* a CNI-backed DRA driver
bridges the gap between a `ResourceClaim` and an actual network interface inside
a pod. This document describes that pattern using kube-ovn as the reference
implementation, identifies API gaps, and proposes the glue needed to make
multi-network-api and DRA work together end-to-end.

---

## The Gap

Today a user wanting pod multi-homing with scheduler awareness must:

1. Deploy a CNI plugin (kube-ovn, Calico, etc.) that manages secondary networks
2. Deploy Multus to multiplex CNI calls
3. Write `NetworkAttachmentDefinition` objects (Multus-specific, not standardized)
4. Annotate pods with `k8s.v1.cni.cncf.io/networks` (Multus-specific)

None of this is visible to the scheduler. The scheduler cannot:
- Refuse to schedule a pod when a subnet's IP pool is exhausted
- Prefer nodes with shorter latency to a specific underlay network
- Track secondary NIC allocation as a first-class resource

DRA solves the scheduler-visibility problem. But DRA alone doesn't know how to
wire a NIC into a pod — it needs a driver that bridges the scheduling decision
to the actual kernel/OVS plumbing.

---

## Target architecture: multi-network-api + DRA (this driver = the DRA backend)

The right end-state is **not** "DRA replaces the network API" — it is the two
layers working together, which is exactly what multi-network-api is designed for
(it explicitly defers interface attachment to DRA; see the Summary quote). Split
by responsibility:

| Layer | Owns | In this stack |
|-------|------|---------------|
| **Network API** (multi-network-api) | network *identity* — "this pod is on network X", typed + admission-validated; (roadmap) policy/service association | a `Network`/`PodNetwork` object replacing the free-form Multus `networks` annotation / NAD |
| **DRA** (this driver) | the *resource + attach* — IPAM as **consumable-capacity** from the subnet pool, **underlay node placement**, the veth/OVS/LSP attach, allocate/release lifecycle | `internal/profiles/nic` + `pkg/plumbing`, IPAM by kube-ovn-controller |

So this driver's coherent long-term identity is **the kube-ovn DRA backend in a
multi-network-api + DRA stack**: the network API says *which* network, the driver
*allocates and attaches* it. This also resolves "is SDN in scope for DRA?" —
network *definition* is the network API's job; the *resource + attach* is DRA's;
neither overreaches.

The seam between the two layers is precisely the three gaps below:

- **Network → DeviceClass binding** (gap 1) — how "I want network X" resolves to
  the right DRA `DeviceClass` + selector.
- **Interface-name ownership** (gap 2).
- **IP-pool exhaustion as a scheduling signal** (gap 3) — now implemented via DRA
  consumable-capacity (`AllowMultipleAllocations` today; capacity-counting next).

**Caveat — this is a target, not a buildable plan yet.** multi-network-api is
still pre-API (requirements + two competing proposals, `PodNetwork` vs
`PodNetworkKind`, no merged types — see the note at the top). So today the
network-identity layer is stood in by NAD/Multus or the driver's own claim
selectors; the ideas here match neither proposal and are kept only to revisit
once one wins. The honest pitch is **"a DRA backend that complements
the network API," not "a Multus replacement that swallows it."**

---

## Reference Implementation: kube-ovn NIC DRA Driver

The driver at [github.com/soer3n/kube-ovn-dra-driver](https://github.com/soer3n/kube-ovn-dra-driver)
(NIC profile) implements this bridge for kube-ovn.

### How it works

**ResourceSlice Publication** (startup):

Each kube-ovn `Subnet` CRD is published as a `Device` in the node's `ResourceSlice`:

```yaml
device:
  name: subnet-ovn-net
  attributes:
    nic.kubeovn.io/subnetName: "ovn-net"
    nic.kubeovn.io/subnetType: "ovn"
    nic.kubeovn.io/cidr:       "10.200.0.0/24"
```

**User API** (`ResourceClaim`):

```yaml
spec:
  devices:
    requests:
      - name: nic0
        exactly:
          deviceClassName: nic.kubeovn.io
          selectors:
            - cel:
                expression: >
                  device.attributes['nic.kubeovn.io'].subnetName.stringValue == 'ovn-net'
    config:
      - requests: ["nic0"]
        opaque:
          driver: nic.kubeovn.io
          parameters:
            kind: NicConfig
            interfaceName: net1
```

**Two-phase plumbing** (the key insight):

```
Phase 1 — PrepareResourceClaims (slow, before pod starts):
  Wait for kube-ovn-controller, which allocates IP/MAC/GW and the logical
  switch port from the claim, to annotate the pod
  Store a plumbing.Spec{IP, MAC, GW, iface-id, ...} keyed by pod UID

Phase 2 — NRI RunPodSandbox (fast, ~2s window, netns exists):
  Retrieve NicDeviceConfig for pod UID
  Create veth pair + OVS port + configure IP/MAC in pod netns
```

The split is necessary because:
- Phase 1 can be slow (waiting for kube-ovn-controller can take seconds)
- Phase 2 has a strict NRI timeout (~2s)
- The pod network namespace does not exist in Phase 1

---

## Flat IPAM Model

The driver uses a **flat IP pool** shared across all nodes — no sub-CIDR per node.
`10.200.0.0/24` is the full pool; any pod on any node can receive any IP.
kube-ovn's OVN logical switch handles inter-node forwarding transparently.

This is a natural fit for multi-network-api's Network abstraction:

```
multi-network-api Network "tenant-net"
  └── kube-ovn Subnet "ovn-net" (CIDR: 10.200.0.0/24)
        └── ResourceSlice Device "subnet-ovn-net"
              └── ResourceClaim → IP allocated → NIC plumbed
```

---

## What multi-network-api Needs From DRA

For the full integration stack to work cleanly, we identified the following
gaps in the current `multi-network-api` + DRA API surface.

### 0. What this driver would have to add for either proposal

- publish `multinetwork.networking.k8s.io/podNetwork` (and
  `podNetworkNamespace` / `podNetworkKind`) next to today's `nic.kubeovn.io/*`
  attributes, mapping a pod network to a kube-ovn subnet;
- report each NIC in the claim's `status.devices[].networkData` (interface
  name, IPs, MAC) and `data`. This is also what would let KubeVirt find a DRA
  NIC without a naming contract (see the binding plugin's `managedTap` notes).

Neither is implemented yet.

### 1. `Network` → `DeviceClass` binding

Today a user must know to select a `DeviceClass: nic.kubeovn.io` in their
`ResourceClaim`. There is no standard way to say "I want an interface on
Network X" and have the system derive the correct `DeviceClass` and device
selector automatically.

**Proposed**: `multi-network-api` should define how a `Network` object maps to
a `DeviceClass` or at minimum publish a standard label/annotation on the
`DeviceClass` so claim templates can reference it by `Network` name.

### 2. Interface name ownership

Today the interface name inside the pod (`net1`, `net2`) is specified in the
`ResourceClaim` config section as driver-opaque parameters. There is no
standard field.

**Proposed**: `multi-network-api` should define a standard field (or annotation)
for the desired interface name, analogous to how NADs work today with
`"interface": "net1"` in the Multus annotation. Both proposals standardize
*reporting* the name (`networkData.interfaceName` in the claim status), not
requesting it.

### 3. IP pool exhaustion signaling

With the flat IPAM model, the scheduler has no way to know when a subnet's IPs
are exhausted. This is the key missing piece for scheduler-aware multi-network.

**Proposed path**: DRA consumable capacity (`DRAConsumableCapacity`, the gate
this driver already needs for `AllowMultipleAllocations`) lets a device declare
a capacity that each allocation consumes. A subnet device would declare e.g.
254 addresses (for a /24), and the scheduler would refuse to schedule pods once
the pool is used up.

---

## Current State vs. Future State

| Capability | Today (this driver) | With multi-network-api + consumable capacity |
|---|---|---|
| Scheduler visibility of NIC allocation | ✅ via ResourceClaim | ✅ |
| Flat IP pool (no sub-CIDR per node) | ✅ | ✅ |
| IP exhaustion visible to scheduler | ❌ driver-side only | ✅ via device capacity |
| Standard network → claim binding | ❌ manual DeviceClass + subnet selector | 🔜 `podNetwork` device attribute (#4 / #5) |
| Attachment reported in the claim status | ❌ pod annotations only | 🔜 `networkData` (#4 / #5) |
| Standard interface name field | ❌ driver-opaque params | 🔜 reported in `networkData`; request field open |
| NetworkPolicy on secondary interface | ❌ | 🔜 multi-network-api roadmap |
| Service on secondary interface | ❌ | 🔜 multi-network-api roadmap + Gateway API |

---

## Running the Demo

The full demo runs on a local kind cluster with Containerlab providing a simulated
VLAN-capable `eth1` on each kind node:

```bash
git clone https://github.com/soer3n/kube-ovn-dra-driver
cd kube-ovn-dra-driver
make kind-demo
kubectl exec multi-nic-demo -- ip addr   # see net1 + net2
```

---

## Open Questions for multi-network-api

1. Should `Network` objects be namespace-scoped or cluster-scoped? For VLAN
   underlay networks that span the whole cluster, cluster-scoped makes more sense.
2. Should the `Network` CRD include CIDR information, or is that entirely the
   driver's responsibility?
3. How should `NetworkPolicy` reference secondary interface networks — by
   `Network` name, by interface name, or by IP range?
