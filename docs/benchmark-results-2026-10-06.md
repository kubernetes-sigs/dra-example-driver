# Secondary-NIC spin-up benchmark results — 2026-10-06

Measured with `make nic-bench-sweep BENCH_COUNTS="2 4 8" BENCH_REPS=10` for each mode, with the current design: kube-ovn-controller (`--enable-dra-nic`, branch `dra-nic-upstream`) allocates the NICs from the claims, and the driver only waits for the allocation and attaches the NICs. All times are pod `create` → `Ready` wall-clock seconds, **median of 10 runs**. See [`benchmarking.md`](benchmarking.md) for the method.

Environment: kind cluster `nic-dra-demo` (1 control-plane + 1 worker, Kubernetes 1.35) with the containerlab VLAN uplink, running in a privileged pod (11 CPUs, 24 GiB) on a homelab node with kernel 6.8. netshoot was pre-pulled on both nodes. Multus v4.2.3 (thick) and the multus-dynamic-networks-controller were installed for the Multus arms.

## Overlay (OVN, `BENCH_MODE=overlay`)

| NICs | DRA | Multus (static) | Multus+hotplug (2→N) |
|---|---|---|---|
| 2 | 7.5s | 21.0s | — |
| 4 | 9.0s | 33.0s | 21s→Ready(2) + 8s hot-plug |
| 8 | 11.0s | 59.0s | 20.5s→Ready(2) + 33.5s hot-plug |

## Underlay (VLAN, `BENCH_MODE=underlay`)

| NICs | DRA | Multus (static) | Multus+hotplug (2→N) |
|---|---|---|---|
| 2 | 8.0s | 20.0s | — |
| 4 | 9.0s | 33.0s | 21s→Ready(2) + 8s hot-plug |
| 8 | 11.0s | 58.5s | 21s→Ready(2) + 33s hot-plug |

## Mixed (half underlay + half overlay, `BENCH_MODE=mixed`)

| NICs | DRA | Multus (static) | Multus+hotplug (2→N) |
|---|---|---|---|
| 2 | 8.0s | 20.0s | — |
| 4 | 8.0s | 33.5s | 21s→Ready(2) + 8s hot-plug |
| 8 | 11.0s | 59.0s | 20s→Ready(2) + 32.5s hot-plug |

## Reading the results

- **DRA stays flat:** 7–12 s for every single one of the 90 DRA runs across all modes and NIC counts. Going from 2 to 8 NICs costs about 3 s.
- **No outliers anymore:** the 70–95 s outliers of the mixed mode in the [August run](benchmark-results-2026-08-17.md) did not occur in any of the 30 mixed runs. That run used the earlier design, in which the driver created the ip CRs itself; with kube-ovn-controller allocating all NICs of the pod in one sync, the tail is gone.
- **Same medians as the old design:** 7.0 / 8.0 / 11.5 s (overlay, August) against 7.5 / 9.0 / 11.0 s now, on different hardware. Moving the allocation into kube-ovn-controller cost no spin-up time.
- **Multus grows linearly:** about 13 s per additional pair of NICs, in every mode, because the CNI delegates run serially in the sandbox setup.
- **Multus+hotplug** gets the pod Ready with 2 NICs after ~21 s, but the total for 8 NICs (~53 s) is close to the static case.

The numbers are not comparable one to one with the August run (different host); compare the ratios between the arms.
