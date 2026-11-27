# Device Taints and Tolerations

These examples demonstrate how DRA device taints and tolerations affect device allocation and pod eviction.

| Example | Demonstrates |
| --- | --- |
| [NoSchedule](device-taint-pod-noschedule/README.md) | Block new allocations without a matching toleration while existing pods keep running. |
| [NoExecute](device-taint-pod-noexecute/README.md) | Evict pods already using tainted devices when they have no matching toleration. |
| [Toleration](device-taint-pod-toleration/README.md) | Compare allocation behavior with and without a matching toleration. |
| [Toleration duration](device-taint-configurable-pod-eviction-time/README.md) | Compare eviction with no toleration, unlimited toleration, and a 300-second toleration. |

Each example's README lists its requirements, commands, expected behavior, and cleanup steps.

Run one example at a time and clean it up before starting another.
They share the `basic-resourceclaimtemplate` namespace and the `example` DeviceTaintRule.
