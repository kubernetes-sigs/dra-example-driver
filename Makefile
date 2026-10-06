# Copyright 2023 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

CONTAINER_TOOL ?= docker
MKDIR    ?= mkdir
TR       ?= tr
DIST_DIR ?= $(CURDIR)/dist
HELM     ?= helm

export IMAGE_GIT_TAG ?= $(shell git describe --tags --always --dirty --match 'v*')
export CHART_GIT_TAG ?= $(shell git describe --tags --always --dirty --match 'chart/*')

include $(CURDIR)/common.mk

BUILDIMAGE_TAG ?= golang$(GOLANG_VERSION)
BUILDIMAGE ?= $(IMAGE_NAME)-build:$(BUILDIMAGE_TAG)

CMDS := $(patsubst ./cmd/%/,%,$(sort $(dir $(wildcard ./cmd/*/))))
CMD_TARGETS := $(patsubst %,cmd-%, $(CMDS))

CHECK_TARGETS := assert-fmt vet lint ineffassign misspell
MAKE_TARGETS := binaries build check fmt test examples cmds coverage generate $(CHECK_TARGETS)

TARGETS := $(MAKE_TARGETS) $(CMD_TARGETS)

DOCKER_TARGETS := $(patsubst %,docker-%, $(TARGETS))
.PHONY: $(TARGETS) $(DOCKER_TARGETS)

GOOS ?= linux

binaries: cmds
ifneq ($(PREFIX),)
cmd-%: COMMAND_BUILD_OPTIONS = -o $(PREFIX)/$(*)
endif
cmds: $(CMD_TARGETS)
$(CMD_TARGETS): cmd-%:
	CGO_LDFLAGS_ALLOW='-Wl,--unresolved-symbols=ignore-in-object-files' GOOS=$(GOOS) \
		go build -ldflags "-s -w -X main.version=$(VERSION)" $(COMMAND_BUILD_OPTIONS) $(MODULE)/cmd/$(*)

build:
	GOOS=$(GOOS) go build ./...

examples: $(EXAMPLE_TARGETS)
$(EXAMPLE_TARGETS): example-%:
	GOOS=$(GOOS) go build ./examples/$(*)

all: check test build binary
check: $(CHECK_TARGETS)

# Apply go fmt to the codebase
fmt:
	go list -f '{{.Dir}}' $(MODULE)/... \
		| xargs gofmt -s -l -w

assert-fmt:
	go list -f '{{.Dir}}' $(MODULE)/... \
		| xargs gofmt -s -l > fmt.out
	@if [ -s fmt.out ]; then \
		echo "\nERROR: The following files are not formatted:\n"; \
		cat fmt.out; \
		rm fmt.out; \
		exit 1; \
	else \
		rm fmt.out; \
	fi

ineffassign:
	ineffassign $(MODULE)/...

lint:
	golangci-lint run ./...

misspell:
	misspell $(MODULE)/...

vet:
	go vet $(MODULE)/...

# Ensure that all log calls support contextual logging.
test: logcheck
.PHONY: logcheck
logcheck:
	(cd hack/tools && GOBIN=$(PWD) go install sigs.k8s.io/logtools/logcheck)
	./logcheck -check-contextual -check-deprecations ./...

COVERAGE_FILE := coverage.out
test: build cmds
	go test -v -coverprofile=$(COVERAGE_FILE) $(MODULE)/...

# The datapath tests in pkg/plumbing create network namespaces and need root;
# they skip otherwise.
.PHONY: test-privileged
test-privileged:
	go test -c -o $(CURDIR)/plumbing.test ./pkg/plumbing
	sudo $(CURDIR)/plumbing.test -test.v
	rm -f $(CURDIR)/plumbing.test

coverage: test
	cat $(COVERAGE_FILE) | grep -v "_mock.go" > $(COVERAGE_FILE).no-mocks
	go tool cover -func=$(COVERAGE_FILE).no-mocks

generate: generate-deepcopy

generate-deepcopy:
	for api in $(APIS); do \
		rm -f $(CURDIR)/api/$(VENDOR)/resource/$${api}/zz_generated.deepcopy.go; \
		controller-gen \
			object:headerFile=$(CURDIR)/hack/boilerplate.generatego.txt \
			paths=$(CURDIR)/api/$(VENDOR)/resource/$${api}/ \
			output:object:dir=$(CURDIR)/api/$(VENDOR)/resource/$${api}; \
	done

###############################################################################
# kind + kube-ovn + multus + NIC DRA driver demo
###############################################################################

KIND_CLUSTER_NAME ?= nic-dra-demo
KIND_CONFIG       ?= $(CURDIR)/demo/kind/kind-no-cni.yaml
NIC_DEMO_DIR      ?= $(CURDIR)/demo/nic-example

# Kubernetes node image. The stable DRA API (resource.k8s.io/v1) requires
# Kubernetes 1.34+; 1.35 is what this repo is built/tested against (k8s.io/*
# deps pinned to v0.35.x). kind v0.31.0 already defaults to v1.35.0, so this is
# only needed to pin a version on older/newer kind binaries. Override to test a
# different release, e.g. KIND_NODE_IMAGE=kindest/node:v1.34.3.
KIND_NODE_IMAGE   ?= kindest/node:v1.35.0

# kube-ovn ref to deploy the chart from. DRA NICs need kube-ovn-controller with
# --enable-dra-nic, which is not in a kube-ovn release yet; the default is the
# branch of the upstream pull request. A bare release tag also does not resolve
# against a typical local clone (tags aren't fetched by default) and silently
# falls through to curl'ing the release tarball below.
KUBE_OVN_VERSION  ?= dra-nic-upstream
# Local kube-ovn clone used to source the Helm chart offline (avoids fetching
# the whole source tarball from GitHub). The chart is read from the
# $(KUBE_OVN_VERSION) git ref; if the clone/ref is absent, the deploy falls back
# to curl'ing the GitHub release tarball (this fallback only makes sense for an
# actual upstream release tag, not the branch above — see KUBE_OVN_REPO).
KUBE_OVN_REPO     ?= $(CURDIR)/../kube-ovn
# Multus version (thick CNI daemonset).
MULTUS_VERSION    ?= v4.2.3

# Images preloaded into the kind cluster so the demo never has to pull them at
# runtime (mirrors the edge-router e2e image preload in kubermatic-virtualization).
#
# The demo deploys a kube-ovn image built from $(KUBE_OVN_VERSION), not the
# upstream release, which lacks --enable-dra-nic. By default it pulls
# docker.io/soer3n/kube-ovn:dra-driver-<version>. To use a local build instead,
# override the vars, e.g.
#   cd ../kube-ovn && make build-go && docker build -t docker.io/kubeovn/kube-ovn:dev -f dist/images/Dockerfile dist/images/
#   make ... KUBE_OVN_REGISTRY=docker.io/kubeovn KUBE_OVN_IMAGE_TAG=dev
# netshoot is pinned by digest.
KUBE_OVN_REGISTRY   ?= docker.io/soer3n
KUBE_OVN_IMAGE_REPO ?= kube-ovn
KUBE_OVN_IMAGE_TAG  ?= dra-driver-$(KUBE_OVN_VERSION:v%=%)
KUBE_OVN_IMAGE      ?= $(KUBE_OVN_REGISTRY)/$(KUBE_OVN_IMAGE_REPO):$(KUBE_OVN_IMAGE_TAG)
NETSHOOT_IMAGE    ?= docker.io/nicolaka/netshoot@sha256:47b907d662d139d1e2f22bfe14f4efca1e3f1feed283572f47c970c780c03b61
# Only the kube-ovn image is preloaded: it is single-arch, so `docker pull` +
# `kind load docker-image` (docker save) handles it cleanly, and preloading pins
# the exact DRA-patched build.
#
# netshoot is deliberately NOT preloaded. It is a public, multi-arch,
# digest-pinned image, and `kind load docker-image` (docker save of a manifest
# list) corrupts containerd's image store ("import-<date>", missing platform
# manifest -> CreateContainerError when a pod tries to start). The nodes have
# registry access, so pods pull netshoot at runtime. To warm it for benchmark
# timing, pull single-arch onto each node first, e.g.
#   docker pull --platform linux/amd64 $(NETSHOOT_IMAGE) && \
#   kind load docker-image --name $(KIND_CLUSTER_NAME) $(NETSHOOT_IMAGE)
PRELOAD_IMAGES    ?= $(KUBE_OVN_IMAGE)

# NIC driver name as registered in the DeviceClass.
NIC_DRIVER_NAME   ?= nic.kubeovn.io

# Benchmark knobs: compare DRA vs Multus secondary-NIC spin-up.
BENCH_MODE         ?= overlay
BENCH_COUNT        ?= 8
BENCH_COUNTS       ?= 2 4 8
BENCH_DIR          ?= $(CURDIR)/.bench
BENCH_TIMEOUT      ?= 300s
BENCH_REPS         ?= 1
# NICs the Multus hot-plug benchmark arm starts with before hot-plugging the
# rest via multus-dynamic-networks-controller (not deployed by kind-demo; see
# https://github.com/k8snetworkplumbingwg/multus-dynamic-networks-controller).
HOTPLUG_BASE       ?= 2
PLUGIN_DS_SELECTOR ?= app.kubernetes.io/instance=kube-ovn-nic-dra

CONTAINERLAB_TOPOLOGY ?= $(CURDIR)/demo/containerlab/vlan-topology.yaml

.PHONY: kind-create kind-delete kind-preload-images kind-deploy-kube-ovn kind-deploy-multus \
        kind-kube-ovn-status kind-test-kube-ovn kind-test-kube-ovn-clean \
        kind-build-driver kind-deploy-driver kind-deploy-nic-prereqs kind-deploy-nic-example \
        kind-demo nic-example-deploy nic-example-clean kind-check-deps \
        kind-deploy-kube-ovn-fixtures kind-vlan-tag-workaround \
        kind-test-vlan kind-deploy-vlan-peer \
        clab-deploy clab-destroy clab-clean-ports kind-frr-update-peers kind-frr-status \
        kind-vlan-nat

## kind-check-deps: verify required tools are installed.
kind-check-deps:
	@for tool in kind kubectl helm docker curl tar; do \
		command -v $$tool >/dev/null 2>&1 || { echo "ERROR: '$$tool' not found in PATH"; exit 1; }; \
	done
	@echo "All required tools found."

## clab-deploy: wire eth1 into kind nodes via containerlab + start FRR gateway.
## Requires containerlab and sudo. Run after kind-create, before kind-deploy-kube-ovn.
clab-deploy: clab-clean-ports
	@echo "Recreating Linux bridge 'vlan-switch' on host..."
	sudo ip link delete vlan-switch 2>/dev/null || true
	sudo ip link add name vlan-switch type bridge
	sudo ip link set vlan-switch up
	@echo "Enabling VLAN filtering on bridge so 802.1q tags pass through..."
	sudo ip link set vlan-switch type bridge vlan_filtering 1
	@echo "Injecting node IPs into FRR config..."
	$(MAKE) kind-frr-update-peers
	@echo "Deploying containerlab VLAN topology + FRR gateway (requires sudo)..."
	sudo containerlab deploy -t $(CONTAINERLAB_TOPOLOGY) --reconfigure
	@echo "Allowing demo VLANs (100-800) and benchmark VLANs (1101-1108) on bridge ports..."
	# 1101-1108 = demo/nic-example/examples/generate.py's BENCH_VLAN_BASE(1100) + 1..MAX_VLANS(8);
	# without these, a bench underlay pod's interface never comes up (not just "no traffic" as
	# docs/benchmarking.md's caveat implies) since kube-ovn can never bind an untagged VLAN.
	for vid in 100 200 300 400 500 600 700 800 1101 1102 1103 1104 1105 1106 1107 1108; do \
		for p in port1 port2 port3; do \
			sudo bridge vlan add vid $$vid dev $$p; \
		done; \
	done
	@echo "FRR gateway is up. BGP sessions will establish once kube-ovn-speaker starts."
	@echo "Check BGP: make kind-frr-status"
	$(MAKE) kind-vlan-nat

## kind-vlan-nat: SNAT the demo VLAN subnets (172.23-172.30.0.0/24) out the
## host's own default-route interface, so a VM attached to one of them (e.g.
## via vlan100-subnet) can reach the real internet, not just other hosts on
## the same VLAN trunk. FRR (clab-deploy above) only handles BGP-advertised
## ROUTING between the fabric and the host — it does not NAT — and these
## subnets are RFC1918 space with no other path out. Idempotent: skips a
## rule that's already present, so repeat `make clab-deploy` runs don't pile
## up duplicates. Run automatically by clab-deploy; safe to re-run standalone
## after a host reboot (iptables rules are NOT persisted across reboots by
## this target — install iptables-persistent or similar if you need that).
kind-vlan-nat:
	$(eval EGRESS_IF := $(shell ip route show default | awk '/^default/{print $$5; exit}'))
	@if [ -z "$(EGRESS_IF)" ]; then \
		echo "ERROR: could not determine the host's default-route interface"; \
		exit 1; \
	fi
	@echo "SNATting VLAN demo subnets out $(EGRESS_IF)..."
	@for subnet in 172.23.0.0/24 172.24.0.0/24 172.25.0.0/24 172.26.0.0/24 \
	               172.27.0.0/24 172.28.0.0/24 172.29.0.0/24 172.30.0.0/24; do \
		sudo iptables -t nat -C POSTROUTING -o $(EGRESS_IF) -s $$subnet -j MASQUERADE 2>/dev/null || \
			sudo iptables -t nat -A POSTROUTING -o $(EGRESS_IF) -s $$subnet -j MASQUERADE; \
	done
	@echo "Done."

## kind-frr-update-peers: inject actual node IPs into the FRR config before deploy.
## Run automatically by clab-deploy; also useful after a cluster recreate.
kind-frr-update-peers:
	$(eval CP_IP := $(shell kubectl get node \
		-l node-role.kubernetes.io/control-plane \
		-o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null))
	$(eval W_IP := $(shell kubectl get node \
		-l '!node-role.kubernetes.io/control-plane' \
		-o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null))
	@if [ -z "$(CP_IP)" ] || [ -z "$(W_IP)" ]; then \
		echo "ERROR: Could not resolve node IPs. Is the kind cluster running?"; \
		exit 1; \
	fi
	@echo "  control-plane: $(CP_IP)   worker: $(W_IP)"
	sed \
		-e 's/CONTROL_PLANE_IP/$(CP_IP)/g' \
		-e 's/WORKER_IP/$(W_IP)/g' \
		$(CURDIR)/demo/containerlab/frr/frr.conf \
		> /tmp/frr-rendered.conf
	sudo cp /tmp/frr-rendered.conf \
		$(CURDIR)/demo/containerlab/clab-nic-dra-demo-vlan/frr-gw/etc/frr/frr.conf 2>/dev/null || true
	@echo "FRR config updated with node IPs."

## kind-frr-status: show BGP session state and learned routes in FRR.
kind-frr-status:
	@echo "=== BGP summary ==="
	sudo docker exec clab-nic-dra-demo-vlan-frr-gw vtysh -c "show bgp summary" 2>/dev/null || \
		echo "  (frr-gw container not running)"
	@echo ""
	@echo "=== BGP learned routes ==="
	sudo docker exec clab-nic-dra-demo-vlan-frr-gw vtysh -c "show bgp ipv4 unicast" 2>/dev/null || true
	@echo ""
	@echo "=== Host routing table (VLAN subnets installed by FRR/zebra) ==="
	ip route show | grep -E '172\.(2[3-9]|30)\.' || echo "  (no VLAN routes yet — BGP may not be established)"

## clab-clean-ports: delete stale host-side veth ports from a previous deploy.
## containerlab names the host end of each link after the topology endpoint
## (vlan-switch:port1, :port2, :port3). Because the kind nodes are ext-containers
## whose lifecycle containerlab does not own, a teardown (including the implicit
## destroy in `deploy --reconfigure`) can leave these veths orphaned in the root
## netns. The next deploy then fails with "interface ... already exists".
## Deleting a veth removes both ends, clearing the matching eth1 in the kind node.
clab-clean-ports:
	@echo "Removing any stale containerlab veth ports (port1-3)..."
	@for p in port1 port2 port3; do \
		sudo ip link delete $$p 2>/dev/null && echo "  deleted $$p" || true; \
	done

## clab-destroy: remove the containerlab VLAN topology and FRR gateway.
clab-destroy:
	@echo "Destroying containerlab VLAN topology (requires sudo)..."
	sudo containerlab destroy -t $(CONTAINERLAB_TOPOLOGY) --cleanup
	@echo "Removing host bridge 'vlan-switch'..."
	sudo ip link delete vlan-switch 2>/dev/null || true
	$(MAKE) clab-clean-ports

## kind-create: create a kind cluster with no CNI and DRA feature-gates enabled.
kind-create: kind-check-deps
	kind create cluster --name $(KIND_CLUSTER_NAME) --config $(KIND_CONFIG) --image $(KIND_NODE_IMAGE)
	@echo "Waiting for API server to be reachable..."
	until kubectl cluster-info --context kind-$(KIND_CLUSTER_NAME) >/dev/null 2>&1; do sleep 2; done
	@echo "Waiting for control-plane static pods (apiserver, scheduler, etcd)..."
	kubectl -n kube-system wait --for=condition=Ready pod \
		-l tier=control-plane --timeout=120s
	@echo "NRI is enabled by default in containerd v2.x (kind v1.35+) — no patching needed."
	@echo "NOTE: nodes will remain NotReady until kube-ovn is deployed (expected)."

## kind-delete: destroy the demo cluster.
kind-delete:
	kind delete cluster --name $(KIND_CLUSTER_NAME)

## kind-preload-images: pull (if missing) and load kube-ovn + netshoot images into the
## kind cluster so deployment and demo pods never pull from a remote registry at runtime.
kind-preload-images:
	@set -e; \
	for img in $(PRELOAD_IMAGES); do \
		if $(CONTAINER_TOOL) image inspect "$$img" >/dev/null 2>&1; then \
			echo "Image already present locally: $$img"; \
		else \
			echo "Pulling $$img ..."; \
			$(CONTAINER_TOOL) pull "$$img"; \
		fi; \
		echo "Loading $$img into kind cluster $(KIND_CLUSTER_NAME) ..."; \
		kind load docker-image "$$img" --name $(KIND_CLUSTER_NAME); \
	done
	@echo "Preloaded images: $(PRELOAD_IMAGES)"

## kind-deploy-kube-ovn: install kube-ovn CNI into the demo cluster via its own Helm chart.
kind-deploy-kube-ovn: kind-preload-images
	@set -e; \
	MASTER_NODE=$$(kubectl get nodes -l node-role.kubernetes.io/control-plane \
		-o jsonpath='{.items[0].metadata.name}'); \
	MASTER_IP=$$(kubectl get nodes -l node-role.kubernetes.io/control-plane \
		-o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'); \
	echo "Control-plane node: $$MASTER_NODE ($$MASTER_IP)"; \
	echo "Labelling control-plane node with kube-ovn/role=master ..."; \
	kubectl label node $$MASTER_NODE kube-ovn/role=master --overwrite; \
	CHART_TMP=$$(mktemp -d); \
	if git -C "$(KUBE_OVN_REPO)" cat-file -e "$(KUBE_OVN_VERSION):charts/kube-ovn/Chart.yaml" 2>/dev/null; then \
		echo "Extracting kube-ovn chart $(KUBE_OVN_VERSION) from local repo $(KUBE_OVN_REPO) ..."; \
		git -C "$(KUBE_OVN_REPO)" archive "$(KUBE_OVN_VERSION)" charts/kube-ovn \
			| tar -x -C $$CHART_TMP --strip-components=2; \
	else \
		echo "Local chart not found; fetching kube-ovn chart $(KUBE_OVN_VERSION) from GitHub into $$CHART_TMP ..."; \
		curl -fsSL https://github.com/kubeovn/kube-ovn/archive/refs/tags/$(KUBE_OVN_VERSION).tar.gz \
			| tar -xz -C $$CHART_TMP --strip-components=3 "kube-ovn-$(KUBE_OVN_VERSION:v%=%)/charts/kube-ovn"; \
	fi; \
	$(HELM) upgrade --install kube-ovn $$CHART_TMP \
		--namespace kube-system \
		--set global.registry.address=$(KUBE_OVN_REGISTRY) \
		--set global.images.kubeovn.repository=$(KUBE_OVN_IMAGE_REPO) \
		--set global.images.kubeovn.tag=$(KUBE_OVN_IMAGE_TAG) \
		--set global.images.pullPolicy=IfNotPresent \
		--set MASTER_NODES=$$MASTER_IP \
		--set networking.NET_STACK=ipv4 \
		--set ipv4.POD_CIDR=10.244.0.0/16 \
		--set ipv4.POD_GATEWAY=10.244.0.1 \
		--set ipv4.SVC_CIDR=10.96.0.0/12 \
		--set ipv4.JOIN_CIDR=100.64.0.0/16 \
		--set func.ENABLE_DRA_NIC=true \
		--timeout=300s --wait; \
	rm -rf $$CHART_TMP
	@echo "Waiting for nodes to become Ready (CNI is now up)..."
	kubectl wait --for=condition=Ready node --all --timeout=180s

## kind-kube-ovn-status: show kube-ovn component health and the default subnet.
kind-kube-ovn-status:
	@echo "=== kube-ovn pods ==="
	kubectl -n kube-system get pods -l app.kubernetes.io/part-of=kube-ovn -o wide 2>/dev/null \
		|| kubectl -n kube-system get pods | grep -E 'ovn|kube-ovn'
	@echo ""
	@echo "=== Nodes ==="
	kubectl get nodes -o wide
	@echo ""
	@echo "=== Subnets ==="
	kubectl get subnets 2>/dev/null || echo "  (kube-ovn CRDs not installed yet)"

## kind-test-kube-ovn: smoke-test the kube-ovn overlay with two pods + pod-to-pod ping.
## Run after kind-deploy-kube-ovn to confirm the CNI works before adding the NIC driver.
kind-test-kube-ovn:
	@echo "Creating two test pods on the default ovn overlay subnet..."
	kubectl apply -f $(CURDIR)/demo/kind/kube-ovn-smoke-test.yaml
	@echo "Waiting for test pods to be Ready..."
	kubectl wait --for=condition=Ready pod/kube-ovn-test-a pod/kube-ovn-test-b --timeout=120s
	$(eval B_IP := $(shell kubectl get pod kube-ovn-test-b -o jsonpath='{.status.podIP}'))
	@echo "Pinging kube-ovn-test-b ($(B_IP)) from kube-ovn-test-a..."
	kubectl exec kube-ovn-test-a -- ping -c 4 -W 2 $(B_IP) && \
		echo "  ✓ kube-ovn overlay pod-to-pod connectivity OK" || \
		echo "  ✗ kube-ovn pod-to-pod ping FAILED"

## kind-test-kube-ovn-clean: remove the kube-ovn smoke-test pods.
kind-test-kube-ovn-clean:
	kubectl delete pod -l app=kube-ovn-test --ignore-not-found

## kind-deploy-multus: install Multus CNI in thick mode.
kind-deploy-multus:
	@echo "Installing Multus $(MULTUS_VERSION) (thick)..."
	kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/$(MULTUS_VERSION)/deployments/multus-daemonset-thick.yml
	@echo "Waiting for multus to be ready..."
	kubectl -n kube-system wait --for=condition=Ready pod -l app=multus --timeout=120s

## kind-build-driver: build the NIC DRA driver image and load it into the kind cluster.
kind-build-driver:
	$(CONTAINER_TOOL) build \
		--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
		-t $(NIC_DRIVER_NAME):dev \
		-f $(CURDIR)/Dockerfile \
		$(CURDIR)
	kind load docker-image $(NIC_DRIVER_NAME):dev --name $(KIND_CLUSTER_NAME)

## kind-deploy-driver: install the NIC DRA kubelet-plugin via Helm.
## Tolerates the control-plane's NoSchedule taint so the demo's 2-node kind
## cluster gets a ResourceSlice on BOTH nodes — needed for fixtures that pin
## pods to the control-plane (07-vlan-peer-pod.yaml, 08-ovn-peer-pod.yaml),
## which otherwise stay Pending ("cannot allocate all claims": no device
## published for that node).
kind-deploy-driver:
	$(HELM) upgrade --install kube-ovn-nic-dra \
		$(CURDIR)/deployments/helm/kube-ovn-dra-driver \
		--namespace kube-system \
		--set deviceProfile=nic \
		--set driverName=$(NIC_DRIVER_NAME) \
		--set image.repository=$(NIC_DRIVER_NAME) \
		--set image.tag=dev \
		--set image.pullPolicy=Never \
		--set-json 'kubeletPlugin.tolerations=[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists","effect":"NoSchedule"}]' \
		--wait

## kind-deploy-kube-ovn-fixtures: apply the kube-ovn underlay fixtures
## (ProviderNetwork, VLANs and the VLAN/overlay Subnets) and wait for reconcile.
kind-deploy-kube-ovn-fixtures:
	@echo "Applying kube-ovn provider network, VLANs and subnets..."
	kubectl apply -f $(NIC_DEMO_DIR)/00-kube-ovn-subnets.yaml
	@echo "Waiting 10s for subnet controllers to reconcile..."
	sleep 10
	@$(MAKE) --no-print-directory kind-vlan-tag-workaround

## kind-vlan-tag-workaround: set tag_request on the localnet port of every VLAN
## subnet. OVN 26.03's northd derives Logical_Switch_Port.tag from tag_request,
## and kube-ovn only writes tag, so VLAN subnets lose their 802.1q tag.
kind-vlan-tag-workaround:
	@set -e; \
	ip=$$(kubectl -n kube-system get pod -l app=ovn-central -o jsonpath='{.items[0].status.podIP}'); \
	for pair in $$(kubectl get subnets -o jsonpath='{range .items[?(@.spec.vlan)]}{.metadata.name}={.spec.vlan}{"\n"}{end}'); do \
		subnet=$${pair%%=*}; id=$$(kubectl get vlan $${pair#*=} -o jsonpath='{.spec.id}'); \
		for i in $$(seq 30); do \
			kubectl -n kube-system exec deploy/ovn-central -c ovn-central -- \
				ovn-nbctl --db=tcp:$$ip:6641 set logical_switch_port localnet.$$subnet tag_request=$$id 2>/dev/null && break; \
			sleep 2; \
		done; \
		echo "  localnet.$$subnet tag_request=$$id"; \
	done

## kind-deploy-nic-prereqs: apply the subnets the driver and any NIC claim
## depend on — but NOT a specific ResourceClaim/pod. The
## DeviceClass itself (nic.kubeovn.io) is Helm-managed by kind-deploy-driver,
## not applied here.
## Deploy this BEFORE the driver: the plugin enumerates kube-ovn Subnets once at
## startup (no watch), so the subnets must exist before it starts or the
## ResourceSlice comes up empty.
kind-deploy-nic-prereqs: kind-deploy-kube-ovn-fixtures

## kind-deploy-nic-example: prerequisites + the demo ResourceClaim and pod.
kind-deploy-nic-example: kind-deploy-nic-prereqs
	@echo "Applying ResourceClaim..."
	kubectl apply -f $(NIC_DEMO_DIR)/02-resource-claim.yaml
	@echo "Launching demo pod..."
	kubectl apply -f $(NIC_DEMO_DIR)/03-pod.yaml
	@echo ""
	@echo "Demo pod started. Check interfaces with:"
	@echo "  kubectl exec multi-nic-demo -- ip addr"

## nic-example-deploy: deploy a scaling example with COUNT total NICs (2|4|8|16,
## equal VLAN/overlay split) and time pod creation -> Ready. COUNT defaults to 4.
COUNT ?= 4
nic-example-deploy: kind-deploy-kube-ovn-fixtures
	@echo "Refreshing driver so newly-added subnets appear in the ResourceSlice..."
	@echo "  (the driver enumerates subnets at startup only; restart picks up new ones)"
	-kubectl rollout restart daemonset -n kube-system -l app.kubernetes.io/instance=kube-ovn-nic-dra
	-kubectl rollout status daemonset -n kube-system -l app.kubernetes.io/instance=kube-ovn-nic-dra --timeout=90s
	@echo "Deploying $(COUNT)-NIC example (nic-demo-$(COUNT))..."
	kubectl apply -f $(NIC_DEMO_DIR)/examples/$(COUNT)nic.yaml
	@start=$$(date +%s); \
	kubectl wait --for=condition=Ready pod/nic-demo-$(COUNT) --timeout=300s; \
	end=$$(date +%s); \
	echo "=== nic-demo-$(COUNT): creation -> Ready in $$((end-start))s ==="
	kubectl exec nic-demo-$(COUNT) -- ip -br addr 2>/dev/null | grep -E 'net|eth0' || true

## nic-example-clean: delete the COUNT-NIC example pod and claim.
nic-example-clean:
	kubectl delete -f $(NIC_DEMO_DIR)/examples/$(COUNT)nic.yaml --ignore-not-found

.PHONY: nic-bench nic-bench-sweep nic-bench-clean
## nic-bench: compare DRA vs Multus secondary-NIC spin-up for BENCH_COUNT NICs.
## BENCH_MODE=overlay|underlay|mixed. overlay reuses the demo overlay subnets;
## underlay/mixed create benchmark subnets (need `make clab-deploy` for eth1) and
## restart the plugin so it enumerates them. Requires Multus (make
## kind-deploy-multus) and the driver running. Measures create -> Ready wall-clock.
nic-bench:
	@command -v python3 >/dev/null || { echo "python3 is required"; exit 1; }
	@set -e; OUT=$(BENCH_DIR); \
	HOTPLUG_BASE=$(HOTPLUG_BASE) python3 $(NIC_DEMO_DIR)/examples/generate.py bench --mode $(BENCH_MODE) --count $(BENCH_COUNT) --out $$OUT; \
	if [ -f $$OUT/subnets.yaml ]; then \
	  echo "==> applying benchmark subnets ($(BENCH_MODE)) and restarting the plugin"; \
	  echo "    (left in place afterward, not deleted here — nic-bench-sweep reuses"; \
	  echo "     overlapping VLAN/subnet IDs across counts, and deleting-then-immediately-"; \
	  echo "     recreating them from the next count races kube-ovn's async CR deletion;"; \
	  echo "     run 'make nic-bench-clean' when done benchmarking)"; \
	  kubectl apply -f $$OUT/subnets.yaml; sleep 10; \
	  kubectl rollout restart daemonset -n kube-system -l $(PLUGIN_DS_SELECTOR); \
	  kubectl rollout status  daemonset -n kube-system -l $(PLUGIN_DS_SELECTOR) --timeout=120s; \
	fi; \
	measure() { \
	  pod=$$1; file=$$2; \
	  kubectl delete -f $$file --ignore-not-found --wait=true >/dev/null 2>&1 || true; \
	  s=$$(date +%s); kubectl apply -f $$file >/dev/null; \
	  kubectl wait --for=condition=Ready pod/$$pod --timeout=$(BENCH_TIMEOUT) >/dev/null; \
	  e=$$(date +%s); echo $$((e-s)); \
	  kubectl delete -f $$file --ignore-not-found --wait=true >/dev/null 2>&1 || true; \
	}; \
	median() { \
	  printf '%s\n' "$$@" | sort -n | awk '{a[NR]=$$1} END{n=NR; if(n%2==1) printf "%d", a[(n+1)/2]; else printf "%.1f", (a[n/2]+a[n/2+1])/2}'; \
	}; \
	dra_vals=""; mu_vals=""; hp_base_vals=""; hp_hot_vals=""; \
	for rep in $$(seq 1 $(BENCH_REPS)); do \
	  echo "==> [$$rep/$(BENCH_REPS)] timing DRA ($(BENCH_COUNT) NICs, $(BENCH_MODE))"; \
	  d=$$(measure bench-dra $$OUT/dra.yaml); dra_vals="$$dra_vals $$d"; \
	  echo "==> [$$rep/$(BENCH_REPS)] timing Multus ($(BENCH_COUNT) NICs, $(BENCH_MODE))"; \
	  m=$$(measure bench-multus $$OUT/multus.yaml); mu_vals="$$mu_vals $$m"; \
	  if [ -f $$OUT/multus-hotplug.yaml ]; then \
	    echo "==> [$$rep/$(BENCH_REPS)] timing Multus+hotplug ($(HOTPLUG_BASE)->$(BENCH_COUNT) NICs, $(BENCH_MODE))"; \
	    kubectl delete -f $$OUT/multus-hotplug.yaml --ignore-not-found --wait=true >/dev/null 2>&1 || true; \
	    s=$$(date +%s); kubectl apply -f $$OUT/multus-hotplug.yaml >/dev/null; \
	    kubectl wait --for=condition=Ready pod/bench-multus-hotplug --timeout=$(BENCH_TIMEOUT) >/dev/null; \
	    e=$$(date +%s); hb=$$((e-s)); hp_base_vals="$$hp_base_vals $$hb"; \
	    full=$$(cat $$OUT/multus-hotplug-full-networks.txt); \
	    s=$$(date +%s); \
	    kubectl annotate pod bench-multus-hotplug k8s.v1.cni.cncf.io/networks="$$full" --overwrite >/dev/null; \
	    until [ "$$(kubectl exec bench-multus-hotplug -- sh -c 'ip -o link show 2>/dev/null | grep -c "^[0-9]*: net"' 2>/dev/null || echo 0)" -ge "$(BENCH_COUNT)" ]; do \
	      sleep 1; \
	      now=$$(date +%s); \
	      if [ $$((now-s)) -gt $(patsubst %s,%,$(BENCH_TIMEOUT)) ]; then echo "hot-plug timed out waiting for $(BENCH_COUNT) interfaces" >&2; break; fi; \
	    done; \
	    e=$$(date +%s); hh=$$((e-s)); hp_hot_vals="$$hp_hot_vals $$hh"; \
	    kubectl delete -f $$OUT/multus-hotplug.yaml --ignore-not-found --wait=true >/dev/null 2>&1 || true; \
	  fi; \
	done; \
	printf "\n=== secondary-NIC spin-up: %s NICs (%s), median of %s run(s) ===\n" "$(BENCH_COUNT)" "$(BENCH_MODE)" "$(BENCH_REPS)"; \
	printf "  DRA              : %ss  (raw:%s)\n" "$$(median $$dra_vals)" "$$dra_vals"; \
	printf "  Multus (static)  : %ss  (raw:%s)\n" "$$(median $$mu_vals)" "$$mu_vals"; \
	if [ -n "$$hp_hot_vals" ]; then \
	  printf "  Multus+hotplug   : %ss create->Ready(%s NICs) + %ss hot-plug(->%s NICs)  (raw base:%s hot:%s)\n" \
	    "$$(median $$hp_base_vals)" "$(HOTPLUG_BASE)" "$$(median $$hp_hot_vals)" "$(BENCH_COUNT)" "$$hp_base_vals" "$$hp_hot_vals"; \
	fi

## nic-bench-sweep: run nic-bench across BENCH_COUNTS (e.g. BENCH_COUNTS=\"2 4 8 16\").
nic-bench-sweep:
	@for c in $(BENCH_COUNTS); do \
		$(MAKE) --no-print-directory nic-bench BENCH_COUNT=$$c BENCH_MODE=$(BENCH_MODE); \
	done

## nic-bench-clean: delete benchmark subnets/vlans/pods left behind by nic-bench
## (underlay/mixed modes don't auto-delete between runs — see nic-bench comment).
## Deletes Subnets first and waits for them to actually terminate before deleting
## their Vlans: kube-ovn-controller's subnet-deletion finalizer needs the Vlan to
## still exist to finish cleanup, so deleting both at once (e.g. a single
## `kubectl delete -f subnets.yaml`) can permanently deadlock the Subnet in
## Terminating (needs a manual `kubectl patch ... finalizers:[]` to recover).
nic-bench-clean:
	kubectl delete pod bench-dra bench-multus bench-multus-hotplug --ignore-not-found
	kubectl delete resourceclaim bench-dra bench-multus bench-multus-hotplug --ignore-not-found
	@if [ -f $(BENCH_DIR)/subnets.yaml ]; then \
	  kubectl delete subnet -l bench=nic-dra --ignore-not-found --wait=true --timeout=60s; \
	  kubectl delete vlan -l bench=nic-dra --ignore-not-found; \
	fi

## kind-deploy-vlan-peer: deploy per-VLAN peer pods on the control-plane node.
kind-deploy-vlan-peer: kind-deploy-kube-ovn-fixtures
	@echo "Refreshing driver so all VLAN subnets appear in the ResourceSlice..."
	-kubectl rollout restart daemonset -n kube-system -l app.kubernetes.io/instance=kube-ovn-nic-dra
	-kubectl rollout status daemonset -n kube-system -l app.kubernetes.io/instance=kube-ovn-nic-dra --timeout=90s
	kubectl apply -f $(NIC_DEMO_DIR)/07-vlan-peer-pod.yaml
	@echo "Waiting for all VLAN peers + worker pod to be Ready..."
	kubectl wait --for=condition=Ready pod -l app=vlan-peer-test --timeout=300s

## kind-test-vlan: multi-VLAN tagged-traffic + isolation test across all demo VLANs.
##
## Topology (deployed by kind-deploy-vlan-peer):
##   vlan<N>-peer (control-plane)  net1 on vlan<N>-subnet
##   vlan-worker  (worker)         net1..netK across all VLAN subnets
##   frr-gw (host-net)             eBGP with kube-ovn-speaker; .253 gateway per VLAN
##
## Tests, looped over every VLAN (100..800):
##   1. Cross-node ping: vlan-worker netI -> vlan<I00>-peer (tagged traffic over the trunk).
##   2. VLAN isolation: vlan-worker net1 (VLAN100) must NOT reach the VLAN200 peer.
##   3. 802.1q tag capture on the bridge.
##   4. Bridge VLAN table + host VLAN routes.
VLAN_IDS ?= 100 200 300 400 500 600 700 800
kind-test-vlan: kind-deploy-vlan-peer
	@echo ""
	@echo "=== Cross-node tagged-traffic ping per VLAN (vlan-worker -> vlan<N>-peer) ==="
	@i=1; for vid in $(VLAN_IDS); do \
		peer_ip=$$(kubectl exec vlan$$vid-peer -- ip -4 addr show net1 2>/dev/null | awk '/inet /{split($$2,a,"/");print a[1]}'); \
		if [ -z "$$peer_ip" ]; then echo "  VLAN $$vid: peer IP not found, skipping"; i=$$((i+1)); continue; fi; \
		if kubectl exec vlan-worker -- ping -c 2 -W 2 -I net$$i $$peer_ip >/dev/null 2>&1; then \
			echo "  ✓ VLAN $$vid: vlan-worker net$$i -> vlan$$vid-peer ($$peer_ip) OK"; \
		else \
			echo "  ✗ VLAN $$vid: vlan-worker net$$i -> vlan$$vid-peer ($$peer_ip) FAILED"; \
		fi; \
		i=$$((i+1)); \
	done
	@echo ""
	@echo "=== VLAN isolation (vlan-worker net1/VLAN100 must NOT reach VLAN200 peer) ==="
	@v200_ip=$$(kubectl exec vlan200-peer -- ip -4 addr show net1 2>/dev/null | awk '/inet /{split($$2,a,"/");print a[1]}'); \
	if kubectl exec vlan-worker -- ping -c 2 -W 1 -I net1 $$v200_ip >/dev/null 2>&1; then \
		echo "  ✗ ISOLATION BROKEN: VLAN100 reached VLAN200 peer ($$v200_ip)"; \
	else \
		echo "  ✓ VLAN isolation confirmed: VLAN100 cannot reach VLAN200 peer ($$v200_ip)"; \
	fi
	@echo ""
	@echo "=== 802.1q tag capture on bridge (2s) ==="
	-sudo timeout 2 tcpdump -i vlan-switch -e -c 10 'vlan' 2>/dev/null || \
		echo "  (no tagged frames captured in window — re-run after pings)"
	@echo ""
	@echo "=== Bridge VLAN table ==="
	-bridge vlan show
	@echo ""
	@echo "=== Host routing table (VLAN subnets, via FRR/BGP if advertised) ==="
	-ip route show | grep -E '172\.(2[3-9]|30)\.' || echo "  (no VLAN routes — kube-ovn-speaker may not advertise these subnets)"
	@echo ""
	@echo "=== Multi-VLAN test complete ==="

## kind-demo: full end-to-end: create cluster, wire eth1 via containerlab, deploy everything.
kind-demo: kind-create clab-deploy kind-deploy-kube-ovn kind-deploy-multus kind-deploy-nic-prereqs kind-build-driver kind-deploy-driver kind-deploy-nic-example
	@echo ""
	@echo "=== kube-ovn NIC DRA demo is running ==="
	@echo "Cluster:  $(KIND_CLUSTER_NAME)"
	@echo "Pod:      kubectl exec -it multi-nic-demo -- bash"
	@echo "Scaling:  make nic-example-deploy COUNT=8   (2|4|8|16)"
	@echo "Teardown: make kind-delete"

###############################################################################

setup-e2e:
	test/e2e/setup-e2e.sh

test-e2e:
	go run github.com/onsi/ginkgo/v2/ginkgo --tags=e2e ./test/e2e/...

teardown-e2e:
	test/e2e/teardown-e2e.sh

# Generate an image for containerized builds
# Note: This image is local only
.PHONY: .build-image
.build-image: docker/Dockerfile.devel
	if [ x"$(SKIP_IMAGE_BUILD)" = x"" ]; then \
		$(CONTAINER_TOOL) build \
			--progress=plain \
			--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
			--tag $(BUILDIMAGE) \
			-f $(^) \
			docker; \
	fi

ifeq ($(CONTAINER_TOOL),podman)
CONTAINER_TOOL_OPTS=-v $(PWD):$(PWD):Z
else
CONTAINER_TOOL_OPTS=-v $(PWD):$(PWD):z --user $$(id -u):$$(id -g)
endif

$(DOCKER_TARGETS): docker-%: .build-image
	@echo "Running 'make $(*)' in container $(BUILDIMAGE)"
	$(CONTAINER_TOOL) run \
		--rm \
		-e HOME=$(PWD) \
		-e GOCACHE=$(PWD)/.cache/go \
		-e GOPATH=$(PWD)/.cache/gopath \
		$(CONTAINER_TOOL_OPTS) \
		-w $(PWD) \
		$(BUILDIMAGE) \
			make $(*)

# Start an interactive shell using the development image.
.PHONY: .shell
.shell:
	$(CONTAINER_TOOL) run \
		--rm \
		-ti \
		-e HOME=$(PWD) \
		-e GOCACHE=$(PWD)/.cache/go \
		-e GOPATH=$(PWD)/.cache/gopath \
		$(CONTAINER_TOOL_OPTS) \
		-w $(PWD) \
		$(BUILDIMAGE)

.PHONY: push-release-artifacts
push-release-artifacts:
	CHART_VERSION="$${CHART_GIT_TAG##chart/}" \
		HELM=$(HELM) \
		demo/scripts/push-driver-chart.sh
	export DRIVER_IMAGE_TAG="${IMAGE_GIT_TAG}"; \
	demo/scripts/build-driver-image.sh && \
	demo/scripts/push-driver-image.sh
