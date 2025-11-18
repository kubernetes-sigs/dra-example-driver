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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	admissionv1 "k8s.io/api/admission/v1"
	resourceapi "k8s.io/api/resource/v1"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	configapi "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
	"sigs.k8s.io/dra-example-driver/internal/profiles/gpu"
	"sigs.k8s.io/dra-example-driver/pkg/consts"
)

func TestReadyEndpoint(t *testing.T) {
	s := httptest.NewServer(newMux(nil, nil))
	t.Cleanup(s.Close)

	res, err := http.Get(s.URL + "/readyz")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func TestResourceClaimValidatingWebhook(t *testing.T) {
	unknownResource := metav1.GroupVersionResource{
		Group:    "resource.k8s.io",
		Version:  "v1",
		Resource: "unknownresources",
	}

	validGPUConfig := &configapi.GpuConfig{
		Sharing: &configapi.GpuSharing{
			Strategy: configapi.TimeSlicingStrategy,
			TimeSlicingConfig: &configapi.TimeSlicingConfig{
				Interval: configapi.DefaultTimeSlice,
			},
		},
	}

	invalidGPUConfigs := []*configapi.GpuConfig{
		{
			Sharing: &configapi.GpuSharing{
				Strategy: configapi.TimeSlicingStrategy,
				TimeSlicingConfig: &configapi.TimeSlicingConfig{
					Interval: "InvalidInterval",
				},
			},
		},
		{
			Sharing: &configapi.GpuSharing{
				Strategy: configapi.SpacePartitioningStrategy,
				SpacePartitioningConfig: &configapi.SpacePartitioningConfig{
					PartitionCount: -1,
				},
			},
		},
	}

	tests := map[string]struct {
		admissionReview      *admissionv1.AdmissionReview
		requestContentType   string
		expectedResponseCode int
		expectedAllowed      bool
		expectedMessage      string
	}{
		"bad contentType": {
			requestContentType:   "invalid type",
			expectedResponseCode: http.StatusUnsupportedMediaType,
		},
		"invalid AdmissionReview": {
			admissionReview:      &admissionv1.AdmissionReview{},
			expectedResponseCode: http.StatusBadRequest,
		},
		"valid GpuConfig in ResourceClaim": {
			admissionReview: admissionReviewWithObject(
				resourceClaimWithGpuConfigs(validGPUConfig),
				resourceClaimResourceV1,
			),
			expectedAllowed: true,
		},
		"invalid GpuConfigs in ResourceClaim": {
			admissionReview: admissionReviewWithObject(
				resourceClaimWithGpuConfigs(invalidGPUConfigs...),
				resourceClaimResourceV1,
			),
			expectedAllowed: false,
			expectedMessage: "2 configs failed to validate: object at spec.devices.config[0].opaque.parameters is invalid: unknown time-slice interval: InvalidInterval; object at spec.devices.config[1].opaque.parameters is invalid: invalid partition count: -1",
		},
		"valid GpuConfig in ResourceClaimTemplate": {
			admissionReview: admissionReviewWithObject(
				resourceClaimTemplateWithGpuConfigs(validGPUConfig),
				resourceClaimTemplateResourceV1,
			),
			expectedAllowed: true,
		},
		"invalid GpuConfigs in ResourceClaimTemplate": {
			admissionReview: admissionReviewWithObject(
				resourceClaimTemplateWithGpuConfigs(invalidGPUConfigs...),
				resourceClaimTemplateResourceV1,
			),
			expectedAllowed: false,
			expectedMessage: "2 configs failed to validate: object at spec.spec.devices.config[0].opaque.parameters is invalid: unknown time-slice interval: InvalidInterval; object at spec.spec.devices.config[1].opaque.parameters is invalid: invalid partition count: -1",
		},
		"valid GpuConfig in ResourceClaim v1beta1": {
			admissionReview: admissionReviewWithObject(
				toResourceClaimV1Beta1(resourceClaimWithGpuConfigs(validGPUConfig)),
				resourceClaimResourceV1Beta1,
			),
			expectedAllowed: true,
		},
		"invalid GpuConfigs in ResourceClaim v1beta1": {
			admissionReview: admissionReviewWithObject(
				toResourceClaimV1Beta1(resourceClaimWithGpuConfigs(invalidGPUConfigs...)),
				resourceClaimResourceV1Beta1,
			),
			expectedAllowed: false,
			expectedMessage: "2 configs failed to validate: object at spec.devices.config[0].opaque.parameters is invalid: unknown time-slice interval: InvalidInterval; object at spec.devices.config[1].opaque.parameters is invalid: invalid partition count: -1",
		},
		"valid GpuConfig in ResourceClaimTemplate v1beta1": {
			admissionReview: admissionReviewWithObject(
				toResourceClaimTemplateV1Beta1(resourceClaimTemplateWithGpuConfigs(validGPUConfig)),
				resourceClaimTemplateResourceV1Beta1,
			),
			expectedAllowed: true,
		},
		"invalid GpuConfigs in ResourceClaimTemplate v1beta1": {
			admissionReview: admissionReviewWithObject(
				toResourceClaimTemplateV1Beta1(resourceClaimTemplateWithGpuConfigs(invalidGPUConfigs...)),
				resourceClaimTemplateResourceV1Beta1,
			),
			expectedAllowed: false,
			expectedMessage: "2 configs failed to validate: object at spec.spec.devices.config[0].opaque.parameters is invalid: unknown time-slice interval: InvalidInterval; object at spec.spec.devices.config[1].opaque.parameters is invalid: invalid partition count: -1",
		},
		"unknown resource type": {
			admissionReview: admissionReviewWithObject(
				resourceClaimWithGpuConfigs(validGPUConfig),
				unknownResource,
			),
			expectedAllowed: false,
			expectedMessage: "expected resource to be one of [{resource.k8s.io v1 resourceclaims} {resource.k8s.io v1beta1 resourceclaims} {resource.k8s.io v1beta2 resourceclaims} {resource.k8s.io v1 resourceclaimtemplates} {resource.k8s.io v1beta1 resourceclaimtemplates} {resource.k8s.io v1beta2 resourceclaimtemplates}], got {resource.k8s.io v1 unknownresources}",
		},
	}

	sb := gpu.ConfigSchemeBuilder
	assert.NoError(t, sb.AddToScheme(configScheme))

	s := httptest.NewServer(newMux(newConfigDecoder(), gpu.ValidateConfig))
	t.Cleanup(s.Close)

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			requestBody, err := json.Marshal(test.admissionReview)
			require.NoError(t, err)

			contentType := test.requestContentType
			if contentType == "" {
				contentType = "application/json"
			}

			res, err := http.Post(s.URL+"/validate-resource-claim-parameters", contentType, bytes.NewReader(requestBody))
			require.NoError(t, err)
			expectedResponseCode := test.expectedResponseCode
			if expectedResponseCode == 0 {
				expectedResponseCode = http.StatusOK
			}
			assert.Equal(t, expectedResponseCode, res.StatusCode)
			if res.StatusCode != http.StatusOK {
				// We don't have an AdmissionReview to validate
				return
			}

			responseBody, err := io.ReadAll(res.Body)
			require.NoError(t, err)
			res.Body.Close()

			responseAdmissionReview, err := readAdmissionReview(responseBody)
			assert.NoError(t, err)
			assert.Equal(t, test.expectedAllowed, responseAdmissionReview.Response.Allowed)
			if !test.expectedAllowed {
				assert.Equal(t, test.expectedMessage, string(responseAdmissionReview.Response.Result.Message))
			}
		})
	}
}

func admissionReviewWithObject(obj runtime.Object, resource metav1.GroupVersionResource) *admissionv1.AdmissionReview {
	requestedAdmissionReview := &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			Resource: resource,
			Object: runtime.RawExtension{
				Object: obj,
			},
		},
	}
	requestedAdmissionReview.SetGroupVersionKind(admissionv1.SchemeGroupVersion.WithKind("AdmissionReview"))
	return requestedAdmissionReview
}

func resourceClaimWithGpuConfigs(gpuConfigs ...*configapi.GpuConfig) *resourceapi.ResourceClaim {
	resourceClaim := &resourceapi.ResourceClaim{
		Spec: resourceClaimSpecWithGpuConfigs(gpuConfigs...),
	}
	resourceClaim.SetGroupVersionKind(resourceapi.SchemeGroupVersion.WithKind("ResourceClaim"))
	return resourceClaim
}

func resourceClaimTemplateWithGpuConfigs(gpuConfigs ...*configapi.GpuConfig) *resourceapi.ResourceClaimTemplate {
	resourceClaimTemplate := &resourceapi.ResourceClaimTemplate{
		Spec: resourceapi.ResourceClaimTemplateSpec{
			Spec: resourceClaimSpecWithGpuConfigs(gpuConfigs...),
		},
	}
	resourceClaimTemplate.SetGroupVersionKind(resourceapi.SchemeGroupVersion.WithKind("ResourceClaimTemplate"))
	return resourceClaimTemplate
}

func resourceClaimSpecWithGpuConfigs(gpuConfigs ...*configapi.GpuConfig) resourceapi.ResourceClaimSpec {
	resourceClaimSpec := resourceapi.ResourceClaimSpec{}
	for _, gpuConfig := range gpuConfigs {
		gpuConfig.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   configapi.GroupName,
			Version: configapi.Version,
			Kind:    "GpuConfig",
		})
		deviceConfig := resourceapi.DeviceClaimConfiguration{
			DeviceConfiguration: resourceapi.DeviceConfiguration{
				Opaque: &resourceapi.OpaqueDeviceConfiguration{
					Driver: consts.DriverName,
					Parameters: runtime.RawExtension{
						Object: gpuConfig,
					},
				},
			},
		}
		resourceClaimSpec.Devices.Config = append(resourceClaimSpec.Devices.Config, deviceConfig)
	}
	return resourceClaimSpec
}

func toResourceClaimV1Beta1(v1Claim *resourceapi.ResourceClaim) *resourcev1beta1.ResourceClaim {
	v1beta1Claim := &resourcev1beta1.ResourceClaim{}
	if err := scheme.Convert(v1Claim, v1beta1Claim, nil); err != nil {
		panic(fmt.Sprintf("failed to convert ResourceClaim to v1beta1: %v", err))
	}
	return v1beta1Claim
}

func toResourceClaimTemplateV1Beta1(v1Template *resourceapi.ResourceClaimTemplate) *resourcev1beta1.ResourceClaimTemplate {
	v1beta1Template := &resourcev1beta1.ResourceClaimTemplate{}
	if err := scheme.Convert(v1Template, v1beta1Template, nil); err != nil {
		panic(fmt.Sprintf("failed to convert ResourceClaimTemplate to v1beta1: %v", err))
	}
	return v1beta1Template
}
