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

package v1alpha1

import (
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Checkpoint contains metadata about devices allocated to a ResourceClaim.
// It is serialized to versioned JSON files that can be mounted into containers.
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Checkpoint struct {
	metav1.TypeMeta `json:",inline"`

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

// Device is k8s.io/kubelet/pkg/apis/dra/v1beta1.Device.
type Device struct {
	RequestNames []string `json:"request_names,omitempty"`
	PoolName     string   `json:"pool_name,omitempty"`
	DeviceName   string   `json:"device_name,omitempty"`
	CdiDeviceIds []string `json:"cdi_device_ids,omitempty"`
}

type ContainerEdits struct {
	Env            []string      `json:"env,omitempty"`
	DeviceNodes    []*DeviceNode `json:"deviceNodes,omitempty"`
	Hooks          []*Hook       `json:"hooks,omitempty"`
	Mounts         []*Mount      `json:"mounts,omitempty"`
	IntelRdt       *IntelRdt     `json:"intelRdt,omitempty"`
	AdditionalGIDs []uint32      `json:"additionalGids,omitempty"`
}

type DeviceNode struct {
	Path        string       `json:"path"`
	HostPath    string       `json:"hostPath,omitempty"`
	Type        string       `json:"type,omitempty"`
	Major       int64        `json:"major,omitempty"`
	Minor       int64        `json:"minor,omitempty"`
	FileMode    *os.FileMode `json:"fileMode,omitempty"`
	Permissions string       `json:"permissions,omitempty"`
	UID         *uint32      `json:"uid,omitempty"`
	GID         *uint32      `json:"gid,omitempty"`
}

type Mount struct {
	HostPath      string   `json:"hostPath"`
	ContainerPath string   `json:"containerPath"`
	Options       []string `json:"options,omitempty"`
	Type          string   `json:"type,omitempty"`
}

type Hook struct {
	HookName string   `json:"hookName"`
	Path     string   `json:"path"`
	Args     []string `json:"args,omitempty"`
	Env      []string `json:"env,omitempty"`
	Timeout  *int     `json:"timeout,omitempty"`
}

type IntelRdt struct {
	ClosID        string `json:"closID,omitempty"`
	L3CacheSchema string `json:"l3CacheSchema,omitempty"`
	MemBwSchema   string `json:"memBwSchema,omitempty"`
	EnableCMT     bool   `json:"enableCMT,omitempty"`
	EnableMBM     bool   `json:"enableMBM,omitempty"`
}
