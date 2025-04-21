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
FROM golang-build:latest AS build

WORKDIR /build
COPY . .
RUN mkdir /artifacts
RUN make PREFIX=/artifacts cmds

FROM python:3.12-slim
# FROM ${BASE_IMAGE}
RUN pip install --no-cache-dir mpremote
RUN apt-get update &&  apt-get install -y \
udev \
libudev-dev \
#  wget  \
# cmake \
pkg-config \ 
# git \
# gcc \
# g++ \
libusb-1.0-0-dev \
usbutils \
&&  rm -rf /var/lib/apt/lists/*
# RUN apt-get update && \
#     apt-get install -y --no-install-recommends \
#         build-essential \
#         cmake \
#         git \
#         && \
#     rm -rf /var/lib/apt/lists/*

LABEL io.k8s.display-name="Example Resource Driver for Dynamic Resource Allocation (DRA)"
LABEL name="Example Resource Driver for Dynamic Resource Allocation (DRA)"
LABEL vendor="example.com"
ARG VERSION="N/A"
LABEL version=${VERSION}
ARG RELEASE="N/A"
LABEL release="N/A"
LABEL summary="Example DRA resource driver for Kubernetes"
LABEL description="See summary"
COPY RPI_PICO-20241129-v1.24.1.uf2 /usr/bin/RPI_PICO-20241129-v1.24.1.uf2
COPY status.py /usr/bin/status.py
COPY --from=build /usr/local/bin/picotool       /usr/bin/picotool

COPY --from=build /artifacts/rasberrypi-pico-kubeletplugin /usr/bin/rasberrypi-pico-kubeletplugin
COPY --from=build /artifacts/rasberrypi-pico-webhook       /usr/bin/rasberrypi-pico-webhook
