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

package metrics

import (
	"errors"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"k8s.io/component-base/metrics/legacyregistry"
)

func TestObservePrepareClaim(t *testing.T) {
	t.Parallel()

	ObservePrepareClaim(nil, 25*time.Millisecond)
	ObservePrepareClaim(errors.New("prepare failed"), 50*time.Millisecond)

	require.Equal(t, float64(1), counterValue(t, "dra_example_driver_prepare_claims_total", map[string]string{"result": "success"}))
	require.Equal(t, float64(1), counterValue(t, "dra_example_driver_prepare_claims_total", map[string]string{"result": "error"}))
}

func TestObserveUnprepareClaim(t *testing.T) {
	t.Parallel()

	ObserveUnprepareClaim(nil, 10*time.Millisecond)
	ObserveUnprepareClaim(errors.New("unprepare failed"), 20*time.Millisecond)

	require.Equal(t, float64(1), counterValue(t, "dra_example_driver_unprepare_claims_total", map[string]string{"result": "success"}))
	require.Equal(t, float64(1), counterValue(t, "dra_example_driver_unprepare_claims_total", map[string]string{"result": "error"}))
}

func counterValue(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()

	metrics, err := legacyregistry.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, metricFamily := range metrics {
		if metricFamily.GetName() != name {
			continue
		}
		for _, metric := range metricFamily.GetMetric() {
			if !labelsMatch(metric.GetLabel(), labels) {
				continue
			}
			return metric.GetCounter().GetValue()
		}
	}

	t.Fatalf("metric %q with labels %v not found", name, labels)
	return 0
}

func labelsMatch(got []*dto.LabelPair, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, label := range got {
		if want[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}
