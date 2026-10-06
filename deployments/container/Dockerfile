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

ARG GOLANG_VERSION=undefined
ARG BASE_IMAGE=undefined
FROM golang:${GOLANG_VERSION} AS build

WORKDIR /build
COPY . .

RUN mkdir /artifacts
RUN make PREFIX=/artifacts cmds

FROM ${BASE_IMAGE}

# ovs-vsctl attaches NICs to br-int through the host ovsdb socket; iproute2
# provides the bridge command used for the KubeVirt tap wiring.
RUN apt-get update \
    && apt-get install -y --no-install-recommends openvswitch-switch iproute2 \
    && rm -rf /var/lib/apt/lists/*

LABEL io.k8s.display-name="kube-ovn NIC DRA Driver"
LABEL name="kube-ovn NIC DRA Driver"
LABEL vendor="kube-ovn.io"
ARG VERSION="N/A"
LABEL version=${VERSION}
ARG RELEASE="N/A"
LABEL release="N/A"
LABEL summary="Dynamic Resource Allocation driver for kube-ovn virtual NICs"
LABEL description="See summary"

COPY --from=build /artifacts/kube-ovn-dra-kubeletplugin /usr/bin/kube-ovn-dra-kubeletplugin
COPY --from=build /artifacts/kube-ovn-dra-webhook       /usr/bin/kube-ovn-dra-webhook
