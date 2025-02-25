/* Copyright(C) 2022. Huawei Technologies Co.,Ltd. All rights reserved.
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

// Package common a series of common function
package common

import "sync/atomic"

// AtomicBool is an atomic Boolean.
type AtomicBool struct{ v uint32 }

// NewAtomicBool creates a AtomicBool.
func NewAtomicBool(initial bool) *AtomicBool {
	return &AtomicBool{v: boolToUint(initial)}
}

// Load atomically loads the Boolean.
func (b *AtomicBool) Load() bool {
	return atomic.LoadUint32(&b.v) == 1
}

// Store atomically stores the passed value.
func (b *AtomicBool) Store(new bool) {
	atomic.StoreUint32(&b.v, boolToUint(new))
}

func boolToUint(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}
