# Benchmarking: DRA vs. Multus secondary-NIC spin-up

This is the exploration's central measurement: **how long does a pod take to go
from created → Ready as a function of how many secondary NICs it requests**, for
the DRA driver vs. Multus.

Multus attaches secondary NICs **serially** in the pod's CNI/sandbox-setup path
(one delegate CNI call per network), so latency grows ~linearly with NIC count.
With DRA, kube-ovn-controller allocates all NICs of the pod in one pod sync,
the driver waits for them **concurrently** in `PrepareResourceClaims` and does
the attach via an NRI hook, so it *should* scale flatter. `make
nic-bench` quantifies the difference.

> The *direction* (parallel vs. serial) is structural; the *magnitude* is what the
> benchmark exists to establish — don't quote a number until you've run it.

## What it measures

For a given NIC count `N` and mode, the benchmark deploys two **equivalent**
netshoot pods and times each from `kubectl apply` to `condition=Ready`:

- **DRA pod** — one `ResourceClaim` with `N` requests (one subnet per NIC).
- **Multus pod** — `N` `NetworkAttachmentDefinition`s + the
  `k8s.v1.cni.cncf.io/networks` annotation.

Both consume the same kube-ovn subnets, so the only variable is the attach
mechanism. (Each NIC uses a *distinct* subnet, so this does **not** need the
`DRAConsumableCapacity` gate — that's only for the shared-subnet case.)

## Prerequisites

A kind cluster with kube-ovn **and the driver**, plus **Multus** (the benchmark's
other half), and `python3` (the fixture generator).

```bash
export KUBECONFIG=~/.kube/kind-config        # ensure the context is kind-nic-dra-demo

# 1. Build and deploy the driver from this checkout:
make kind-build-driver kind-deploy-driver

# 2. Multus must be installed for the Multus side of the comparison:
make kind-deploy-multus

# 3. (fairness) pre-pull netshoot onto both nodes so timing reflects attach, not a
#    one-time image pull. Use crictl — NOT `kind load`, which corrupts the
#    multi-arch image (see docs/nic-driver.md).
for n in nic-dra-demo-control-plane nic-dra-demo-worker; do
  docker exec "$n" crictl pull docker.io/nicolaka/netshoot@sha256:47b907d662d139d1e2f22bfe14f4efca1e3f1feed283572f47c970c780c03b61
done
```

## Running it

```bash
# single count, overlay (no containerlab needed):
make nic-bench BENCH_MODE=overlay BENCH_COUNT=8

# the headline curve — DRA flat vs Multus linear across counts:
make nic-bench-sweep BENCH_COUNTS="2 4 8" BENCH_MODE=overlay

# underlay / mixed exercise the VLAN datapath — need containerlab up first:
make clab-deploy
make nic-bench-sweep BENCH_COUNTS="2 4 8" BENCH_MODE=mixed
```

Each run prints a `DRA vs Multus — create→Ready (s)` line per count.

### Knobs (Makefile variables)

| Variable | Default | Meaning |
|----------|---------|---------|
| `BENCH_MODE` | `overlay` | `overlay` \| `underlay` \| `mixed` |
| `BENCH_COUNT` | `8` | NIC count for `nic-bench` |
| `BENCH_COUNTS` | `2 4 8` | counts swept by `nic-bench-sweep` |
| `BENCH_TIMEOUT` | `300s` | per-pod Ready wait |
| `BENCH_DIR` | `$(CURDIR)/.bench` | scratch dir for generated fixtures (gitignored) |

**Count limits:** `overlay` and `underlay` cap at **8** NICs (8 subnets each);
`mixed` goes to **16** (8 underlay + 8 overlay). For 16 NICs use `BENCH_MODE=mixed`.

## How it works

`make nic-bench` calls `demo/nic-example/examples/generate.py bench --mode … --count
… --out .bench`, which emits `dra.yaml` and `multus.yaml` (and, for
underlay/mixed, the benchmark subnets). It then:

1. (underlay/mixed only) applies the bench subnets and waits until the plugin
   publishes them.
2. `measure(dra.yaml)` → apply, wait Ready, record seconds, delete.
3. `measure(multus.yaml)` → same.
4. prints the comparison.

## Interpreting results & caveats

- **Expected shape:** DRA roughly flat (bounded by one kube-ovn-controller pod
  sync + `maxPrepareConcurrency`), Multus rising ~linearly with `N`.
- **Fairness:** the bench runs DRA first, then Multus; both use netshoot. If it
  isn't pre-pulled (step 3), the first pod pays the image pull and skews that
  measurement. Pre-pull, or run the sweep twice and use the second pass.
- **Underlay/mixed needs a real gateway, not just the bridge allowlist.**
  `make clab-deploy` allows the bench VLAN ids (1101-1108, `BENCH_VLAN_BASE`
  + 1..`MAX_VLANS`) on the containerlab bridge alongside the demo's 100-800,
  *and* gives `frr-gw` a VLAN sub-interface + IP for each one
  (`10.<210+i>.0.1/24`, matching `bench_underlay()`'s declared subnet
  gateway). Both are required: a VLAN underlay subnet's L3 gateway is an
  *external* device (this project's FRR container), not an OVN-internal one
  — unlike the bridge allowlist alone, a missing gateway interface means
  kube-ovn's ARP-reachability check for the gateway never succeeds and the
  interface never comes up at all (`resolve MAC address of X timeout` /
  `network ... not ready for interface netN after N checks`) — it is not
  just "no north-south traffic." If you hit this on a topology deployed
  before this fix, re-run `make clab-deploy`.
- **It's a spin-up benchmark, not a throughput/datapath benchmark.**

See [`kndm-dranet-comparison.md`](kndm-dranet-comparison.md) for why attach timing
is the strongest concrete advantage of the DRA approach over Multus, and
[`nic-driver.md`](nic-driver.md) for the allocation and attach flow.
