/*
 * Copyright 2024 The Kubernetes Authors.
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

package v1alpha1

import (
	"errors"
	"fmt"
	"math"
)

// Validate ensures that NetConfig has a valid set of values.
func (c *NetConfig) Validate() error {
	if c.BandwidthBurst == nil {
		return errors.New("no burst set")
	}
	return c.BandwidthBurst.Validate()
}

// Validate ensures that GpuSharingStrategy has a valid set of values.
func (b BandwidthBurstEntry) Validate() error {
	if err := validateRateAndBurst(b.IngressBurst); err != nil {
		return fmt.Errorf("invalid ingressBurst: %v", err)
	}
	if err := validateRateAndBurst(b.EgressBurst); err != nil {
		return fmt.Errorf("invalid egressBurst: %v", err)
	}
	return nil
}

func validateRateAndBurst(burst uint64) error {
	if burst > 0 && burst/8 >= math.MaxUint32 {
		return errors.New("burst cannot be more than 4GB")
	}
	return nil
}
