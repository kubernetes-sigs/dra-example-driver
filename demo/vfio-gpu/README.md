# Synthetic vfio-pci devices

`fake-iommu` and `fake-pci` are out-of-tree kernel modules that publish synthetic PCI devices and bind them to `vfio-pci`. The `vfio-gpu` profile only discovers devices that are already bound, so both the kind demo and KubeVirt e2e use this directory to create those devices. Real hardware bound to `vfio-pci` needs none of this.

The modules are built against the kernel they are loaded into. A `.ko` built on another machine will not load.

| File | Purpose |
| --- | --- |
| `fake-iommu/` | Software IOMMU for the synthetic PCI domain (default `0xfaca`) |
| `fake-pci/` | Synthetic PCI host bridge and devices (default vendor `0xe1a5`, device `0xd0c5`) |
| `setup-host-vfio-pci.sh` | Build, load, and bind on the current host. Use this before a kind cluster |
| `setup-node-vfio.sh` | Same steps inside a VM, plus `/dev/vfio/vfio` permissions and `allow_unsafe_interrupts` |
| `setup-fake-pci-host.sh` | Load and bind helper used by both scripts |

Kernel modules are GPL-2.0. The shell scripts are Apache-2.0.

## Kind host

```bash
sudo FAKE_PCI_DEVICES=8 bash demo/vfio-gpu/setup-host-vfio-pci.sh
ls /sys/bus/pci/drivers/vfio-pci/
```

Then create the cluster with `VFIO_GPU=true`, as described in [`demo/clusters/kind/README.md`](../clusters/kind/README.md).

## KubeVirt `k8s-*` workers

virt-launcher runs inside the provider VM, so KubeVirt copies this directory into the worker and runs `setup-node-vfio.sh` there. Do not build the modules on the cluster-up host and copy the `.ko` in.

```bash
sudo FAKE_PCI_DEVICES=8 FAKE_IOMMU=true bash setup-node-vfio.sh
```

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `FAKE_PCI_DEVICES` | `8` | Number of synthetic PCI devices |
| `FAKE_IOMMU` | `true` | Load `fake-iommu`. Required for `vfio-pci` bind |
| `FAKE_PCI_DOMAIN` | `0xfaca` | PCI domain of the synthetic bridge |
| `FAKE_PCI_VENDOR_ID` | `0xe1a5` | PCI vendor ID |
| `FAKE_PCI_DEVICE_ID` | `0xd0c5` | PCI device ID |
