# Secondary-NIC spin-up benchmark results — 2026-08-17/18

Measured with `make nic-bench-sweep` (`BENCH_REPS=10` for the final numbers below, `Makefile` extended this session to support repeated runs + median), kind cluster `nic-dra-demo` (1 control-plane + 1 worker), containerlab VLAN uplink, on a single bare-metal host (Intel NUC11TNKi5). All times are pod `create` → `Ready` wall-clock seconds. See `docs/benchmarking.md` for methodology and caveats.

> These numbers were measured with an earlier design in which the driver
> reserved the addresses itself by creating kube-ovn ip CRs. The current
> design, in which kube-ovn-controller allocates from the claim, has not been
> re-measured yet; the Multus arms are unaffected.

Three arms:

- **DRA** — one `ResourceClaim` requesting all N NICs, IPAM in parallel during `PrepareResourceClaims`.
- **Multus (static)** — N `NetworkAttachmentDefinition`s in the pod's `k8s.v1.cni.cncf.io/networks` annotation from creation, attached serially in the sandbox-setup path.
- **Multus+hotplug** — pod starts with `HOTPLUG_BASE=2` NICs; the rest are added afterward by patching the networks annotation, picked up by the public [`multus-dynamic-networks-controller`](https://github.com/k8snetworkplumbingwg/multus-dynamic-networks-controller) (Apache-2.0, deployed fresh for this session — independent of any Kubermatic-internal integration of the same tool). Reported as `create→Ready(2 NICs) + hot-plug(→N NICs) = total`.

All figures below are the **median of 10 runs** per data point; raw values are quoted inline in the notes below wherever an outlier occurred.

## Overlay (OVN, `BENCH_MODE=overlay`)

| NICs | DRA | Multus (static) | Multus+hotplug (2→N) |
|---|---|---|---|
| 2 | 7.0s | 18.0s | — |
| 4 | 8.0s | 30.0s | 19s→Ready(2) + 7s hot-plug = 26s total |
| 8 | 11.5s | 53.0s | 18.5s→Ready(2) + 30s hot-plug = 48.5s total |

No outliers across all 30 overlay reps (10 per count × 3 counts) — every DRA value landed in the 7-12s band.

## Underlay (VLAN, `BENCH_MODE=underlay`)

| NICs | DRA | Multus (static) | Multus+hotplug (2→N) |
|---|---|---|---|
| 2 | 7.5s | 18.0s | — |
| 4 | 8.0s | 31.0s | 18s→Ready(2) + 6s hot-plug = 24s total |
| 8 | 11.5s | 52.5s | 18s→Ready(2) + 30s hot-plug = 48s total |

No outliers across all 30 underlay reps — every DRA value landed in the 7-13s band.

## Mixed (half underlay + half overlay, `BENCH_MODE=mixed`)

| NICs | DRA | Multus (static) | Multus+hotplug (2→N) |
|---|---|---|---|
| 2 | 7.0s | 18.5s | — |
| 4 | 8.0s | 30.0s | 19s→Ready(2) + 6s hot-plug = 25s total |
| 8 | 11.0s | 54.0s | 18s→Ready(2) + 30s hot-plug = 48s total |

**Outliers — real, intermittent, ~1-in-15.** Unlike overlay and underlay, mixed mode threw two DRA outliers across its 30 reps: count=2 rep gave 95s (raw: 7,7,7,9,8,9,6,7,7,**95**), count=8 rep gave 76s (raw: **76**,12,11,12,11,10,12,11,11,11); count=4's 10 reps were all clean (8-10s). 2/30 ≈ 6.7%, occurring at counts 2 and 8 but not 4 — no pattern tying it to NIC count. Median is robust to a single outlier, so the table above reflects normal-case performance; the outliers are the real finding here, not the medians.

**Investigation.** An earlier pass in this session (before the 10-rep runs) saw one 87s/82s pair of outliers at counts 2 and 4, ran 8 isolated single-shot retests, saw zero recurrences, and concluded — incorrectly — that it was a one-off artifact of a busy debugging session. The 10-rep runs above prove that conclusion wrong: the anomaly is real and reproducible, just intermittent (roughly 1-in-15 mixed-mode reps), and 8 single-shot trials at that rate have a plausible (~55-65%) chance of missing it entirely by chance, which is exactly what happened.

Hardware was checked as a possible cause and substantially ruled out: `systemd-detect-virt` reports `none` (bare metal, not a VM — no hypervisor "noisy neighbor" jitter possible). A system sampler (load average + memory, 2s interval) ran continuously through the entire mixed 10-rep sweep. Across the whole run, 1-minute load average stayed under 1.0 essentially throughout (one brief early peak of 2.86 on an 8-core host, unrelated to either known outlier window) and available memory stayed flat around ~7GB the entire time — no drop, no spike, no visible correlation with either outlier. Kubernetes event retention had already rotated past the exact outlier timestamps by the time this was checked, so an exact-timestamp correlation wasn't possible, but the coarse and fine-grained sampler data across the full sweep window show nothing anomalous.

**Conclusion: root cause unidentified.** Ruled out: NIC count (hits 2 and 8, not 4), host CPU contention, host memory pressure, VM/hypervisor jitter. Most likely explanation given what's ruled out: an occasional slow round-trip somewhere in the software stack specific to the mixed-mode code path (e.g. etcd/API-server latency, a lock or serialization point in kube-ovn-controller or the driver when a claim mixes device types) — but this is speculation, not verified. Real candidate for further investigation, e.g. with driver/controller trace-level logging captured continuously (not just tailed after the fact) so a future occurrence can be inspected directly rather than reconstructed after the fact.

## Reading across all three modes

- **DRA is flat within overlay and underlay** (7-13s across 2→8 NICs, zero outliers in 60 reps) — bounded by one IPAM round-trip + `maxPrepareConcurrency`, structurally independent of N. **Mixed mode is flat on the median but has a real, intermittent tail** (~1-in-15 reps spiking to 70-100s) that overlay/underlay don't show.
- **Multus (static) grows ~linearly with N** in every mode — the serial per-NIC CNI delegate cost in the sandbox-setup path doesn't care whether the NIC is overlay or VLAN.
- **Multus+hotplug barely improves *total* time** (e.g. overlay 8 NICs: 48.5s hotplug vs. 53.0s static) — the serial per-NIC cost doesn't disappear, it relocates outside the pod's initial Ready gate. What it *does* buy is much earlier **usable-with-2-NICs** readiness (~18-19s instead of the full static wait). A decoupling win, not a throughput win — structurally why Kubermatic's `AZEdgeRouter` uses this exact pattern for incremental per-VPC transit NIC onboarding, not to make total NIC provisioning faster.

## Benchmark tooling fixes made for these runs

1. **Driver DaemonSet crash-loop on rollout-restart** — `hostNetwork: true` + fixed healthcheck port + `maxSurge: 1` meant a new pod could never bind the port before the old one died. Fixed in `deployments/helm/kube-ovn-dra-driver/values.yaml` with `maxSurge: 0, maxUnavailable: 1`.
2. **Benchmark VLANs not in the containerlab bridge allowlist** — `make clab-deploy` only allowed VLANs 100-800; bench VLANs (1101-1108) couldn't pass the bridge. Fixed in the `Makefile`'s `clab-deploy` target.
3. **Benchmark VLANs had no gateway device** — nothing answered ARP for the bench subnets' gateway IPs (VLAN underlay gateways are external). Fixed: `demo/containerlab/vlan-topology.yaml` gives `frr-gw` a sub-interface + IP per bench VLAN.
4. **`nic-bench-sweep` delete/recreate race on overlapping VLAN IDs** — each count's cleanup raced the next count's re-creation. Fixed: no auto-delete between counts; `make nic-bench-clean` cleans up deliberately.
5. **kube-ovn Subnet finalizer deadlock when its Vlan is gone** — a Subnet's deletion finalizer never clears if the referenced Vlan is deleted first. A kube-ovn bug, worked around by deleting Subnets before Vlans in `make nic-bench-clean`, or `kubectl patch ... finalizers:[]` when already stuck.

The mixed-mode DRA outlier above is still unresolved.
