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

package plumbing

import "sync"

// PendingStore bridges the two phases of the attach lifecycle, which run in
// different callbacks at different times:
//
//   - PrepareResourceClaims (allocation resolved, no sandbox yet) calls Add to
//     stash one Spec per NIC, keyed by pod UID.
//   - The NRI RunPodSandbox hook calls Take(podUID) to drain every Spec for the
//     pod, fills in the now-known NetnsPath, and runs Attach.
//
// A pod may claim more than one NIC, so each key maps to a slice of Specs.
//
// The store lives in memory only. A plugin restart between Prepare and
// RunPodSandbox loses the pending Specs, so that pod starts without its DRA
// NICs; detaching after a restart falls back to the OVS external_ids (see
// SandboxHandler.OnStopPodSandbox).
type PendingStore struct {
	mu       sync.Mutex
	byPodUID map[string][]Spec
	// attached holds the Specs of attached NICs, with the sandbox fields filled
	// in, so StopPodSandbox can detach them.
	attached map[string][]Spec
}

// NewPendingStore returns an empty store.
func NewPendingStore() *PendingStore {
	return &PendingStore{byPodUID: make(map[string][]Spec), attached: make(map[string][]Spec)}
}

// Add records a NIC Spec to attach when the pod's sandbox appears.
func (p *PendingStore) Add(podUID string, spec Spec) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byPodUID[podUID] = append(p.byPodUID[podUID], spec)
}

// Take removes and returns all pending Specs for a pod. The NRI hook calls this
// from RunPodSandbox; an unknown pod (no DRA NICs) yields nil, len 0.
func (p *PendingStore) Take(podUID string) []Spec {
	p.mu.Lock()
	defer p.mu.Unlock()
	specs := p.byPodUID[podUID]
	delete(p.byPodUID, podUID)
	return specs
}

// MarkAttached records Specs whose NIC was attached for podUID.
func (p *PendingStore) MarkAttached(podUID string, specs ...Spec) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attached[podUID] = append(p.attached[podUID], specs...)
}

// TakeAttached returns and forgets the attached Specs for podUID.
func (p *PendingStore) TakeAttached(podUID string) []Spec {
	p.mu.Lock()
	defer p.mu.Unlock()
	specs := p.attached[podUID]
	delete(p.attached, podUID)
	return specs
}
