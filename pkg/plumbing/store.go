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

// NICStore holds the NICs of every pod on the node between the two phases of
// the attach lifecycle, which run in different callbacks at different times:
//
//   - PrepareResourceClaims (allocation resolved, no sandbox yet) calls Add
//     with one Spec per NIC, keyed by pod UID and claim UID.
//   - The NRI RunPodSandbox hook reads the pod's Specs with Specs, fills in
//     the now-known netns path and runs Attach.
//   - UnprepareResourceClaims drops the claim's Specs with RemoveClaim.
//
// The Specs stay until the claim is unprepared, so a recreated sandbox of the
// same pod gets its NICs again. The kubelet plugin persists them in its
// checkpoint and adds them back after a restart.
type NICStore struct {
	mu    sync.Mutex
	byPod map[string][]Spec
	// attached holds the Specs of attached NICs, with the sandbox fields filled
	// in, so StopPodSandbox can detach them.
	attached map[string][]Spec
}

// NewNICStore returns an empty store.
func NewNICStore() *NICStore {
	return &NICStore{byPod: make(map[string][]Spec), attached: make(map[string][]Spec)}
}

// Add records a NIC of a pod. A Spec for the same claim and interface replaces
// the existing one.
func (p *NICStore) Add(spec Spec) {
	p.mu.Lock()
	defer p.mu.Unlock()
	specs := p.byPod[spec.PodUID]
	for i := range specs {
		if specs[i].ClaimUID == spec.ClaimUID && specs[i].IfaceName == spec.IfaceName {
			specs[i] = spec
			return
		}
	}
	p.byPod[spec.PodUID] = append(specs, spec)
}

// Specs returns a copy of the NICs of a pod; nil for a pod without DRA NICs.
func (p *NICStore) Specs(podUID string) []Spec {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.byPod[podUID]) == 0 {
		return nil
	}
	return append([]Spec(nil), p.byPod[podUID]...)
}

// RemoveClaim forgets the NICs of a claim.
func (p *NICStore) RemoveClaim(claimUID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for podUID, specs := range p.byPod {
		kept := specs[:0]
		for _, spec := range specs {
			if spec.ClaimUID != claimUID {
				kept = append(kept, spec)
			}
		}
		if len(kept) == 0 {
			delete(p.byPod, podUID)
		} else {
			p.byPod[podUID] = kept
		}
	}
}

// MarkAttached records a NIC attached for podUID. A repeated attach of the
// same interface, e.g. from NRI Synchronize, replaces the record.
func (p *NICStore) MarkAttached(podUID string, spec Spec) {
	p.mu.Lock()
	defer p.mu.Unlock()
	attached := p.attached[podUID]
	for i := range attached {
		if attached[i].IfaceName == spec.IfaceName {
			attached[i] = spec
			return
		}
	}
	p.attached[podUID] = append(attached, spec)
}

// TakeAttached returns and forgets the attached Specs for podUID.
func (p *NICStore) TakeAttached(podUID string) []Spec {
	p.mu.Lock()
	defer p.mu.Unlock()
	specs := p.attached[podUID]
	delete(p.attached, podUID)
	return specs
}
