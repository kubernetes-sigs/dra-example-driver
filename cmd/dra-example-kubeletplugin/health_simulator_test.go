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
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

func TestNewHealthSimulator(t *testing.T) {
	names := []string{"gpu-0", "gpu-1", "gpu-2"}
	sim := NewHealthSimulator(names, false)

	for _, name := range names {
		health, message := sim.GetDeviceHealth(name)
		assert.Equal(t, kubeletplugin.HealthStatusHealthy, health, "device %s", name)
		assert.Contains(t, message, name)
		assert.NotEmpty(t, message)
	}
}

func TestGetDeviceHealth_UnknownDevice(t *testing.T) {
	sim := NewHealthSimulator([]string{"gpu-0"}, false)

	health, message := sim.GetDeviceHealth("non-existent")
	assert.Equal(t, kubeletplugin.HealthStatusUnknown, health)
	assert.Equal(t, "Device not found", message)
}

func TestNewHealthSimulator_EmptyDeviceList(t *testing.T) {
	sim := NewHealthSimulator([]string{}, false)

	health, message := sim.GetDeviceHealth("any")
	assert.Equal(t, kubeletplugin.HealthStatusUnknown, health)
	assert.Equal(t, "Device not found", message)
}

func TestNewHealthSimulator_PreservesDeviceNames(t *testing.T) {
	names := []string{"gpu-0-partition-0", "gpu-0-partition-1", "gpu-0-full"}
	sim := NewHealthSimulator(names, false)

	for _, name := range names {
		health, _ := sim.GetDeviceHealth(name)
		assert.Equal(t, kubeletplugin.HealthStatusHealthy, health, "device %s", name)
	}

	health, _ := sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, kubeletplugin.HealthStatusUnknown, health)
}

func TestHasDevice(t *testing.T) {
	sim := NewHealthSimulator([]string{"gpu-0"}, false)
	assert.True(t, sim.HasDevice("gpu-0"))
	assert.False(t, sim.HasDevice("gpu-1"))
}

func TestForceScenario(t *testing.T) {
	tests := map[string]struct {
		scenario        HealthScenario
		expectedHealth  kubeletplugin.HealthStatus
		messageContains string
	}{
		"Healthy": {
			scenario:        ScenarioHealthy,
			expectedHealth:  kubeletplugin.HealthStatusHealthy,
			messageContains: "operating normally",
		},
		"TemperatureWarning": {
			scenario:        ScenarioTemperatureWarning,
			expectedHealth:  kubeletplugin.HealthStatusUnhealthy,
			messageContains: "warning threshold",
		},
		"ECCError": {
			scenario:        ScenarioECCError,
			expectedHealth:  kubeletplugin.HealthStatusUnhealthy,
			messageContains: "ECC error",
		},
		"CommunicationFailure": {
			scenario:        ScenarioCommunicationFailure,
			expectedHealth:  kubeletplugin.HealthStatusUnhealthy,
			messageContains: "communication timeout",
		},
		"Recovering": {
			scenario:        ScenarioRecovering,
			expectedHealth:  kubeletplugin.HealthStatusUnhealthy,
			messageContains: "recovery",
		},
		"Unknown": {
			scenario:        ScenarioUnknown,
			expectedHealth:  kubeletplugin.HealthStatusUnknown,
			messageContains: "cannot be determined",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			sim := NewHealthSimulator([]string{"gpu-test"}, false)
			sim.ForceScenario("gpu-test", tc.scenario)

			health, message := sim.GetDeviceHealth("gpu-test")
			assert.Equal(t, tc.expectedHealth, health)
			assert.Contains(t, strings.ToLower(message), strings.ToLower(tc.messageContains),
				"message %q should contain %q", message, tc.messageContains)
		})
	}
}

func TestHealthTransition(t *testing.T) {
	sim := NewHealthSimulator([]string{"gpu-0"}, false)

	health, _ := sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, kubeletplugin.HealthStatusHealthy, health)

	sim.ForceScenario("gpu-0", ScenarioTemperatureWarning)
	health, msg := sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, kubeletplugin.HealthStatusUnhealthy, health)
	assert.Contains(t, msg, "temperature")

	sim.ForceScenario("gpu-0", ScenarioRecovering)
	health, msg = sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, kubeletplugin.HealthStatusUnhealthy, health)
	assert.Contains(t, strings.ToLower(msg), "recovery")

	sim.ForceScenario("gpu-0", ScenarioHealthy)
	health, msg = sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, kubeletplugin.HealthStatusHealthy, health)
	assert.Contains(t, msg, "operating normally")
}

// TestOverrideFreezesSimulation proves that an annotation override pins a device
// even with auto-simulation enabled: without the freeze, updateDeviceState would
// have advanced the backdated TemperatureWarning into Recovering.
func TestOverrideFreezesSimulation(t *testing.T) {
	sim := NewHealthSimulator([]string{"gpu-0"}, true)
	sim.ForceScenario("gpu-0", ScenarioTemperatureWarning)

	sim.deviceStates["gpu-0"].scenarioStart = time.Now().Add(-5 * time.Minute)

	for i := 0; i < 50; i++ {
		health, msg := sim.GetDeviceHealth("gpu-0")
		require.Equal(t, kubeletplugin.HealthStatusUnhealthy, health)
		require.Contains(t, msg, "temperature")
	}
	require.Equal(t, ScenarioTemperatureWarning, sim.deviceStates["gpu-0"].scenario,
		"overridden device must not leave the forced scenario")
}

func TestClearOverrideReturnsToSimulation(t *testing.T) {
	sim := NewHealthSimulator([]string{"gpu-0"}, false)
	sim.ForceScenario("gpu-0", ScenarioTemperatureWarning)
	require.True(t, sim.deviceStates["gpu-0"].overridden)

	sim.ClearOverride("gpu-0")

	state := sim.deviceStates["gpu-0"]
	assert.False(t, state.overridden, "override flag must be cleared")
	assert.Equal(t, ScenarioHealthy, state.scenario)
	assert.GreaterOrEqual(t, state.temperature, 45, "temperature should reset to nominal range")
	assert.LessOrEqual(t, state.temperature, 54, "temperature should reset to nominal range")

	health, msg := sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, kubeletplugin.HealthStatusHealthy, health)
	assert.Contains(t, msg, "operating normally")
}

func TestUnknownOverrideReportsUnknown(t *testing.T) {
	sim := NewHealthSimulator([]string{"gpu-0"}, false)
	sim.ForceScenario("gpu-0", ScenarioUnknown)

	health, msg := sim.GetDeviceHealth("gpu-0")
	assert.Equal(t, kubeletplugin.HealthStatusUnknown, health)
	assert.Contains(t, msg, "cannot be determined")
}

// TestAutoSimulateDisabledStaysHealthy verifies the default (auto-simulation
// off) keeps non-overridden devices healthy indefinitely, which is what makes
// the example deterministic and its e2e baseline hermetic.
func TestAutoSimulateDisabledStaysHealthy(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	sim := newHealthSimulator([]string{"gpu-0"}, false, func() time.Time { return clock }, rand.New(rand.NewSource(1)))

	for i := 0; i < 100; i++ {
		clock = clock.Add(1 * time.Minute)
		health, _ := sim.GetDeviceHealth("gpu-0")
		require.Equal(t, kubeletplugin.HealthStatusHealthy, health)
	}
}

// TestRecoveryCompletesAfterTwoMinutes exercises the time-based recovery path
// deterministically via an injected clock, including both sides of the boundary.
func TestRecoveryCompletesAfterTwoMinutes(t *testing.T) {
	for _, elapsed := range []time.Duration{
		0,
		30 * time.Second,
		time.Minute,
		90 * time.Second,
		2*time.Minute - time.Nanosecond,
		2 * time.Minute,
		2*time.Minute + time.Nanosecond,
		3 * time.Minute,
	} {
		t.Run(elapsed.String(), func(t *testing.T) {
			base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			clock := base
			sim := newHealthSimulator([]string{"gpu-0"}, true, func() time.Time { return clock }, rand.New(rand.NewSource(1)))

			// Put the device into a non-overridden recovering state as the
			// free-running simulation would, with fault telemetry to reset.
			st := sim.deviceStates["gpu-0"]
			st.scenario = ScenarioRecovering
			st.scenarioStart = clock
			st.recoveryStart = clock
			st.temperature = 91
			st.eccErrorCount = 15
			st.failureCount = 1

			clock = base.Add(elapsed)
			expectedHealth := kubeletplugin.HealthStatusUnhealthy
			if elapsed >= 2*time.Minute {
				expectedHealth = kubeletplugin.HealthStatusHealthy
			}

			// Reporting without a simulation step must use the same threshold.
			peekHealth, peekMessage := sim.PeekDeviceHealth("gpu-0")
			assert.Equal(t, expectedHealth, peekHealth)
			assert.Equal(t, ScenarioRecovering, st.scenario, "peek must not advance the scenario")

			health, message := sim.GetDeviceHealth("gpu-0")
			assert.Equal(t, expectedHealth, health)
			if elapsed < 2*time.Minute {
				assert.Equal(t, ScenarioRecovering, st.scenario)
				assert.Contains(t, peekMessage, "recovery")
				assert.Contains(t, message, "recovery")
				return
			}
			assert.Equal(t, ScenarioHealthy, st.scenario)
			assert.Contains(t, peekMessage, "recovered successfully")
			assert.Contains(t, message, "operating normally")
			assert.GreaterOrEqual(t, st.temperature, 45)
			assert.LessOrEqual(t, st.temperature, 54)
			assert.Zero(t, st.eccErrorCount)
			assert.Zero(t, st.failureCount)
		})
	}
}

// TestTemperatureStaysClamped drives the random walk over many ticks and asserts
// the temperature clamp invariant holds, covering the drift/clamp branches.
func TestTemperatureStaysClamped(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	sim := newHealthSimulator([]string{"gpu-0"}, true, func() time.Time { return clock }, rand.New(rand.NewSource(42)))

	for i := 0; i < 2000; i++ {
		clock = clock.Add(10 * time.Second)
		sim.GetDeviceHealth("gpu-0")
		temp := sim.deviceStates["gpu-0"].temperature
		require.GreaterOrEqual(t, temp, 40)
		require.LessOrEqual(t, temp, 95)
	}
}

func TestGetDeviceHealth_ConcurrentAccess(t *testing.T) {
	names := make([]string, 5)
	for i := range names {
		names[i] = fmt.Sprintf("gpu-%d", i)
	}
	sim := NewHealthSimulator(names, true)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				health, msg := sim.GetDeviceHealth(names[j%len(names)])
				// assert, not require: FailNow must only be called from
				// the test goroutine.
				assert.True(t,
					health == kubeletplugin.HealthStatusHealthy ||
						health == kubeletplugin.HealthStatusUnhealthy ||
						health == kubeletplugin.HealthStatusUnknown,
					"unexpected health status: %v", health)
				assert.NotEmpty(t, msg)
			}
		}()
	}
	wg.Wait()
}
