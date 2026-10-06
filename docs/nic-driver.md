# kube-ovn NIC DRA Driver

A Dynamic Resource Allocation (DRA) driver for kube-ovn virtual NICs, built on
top of the `kubernetes-sigs/dra-example-driver` scaffold. A pod gets secondary
NICs (`net1`, `net2`, …) from `ResourceClaim`s, without Multus, a
`NetworkAttachmentDefinition` or the `k8s.v1.cni.cncf.io/networks` annotation.

## Architecture

kube-ovn owns the network: addresses, MACs, logical switch ports, ip CRs and
their lifecycle. kube-ovn-controller, started with `--enable-dra-nic`, treats
every device allocated by this driver (`--dra-nic-driver-name`, default
`nic.kubeovn.io`) as a third network source next to the default network and
Multus attachments. The driver publishes the subnets and plugs the interfaces.

```
┌──────────────────────────────────────────────────────────────────┐
│  ResourceSlice (per node)                                        │
│  Device: subnet-<name>   (AllowMultipleAllocations)              │
│    attributes: subnetName, subnetType (vlan|ovn), vlanId,        │
│                providerNetwork, provider, vpc                    │
└──────────────────────────────────────────────────────────────────┘
        ↑ published by                         ↓ allocated by kube-scheduler
┌──────────────────────────┐        ┌──────────────────────────────────┐
│ kubelet plugin (DaemonSet)│        │ kube-ovn-controller              │
│ cmd/kube-ovn-dra-         │        │ --enable-dra-nic                 │
│ kubeletplugin             │        │  device → subnet → provider      │
│                           │        │  IPAM, LSP, ip CR, annotations   │
│ NodePrepareResources      │◀──────│  <provider>.kubernetes.io/*      │
│  wait for allocated=true  │ pod    └──────────────────────────────────┘
│  (pkg/nicprepare)         │ annotations
│                           │
│ NRI RunPodSandbox         │        veth into the pod netns, host end on
│  attach (pkg/plumbing) ───┼──────▶ br-int with external_ids:iface-id=<LSP>
│ NRI StopPodSandbox        │        → ovn-controller binds the port
│  detach                   │
└──────────────────────────┘
```

The contract between both sides is written up in kube-ovn's `docs/dra-nic.md`.

## Packages

| Path | Purpose |
|------|---------|
| `api/kube-ovn.io/resource/nic/v1alpha1/` | `NicConfig` opaque config (`interfaceName`) |
| `internal/profiles/nic/` | NIC device profile — enumerates kube-ovn Subnets via the dynamic client |
| `pkg/annotation/` | Reads kube-ovn-controller's allocation from the pod annotations |
| `pkg/nicprepare/` | Resolves a claimed NIC: pod, subnet, provider key, logical switch port, addresses |
| `pkg/plumbing/` | Veth/OVS/netns attach, KubeVirt bridge + tap + DHCP, NRI sandbox handler, `NICStore` |
| `cmd/kube-ovn-dra-kubeletplugin/` | kubelet plugin: ResourceSlices, prepare/unprepare, NRI plugin |
| `cmd/kube-ovn-dra-webhook/` | Optional validating webhook for `NicConfig` |

## Allocation and attach flow

1. **Devices.** The NIC profile publishes each kube-ovn `Subnet` as device
   `subnet-<name>` with the attribute `nic.kubeovn.io/subnetName`. It watches
   Subnets and Vlans and republishes the ResourceSlice within seconds of a
   change; status updates are ignored. kube-ovn-
   controller reads that attribute from the newest ResourceSlice generation of
   the device's pool. The subnet must have a dedicated `spec.provider` (e.g.
   `<subnet>.<namespace>.ovn`); the default provider `ovn` belongs to `eth0`
   and is rejected by both sides.
2. **Allocation.** The scheduler allocates the device to the pod's claim and
   binds the pod. Once the pod is bound and all its claims are allocated,
   kube-ovn-controller allocates IP and MAC, creates the logical switch port and
   the ip CR `<name>.<namespace>.<provider key>`, and writes the pod annotations
   `<provider key>.kubernetes.io/{ip_address,mac_address,cidr,gateway,logical_switch,allocated}`.
   `<name>` is the pod name, or the VM name for a KubeVirt VM with keep-vm-ip,
   in which case `<provider key>.kubernetes.io/virtualmachine` is set as well.
3. **Provider key.** A NIC alone on its provider in the pod is keyed by the
   provider. NICs of one pod sharing a provider are keyed by
   `<provider>.<interfaceName>`, the rule kube-ovn uses for repeated Multus
   attachments of one NAD. `interfaceName` comes from the `NicConfig` that
   applies to the request (default `net1`).
4. **Prepare.** `NodePrepareResources` resolves the pod from the claim's
   `reservedFor` and rejects an interface name that another NIC of the pod
   already uses, in the claim or in another claim of the pod; such a pod
   would otherwise fail later, when the sandbox starts. Then
   `nicprepare.RequestIPAM` waits up to 30 s for
   `allocated="true"` under either provider key and reads the result.
   Dual-stack values are comma separated; the driver configures all addresses
   on pod interfaces. The NICs of a claim are resolved concurrently. Each NIC
   becomes a `plumbing.Spec` in the `NICStore`, keyed by pod and claim, and in
   the checkpoint, which restores the store after a plugin restart. The Specs
   stay until the claim is unprepared, so a recreated sandbox of the pod gets
   its NICs again.
5. **Attach.** The NRI `RunPodSandbox` hook reads the pod's Specs, fills in
   the netns path and sandbox ID, and runs `Attach` per NIC: create a veth pair
   (names hashed from sandbox ID and interface, IFNAMSIZ-safe), move the pod
   end into the netns and rename it, set MAC, MTU, addresses (IPv6 without DAD)
   and per-NIC routes (never a default route), then add the host end to
   `br-int` with `external_ids:iface-id=<logical switch port>` plus
   `vendor=kube-ovn`, `pod_name`, `pod_namespace`, `ip`, `pod_netns` and the
   owner mark `kube-ovn-dra-driver=nic`. If one NIC fails, the NICs already
   attached are detached again and the sandbox fails. When the plugin
   (re)connects, NRI `Synchronize` runs the same attach for every running
   sandbox, which covers a pod started while the plugin was down; `Attach` is
   idempotent and skips what exists.
6. **Detach.** The NRI `StopPodSandbox` hook detaches the NICs it attached and
   then removes every OVS port with the pod's owner mark, which also covers
   pods attached before a plugin restart. A keep-vm-ip VM's port outlives its
   pods, so a leftover port would steal the binding from the next pod.
7. **Release.** Nothing: `NodeUnprepareResources` only drops the NICs from the
   store, the checkpoint and
   CDI spec. kube-ovn-controller releases the address when the pod is deleted,
   or keeps it for a VM that still exists, as for Multus attachments.

### Overlay and underlay

Both subnet types take the same path. For a VLAN underlay subnet,
kube-ovn-controller creates the logical switch port as well, and the logical
switch reaches the VLAN through its localnet port on the provider bridge. The
`subnetType`, `vlanId` and `providerNetwork` attributes only help claims select
subnets.

> OVN 26.03's northd derives `Logical_Switch_Port.tag` from `tag_request`, and
> kube-ovn only writes `tag`, so VLAN subnets lose their tag on clusters with
> that OVN. `make kind-vlan-tag-workaround` (run by
> `kind-deploy-kube-ovn-fixtures`) sets `tag_request` on the localnet ports
> until kube-ovn is fixed.

### KubeVirt VMs

QEMU needs a tap device, not a veth. For pods labelled
`kubevirt.io=virt-launcher`, `Attach` also renames the veth out of the way,
creates a Linux bridge and a tap device named like the NIC (owned by the qemu
user, uid 107), enslaves both to the bridge and starts a single-client DHCPv4
server on the bridge that hands out the allocated IPv4 address, gateway and
routes. The guest only gets the IPv4 address of a dual-stack NIC; IPv6-only
VMIs get no DHCP server. The
[kube-ovn network binding plugin](https://github.com/soer3n/kube-ovn-network-binding-plugin)
adds the tap to the VM's domain, since KubeVirt's built-in bindings do not
resolve DRA networks yet. kube-ovn names the VM's port after the VM, so IP and
MAC survive VM restarts.

The bridge, tap and DHCP server are a workaround as well: once KubeVirt's
`managedTap` attachment resolves DRA networks, KubeVirt creates the tap itself,
and the driver only needs to hand over the veth. The plugin's README describes
the expected migration, and why reporting the NIC in the ResourceClaim status
(`status.devices[].networkData`) would also remove the interface naming
contract between claim templates and VMs.

## Local dev deployment on kind

A full kube-ovn + NIC DRA stack can be brought up in a local
[kind](https://kind.sigs.k8s.io/) cluster. All targets live in the `Makefile`
under the `kind-*` prefix and use `demo/kind/kind-no-cni.yaml` (default CNI
disabled, DRA feature-gates enabled, `/lib/modules` mounted for OVS).

### Prerequisites

`kind`, `kubectl`, `helm`, `docker`, `curl`, `tar` (verified by
`make kind-check-deps`). The VLAN connectivity test additionally needs
`containerlab` and `sudo`. kube-ovn is deployed from the chart of
`KUBE_OVN_VERSION` in the sibling checkout `../kube-ovn` (`KUBE_OVN_REPO`) with
`func.ENABLE_DRA_NIC=true`, using an image built from that branch
(`KUBE_OVN_IMAGE`).

**Kubernetes version:** the driver uses the stable DRA API
(`resource.k8s.io/v1`), which requires **Kubernetes 1.34+**; the repo is built
and tested against **1.35** (`k8s.io/*` deps pinned to `v0.35.x`). `kind-create`
pins the node image via `KIND_NODE_IMAGE` (default `kindest/node:v1.35.0`).
**kind v0.31.0** already defaults to v1.35.0, so a recent kind binary needs no
extra flags; older kind releases (≤ v0.29, which default to ≤ v1.33) won't serve
the stable DRA API. Override to test another release:

```bash
make kind-create KIND_NODE_IMAGE=kindest/node:v1.34.3
```

### One-shot demo

```bash
# Create cluster, wire VLAN uplink via containerlab, deploy kube-ovn (+ Multus
# for the benchmark), build & load the driver image, install the chart, apply
# the NIC example.
make kind-demo

# Tear down
make kind-delete
```

### Step by step

```bash
make kind-create            # kind cluster, no CNI, DRA gates on (nodes NotReady until CNI)
make clab-deploy            # OPTIONAL: containerlab VLAN uplink + FRR BGP gateway (needs sudo)
make kind-deploy-kube-ovn   # install kube-ovn CNI via its Helm chart -> nodes become Ready
make kind-deploy-multus     # OPTIONAL: Multus (thick mode), only for make nic-bench
make kind-deploy-nic-prereqs # provider network, VLANs, subnets
make kind-build-driver      # docker build -> kind load docker-image nic.kubeovn.io:dev
make kind-deploy-driver     # helm install kube-ovn-nic-dra (deviceProfile=nic)
make kind-deploy-nic-example # ResourceClaim + demo pod
```

### Disabling kind's default CNI and installing kube-ovn

kind ships with kindnet as its default CNI. kube-ovn must own the pod network,
so kindnet is disabled at cluster-creation time in `demo/kind/kind-no-cni.yaml`:

```yaml
networking:
  disableDefaultCNI: true          # no kindnet — kube-ovn takes over
  podSubnet: "10.244.0.0/16"       # kube-ovn POD_CIDR must match
  serviceSubnet: "10.96.0.0/12"
```

Each node also mounts `/lib/modules` (read-only) so OVS can load its kernel
modules, and enables the `DynamicResourceAllocation` feature-gate on the
apiserver, controller-manager and kubelet (required for ResourceSlices).

With no CNI installed, **nodes stay `NotReady` after `make kind-create` — this
is expected.** `make kind-deploy-kube-ovn` then installs the kube-ovn Helm chart
(version `KUBE_OVN_VERSION`), which:

1. Labels the control-plane node `kube-ovn/role=master`.
2. Fetches the chart for the pinned version and `helm upgrade --install`s it
   into `kube-system` with `POD_CIDR`/`SVC_CIDR`/`JOIN_CIDR` matching the kind
   config and `func.ENABLE_DRA_NIC=true`.
3. Waits for all nodes to report `Ready` — at which point the CNI is live.

Verify kube-ovn on its own before adding the NIC driver:

```bash
make kind-kube-ovn-status   # component pods, node status, kube-ovn subnets
make kind-test-kube-ovn     # two pods on the default overlay + pod-to-pod ping
make kind-test-kube-ovn-clean  # remove the smoke-test pods
```

`kind-test-kube-ovn` applies `demo/kind/kube-ovn-smoke-test.yaml` (two
`netshoot` pods), waits for them to be Ready, and pings one from the other —
a quick confirmation that the overlay datapath works.

### Underlay / VLAN-backed networks via containerlab

The VLAN demo uses [containerlab](https://containerlab.dev/) to give the kind
nodes a second NIC (`eth1`) wired to a shared Linux bridge, plus an FRR BGP
gateway so the host can route into the pod VLANs. Topology
(`demo/containerlab/vlan-topology.yaml`):

```
  ┌───────────────────┐         ┌───────────────────┐
  │ control-plane eth1│──port1──┤                   │
  ├───────────────────┤         │   vlan-switch     │
  │ worker eth1       │──port2──┤  (Linux bridge,   │
  └───────────────────┘         │  VLAN filtering)  │
  ┌───────────────────┐         │                   │
  │ frr-gw eth1 (trunk)│─port3──┤                   │
  │  eth1.100 / eth1.200│        └───────────────────┘
  │  BGP ASN 65001     │
  └───────────────────┘
```

`make clab-deploy` (needs `sudo` + `containerlab`):

1. (Re)creates the host bridge `vlan-switch` with `vlan_filtering 1`.
2. Renders the FRR config with the live node IPs (`kind-frr-update-peers`).
3. Deploys the topology — wires each node's `eth1` and the FRR trunk into the
   bridge, brings up `eth1.100`/`eth1.200` sub-interfaces on FRR.
4. Allows VLAN 100 and 200 on bridge ports `port1`–`port3`.

FRR (ASN 65001) peers over eBGP with `kube-ovn-speaker` (ASN 65000) on each
node and redistributes the VLAN subnet routes into the host kernel via zebra —
so the host reaches pods without any manual `ip route add`.

Run **`make clab-deploy` after `make kind-create` but before
`make kind-deploy-kube-ovn`**, so `eth1` exists when kube-ovn programs the
provider network.

> **This containerlab FRR + `kube-ovn-speaker` setup is a self-contained kind
> stand-in, not the production topology.** In a kubermatic-virtualization
> deployment the `virtualization.k8c.io/EdgeRouter` serves as the VLAN subnet's
> routed gateway and north-south edge: it attaches directly to the subnet
> (`peers.internal`) as the gateway and advertises it to the upstream fabric
> (`peers.external`, with BFD/VRF/EVPN). That replaces **both** the demo
> `frr-gw` **and** `kube-ovn-speaker` — the speaker only exists to announce OVN
> *overlay* pod IPs over BGP, which is unnecessary for VLAN underlay where pods
> are directly L2-reachable on the VLAN. The demo peers with the speaker only
> because `frr-gw` is not itself the on-VLAN gateway and learns the routes via
> BGP redistribution instead.

#### How the NIC driver consumes the VLAN underlay

`demo/nic-example/00-kube-ovn-subnets.yaml` defines the kube-ovn objects the
DRA driver enumerates into ResourceSlices:

```yaml
ProviderNetwork external   # defaultInterface: eth1   (the containerlab uplink)
Vlan vlan100  (id 100, provider external)  ─┐
Subnet vlan100-subnet 172.23.0.0/24  vlan: vlan100   provider: external.vlan100-subnet.ovn
Vlan vlan200  (id 200, provider external)  ─┐
Subnet vlan200-subnet 172.24.0.0/24  vlan: vlan200   provider: external.vlan200-subnet.ovn
```

kube-ovn creates `eth1.100` / `eth1.200` sub-interfaces inside each node, and
the NIC profile publishes one `subnet-<name>` device (`subnetType=vlan`,
`vlanId=...`) per subnet. A pod then selects the VLAN it wants via a CEL
selector on its `ResourceClaim` — e.g. `subnetName == "vlan100-subnet"`.
kube-ovn-controller creates the NIC's port on the VLAN subnet's logical switch,
and the driver plugs the NIC into `br-int`; OVN forwards its traffic through
the localnet port to `br-external` and the `eth1` trunk with tag 100.

End-to-end VLAN path:

```bash
make kind-create
make clab-deploy            # eth1 uplink + FRR BGP gateway  (sudo)
make kind-deploy-kube-ovn
make kind-deploy-nic-prereqs  # provider network, VLANs, subnets
make kind-build-driver
make kind-deploy-driver
make kind-deploy-nic-example  # claims, demo pod
make kind-test-vlan           # dual-VLAN traffic + host routing + isolation checks
```

`make kind-frr-status` shows the BGP session state and the VLAN routes learned
into the host table; `make clab-destroy` tears down the topology and bridge.

### Iterating on the driver

After a code change, rebuild and reload the image, then restart the plugin:

```bash
make kind-build-driver
make kind-deploy-driver     # helm upgrade --install, pullPolicy=Never uses the loaded image
kubectl -n kube-system rollout restart ds/kube-ovn-nic-dra-kube-ovn-dra-driver-kubeletplugin
```

### Verifying

```bash
# DRA driver published its devices
kubectl get resourceslices

# Demo pod NICs
kubectl exec multi-nic-demo -- ip addr

# Dual-VLAN tagged-traffic + host-routing test (requires clab-deploy first)
make kind-test-vlan
```

Tunable variables (override on the `make` command line):

| Variable | Default | Purpose |
|----------|---------|---------|
| `KIND_CLUSTER_NAME` | `nic-dra-demo` | kind cluster name |
| `KIND_NODE_IMAGE` | `kindest/node:v1.35.0` | Kubernetes node image (must be ≥ v1.34) |
| `KUBE_OVN_VERSION` | `dra-nic-upstream` | kube-ovn git ref to take the chart from — the branch with `--enable-dra-nic`, not a release tag |
| `KUBE_OVN_REPO` | `../kube-ovn` | local kube-ovn checkout holding `KUBE_OVN_VERSION` |
| `KUBE_OVN_IMAGE` | `docker.io/soer3n/kube-ovn:dra-driver-<version>` | kube-ovn image built from `KUBE_OVN_VERSION` |
| `MULTUS_VERSION` | `v4.2.3` | Multus daemonset version |
| `NIC_DRIVER_NAME` | `nic.kubeovn.io` | driver name / image repo |

## Testing

| Command | Scope |
|---------|-------|
| `make test` | unit tests of all packages, plus `logcheck` |
| `make test-privileged` | `pkg/plumbing` datapath tests: veth, netns, addresses, routes, KubeVirt bridge/tap/DHCP in throwaway network namespaces (needs root; OVS is not involved) |
| `make setup-e2e test-e2e teardown-e2e` | kind e2e: ResourceSlices, allocation by kube-ovn-controller and address on the pod NIC, release on pod deletion, two NICs on one subnet, overlay ping between pods sharing a subnet |
| `E2E_CONTAINERLAB=1 make setup-e2e test-e2e` | adds the VLAN underlay spec (ping to the FRR gateway) |

kube-ovn's own `make kube-ovn-dra-e2e` covers the controller side without a
driver, using a static ResourceSlice.

## Shared subnets (multiple pods, one Subnet)

A kube-ovn Subnet is a **shared IP pool**, not an exclusive device. DRA, however,
allocates a plain `Device` **exclusively** — so until this was addressed, a
second pod selecting the same subnet stayed `Pending`. Verified live:

```
$ kubectl get pod share-a share-b
NAME       STATUS
share-a    Running       # got subnet-ovn-subnet
share-b    Pending       # FailedScheduling: 1 cannot allocate all claims
```

**Fix (driver-only).** `internal/profiles/nic` publishes each subnet device with
`AllowMultipleAllocations: true`, so many claims can allocate the same subnet
device; each pod still gets its own address, MAC and logical switch port from
kube-ovn-controller. The subnet genuinely has hundreds of IPs.

- **Feature gate:** `AllowMultipleAllocations` (and the richer consumable-capacity
  API) are gated by **`DRAConsumableCapacity`** — *alpha, default-off* in k8s
  1.34/1.35. It must be enabled on **apiserver + scheduler + kubelet**; the kind
  config (`demo/kind/kind-no-cni.yaml`) sets it.
- **Demo/test:** `demo/nic-example/examples/shared-subnet.yaml` (two pods on
  `ovn-subnet`, overlay → no containerlab) and the e2e spec
  *"connects pods that share a subnet over the overlay"*.

**`AllowMultipleAllocations` vs consumable-capacity — pick per goal:**

| | `AllowMultipleAllocations: true` (used now) | Consumable capacity (`Device.Capacity` + `RequestPolicy`) |
|---|---|---|
| Effect | unlimited sharing of the device | each request consumes 1 unit of a finite pool |
| Scheduler enforces IP exhaustion | ❌ no | ✅ yes (refuses to schedule when the pool is full) |
| Driver work | one field | publish capacity from the Subnet's free-IP count + keep it fresh |
| Same gate | `DRAConsumableCapacity` | `DRAConsumableCapacity` |

`AllowMultipleAllocations` is the minimal fix that makes shared subnets work.
Consumable-capacity is the KND-idiomatic upgrade that also makes **IP-pool
exhaustion a scheduling decision** (a genuine day-0 advantage over Multus) — the
natural next step if the driver continues. See
[`kndm-dranet-comparison.md`](kndm-dranet-comparison.md).
