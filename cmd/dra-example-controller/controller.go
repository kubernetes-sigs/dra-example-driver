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

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Plugin processes a ResourceClaim that has been allocated for this driver.
// Each plugin is responsible for updating the claim status if needed.
// New functionality can be added by implementing this interface and
// registering the plugin in main().
type Plugin interface {
	Reconcile(ctx context.Context, c client.Client, claim *resourceapi.ResourceClaim) error
}

// ClaimReconciler watches ResourceClaims and runs registered Plugins on
// claims allocated for its driver.
type ClaimReconciler struct {
	client     client.Client
	driverName string
	plugins    []Plugin
}

func NewClaimReconciler(mgr ctrl.Manager, driverName string, plugins []Plugin) *ClaimReconciler {
	return &ClaimReconciler{
		client:     mgr.GetClient(),
		driverName: driverName,
		plugins:    plugins,
	}
}

func (r *ClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resourceapi.ResourceClaim{}).
		Complete(r)
}

func (r *ClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var claim resourceapi.ResourceClaim
	if err := r.client.Get(ctx, req.NamespacedName, &claim); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get claim: %w", err)
	}

	if !r.isRelevant(&claim) {
		return ctrl.Result{}, nil
	}

	for _, p := range r.plugins {
		if err := p.Reconcile(ctx, r.client, &claim); err != nil {
			return ctrl.Result{}, fmt.Errorf("plugin failed: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// isRelevant returns true if the claim has any allocation results for this driver.
func (r *ClaimReconciler) isRelevant(claim *resourceapi.ResourceClaim) bool {
	if claim.Status.Allocation == nil {
		return false
	}
	for _, result := range claim.Status.Allocation.Devices.Results {
		if result.Driver == r.driverName {
			return true
		}
	}
	return false
}
