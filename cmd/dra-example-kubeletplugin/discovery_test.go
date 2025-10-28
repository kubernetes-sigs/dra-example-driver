package main

import (
	"testing"

	resourceapi "k8s.io/api/resource/v1"
)

func TestParseDeviceAttributes(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		wantErr  bool
		wantVals map[resourceapi.QualifiedName]func(resourceapi.DeviceAttribute) bool
	}{
		{
			name:     "empty",
			input:    []string{},
			wantErr:  false,
			wantVals: map[resourceapi.QualifiedName]func(resourceapi.DeviceAttribute) bool{},
		},
		{
			name:    "invalid format (missing '=')",
			input:   []string{"invalid"},
			wantErr: true,
		},
		{
			name:  "typed values",
			input: []string{"boolTrue=true", "boolFalse=false", "count=42", "driverVersion=1.2.3", "pre=1.0.0-beta.1", "name=LATEST-GPU-MODEL"},
			wantVals: map[resourceapi.QualifiedName]func(resourceapi.DeviceAttribute) bool{
				"boolTrue":      func(a resourceapi.DeviceAttribute) bool { return a.BoolValue != nil && *a.BoolValue },
				"boolFalse":     func(a resourceapi.DeviceAttribute) bool { return a.BoolValue != nil && !*a.BoolValue },
				"count":         func(a resourceapi.DeviceAttribute) bool { return a.IntValue != nil && *a.IntValue == 42 },
				"driverVersion": func(a resourceapi.DeviceAttribute) bool { return a.VersionValue != nil && *a.VersionValue == "1.2.3" },
				"pre": func(a resourceapi.DeviceAttribute) bool {
					return a.VersionValue != nil && *a.VersionValue == "1.0.0-beta.1"
				},
				"name": func(a resourceapi.DeviceAttribute) bool {
					return a.StringValue != nil && *a.StringValue == "LATEST-GPU-MODEL"
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDeviceAttributes(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for k, check := range tt.wantVals {
				a, ok := got[k]
				if !ok {
					t.Fatalf("missing key %q", k)
				}
				if !check(a) {
					t.Fatalf("value check failed for %q: %+v", k, a)
				}
			}
		})
	}
}

func TestIsSemanticVersion(t *testing.T) {
	cases := []struct {
		name string
		v    string
		ok   bool
	}{
		{"basic", "1.2.3", true},
		{"prerelease", "1.0.0-beta", true},
		{"prerelease+build", "1.0.0-beta+build", true},
		{"zeros", "0.0.1", true},
		{"missing patch", "1.2", false},
		{"too many parts", "1.2.3.4", false},
		{"invalid char", "1.0.x", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSemanticVersion(c.v); got != c.ok {
				t.Fatalf("isSemanticVersion(%q)=%v, want %v", c.v, got, c.ok)
			}
		})
	}
}
