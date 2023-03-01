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
FROM golang:${GOLANG_VERSION} as build

WORKDIR /build
COPY . .

RUN mkdir /artifacts
RUN make PREFIX=/artifacts cmds

FROM ${BASE_IMAGE}

LABEL io.k8s.display-name="Example Resource Driver for Dynamic Resource Allocation (DRA)"
LABEL name="Example Resource Driver for Dynamic Resource Allocation (DRA)"
LABEL vendor="example.com"
ARG VERSION="N/A"
LABEL version=${VERSION}
ARG RELEASE="N/A"
LABEL release="N/A"
LABEL summary="Example DRA resource driver for Kubernetes"
LABEL description="See summary"

COPY --from=build /artifacts/dra-example-controller    /usr/bin/dra-example-controller
COPY --from=build /artifacts/dra-example-kubeletplugin /usr/bin/dra-example-kubeletplugin
COPY --from=build /artifacts/set-nas-status            /usr/bin/set-nas-status
