# kube-ovn NIC DRA Driver

A [Dynamic Resource Allocation
(DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
driver for **kube-ovn virtual NICs**. It publishes each kube-ovn `Subnet` on a
node as a DRA device, so a workload can request a secondary network interface
(`net1`, `net2`, …) through a `ResourceClaim` instead of a Multus annotation.

> Forked from
> [`kubernetes-sigs/dra-example-driver`](https://github.com/kubernetes-sigs/dra-example-driver)
> and rewritten around a single `nic` device profile.

> 📖 **New here?** Jump to [Documentation](#documentation) for a map of the design
> docs and the "is this the right approach?" decision aids before diving in.

## How it works

kube-ovn-controller, started with the experimental `--enable-dra-nic` flag,
treats every device this driver allocates as a pod network attachment, the same
way it treats a Multus one. The driver does not allocate anything itself:

1. The kubelet plugin publishes every kube-ovn `Subnet` as a device in the
   node's ResourceSlice (attribute `subnetName`).
2. The scheduler allocates a device to the pod's `ResourceClaim`.
3. kube-ovn-controller resolves device → subnet → the subnet's provider,
   allocates IP and MAC, creates the logical switch port and ip CR, and writes
   the `<provider>.kubernetes.io/*` pod annotations.
4. In `NodePrepareResources`, the driver waits for
   `<provider>.kubernetes.io/allocated` and reads the result.
5. An NRI hook (`RunPodSandbox`) creates a veth into the pod (`net1`, …),
   configures IP, MAC and routes, and attaches the host end to `br-int` with
   `external_ids:iface-id` set to the logical switch port. OVN binds it.
6. For KubeVirt VMs the hook also adds a bridge, a tap device and a small
   DHCP server in the pod; the
   [kube-ovn network binding plugin](https://github.com/soer3n/kube-ovn-network-binding-plugin)
   hands the tap to the VM.

On pod deletion the NRI `StopPodSandbox` hook removes the interface, and
kube-ovn-controller releases the address, or keeps it for a VM that still
exists. Overlay and VLAN underlay subnets work the same way; an underlay
subnet reaches its VLAN through kube-ovn's localnet port.

The contract between kube-ovn and the driver is described in kube-ovn's
`docs/dra-nic.md`. See [`docs/nic-driver.md`](docs/nic-driver.md) for the full
design.

> **Status:** experimental. The kube-ovn side is proposed upstream and not in a
> kube-ovn release yet.

## Requirements

- **kube-ovn with `--enable-dra-nic`:** branch `dra-nic-upstream` of
  [github.com/soer3n/kube-ovn](https://github.com/soer3n/kube-ovn), proposed
  upstream. Install it with `ENABLE_DRA_NIC=true` (`install.sh`),
  `func.ENABLE_DRA_NIC=true` (chart) or `features.enableDraNic=true` (v2 chart).
  Every subnet used for DRA NICs needs a dedicated provider, e.g.
  `<subnet>.<namespace>.ovn`; the default provider `ovn` belongs to `eth0`.
- **Kubernetes 1.34+** for the stable DRA API (`resource.k8s.io/v1`); built and
  tested against 1.35. Several pods sharing one subnet device need the
  `DRAConsumableCapacity` feature gate.
- **containerd with NRI** (enabled by default in containerd 2.x).

## Quickstart (kind)

A full kube-ovn + NIC DRA stack can be brought up in a local
[kind](https://kind.sigs.k8s.io/) cluster. All targets are under the `kind-*` /
`clab-*` prefixes in the `Makefile`. kube-ovn is deployed from the chart of
`KUBE_OVN_VERSION` (default `dra-nic-upstream`) in the sibling checkout
`../kube-ovn`, with an image built from that branch (see the `KUBE_OVN_*`
variables in the `Makefile`).

```bash
# Create cluster, wire the VLAN uplink via containerlab + FRR, deploy kube-ovn
# (+ Multus for the benchmark), build & load the driver image, install the
# chart, apply the NIC example.
make kind-demo

# Tear down
make kind-delete
```

Step by step:

```bash
make kind-create             # kind cluster, no CNI, DRA feature-gates on
make clab-deploy             # OPTIONAL: containerlab VLAN uplink + FRR BGP gateway (needs sudo)
make kind-deploy-kube-ovn    # kube-ovn CNI with --enable-dra-nic -> nodes Ready
make kind-deploy-multus      # OPTIONAL: Multus, only for the DRA vs Multus benchmark
make kind-deploy-nic-prereqs # provider network, VLANs and subnets (before the driver)
make kind-build-driver       # docker build -> kind load
make kind-deploy-driver      # helm install (deviceProfile=nic)
make kind-deploy-nic-example # ResourceClaim + demo pod
make kind-test-vlan          # dual-VLAN traffic + host-routing + isolation checks (needs clab-deploy)
```

See [`docs/nic-driver.md`](docs/nic-driver.md) for the containerlab topology, the VLAN
underlay vs OVN overlay device types, and the tunable `make` variables.

## Install with Helm

```bash
helm install kube-ovn-dra-driver deployments/helm/kube-ovn-dra-driver \
  --namespace kube-system \
  --set deviceProfile=nic
# driverName defaults to "nic.kubeovn.io", which is also kube-ovn-controller's
# default --dra-nic-driver-name. The optional validating webhook is behind
# --set webhook.enabled=true.
```

## Requesting a NIC

The driver publishes one `subnet-<name>` device per kube-ovn `Subnet`, with
attributes under the `nic.kubeovn.io/*` domain (`subnetName`, `subnetType`,
`vlanId`, `providerNetwork`, `provider`, `vpc`). A pod selects the subnet it
wants with a CEL selector on its `ResourceClaim`, and names the interface with
the optional `NicConfig` (default `net1`, at most 15 characters):

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  name: my-nic
spec:
  devices:
    requests:
      - name: nic
        exactly:
          deviceClassName: nic.kubeovn.io
          selectors:
            - cel:
                expression: "device.attributes['nic.kubeovn.io'].subnetName == 'vlan100-subnet'"
          count: 1
    config:
      - requests: ["nic"]
        opaque:
          driver: nic.kubeovn.io
          parameters:
            apiVersion: nic.resource.kube-ovn.io/v1alpha1
            kind: NicConfig
            interfaceName: net1
```

Several NICs of one pod may use the same subnet as long as their interface
names differ; kube-ovn then keys each NIC by `<provider>.<interfaceName>`, as
for repeated Multus attachments. Until the CDI device naming is fixed, put such
NICs in separate claims (see
[`same-subnet-2nic.yaml`](demo/nic-example/examples/same-subnet-2nic.yaml)).

Worked examples live in [`demo/nic-example/`](demo/nic-example/): a one-underlay
+ one-overlay starting claim, shared-subnet and same-subnet fixtures, plus
scaling fixtures for 2/4/8/16 NICs under
[`demo/nic-example/examples/`](demo/nic-example/examples/) (regenerate the
generated ones with `examples/generate.py`):

```bash
make nic-example-deploy COUNT=4   # 2 VLAN underlay + 2 OVN overlay NICs
```

## Layout

| Path | Purpose |
|------|---------|
| `cmd/kube-ovn-dra-kubeletplugin/` | DRA kubelet plugin (DaemonSet) — publishes ResourceSlices, prepares claims, NRI hook |
| `cmd/kube-ovn-dra-webhook/` | Validating admission webhook for `NicConfig` opaque config |
| `internal/profiles/nic/` | The `nic` device profile — enumerates kube-ovn Subnets |
| `api/kube-ovn.io/resource/nic/v1alpha1/` | `NicConfig` opaque-config type |
| `pkg/annotation/` | Reads kube-ovn-controller's allocation from the pod annotations |
| `pkg/nicprepare/` | Resolves a claimed NIC's allocation and logical switch port |
| `pkg/plumbing/` | Veth/OVS/netns attach, KubeVirt tap + DHCP, NRI sandbox handler |
| `deployments/helm/kube-ovn-dra-driver/` | Helm chart |
| `demo/` | kind + containerlab/FRR demo stack and NIC examples |
| `docs/` | Architecture and design notes |

## Documentation

**Start here** — a map of the repo's docs, by what you're trying to do:

*Understand / run the driver:*
- [`docs/nic-driver.md`](docs/nic-driver.md) — the main design doc: allocation
  and attach flow, device attributes, shared subnets, kind/VLAN demo, testing.
- [`docs/architecture.md`](docs/architecture.md) — code walk-through.
- [`docs/benchmarking.md`](docs/benchmarking.md) — `make nic-bench`: DRA vs. Multus
  secondary-NIC spin-up (the attach-timing measurement).

*Evaluate the direction (decision aids — read these if you're asking "is this the
right approach?"):*
- [`docs/kndm-dranet-comparison.md`](docs/kndm-dranet-comparison.md) — this driver
  vs. KNDM/DraNet/SR-IOV/ovn-kubernetes OKEP; "can we just use DraNet/Multus?";
  is SDN in scope for DRA?; hot-plug; strategic options.
- [`docs/multi-network-api-integration.md`](docs/multi-network-api-integration.md)
  — the target architecture: **multi-network-api + DRA**, with this driver as the
  DRA backend (network API owns *which* network; DRA owns *allocate + attach*).

## Testing

```bash
make test               # unit tests (with logcheck)
make test-privileged    # pkg/plumbing datapath tests in network namespaces (sudo)
make setup-e2e test-e2e # kind e2e; E2E_CONTAINERLAB=1 adds the VLAN underlay specs
make teardown-e2e
```

## License

Apache 2.0 — see [`LICENSE`](LICENSE).
