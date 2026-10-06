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

package v1alpha1

import "testing"

func TestNicConfigNormalize(t *testing.T) {
	c := &NicConfig{}
	if err := c.Normalize(); err != nil || c.InterfaceName != "net1" {
		t.Errorf("Normalize() = %v, InterfaceName %q; want net1", err, c.InterfaceName)
	}
	c = &NicConfig{InterfaceName: "eth9"}
	if err := c.Normalize(); err != nil || c.InterfaceName != "eth9" {
		t.Errorf("Normalize() changed an explicit InterfaceName to %q", c.InterfaceName)
	}
	if d := DefaultNicConfig(); d.InterfaceName != "net1" || d.Kind != NicConfigKind {
		t.Errorf("DefaultNicConfig() = %+v", d)
	}
}

func TestNicConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		iface   string
		wantErr bool
	}{
		{"empty defaults", "", false},
		{"net1", "net1", false},
		{"15 characters", "abcdefghijklmno", false},
		{"16 characters", "abcdefghijklmnop", true},
		{"dot", ".", true},
		{"dot dot", "..", true},
		{"slash", "net/1", true},
		{"colon", "net:1", true},
		{"space", "net 1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&NicConfig{InterfaceName: tt.iface}).Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tt.iface, err, tt.wantErr)
			}
		})
	}
}
