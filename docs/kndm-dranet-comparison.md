# Kube-OVN DRA NIC driver vs. KNDM / DraNet — comparison & options

Status: design note / decision aid (2026-06)

This note compares the kube-ovn DRA NIC driver in this repo with the emerging
upstream DRA-networking ecosystem, and lays out the strategic options for the
effort.

## Purpose & scope of this exploration

This repo is an **exploration**, not a product bid. It has two aims:

1. **Investigate DRA in the SDN space**, anchored on a concrete problem: Multus
   attaches secondary NICs *serially* during pod sandbox setup, so startup latency
   grows ~linearly with NIC count. The question: does DRA do materially better for
   kube-ovn virtual NICs? (Finding: yes, structurally — kube-ovn allocates all
   NICs of a pod in one pass and the driver waits for them **concurrently**,
   rather than serially like Multus's delegate chain, plus
   scheduler-aware placement, IP-pool capacity, and clean lifecycle. The latency
   *magnitude* still needs measuring with `make nic-bench`.)
2. **Map the adjacent, still-emerging upstream work** and where an SDN DRA driver
   sits relative to it — **KNDM / DraNet**, **ovn-kubernetes OKEP-6391 /
   dra-driver-sriov** (hardware/SR-IOV), **KubeVirt VEP-183** (DRA as a VM
   `NetworkSource`), and **SIG-Network multi-network-api** (the network-definition
   layer this driver would back; see
   [`multi-network-api-integration.md`](multi-network-api-integration.md)).

The intent is to produce a working reference + an honest assessment that can feed
the upstream conversation (SIG-Network / multi-networking, KubeVirt SIG-Network),
**not** to claim DRA should replace Multus or the network API. The strongest,
most defensible findings to carry into that conversation: the SDN-vs-hardware
backend distinction (the SDN-synthesis category is currently uncovered by the
reference drivers), the three API gaps below (Network→DeviceClass binding,
interface-name ownership, IP-exhaustion-as-consumable-capacity), and the
attach-timing data (parallel DRA vs. serial Multus).

## The upstream landscape (one ecosystem)

- **KNDM — Kubernetes Network Driver Model** (arXiv 2506.23628): a declarative,
  modular model for network provisioning built on **DRA + NRI + OCI runtime-spec
  changes** (`netDevices`, runc 1.4 / OCI 1.3.0), replacing imperative CNI/Multus
  chains.
- **DraNet** (`kubernetes-sigs/dranet`): the SIG-network **reference KND driver**.
  Discovers real host network devices and attaches them to pods.
- **KubeVirt VEP-183 `NetworkDevicesWithDRA`** (kubevirt/enhancements#183,
  kubevirt/kubevirt#15995): adds `ResourceClaim` as a first-class `NetworkSource`
  so VMs consume NICs via **externally supplied** DRA drivers. Explicitly keeps
  Multus supported; **hot-plug of DRA network devices is an explicit Non-Goal.**

Our driver sits in the **DraNet slot** — it is a KND-shaped driver — but with an
**SDN backend (kube-ovn)** instead of hardware passthrough.

## Where we already align with KNDM/DraNet

| Dimension | DraNet | This driver |
|---|---|---|
| DRA kubelet plugin + **NRI** netns plumbing | yes | yes (`pkg/plumbing`) |
| Publishes a per-node ResourceSlice | yes (`pkg/inventory`) | yes (`internal/profiles/nic`) |
| Opaque per-NIC config | yes | yes (`NicConfig.interfaceName`) |
| Day-0 only; hot-plug deferred | yes | yes (matches VEP-183 Non-Goal) |
| Toward OCI `netDevices` runtime plumbing | yes | not yet |

> **On OCI `netDevices` (why NRI today, not `netDevices`):** `linux.netDevices`
> *moves an existing host device* into the container netns — purpose-built for
> hardware passthrough (DraNet/SR-IOV), where that's the whole job. An SDN driver
> must first **synthesize** the device (veth + OVS port + `iface-id` + IPAM), which
> `netDevices` doesn't do; it could at most replace the "move the pod-side veth end
> into the netns" sub-step. And there's no DRA→`netDevices` path yet anyway: a DRA
> driver shapes the OCI spec via **CDI ContainerEdits**, and CDI doesn't carry
> `netDevices`. So **NRI** (available in containerd 2.x) is used today — it's the
> hook where the driver does the full synthesis. When the `netDevices` path
> matures it would offload only the netns-move sub-step; the OVS/IPAM logic stays
> driver-side. So `netDevices` is the native endgame for the *device-injection
> step* (and for hardware passthrough), not a replacement for this driver's attach.
## Where we fundamentally diverge (the deciding factor)

DraNet = **passthrough of finite, node-present, real devices**.
This driver = **on-demand creation of virtual SDN endpoints**.

| Aspect | DraNet (hardware) | This driver (SDN/kube-ovn) |
|---|---|---|
| Device source | discovered inventory (PCI NICs, SR-IOV VFs, cloud ENIs, RDMA) | kube-ovn **Subnets** (no inventory to discover) |
| Natural ResourceSlice model | **exclusive / partitionable** (scarce hardware) | **consumable-capacity** (subnet = shared IP pool) |
| Attach primitive | netlink **move** of an existing device | **create veth + OVS port + IPAM + OVN LSP** |
| Control-plane interlock | none/minimal | kube-ovn-controller (LSP lifecycle, GC) |
| Scheduling value | **NUMA/PCI topology alignment** (its raison d'être) | mostly **underlay provider placement**; overlay is node-agnostic |

Consequence: the original "subnet = one exclusive device" model was wrong for a
shared pool — a 2nd pod selecting the same subnet failed to allocate. **Fixed**:
the profile now publishes subnet devices with `AllowMultipleAllocations=true`
(needs the `DRAConsumableCapacity` gate), so many pods share a subnet, each
getting its own IP — verified by the `shared subnet` e2e spec. The richer,
fully KND-idiomatic upgrade is **consumable-capacity** (publish the pool size as
capacity, each request consumes one), which additionally makes IP-pool exhaustion
a *scheduling* decision; see "Shared subnets" in
[`nic-driver.md`](nic-driver.md).

## "kube-ovn backend for the KND model?" — yes to the model, no to DraNet's code

- **A DraNet plugin/backend is a poor fit.** DraNet is architected around
  discovering and moving host devices; there is no clean plug point for
  "synthesize a veth on an SDN bridge with IPAM + LSP." Hardware-passthrough and
  virtual/SDN are two backend *categories* that share only the KND front-end.
- **Aligning with KNDM conventions is the real win, and it's cheap.** Same
  DRA+NRI+OCI-netDevices patterns, DeviceClass/attribute conventions, opaque
  config. Result: **KubeVirt VEP-183 consumes this driver exactly like DraNet**
  ("externally supplied DRA drivers") with no special-casing.

## Operational patterns worth borrowing from DraNet (also fix known gaps)

1. **Watch + re-publish the ResourceSlice** (vs one-shot `EnumerateDevices`) —
   fixes "a new Subnet needs a driver restart to appear."
2. **bbolt-persisted prepared state** (vs in-memory pending store) — fixes
   "state lost on plugin restart."
3. **`resourceclaims/driver` associated-node + status patching** — more robust
   node-association than the current flow.
4. **Consumable-capacity subnet devices** — the multi-pod-per-subnet bug is
   already fixed via `AllowMultipleAllocations`; consumable-capacity is the richer,
   KND-idiomatic upgrade that *also* makes IP-pool exhaustion a scheduling
   decision.

## Hot-plug status (settled)

- `pod.spec.resourceClaims` and `ResourceClaim.spec` are **immutable** (k8s 1.35
  and 1.36, verified in source). No DRA path to add a NIC to a running pod.
- VEP-183 lists DRA-NIC hot-plug as an explicit **Non-Goal** — even KubeVirt
  defers it.
- Works today for "change a workload's NIC set": edit the Deployment/StatefulSet
  **pod template** → rollout (pod recreation, not in-place). A
  `ResourceClaimTemplate` gives each pod its own claim (good practice for
  replicas; no longer required to dodge the exclusive-device problem now that
  subnets allow multiple allocations).
- True in-place hot-plug remains out-of-band (Multus-dynamic model) or a future
  upstream effort; SDN overlay is the *easiest* case (node-agnostic) and a good
  argument to bring upstream.

### Is the lack of hot-plug a blocker? (container vs VM)

- **Containers: edge case, not a blocker.** Containers are cattle — the k8s idiom
  is immutable pods; "change the NIC set" = edit the pod template and roll it.
  Day-0 (NIC present at start) covers essentially all container needs; hot-plug
  into a running container is rare and arguably an anti-pattern.
- **VMs (KubeVirt): a real feature, but still not a DRA blocker.** VMs are pets;
  "add a NIC to a live VM" is a normal operation users expect. It is still not a
  reason to avoid the DRA path because: (1) **no DRA driver does it yet** (VEP-183
  Non-Goal, DraNet/OKEP are day-0) — so choosing DRA is at parity, not a
  regression; (2) **KubeVirt already hot-plugs Multus interfaces** via its own
  mechanism, independent of DRA, so the capability isn't lost; (3) the **SDN
  overlay case is the easiest to hot-plug later** (node-agnostic, nothing to
  physically move), making it a good upstream argument when DRA hot-plug matures.
- **Net:** lack of DRA hot-plug does not touch the day-0 wins (scheduler
  awareness, IP-pool capacity, attach latency, lifecycle/GC), which are the actual
  reasons to prefer DRA. Treat it as deferred, not disqualifying.

## Strategic options

The value of this driver hinges on one question: **is "Multus-free / DRA-native
kube-ovn NICs" a real product requirement (e.g. KubeVirt VMs on kube-ovn in the
virtualization product), or was this exploratory?**

- **A. Continue as the SDN KND driver (KNDM-aligned).** Justified if there is a
  real consumer (KubeVirt-on-kube-ovn wanting a clean DRA API, dropping Multus).
  Requires: consumable-capacity remodel, watch/republish, bbolt, and landing
  kube-ovn's `--enable-dra-nic` (DRA claims as a network source) upstream.
  Day-0 only.
- **B. Park it; ride Multus.** Multus + kube-ovn already does day-0 secondary
  NICs. The driver's only edge is "no Multus / DRA-native." If that isn't
  pressing, keep Multus and revisit when KNDM/VEP-183 matures and pulls.
- **C. Invest upstream, not in a private driver.** Land the kube-ovn-side DRA
  support in kube-ovn proper, and establish "SDN KND driver" as a recognized
  category with sig-network/DraNet. The driver becomes a reference/contribution.
  (Aligns with the existing kube-ovn DRA upstreaming goal.)
- **D. Keep it as a demonstrator.** Use it to inform the upstream hot-plug /
  SDN-KND conversation and as a learning artifact; do not productize.

Note: most of the durable, hard work is **kube-ovn-side**: kube-ovn-controller
treating DRA claims as a network source, with IPAM, logical switch ports,
keep-vm-ip and GC. That is the higher-leverage place to invest if the effort
continues — the node-side driver is thin by comparison.

## FAQ: "Can we just use DraNet instead of building a kube-ovn driver?"

**Short answer: no — DraNet is the wrong backend category for an SDN.** You can
adopt the *model* it implements (KNDM), but DraNet itself cannot be the kube-ovn
NIC backend.

**Why not.** DraNet is built for **passthrough of real, node-present hardware**:
its architecture is *discover an inventory of host devices (PCI NICs, SR-IOV VFs,
cloud ENIs, RDMA) → `netlink`-move an existing device into the pod netns*.
kube-ovn is the opposite: there is **nothing to discover**, and the attach
primitive is to **create** a virtual endpoint on demand (veth + OVS port + IPAM +
OVN logical switch port) with a kube-ovn-controller interlock for LSP lifecycle
and GC. There is no clean plug point in DraNet for "synthesize a veth on an OVS
bridge with IPAM + an LSP." Hardware-passthrough and virtual/SDN are two backend
*categories* that share only the KND front-end (see the divergence table above).

**ResourceSlice model also differs.** DraNet models scarce hardware as
**exclusive/partitionable** devices; a kube-ovn Subnet is a **shared IP pool**
(consumable-capacity). Modelling the subnet as an exclusive device was the source
of the original "second pod on the same subnet fails to allocate" bug — now fixed
via `AllowMultipleAllocations` (consumable-capacity is the richer follow-up).

**What to reuse instead.** Align this (thin) kube-ovn driver with KNDM/DraNet
**conventions** — DRA + NRI (+ future OCI `netDevices`), DeviceClass/attribute
conventions, opaque per-NIC config. That alignment is cheap and is the real
payoff: **KubeVirt VEP-183 then consumes this driver exactly like it would
DraNet** ("externally supplied DRA drivers"), with no special-casing. The heavy
lifting stays kube-ovn-side, not in the node driver.

**The actual "build nothing" alternative is Multus, not DraNet.** Multus +
kube-ovn already does day-0 secondary NICs today (option B above). The only thing
a DRA driver adds over that is *Multus-free, DRA-native, scheduler-visible* NICs
and a clean KubeVirt-on-kube-ovn path. If that isn't a hard requirement, ride
Multus; if it is, a KND-aligned kube-ovn driver is the right build and DraNet
cannot substitute for it.

**Hot-plug caveat (applies to all of them).** Switching to DraNet would not
unlock NIC hot-plug: DRA claim specs are immutable (k8s 1.35/1.36) and VEP-183
lists DRA-NIC hot-plug as an explicit Non-Goal. DraNet, this driver, and KubeVirt
are all day-0-only here.

### FAQ: "DraNet for trunk/VF passthrough into VMs vs. this driver for SDN?"

These are **complementary, not competing** — different layers, and a cluster can
run both (different `DeviceClass`es; a VM could even get one of each).

- **DraNet / SR-IOV-DRA = hardware passthrough into VMs.** Via KubeVirt VEP-183 a
  VM consumes a DRA device as a `NetworkSource`, so DraNet can hand a bare-metal
  NIC or an SR-IOV VF to the virt-launcher pod, which KubeVirt bridges into the
  VM. Good for **line-rate access to the physical fabric**. "Trunk" is a property
  of the *device*, not something DraNet does: pass a trunk-capable physical NIC
  (exclusive to one VM) or, to scale, **SR-IOV VFs in VGT mode** (each VF a
  device; the VM does its own 802.1q). It gives **raw L2**, not kube-ovn
  IPAM/subnets/OVN policy/overlay.
- **This driver = virtual SDN endpoints.** It synthesizes veth + OVS port + IPAM
  (+ LSP) on a kube-ovn bridge — kube-ovn-managed networking, not hardware.

**Can `AllowMultipleAllocations` give one trunk to many VMs?** Only in the SDN
sense. The multiplexing is done by **OVS** (each VM gets its own tagged access
port on the shared provider bridge, all egressing the one physical uplink);
`AllowMultipleAllocations` is just the DRA-level permission that models the shared
subnet/uplink. It does **not** apply to DraNet passthrough — you cannot move one
physical netdev into multiple netns; sharing a physical trunk across VMs needs
SR-IOV VFs or a software bridge (i.e. the SDN model again).

**Trunk ports belong to ovs-cni, not this driver.** This driver gives a
*single-VLAN access port* (OVS `tag=`). A simulated fabric node (router/leaf/spine,
e.g. clabernetes) wants a real **trunk** (many VLANs, the VM tags/untags itself).
That is already solved today by **ovs-cni**, which supports `vlan` (access) and
`trunk` ports on a pre-existing OVS bridge — and the bridge can be the one
kube-ovn already builds from a `ProviderNetwork` (`br-<provider>`, with the uplink
attached). So the trunk/fabric story is: **kube-ovn owns the provider bridge +
uplink; ovs-cni (via Multus NAD) hands VMs trunk ports on it.** No trunk device
type is needed in this driver. (Caveats: it rides Multus; IPAM is ovs-cni's job —
fine for router/trunk ports that self-address; verify kube-ovn's port GC leaves
ovs-cni ports on the provider bridge alone.)

This **narrows the DRA driver's scope** to its strongest case: timely,
scheduler-aware, IPAM-managed **secondary NICs on pods** — the one thing
Multus/ovs-cni plumbing handles worst (serial attach, no scheduler visibility, no
capacity accounting). Trunks and hardware passthrough are covered by ovs-cni and
DraNet/SR-IOV respectively.

## Related upstream efforts (and how they differ from this driver)

Two concrete reference points, both reviewed 2026-06. The key finding: **neither
does what this driver does**, and they differ from each other too — together they
show the upstream direction is "DRA selects/attaches a *real, pre-existing
device*", not "DRA synthesizes a *virtual SDN endpoint*".

### ovn-kubernetes OKEP-6391 — DRA for OVN-K networks

`ovn-kubernetes/ovn-kubernetes` PR #6446 (`docs/okeps/okep-6391-dra.md`).
Note this is a **different project** from kube-ovn (driver id `k8s.ovn.org`).

- **Scope: accelerated networking (SR-IOV), not virtual NICs.** It replaces the
  `sriov-network-device-plugin` + `network-resources-injector` + multus-deviceID
  stack with a first-party DRA driver that **discovers real host SR-IOV VFs** and
  DRA-allocates them. Explicit Non-Goal: "not a generic multi-network DRA driver
  competing with dranet / Mellanox dra-driver", no RDMA/IB.
- **It keeps NADs / Multus.** The NAD (or `UserDefinedNetwork`) still *defines the
  network*; DRA only changes *how a pod is matched to a hardware device*. A new
  annotation `k8s.ovn.org/deviceClass` on the NAD/UDN points at a `DeviceClass`;
  on CNI ADD ovn-k uses the allocated device's **PCI BDF as the SR-IOV DeviceID**
  in its existing flow. In-process kubelet plugin owned by `ovnkube-node`.
- **Takeaway:** even a sibling OVN CNI uses DRA for the *device*, and **NADs for
  the *network*** — it is **not** going Multus-free/DRA-native for network
  definition. That is the opposite of this driver's distinctive bet.

### DraNet GKE multi-network example

`kubernetes-sigs/dranet/examples/demo_gke_multinetwork`.

- **Cloud provisions real host NICs.** `dranetctl gke acceleratorpod create …
  --additional-network-interfaces 2` + `--enable-multi-networking` makes **GCE add
  extra host NICs** to the VMs (one per additional VPC network). DraNet then
  *discovers* those NICs; pods select them by attribute, e.g.
  `device.attributes["dra.net"].name == "eth1"` or
  `cloudNetwork.contains("dranet-net")`, and DraNet **moves/attaches the existing
  host NIC** into the pod. Nothing is synthesized.
- **Takeaway:** the "one real host NIC per network" model **does not map to
  kube-ovn**, which is a software overlay/underlay over a shared uplink — there is
  no per-network host device to discover. So this pattern can't be borrowed; the
  synthesis path (veth + OVS + IPAM + LSP) is still required.

### What this means for the "right direction?" decision

- **All references = attach a pre-existing real device** (SR-IOV VF, cloud host
  NIC). This driver = **create a virtual SDN endpoint on demand**. It remains the
  only one in the SDN-virtual category (reinforces the divergence table above).
- The ecosystem shape is **NAD defines the network, DRA allocates the
  accelerator** — so DRA-as-the-network-attachment-API (the Multus-free bet) is
  not where upstream is heading for *virtual* networks. That sharpens the fork:
  either (C) establish "SDN KND driver" as a recognized category with sig-network,
  or (B) ride Multus until KNDM/VEP-183 pulls.
- If **SR-IOV on kube-ovn** ever becomes the real requirement, mirror OKEP-6391
  (discover VFs → DRA-select → keep NADs) by adding it to *kube-ovn* — do not
  extend this virtual-NIC driver for it.

## Is the SDN approach "out of scope" for DRA?

Not mechanically — but it sits at the edge of what DRA is *for*. Three levels:

1. **Framework: in scope.** DRA is device-source-agnostic; a "device" is whatever
   the driver publishes — nothing requires real hardware. This driver works
   end-to-end (subnets → ResourceSlice → claim → NRI synthesizes
   veth+OVS+IPAM+LSP), and the shared-subnet IP pool maps cleanly onto
   **consumable-capacity / KEP-4815 partitionable devices**. The *mechanism* fits.
2. **Design intent: uneven.** DRA's value is **topology-aware scheduling of
   constrained resources**. A kube-ovn **overlay** endpoint is node-agnostic with
   ~unbounded capacity, so the scheduler gains little — DRA is then just a
   provisioning trigger (via NRI), not allocation logic. The **underlay/provider**
   case is the opposite: "pod needs VLAN X → must land on a node whose provider
   bridge has that uplink" is genuine DRA node-selection value.
3. **Ownership: partly a network-API concern.** DRA models *resources that are
   allocated/consumed*. "Give me an IP from this pool" / "give me this VF" is a
   resource; "wire me to overlay X" is closer to **configuration**, which is what
   NAD / `multi-network-api` are meant to own. That's why every upstream reference
   keeps **NAD = network, DRA = device**.

So the genuinely DRA-shaped parts of this driver are **(a) IPAM as
consumable-capacity** and **(b) underlay provider placement**. The pure-overlay
"attach me to the SDN" part is the weakest DRA justification and the most likely
to be considered network-API territory. If the effort continues, lean into (a)
and (b); don't pitch it as "DRA replaces the network API."

### …but Multus has real weaknesses (so this isn't a free "just ride Multus")

The "ride Multus" option (B) is lower-risk, not cost-free. The pain points that
actually motivate a DRA-native path:

- **Scheduler-blind.** `k8s.v1.cni.cncf.io/networks` annotations are invisible to
  the scheduler: it can't refuse a pod when a subnet's IP pool is exhausted, can't
  place by underlay/provider availability. This is precisely DRA's strength —
  and the strongest argument for the approach.
- **No capacity accounting.** A subnet's "254 IPs" is not expressible as a
  schedulable quantity; exhaustion surfaces as a late CNI failure, not a
  scheduling decision. (DRA consumable-capacity fixes this.)
- **Serial attach latency.** Multus invokes delegate CNIs serially, so pod
  spin-up grows ~linearly with NIC count (~5s/NIC is the figure cited in the demo
  notes — *not yet independently measured*; `make nic-bench` is what quantifies
  it). On the DRA path kube-ovn allocates all NICs of the pod in one pass and the
  attach runs without per-NIC CNI forks, so it *should* scale flatter — see
  [`benchmark-results-2026-08-17.md`](benchmark-results-2026-08-17.md).
- **Late, untyped validation.** Free-form annotations + NADs aren't validated at
  admission; typos/wrong-provider fail at CNI ADD time. DeviceClass + opaque
  config (+ a webhook) catch these earlier and are admin-gated.
- **Architectural layering (separation of concerns).** Multus is a *meta-CNI
  multiplexer wedged into the CRI→CNI call path*: the runtime calls Multus as the
  cluster CNI, and Multus delegates to the real CNIs, chaining them serially.
  Mixing "which networks does this pod get" into a shim in the datapath-setup call
  is what also causes the serial-attach latency above — same root cause. DRA
  separates the concerns cleanly instead: **resource allocation/scheduling on the
  kubelet side (DRA), network plumbing as a driver on the CNI/NRI side** — no
  multiplexing layer in between. (This is the principled framing; "fewer
  components" is *not* the argument — DRA adds its own driver/DeviceClasses. The
  one real component reduction is for **SR-IOV**, where DRA subsumes the
  device-plugin *and* the network-resources-injector webhook entirely.)
- **Weak lifecycle/GC.** A NAD is just config; there's no first-class
  allocate/release. Reserved-IP / LSP cleanup is ad hoc (exactly the GC class of
  bug this effort had to fix kube-ovn-side).
- **VM integration.** KubeVirt VEP-183 is moving to DRA as a `NetworkSource`
  precisely to get a cleaner model than Multus annotations.

**Synthesis.** The decision is not binary. Several Multus weaknesses
(scheduler-awareness, capacity, attach latency, lifecycle) line up exactly with
DRA's strengths — which is the legitimate value case for this driver *even though*
SDN sits at DRA's edge. The honest split: use **DRA for the resource/scheduling
parts** (IPAM-as-capacity, underlay placement, lifecycle/GC) where it clearly
beats Multus; leave **network *definition*** to a network API rather than claiming
DRA should replace it. That is the defensible "right direction" if (A)/(C) is
chosen — and it's also why (B) "ride Multus" should be read as "ride Multus *for
now*", not "Multus is fine forever".
