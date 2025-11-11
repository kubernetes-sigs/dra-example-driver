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

	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

// HealthScenario is a simulated device condition. A real driver would derive
// health from actual device telemetry (NVML events, sysfs, a vendor SDK, ...);
// here we fake it so the example can demonstrate the KEP-4680 reporting path.
type HealthScenario int

const (
	ScenarioHealthy HealthScenario = iota
	ScenarioTemperatureWarning
	ScenarioECCError
	ScenarioCommunicationFailure
	ScenarioRecovering
	// ScenarioUnknown models a device whose health cannot be determined (the
	// third KEP-4680 state). It is reached only via an explicit override, since
	// the random simulation never produces Unknown on its own.
	ScenarioUnknown
)

// simulatedRecoveryDuration is shared by scenario transitions and health reports
// so a recovering device cannot report Healthy before recovery completes.
const simulatedRecoveryDuration = 2 * time.Minute

type DeviceHealthState struct {
	scenario      HealthScenario
	scenarioStart time.Time
	temperature   int
	eccErrorCount int
	failureCount  int
	recoveryStart time.Time
	// overridden marks a device whose scenario was pinned by an annotation
	// override. While set, the random simulation is frozen so the forced state
	// stays put until the override changes or is removed (see ClearOverride).
	overridden bool
}

type HealthSimulator struct {
	mu           sync.RWMutex
	deviceStates map[string]*DeviceHealthState
	rand         *rand.Rand
	// now returns the current time. It is a field (rather than a direct
	// time.Now call) so tests can inject a deterministic clock.
	now func() time.Time
	// autoSimulate enables the random scenario walk. When false (the default),
	// devices stay healthy until an annotation override pins them to a scenario,
	// which keeps the example deterministic and reproducible. Set it to true to
	// watch devices drift through simulated faults on their own.
	autoSimulate bool
}

// NewHealthSimulator builds a simulator for the given devices. autoSimulate
// controls whether devices randomly walk through simulated faults; when false
// they report healthy until pinned by an annotation override.
func NewHealthSimulator(deviceNames []string, autoSimulate bool) *HealthSimulator {
	return newHealthSimulator(deviceNames, autoSimulate, time.Now, rand.New(rand.NewSource(time.Now().UnixNano())))
}

// newHealthSimulator is the injectable constructor used by tests to supply a
// deterministic clock and rand source.
func newHealthSimulator(deviceNames []string, autoSimulate bool, now func() time.Time, rng *rand.Rand) *HealthSimulator {
	simulator := &HealthSimulator{
		deviceStates: make(map[string]*DeviceHealthState),
		rand:         rng,
		now:          now,
		autoSimulate: autoSimulate,
	}

	for _, name := range deviceNames {
		simulator.deviceStates[name] = &DeviceHealthState{
			scenario:      ScenarioHealthy,
			scenarioStart: now(),
			temperature:   45 + simulator.rand.Intn(10),
		}
	}

	return simulator
}

// HasDevice reports whether the simulator knows about the named device.
func (s *HealthSimulator) HasDevice(deviceName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.deviceStates[deviceName]
	return ok
}

// ForceScenario pins a device to the given scenario and marks it as overridden,
// so the random simulation stops evolving it until ClearOverride is called. This
// is what the driver's annotation-override path uses to make a device's health
// deterministic for the demo.
func (s *HealthSimulator) ForceScenario(deviceName string, scenario HealthScenario) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.deviceStates[deviceName]
	if !exists {
		return
	}
	state.scenario = scenario
	state.scenarioStart = s.now()
	state.overridden = true
	switch scenario {
	case ScenarioHealthy:
		// Reset accumulated fault state so a forced-healthy device reports a
		// nominal temperature rather than a stale warning-range value.
		state.temperature = 45 + s.rand.Intn(10)
		state.eccErrorCount = 0
		state.failureCount = 0
	case ScenarioRecovering:
		state.recoveryStart = s.now()
	case ScenarioTemperatureWarning:
		state.temperature = 80
	case ScenarioECCError:
		state.eccErrorCount = 15
	case ScenarioCommunicationFailure:
		state.failureCount++
	}
}

// ClearOverride removes an annotation override and returns the device to the
// free-running simulation in a clean healthy state.
func (s *HealthSimulator) ClearOverride(deviceName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.deviceStates[deviceName]
	if !exists {
		return
	}
	state.overridden = false
	state.scenario = ScenarioHealthy
	state.scenarioStart = s.now()
	state.temperature = 45 + s.rand.Intn(10)
	state.eccErrorCount = 0
	state.failureCount = 0
}

// GetDeviceHealth advances the simulation for the device by one step and returns
// its current health. This is the mutating read used by the periodic poll.
func (s *HealthSimulator) GetDeviceHealth(deviceName string) (kubeletplugin.HealthStatus, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.deviceStates[deviceName]
	if !exists {
		return kubeletplugin.HealthStatusUnknown, "Device not found"
	}

	s.updateDeviceState(deviceName, state)
	return s.generateHealthStatusAndMessage(deviceName, state)
}

// PeekDeviceHealth returns the device's current health without advancing the
// simulation. buildHealthReport uses it so that fanning a report out to multiple
// subscribers does not step the state machine multiple times per poll.
func (s *HealthSimulator) PeekDeviceHealth(deviceName string) (kubeletplugin.HealthStatus, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.deviceStates[deviceName]
	if !exists {
		return kubeletplugin.HealthStatusUnknown, "Device not found"
	}
	return s.generateHealthStatusAndMessage(deviceName, state)
}

func (s *HealthSimulator) updateDeviceState(deviceName string, state *DeviceHealthState) {
	// A device pinned by an annotation override does not evolve on its own; its
	// state stays exactly as forced until the override is changed or removed.
	if state.overridden {
		return
	}
	// Without auto-simulation enabled, a device simply holds its current state
	// (healthy, unless an override moved it), keeping the example reproducible.
	if !s.autoSimulate {
		return
	}

	now := s.now()
	timeSinceScenarioStart := now.Sub(state.scenarioStart)

	if state.scenario == ScenarioRecovering {
		if now.Sub(state.recoveryStart) >= simulatedRecoveryDuration {
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

func (s *HealthSimulator) generateHealthStatusAndMessage(deviceName string, state *DeviceHealthState) (kubeletplugin.HealthStatus, string) {
	switch state.scenario {
	case ScenarioHealthy:
		return kubeletplugin.HealthStatusHealthy,
			fmt.Sprintf("Device %s operating normally, temperature: %d°C", deviceName, state.temperature)

	case ScenarioTemperatureWarning:
		if state.temperature >= 90 {
			return kubeletplugin.HealthStatusUnhealthy,
				fmt.Sprintf("Critical: %s temperature at %d°C (exceeds safe threshold of 85°C)", deviceName, state.temperature)
		}
		return kubeletplugin.HealthStatusUnhealthy,
			fmt.Sprintf("%s temperature: %d°C (warning threshold exceeded, safe limit is 85°C)", deviceName, state.temperature)

	case ScenarioECCError:
		return kubeletplugin.HealthStatusUnhealthy,
			fmt.Sprintf("ECC error count exceeded threshold on %s (%d errors in last hour)", deviceName, state.eccErrorCount)

	case ScenarioCommunicationFailure:
		return kubeletplugin.HealthStatusUnhealthy,
			fmt.Sprintf("Driver communication timeout on %s (attempt %d)", deviceName, state.failureCount)

	case ScenarioRecovering:
		recoveryDuration := s.now().Sub(state.recoveryStart)
		switch {
		case recoveryDuration < 30*time.Second:
			return kubeletplugin.HealthStatusUnhealthy,
				fmt.Sprintf("%s initiating recovery sequence (diagnostics in progress)", deviceName)
		case recoveryDuration < simulatedRecoveryDuration:
			return kubeletplugin.HealthStatusUnhealthy,
				fmt.Sprintf("%s recovery in progress (running self-tests)", deviceName)
		default:
			return kubeletplugin.HealthStatusHealthy,
				fmt.Sprintf("%s recovered successfully, all diagnostics passed", deviceName)
		}

	case ScenarioUnknown:
		return kubeletplugin.HealthStatusUnknown,
			fmt.Sprintf("Health of device %s cannot be determined (driver lost contact with the device)", deviceName)

	default:
		return kubeletplugin.HealthStatusUnknown,
			fmt.Sprintf("Unknown state for device %s", deviceName)
	}
}
