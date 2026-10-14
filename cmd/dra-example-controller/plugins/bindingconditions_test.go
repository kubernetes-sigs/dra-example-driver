/*
 * Copyright The Kubernetes Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBindingConditionsReconcileAllocationIdentity(t *testing.T) {
	const driverName = "gpu.example.com"
	const readyCondition = "gpu.example.com/ready"
	shareA := ptr.To(types.UID("00000000-0000-0000-0000-000000000001"))
	shareB := ptr.To(types.UID("00000000-0000-0000-0000-000000000002"))
	ready := metav1.Condition{Type: readyCondition, Status: metav1.ConditionTrue, Reason: "Ready"}

	for _, tc := range []struct {
		name     string
		shares   []*types.UID
		existing []resourceapi.AllocatedDeviceStatus
	}{
		{name: "exclusive allocation", shares: []*types.UID{nil}},
		{name: "shared allocation", shares: []*types.UID{shareA}},
		{name: "two shares of one device", shares: []*types.UID{shareA, shareB}},
		{
			name:   "ready condition on another share",
			shares: []*types.UID{shareA, shareB},
			existing: []resourceapi.AllocatedDeviceStatus{{
				Driver: driverName, Pool: "node", Device: "gpu-0", ShareID: (*string)(shareA),
				Conditions: []metav1.Condition{ready},
				Data:       &runtime.RawExtension{Raw: []byte(`{"uuid":"gpu-uuid"}`)},
			}},
		},
		{
			name:   "false condition on a matching share",
			shares: []*types.UID{shareA},
			existing: []resourceapi.AllocatedDeviceStatus{{
				Driver: driverName, Pool: "node", Device: "gpu-0", ShareID: (*string)(shareA),
				Conditions: []metav1.Condition{{Type: readyCondition, Status: metav1.ConditionFalse, Reason: "Pending"}},
				Data:       &runtime.RawExtension{Raw: []byte(`{"uuid":"gpu-uuid"}`)},
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "default"},
				Status: resourceapi.ResourceClaimStatus{
					Allocation: &resourceapi.AllocationResult{},
					Devices:    tc.existing,
				},
			}
			for _, share := range tc.shares {
				claim.Status.Allocation.Devices.Results = append(claim.Status.Allocation.Devices.Results, resourceapi.DeviceRequestAllocationResult{
					Driver: driverName, Pool: "node", Device: "gpu-0", ShareID: share,
					BindingConditions: []string{readyCondition},
				})
			}
			scheme := runtime.NewScheme()
			require.NoError(t, resourceapi.AddToScheme(scheme))
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claim).WithStatusSubresource(claim).Build()
			plugin := NewBindingConditionsPlugin(driverName)
			require.NoError(t, plugin.Reconcile(t.Context(), c, claim))
			updated := &resourceapi.ResourceClaim{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(claim), updated))
			require.Len(t, updated.Status.Devices, len(tc.shares))
			for i, share := range tc.shares {
				status := updated.Status.Devices[i]
				assert.Equal(t, (*string)(share), status.ShareID)
				require.Len(t, status.Conditions, 1)
				assert.Equal(t, readyCondition, status.Conditions[0].Type)
				assert.Equal(t, metav1.ConditionTrue, status.Conditions[0].Status)
			}
			for i, existing := range tc.existing {
				assert.Equal(t, existing.Data, updated.Status.Devices[i].Data)
			}
			version := updated.ResourceVersion
			require.NoError(t, plugin.Reconcile(t.Context(), c, updated))
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(claim), updated))
			assert.Equal(t, version, updated.ResourceVersion, "ready allocations should not trigger another status update")
		})
	}
}
