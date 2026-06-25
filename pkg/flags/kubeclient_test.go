/*
 * Copyright 2026 The Kubernetes Authors.
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

package flags

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/metrics/legacyregistry"
)

func TestRestClientMetricsRegistered(t *testing.T) {
	t.Parallel()

	// Gauge metrics from the restclient package are exported as soon as the
	// package is linked via kubeclient.go's blank import.
	require.True(t, hasMetricFamily(t, "rest_client_transport_cache_entries"))
}

func TestRestClientMetricsObservedOnRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"NodeList","apiVersion":"v1","items":[]}`))
	}))
	t.Cleanup(server.Close)

	client, err := coreclientset.NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)

	_, err = client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)

	require.True(t, hasMetricFamily(t, "rest_client_requests_total"))
}

func hasMetricFamily(t *testing.T, name string) bool {
	t.Helper()

	metricFamilies, err := legacyregistry.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range metricFamilies {
		if family.GetName() == name {
			return true
		}
	}
	return false
}
