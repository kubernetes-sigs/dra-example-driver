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
	"sync"
	"time"

	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"
)

type HealthScenario int

const (
	ScenarioHealthy HealthScenario = iota
	ScenarioTemperatureWarning
	ScenarioECCError
	ScenarioCommunicationFailure
	ScenarioRecovering
)

type DeviceHealthState struct {
	scenario      HealthScenario
	scenarioStart time.Time
	temperature   int
	eccErrorCount int
	failureCount  int
	recoveryStart time.Time
}

type HealthSimulator struct {
	mu           sync.RWMutex
	deviceStates map[string]*DeviceHealthState
	rand         *rand.Rand
}

func NewHealthSimulator(deviceNames []string) *HealthSimulator {
	simulator := &HealthSimulator{
		deviceStates: make(map[string]*DeviceHealthState),
		rand:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for _, name := range deviceNames {
		simulator.deviceStates[name] = &DeviceHealthState{
			scenario:      ScenarioHealthy,
			scenarioStart: time.Now(),
			temperature:   45 + simulator.rand.Intn(10),
		}
	}

	return simulator
}

func (s *HealthSimulator) ForceScenario(deviceName string, scenario HealthScenario) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.deviceStates[deviceName]
	if !exists {
		return
	}
	state.scenario = scenario
	state.scenarioStart = time.Now()
	if scenario == ScenarioRecovering {
		state.recoveryStart = time.Now()
	}
	if scenario == ScenarioTemperatureWarning {
		state.temperature = 80
	}
	if scenario == ScenarioECCError {
		state.eccErrorCount = 15
	}
	if scenario == ScenarioCommunicationFailure {
		state.failureCount++
	}
}

func (s *HealthSimulator) GetDeviceHealth(deviceName string) (drahealthv1alpha1.HealthStatus, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.deviceStates[deviceName]
	if !exists {
		return drahealthv1alpha1.HealthStatus_UNKNOWN, "Device not found"
	}

	s.updateDeviceState(deviceName, state)
	return s.generateHealthStatusAndMessage(deviceName, state)
}

func (s *HealthSimulator) updateDeviceState(deviceName string, state *DeviceHealthState) {
	now := time.Now()
	timeSinceScenarioStart := now.Sub(state.scenarioStart)

	if state.scenario == ScenarioRecovering {
		if timeSinceScenarioStart > 2*time.Minute {
			state.scenario = ScenarioHealthy
			state.temperature = 45 + s.rand.Intn(10)
			state.eccErrorCount = 0
			state.failureCount = 0
			state.scenarioStart = now
		}
		return
	}

	if s.rand.Float32() < 0.3 {
		delta := s.rand.Intn(5) - 2 // -2 to +2 degrees
		state.temperature += delta
		if state.temperature < 40 {
			state.temperature = 40
		}
		if state.temperature > 95 {
			state.temperature = 95
		}
	}

	switch state.scenario {
	case ScenarioHealthy:
		// Small probability of transitioning to a problem scenario
		if timeSinceScenarioStart > 1*time.Minute {
			probability := s.rand.Float32()
			switch {
			case probability < 0.05: // 5% chance of temperature warning
				state.scenario = ScenarioTemperatureWarning
				state.temperature = 75 + s.rand.Intn(15) // 75-89°C
				state.scenarioStart = now
			case probability < 0.08: // 3% chance of ECC error
				state.scenario = ScenarioECCError
				state.eccErrorCount = 10 + s.rand.Intn(20) // 10-29 errors
				state.scenarioStart = now
			case probability < 0.10: // 2% chance of communication failure
				state.scenario = ScenarioCommunicationFailure
				state.failureCount++
				state.scenarioStart = now
			}
		}

	case ScenarioTemperatureWarning:
		// Temperature warnings persist for 1-2 minutes then either recover or escalate
		if timeSinceScenarioStart > 90*time.Second {
			if s.rand.Float32() < 0.7 {
				state.scenario = ScenarioRecovering
				state.scenarioStart = now
				state.recoveryStart = now
			} else { // 30% chance of escalation to critical
				state.temperature = 90 + s.rand.Intn(5)
			}
		}

	case ScenarioECCError:
		// ECC errors accumulate over time
		if s.rand.Float32() < 0.3 {
			state.eccErrorCount += s.rand.Intn(5)
		}
		// After 1 minute, initiate recovery
		if timeSinceScenarioStart > 1*time.Minute {
			state.scenario = ScenarioRecovering
			state.scenarioStart = now
			state.recoveryStart = now
		}

	case ScenarioCommunicationFailure:
		// Communication failures persist for 30-60 seconds then recover
		if timeSinceScenarioStart > time.Duration(30+s.rand.Intn(30))*time.Second {
			state.scenario = ScenarioRecovering
			state.scenarioStart = now
			state.recoveryStart = now
		}
	}
}

func (s *HealthSimulator) generateHealthStatusAndMessage(deviceName string, state *DeviceHealthState) (drahealthv1alpha1.HealthStatus, string) {
	switch state.scenario {
	case ScenarioHealthy:
		return drahealthv1alpha1.HealthStatus_HEALTHY,
			fmt.Sprintf("Device %s operating normally, temperature: %d°C", deviceName, state.temperature)

	case ScenarioTemperatureWarning:
		if state.temperature >= 90 {
			return drahealthv1alpha1.HealthStatus_UNHEALTHY,
				fmt.Sprintf("Critical: %s temperature at %d°C (exceeds safe threshold of 85°C)", deviceName, state.temperature)
		}
		return drahealthv1alpha1.HealthStatus_UNHEALTHY,
			fmt.Sprintf("%s temperature: %d°C (warning threshold exceeded, safe limit is 85°C)", deviceName, state.temperature)

	case ScenarioECCError:
		return drahealthv1alpha1.HealthStatus_UNHEALTHY,
			fmt.Sprintf("ECC error count exceeded threshold on %s (%d errors in last hour)", deviceName, state.eccErrorCount)

	case ScenarioCommunicationFailure:
		return drahealthv1alpha1.HealthStatus_UNHEALTHY,
			fmt.Sprintf("Driver communication timeout on %s (attempt %d)", deviceName, state.failureCount)

	case ScenarioRecovering:
		recoveryDuration := time.Since(state.recoveryStart)
		if recoveryDuration < 30*time.Second {
			return drahealthv1alpha1.HealthStatus_UNHEALTHY,
				fmt.Sprintf("%s initiating recovery sequence (diagnostics in progress)", deviceName)
		} else if recoveryDuration < 1*time.Minute {
			return drahealthv1alpha1.HealthStatus_UNHEALTHY,
				fmt.Sprintf("%s recovery in progress (running self-tests)", deviceName)
		} else {
			return drahealthv1alpha1.HealthStatus_HEALTHY,
				fmt.Sprintf("%s recovered successfully, all diagnostics passed", deviceName)
		}

	default:
		return drahealthv1alpha1.HealthStatus_UNKNOWN,
			fmt.Sprintf("Unknown state for device %s", deviceName)
	}
}

