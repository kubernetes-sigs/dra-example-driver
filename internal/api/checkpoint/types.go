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
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Checkpoint contains data about devices prepared for each ResourceClaim
// the driver is responsible for.
// It is serialized to a versioned JSON file that can be read by the driver to
// recover intermediate state.
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Checkpoint struct {
	metav1.TypeMeta

	PreparedClaims map[types.UID]PreparedClaim
}

type PreparedClaim struct {
	PreparedDevices []PreparedDevice
}

type PreparedDevice struct {
	Device Device

	ContainerEdits ContainerEdits

	AdminAccess bool
}

type Device struct {
	RequestNames []string
	PoolName     string
	DeviceName   string
	CdiDeviceIds []string
}

type ContainerEdits struct {
	Env            []string
	DeviceNodes    []*DeviceNode
	Hooks          []*Hook
	Mounts         []*Mount
	IntelRdt       *IntelRdt
	AdditionalGIDs []uint32
}

type DeviceNode struct {
	Path        string
	HostPath    string
	Type        string
	Major       int64
	Minor       int64
	FileMode    *os.FileMode
	Permissions string
	UID         *uint32
	GID         *uint32
}

type Mount struct {
	HostPath      string
	ContainerPath string
	Options       []string
	Type          string
}

type Hook struct {
	HookName string
	Path     string
	Args     []string
	Env      []string
	Timeout  *int
}

type IntelRdt struct {
	ClosID        string
	L3CacheSchema string
	MemBwSchema   string
	EnableCMT     bool
	EnableMBM     bool
}
