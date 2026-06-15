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

package helpers

import "fmt"

// GetCDIDeviceID returns the per-claim identifier for an allocated device. For
// a share of a consumable-capacity device the ShareID is appended so that
// multiple allocations of the same device map to distinct CDI devices; an
// exclusively allocated device (nil ShareID) uses its name unchanged.
func GetCDIDeviceID(device string, shareId *string) string {
	if shareId != nil {
		return fmt.Sprintf("%s-%s", device, *shareId)
	}
	return device
}
