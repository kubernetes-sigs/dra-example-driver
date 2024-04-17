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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	resourceapi "k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	gpucrd "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
)

func StartClaimParametersGenerator(ctx context.Context, config *Config) error {
	// Build a client set config
	csconfig, err := config.flags.kubeClientConfig.NewClientSetConfig()
	if err != nil {
		return fmt.Errorf("error creating client set config: %w", err)
	}

	// Create a new dynamic client
	dynamicClient, err := dynamic.NewForConfig(csconfig)
	if err != nil {
		return fmt.Errorf("error creating dynamic client: %w", err)
	}

	klog.Info("Starting ResourceClaimParamaters generator")

	// Set up informer to watch for GpuClaimParameters objects
	gpuClaimParametersInformer := newGpuClaimParametersInformer(ctx, dynamicClient)

	// Set up handler for events
	_, err = gpuClaimParametersInformer.AddEventHandler(newGpuClaimParametersHandler(ctx, config.clientSets.Core, dynamicClient))
	if err != nil {
		return fmt.Errorf("error adding event handler: %w", err)
	}

	// Start informer
	go gpuClaimParametersInformer.Run(ctx.Done())

	return nil
}

func newGpuClaimParametersInformer(ctx context.Context, dynamicClient dynamic.Interface) cache.SharedIndexInformer {
	// Set up shared index informer for GpuClaimParameters objects
	gvr := schema.GroupVersionResource{
		Group:    gpucrd.GroupName,
		Version:  gpucrd.Version,
		Resource: strings.ToLower(gpucrd.GpuClaimParametersKind),
	}

	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return dynamicClient.Resource(gvr).Watch(ctx, metav1.ListOptions{})
			},
		},
		&unstructured.Unstructured{},
		0, // resyncPeriod
		cache.Indexers{},
	)

	return informer
}

func newGpuClaimParametersHandler(ctx context.Context, clientset kubernetes.Interface, dynamicClient dynamic.Interface) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			unstructured, ok := obj.(*unstructured.Unstructured)
			if !ok {
				klog.Errorf("Error converting object to *unstructured.Unstructured: %v", obj)
			}

			var gpuClaimParameters gpucrd.GpuClaimParameters
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &gpuClaimParameters)
			if err != nil {
				klog.Errorf("Error converting *unstructured.Unstructured to GpuClaimParameters: %v", err)
				return
			}

			if err := createOrUpdateResourceClaimParameters(ctx, clientset, &gpuClaimParameters); err != nil {
				klog.Errorf("Error creating ResourceClaimParameters: %v", err)
				return
			}
		},
		UpdateFunc: func(oldObj any, newObj any) {
			unstructured, ok := newObj.(*unstructured.Unstructured)
			if !ok {
				klog.Errorf("Error converting object to *unstructured.Unstructured: %v", newObj)
			}

			var gpuClaimParameters gpucrd.GpuClaimParameters
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &gpuClaimParameters)
			if err != nil {
				klog.Errorf("Error converting *unstructured.Unstructured to GpuClaimParameters: %v", err)
				return
			}

			if err := createOrUpdateResourceClaimParameters(ctx, clientset, &gpuClaimParameters); err != nil {
				klog.Errorf("Error updating ResourceClaimParameters: %v", err)
				return
			}
		},
	}
}

func newResourceClaimParametersFromGpuClaimParameters(gpuClaimParameters *gpucrd.GpuClaimParameters) (*resourceapi.ResourceClaimParameters, error) {
	rawSpec, err := json.Marshal(gpuClaimParameters.Spec)
	if err != nil {
		return nil, fmt.Errorf("error marshaling GpuClaimParamaters to JSON: %w", err)
	}

	resourceCount := 1
	if gpuClaimParameters.Spec.Count != nil {
		resourceCount = *gpuClaimParameters.Spec.Count
	}

	selector := "true"
	shareable := true

	var resourceRequests []resourceapi.ResourceRequest
	for i := 0; i < resourceCount; i++ {
		request := resourceapi.ResourceRequest{
			ResourceRequestModel: resourceapi.ResourceRequestModel{
				NamedResources: &resourceapi.NamedResourcesRequest{
					Selector: selector,
				},
			},
		}
		resourceRequests = append(resourceRequests, request)
	}

	resourceClaimParameters := &resourceapi.ResourceClaimParameters{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "resource-claim-parameters-",
			Namespace:    gpuClaimParameters.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         gpuClaimParameters.APIVersion,
					Kind:               gpuClaimParameters.Kind,
					Name:               gpuClaimParameters.Name,
					UID:                gpuClaimParameters.UID,
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		GeneratedFrom: &resourceapi.ResourceClaimParametersReference{
			APIGroup: gpucrd.GroupName,
			Kind:     gpuClaimParameters.Kind,
			Name:     gpuClaimParameters.Name,
		},
		DriverRequests: []resourceapi.DriverRequests{
			{
				DriverName:       DriverName,
				VendorParameters: runtime.RawExtension{Raw: rawSpec},
				Requests:         resourceRequests,
			},
		},
		Shareable: shareable,
	}

	return resourceClaimParameters, nil
}

func createOrUpdateResourceClaimParameters(ctx context.Context, clientset kubernetes.Interface, gpuClaimParameters *gpucrd.GpuClaimParameters) error {
	namespace := gpuClaimParameters.Namespace

	// Get a list of existing ResourceClaimParameters in the same namespace as the incoming GpuClaimParameters
	existing, err := clientset.ResourceV1alpha2().ResourceClaimParameters(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing existing ResourceClaimParameters: %w", err)
	}

	// Build a new ResourceClaimParameters object from the incoming GpuClaimParameters object
	resourceClaimParameters, err := newResourceClaimParametersFromGpuClaimParameters(gpuClaimParameters)
	if err != nil {
		return fmt.Errorf("error building new ResourceClaimParameters object from a GpuClaimParameters object: %w", err)
	}

	// If there is an existing ResourceClaimParameters generated from the incoming GpuClaimParameters object, then update it
	if len(existing.Items) > 0 {
		for _, item := range existing.Items {
			if (item.GeneratedFrom.APIGroup == gpucrd.GroupName) &&
				(item.GeneratedFrom.Kind == gpuClaimParameters.Kind) &&
				(item.GeneratedFrom.Name == gpuClaimParameters.Name) {
				klog.Infof("ResourceClaimParameters already exists for GpuClaimParameters %s/%s, updating it", namespace, gpuClaimParameters.Name)

				// Copy the matching ResourceClaimParameters metadata into the new ResourceClaimParameters object before updating it
				resourceClaimParameters.ObjectMeta = *item.ObjectMeta.DeepCopy()

				_, err = clientset.ResourceV1alpha2().ResourceClaimParameters(namespace).Update(ctx, resourceClaimParameters, metav1.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("error updating ResourceClaimParameters object: %w", err)
				}

				return nil
			}
		}
	}

	// Otherwise create a new ResourceClaimParameters object from the incoming GpuClaimParameters object
	_, err = clientset.ResourceV1alpha2().ResourceClaimParameters(namespace).Create(ctx, resourceClaimParameters, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("error creating ResourceClaimParameters object from GpuClaimParameters object: %w", err)
	}

	klog.Infof("Created ResourceClaimParameters for GpuClaimParameters %s/%s", namespace, gpuClaimParameters.Name)
	return nil
}
