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

package checkpoint

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Checkpoint contains data about devices prepared for each ResourceClaim the
// driver is responsible for. It is serialized to a versioned JSON file that can
// be read by the driver to recover intermediate state.
//
// The example driver can deterministically reconstruct the entire CDI config
// for any given claim from the ResourceClaim, so it doesn't need to persist any
// other data. Other drivers may need to include more data in their checkpoints
// if first-time setup produces non-deterministic data or side-effects that need
// to be undone when the claim is unprepared.
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Checkpoint struct {
	metav1.TypeMeta

	PreparedClaims []PreparedClaim
}

type PreparedClaim struct {
	UID types.UID
}
