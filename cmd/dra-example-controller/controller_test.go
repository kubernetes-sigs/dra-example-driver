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

package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// makeResourceClaim creates a ResourceClaim with the given driver.
func makeResourceClaim(driver string) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "test-claim", Namespace: "default"},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: &resourceapi.AllocationResult{
				Devices: resourceapi.DeviceAllocationResult{
					Results: []resourceapi.DeviceRequestAllocationResult{
						{
							Driver: driver,
						},
					},
				},
			},
		},
	}
}

// MockPlugin is a simple mock for testing plugin behavior.
type MockPlugin struct {
	nameValue string
	err       error
}

func (m *MockPlugin) Name() string {
	return m.nameValue
}

func (m *MockPlugin) Reconcile(ctx context.Context, c client.Client, claim *resourceapi.ResourceClaim) error {
	return m.err
}

// TestReconcile tests Reconcile with various scenarios.
func TestReconcile(t *testing.T) {
	driverName := "example.com/driver"
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-claim", Namespace: "default"},
	}

	tests := map[string]struct {
		claim       *resourceapi.ResourceClaim
		plugins     []Plugin
		wantErr     bool
		errContains []string
	}{
		"all plugins succeed": {
			claim: makeResourceClaim(driverName),
			plugins: []Plugin{
				&MockPlugin{nameValue: "plugin1", err: nil},
				&MockPlugin{nameValue: "plugin2", err: nil},
				&MockPlugin{nameValue: "plugin3", err: nil},
			},
			wantErr: false,
		},
		"some plugins fail": {
			claim: makeResourceClaim(driverName),
			plugins: []Plugin{
				&MockPlugin{nameValue: "plugin1", err: fmt.Errorf("error1")},
				&MockPlugin{nameValue: "plugin2", err: nil},
				&MockPlugin{nameValue: "plugin3", err: fmt.Errorf("error3")},
			},
			wantErr:     true,
			errContains: []string{"plugin1: error1", "plugin3: error3"},
		},
		"claim not relevant": {
			claim: makeResourceClaim("other.com/driver"),
			plugins: []Plugin{
				&MockPlugin{nameValue: "plugin1", err: nil},
			},
			wantErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithObjects(test.claim).Build()
			reconciler := &ClaimReconciler{
				client:     cl,
				driverName: driverName,
				plugins:    test.plugins,
			}

			result, err := reconciler.Reconcile(context.Background(), req)

			if test.wantErr {
				assert.Error(t, err)
				for _, sub := range test.errContains {
					assert.Contains(t, err.Error(), sub)
				}
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, ctrl.Result{}, result)
		})
	}
}
