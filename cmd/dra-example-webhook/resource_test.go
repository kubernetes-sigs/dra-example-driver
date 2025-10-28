/*
Copyright 2025 The Kubernetes Authors.

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

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	resourcev1 "k8s.io/api/resource/v1"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	resourcev1beta2 "k8s.io/api/resource/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestExtractResourceClaim(t *testing.T) {
	// Create test objects for each version
	v1Claim := &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-v1-claim",
			Namespace: "default",
		},
		Spec: resourcev1.ResourceClaimSpec{
			Devices: resourcev1.DeviceClaim{
				Requests: []resourcev1.DeviceRequest{
					{
						Name: "test-device",
						Exactly: &resourcev1.ExactDeviceRequest{
							DeviceClassName: "test-class",
						},
					},
				},
			},
		},
	}

	v1beta1Claim := &resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-v1beta1-claim",
			Namespace: "default",
		},
		Spec: resourcev1beta1.ResourceClaimSpec{
			Devices: resourcev1beta1.DeviceClaim{
				Requests: []resourcev1beta1.DeviceRequest{
					{
						Name:            "test-device",
						DeviceClassName: "test-class",
					},
				},
			},
		},
	}

	v1beta2Claim := &resourcev1beta2.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-v1beta2-claim",
			Namespace: "default",
		},
		Spec: resourcev1beta2.ResourceClaimSpec{
			Devices: resourcev1beta2.DeviceClaim{
				Requests: []resourcev1beta2.DeviceRequest{
					{
						Name: "test-device",
						Exactly: &resourcev1beta2.ExactDeviceRequest{
							DeviceClassName: "test-class",
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name         string
		resource     metav1.GroupVersionResource
		obj          runtime.Object
		expectedName string
		expectError  bool
	}{
		{
			name:         "v1 ResourceClaim",
			resource:     resourceClaimResourceV1,
			obj:          v1Claim,
			expectedName: "test-v1-claim",
			expectError:  false,
		},
		{
			name:         "v1beta1 ResourceClaim",
			resource:     resourceClaimResourceV1Beta1,
			obj:          v1beta1Claim,
			expectedName: "test-v1beta1-claim",
			expectError:  false,
		},
		{
			name:         "v1beta2 ResourceClaim",
			resource:     resourceClaimResourceV1Beta2,
			obj:          v1beta2Claim,
			expectedName: "test-v1beta2-claim",
			expectError:  false,
		},
		{
			name:         "Unsupported version",
			resource:     metav1.GroupVersionResource{Group: "resource.k8s.io", Version: "v2", Resource: "resourceclaims"},
			obj:          v1Claim,
			expectedName: "",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Serialize the object
			data, err := json.Marshal(tt.obj)
			assert.NoError(t, err)

			// Create AdmissionReview
			ar := admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					Resource: tt.resource,
					Object: runtime.RawExtension{
						Raw: data,
					},
				},
			}

			// Call the function
			result, err := extractResourceClaim(ar)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedName, result.Name)
				assert.Equal(t, "default", result.Namespace)
				assert.Equal(t, "test-device", result.Spec.Devices.Requests[0].Name)
				assert.Equal(t, "test-class", result.Spec.Devices.Requests[0].Exactly.DeviceClassName)
			}
		})
	}
}
