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

package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"
)

func TestNewHealthSimulator(t *testing.T) {
	names := []string{"gpu-0", "gpu-1", "gpu-2"}
	sim := NewHealthSimulator(names)

	for _, name := range names {
		health, message := sim.GetDeviceHealth(name)
		assert.Equal(t, drahealthv1alpha1.HealthStatus_HEALTHY, health, "device %s", name)
		assert.Contains(t, message, name)
		assert.NotEmpty(t, message)
	}
}

func TestGetDeviceHealth_UnknownDevice(t *testing.T) {
	sim := NewHealthSimulator([]string{"gpu-0"})

	health, message := sim.GetDeviceHealth("non-existent")
	assert.Equal(t, drahealthv1alpha1.HealthStatus_UNKNOWN, health)
	assert.Equal(t, "Device not found", message)
}

func TestNewHealthSimulator_EmptyDeviceList(t *testing.T) {
	sim := NewHealthSimulator([]string{})

	health, message := sim.GetDeviceHealth("any")
	assert.Equal(t, drahealthv1alpha1.HealthStatus_UNKNOWN, health)
	assert.Equal(t, "Device not found", message)
}

func TestNewHealthSimulator_PreservesDeviceNames(t *testing.T) {
	names := []string{"gpu-0-partition-0", "gpu-0-partition-1", "gpu-0-full"}
	sim := NewHealthSimulator(names)

	for _, name := range names {
		health, _ := sim.GetDeviceHealth(name)
		assert.Equal(t, drahealthv1alpha1.HealthStatus_HEALTHY, health, "device %s", name)
	}

	health, _ := sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, drahealthv1alpha1.HealthStatus_UNKNOWN, health)
}

func TestForceScenario(t *testing.T) {
	tests := map[string]struct {
		scenario        HealthScenario
		expectedHealth  drahealthv1alpha1.HealthStatus
		messageContains string
	}{
		"Healthy": {
			scenario:        ScenarioHealthy,
			expectedHealth:  drahealthv1alpha1.HealthStatus_HEALTHY,
			messageContains: "operating normally",
		},
		"TemperatureWarning": {
			scenario:        ScenarioTemperatureWarning,
			expectedHealth:  drahealthv1alpha1.HealthStatus_UNHEALTHY,
			messageContains: "warning threshold",
		},
		"ECCError": {
			scenario:        ScenarioECCError,
			expectedHealth:  drahealthv1alpha1.HealthStatus_UNHEALTHY,
			messageContains: "ECC error",
		},
		"CommunicationFailure": {
			scenario:        ScenarioCommunicationFailure,
			expectedHealth:  drahealthv1alpha1.HealthStatus_UNHEALTHY,
			messageContains: "communication timeout",
		},
		"Recovering": {
			scenario:        ScenarioRecovering,
			expectedHealth:  drahealthv1alpha1.HealthStatus_UNHEALTHY,
			messageContains: "recovery",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			sim := NewHealthSimulator([]string{"gpu-test"})
			sim.ForceScenario("gpu-test", tc.scenario)

			health, message := sim.GetDeviceHealth("gpu-test")
			assert.Equal(t, tc.expectedHealth, health)
			assert.Contains(t, strings.ToLower(message), strings.ToLower(tc.messageContains),
				"message %q should contain %q", message, tc.messageContains)
		})
	}
}

func TestHealthTransition(t *testing.T) {
	sim := NewHealthSimulator([]string{"gpu-0"})

	health, _ := sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, drahealthv1alpha1.HealthStatus_HEALTHY, health)

	sim.ForceScenario("gpu-0", ScenarioTemperatureWarning)
	health, msg := sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, drahealthv1alpha1.HealthStatus_UNHEALTHY, health)
	assert.Contains(t, msg, "temperature")

	sim.ForceScenario("gpu-0", ScenarioRecovering)
	health, msg = sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, drahealthv1alpha1.HealthStatus_UNHEALTHY, health)
	assert.Contains(t, strings.ToLower(msg), "recovery")

	sim.ForceScenario("gpu-0", ScenarioHealthy)
	health, msg = sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, drahealthv1alpha1.HealthStatus_HEALTHY, health)
	assert.Contains(t, msg, "operating normally")
}

func TestGetDeviceHealth_ConcurrentAccess(t *testing.T) {
	names := make([]string, 5)
	for i := range names {
		names[i] = "gpu-" + string(rune('0'+i))
	}
	sim := NewHealthSimulator(names)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				health, msg := sim.GetDeviceHealth(names[j%len(names)])
				require.True(t,
					health == drahealthv1alpha1.HealthStatus_HEALTHY ||
						health == drahealthv1alpha1.HealthStatus_UNHEALTHY ||
						health == drahealthv1alpha1.HealthStatus_UNKNOWN,
					"unexpected health status: %v", health)
				require.NotEmpty(t, msg)
			}
		}()
	}
	wg.Wait()
}
