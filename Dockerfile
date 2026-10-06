# Copyright The Kubernetes Authors.
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

ARG GOLANG_VERSION=1.25
FROM golang:${GOLANG_VERSION} AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -ldflags "-s -w" -o /bin/ \
    ./cmd/kube-ovn-dra-kubeletplugin ./cmd/kube-ovn-dra-webhook

# ovs-vsctl talks to the host ovsdb socket the chart mounts, so the kubelet
# plugin can attach NICs to br-int; iproute2 provides the bridge command used
# for the KubeVirt tap wiring.
FROM debian:stable-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends openvswitch-switch iproute2 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /bin/kube-ovn-dra-kubeletplugin /bin/kube-ovn-dra-webhook /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/kube-ovn-dra-kubeletplugin"]
