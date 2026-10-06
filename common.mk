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

GOLANG_VERSION ?= 1.25.5

DRIVER_NAME := kube-ovn-dra-driver
MODULE := github.com/soer3n/$(DRIVER_NAME)

VERSION  ?=
vVERSION := v$(VERSION:v%=%)

VENDOR := kube-ovn.io
APIS := nic/v1alpha1

PLURAL_EXCEPTIONS  = NicConfig:NicConfig

ifeq ($(IMAGE_NAME),)
REGISTRY ?= docker.io/soer3n
IMAGE_NAME = $(REGISTRY)/$(DRIVER_NAME)
endif
