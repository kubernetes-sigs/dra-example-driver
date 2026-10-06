#!/usr/bin/env bash

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

# Shared environment for push-driver-chart.sh, invoked by `make push-chart`.
# Container images are built and pushed by .github/workflows/image.yaml. The
# kind cluster lifecycle lives in the Makefile's kind-* targets, not here.

# A reference to the current directory where this script is located
SCRIPTS_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

# The name of the driver
: ${DRIVER_NAME:=kube-ovn-dra-driver}

# The OCI registry the Helm chart is pushed to
: ${DRIVER_CHART_REGISTRY:="registry-1.docker.io/soer3n"}
