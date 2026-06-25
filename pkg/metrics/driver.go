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
	"time"

	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const (
	resultSuccess = "success"
	resultError   = "error"
)

var (
	PrepareClaimsTotal = k8smetrics.NewCounterVec(&k8smetrics.CounterOpts{
		Namespace:      Namespace,
		Subsystem:      Subsystem,
		Name:           "prepare_claims_total",
		StabilityLevel: k8smetrics.BETA,
		Help:           "Total number of resource claim prepare operations handled by the driver.",
	}, []string{"result"})

	PrepareClaimDurationSeconds = k8smetrics.NewHistogramVec(&k8smetrics.HistogramOpts{
		Namespace:      Namespace,
		Subsystem:      Subsystem,
		Name:           "prepare_claim_duration_seconds",
		StabilityLevel: k8smetrics.BETA,
		Help:           "Latency in seconds of resource claim prepare operations handled by the driver.",
		Buckets:        k8smetrics.DefBuckets,
	}, []string{"result"})

	UnprepareClaimsTotal = k8smetrics.NewCounterVec(&k8smetrics.CounterOpts{
		Namespace:      Namespace,
		Subsystem:      Subsystem,
		Name:           "unprepare_claims_total",
		StabilityLevel: k8smetrics.BETA,
		Help:           "Total number of resource claim unprepare operations handled by the driver.",
	}, []string{"result"})

	UnprepareClaimDurationSeconds = k8smetrics.NewHistogramVec(&k8smetrics.HistogramOpts{
		Namespace:      Namespace,
		Subsystem:      Subsystem,
		Name:           "unprepare_claim_duration_seconds",
		StabilityLevel: k8smetrics.BETA,
		Help:           "Latency in seconds of resource claim unprepare operations handled by the driver.",
		Buckets:        k8smetrics.DefBuckets,
	}, []string{"result"})

	FatalBackgroundErrorsTotal = k8smetrics.NewCounter(&k8smetrics.CounterOpts{
		Namespace:      Namespace,
		Subsystem:      Subsystem,
		Name:           "fatal_background_errors_total",
		StabilityLevel: k8smetrics.BETA,
		Help:           "Total number of fatal background errors reported by the driver.",
	})

	driverMetrics = []k8smetrics.Registerable{
		PrepareClaimsTotal,
		PrepareClaimDurationSeconds,
		UnprepareClaimsTotal,
		UnprepareClaimDurationSeconds,
		FatalBackgroundErrorsTotal,
	}
)

func init() {
	for _, metric := range driverMetrics {
		legacyregistry.MustRegister(metric)
	}
	initDriverMetricSeries()
}

// initDriverMetricSeries creates zero-valued counter time series so they are
// visible in /metrics before the first prepare or unprepare operation.
func initDriverMetricSeries() {
	for _, result := range []string{resultSuccess, resultError} {
		PrepareClaimsTotal.WithLabelValues(result).Add(0)
		UnprepareClaimsTotal.WithLabelValues(result).Add(0)
	}
}

// ObservePrepareClaim records metrics for a single prepare operation.
func ObservePrepareClaim(err error, duration time.Duration) {
	result := resultSuccess
	if err != nil {
		result = resultError
	}
	PrepareClaimsTotal.WithLabelValues(result).Inc()
	PrepareClaimDurationSeconds.WithLabelValues(result).Observe(duration.Seconds())
}

// ObserveUnprepareClaim records metrics for a single unprepare operation.
func ObserveUnprepareClaim(err error, duration time.Duration) {
	result := resultSuccess
	if err != nil {
		result = resultError
	}
	UnprepareClaimsTotal.WithLabelValues(result).Inc()
	UnprepareClaimDurationSeconds.WithLabelValues(result).Observe(duration.Seconds())
}
