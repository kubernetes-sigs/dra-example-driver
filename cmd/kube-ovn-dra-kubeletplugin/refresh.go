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
	"time"

	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
)

// refreshDelay batches bursts of subnet changes, e.g. from applying a
// manifest with many subnets, into one republish.
const refreshDelay = 2 * time.Second

// refreshDevicesLoop republishes the devices after the profile reported a
// change on trigger. It waits refreshDelay after a trigger to batch bursts,
// and publishes only when the devices actually changed. It returns when ctx
// is done.
func refreshDevicesLoop(
	ctx context.Context,
	trigger <-chan struct{},
	delay time.Duration,
	refresh func() (resourceslice.DriverResources, bool, error),
	publish func(context.Context, resourceslice.DriverResources) error,
) {
	logger := klog.FromContext(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-trigger:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		// Triggers that arrived while waiting are covered by this refresh.
		select {
		case <-trigger:
		default:
		}
		resources, changed, err := refresh()
		if err != nil {
			logger.Error(err, "Refreshing devices failed")
			continue
		}
		if !changed {
			continue
		}
		if err := publish(ctx, resources); err != nil {
			logger.Error(err, "Publishing refreshed devices failed")
			continue
		}
		logger.Info("Published refreshed devices")
	}
}
