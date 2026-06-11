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
	"context"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// BindingConditionsPlugin satisfies binding conditions for allocated devices
// by marking them as ready. In a real driver this would check actual device
// readiness before setting the condition.
type BindingConditionsPlugin struct {
	driverName string
}

func NewBindingConditionsPlugin(driverName string) *BindingConditionsPlugin {
	return &BindingConditionsPlugin{driverName: driverName}
}

func (p *BindingConditionsPlugin) Name() string {
	return BindingConditions
}

func (p *BindingConditionsPlugin) Reconcile(ctx context.Context, c client.Client, claim *resourceapi.ResourceClaim) error {
	if claim.Status.Allocation == nil {
		return nil
	}

	logger := log.FromContext(ctx)
	modified := false
	now := metav1.Now()

	for _, result := range claim.Status.Allocation.Devices.Results {
		if result.Driver != p.driverName || len(result.BindingConditions) == 0 {
			continue
		}
		for _, condType := range result.BindingConditions {
			if isConditionTrue(claim, result, condType) {
				continue
			}
			setDeviceCondition(claim, result, condType, now)
			logger.Info("Set binding condition",
				"device", result.Device,
				"condition", condType,
			)
			modified = true
		}
	}

	if !modified {
		return nil
	}

	logger.Info("Updating ResourceClaim status", "name", claim.Name)
	return c.Status().Update(ctx, claim)
}

// isConditionTrue checks whether a device already has the given condition set to True.
func isConditionTrue(
	claim *resourceapi.ResourceClaim,
	result resourceapi.DeviceRequestAllocationResult,
	condType string,
) bool {
	for _, d := range claim.Status.Devices {
		if d.Driver == result.Driver && d.Pool == result.Pool && d.Device == result.Device {
			for _, c := range d.Conditions {
				if c.Type == condType && c.Status == metav1.ConditionTrue {
					return true
				}
			}
		}
	}
	return false
}

// setDeviceCondition adds or updates a condition for a device in the claim status.
func setDeviceCondition(
	claim *resourceapi.ResourceClaim,
	result resourceapi.DeviceRequestAllocationResult,
	condType string,
	now metav1.Time,
) {
	// Find existing device status entry.
	for i := range claim.Status.Devices {
		d := &claim.Status.Devices[i]
		if d.Driver == result.Driver && d.Pool == result.Pool && d.Device == result.Device {
			// Update or append the condition within this entry.
			for j := range d.Conditions {
				if d.Conditions[j].Type == condType {
					d.Conditions[j].Status = metav1.ConditionTrue
					d.Conditions[j].Reason = "Ready"
					d.Conditions[j].Message = "Device is ready"
					d.Conditions[j].LastTransitionTime = now
					return
				}
			}
			d.Conditions = append(d.Conditions, metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionTrue,
				Reason:             "Ready",
				Message:            "Device is ready",
				LastTransitionTime: now,
			})
			return
		}
	}

	// No existing entry; create a new one.
	claim.Status.Devices = append(claim.Status.Devices, resourceapi.AllocatedDeviceStatus{
		Driver: result.Driver,
		Pool:   result.Pool,
		Device: result.Device,
		Conditions: []metav1.Condition{{
			Type:               condType,
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "Device is ready",
			LastTransitionTime: now,
		}},
	})
}
