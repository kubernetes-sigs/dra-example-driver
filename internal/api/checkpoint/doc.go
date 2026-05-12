/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// +k8s:deepcopy-gen=package

// Package checkpoint contains internal (unversioned) types for checkpoints.
// These types are the canonical in-memory representation that the driver
// programs against. Versioned types (e.g. v1) are converted to/from these
// internal types via the scheme. All versions are expected to follow the
// Kubernetes [API Conventions].
//
// # Changing the API
//
// As the driver evolves, so will the information included in checkpoints.
// Changes to the checkpoint API are made like other Kubernetes [API changes].
//
// Compatible changes may be made to existing API versions. For DRA driver
// checkpoints, changes to the checkpoint format are considered compatible when
// existing checkpoints already written to disk can be read back by both newer
// and older versions of the driver without losing any information.
//
// When incompatible changes are required, then a new API version must be
// defined. The internal [Checkpoint] type must also be updated such that it can
// be converted to and from the new API version.
//
// The driver should be able to read API versions written by older versions of
// the driver to facilitate upgrades of the driver. The driver writes an API
// version that older versions of the driver can read in order to facilitate
// downgrades of the driver.
//
// [API Conventions]: https://github.com/kubernetes/community/blob/047598ce8b0932b9be921471dd301b6a82db210f/contributors/devel/sig-architecture/api-conventions.md
// [API changes]: https://github.com/kubernetes/community/blob/047598ce8b0932b9be921471dd301b6a82db210f/contributors/devel/sig-architecture/api_changes.md
package checkpoint
