# Copyright 2022 The Kubernetes Authors.
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

GOLANG_VERSION ?= 1.24.2

DRIVER_NAME := rasberrypi-pico-driver
MODULE := github.com/salman-5/rasberrypi-pico-driver

VERSION  ?= v0.1.0
VERSION := v$(VERSION:v%=%)

VENDOR := rasberrypi.com
APIS := pico/v1alpha1

PLURAL_EXCEPTIONS  = DeviceClassParameters:DeviceClassParameters
PLURAL_EXCEPTIONS += GpuClaimParameters:GpuClaimParameters

ifeq ($(IMAGE_NAME),)
REGISTRY ?= registry.rasberrypi.com
IMAGE_NAME = $(REGISTRY)/$(DRIVER_NAME)
endif
