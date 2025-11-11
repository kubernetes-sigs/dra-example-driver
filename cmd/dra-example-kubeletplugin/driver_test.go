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
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"sigs.k8s.io/dra-example-driver/internal/profiles/cpu"
)

const (
	testNodeName   = "test-node"
	testDriverName = "cpu.example.com"
)

// newDriverTestConfig builds a Config that lets NewDriver run for real against
// a fake clientset: the kubeletplugin helper listens on unix sockets under a
// temporary directory and the healthcheck server on an ephemeral port.
//
// It uses os.MkdirTemp rather than t.TempDir because unix socket paths are
// limited to ~104 bytes on macOS and t.TempDir embeds the test name.
func newDriverTestConfig(t *testing.T, deviceHealth bool, healthcheckPort int) (*Config, *error) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "dra")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })

	flags := &Flags{
		nodeName:                      testNodeName,
		driverName:                    testDriverName,
		profile:                       "cpu",
		cdiRoot:                       filepath.Join(tmp, "cdi"),
		kubeletRegistrarDirectoryPath: filepath.Join(tmp, "r"),
		kubeletPluginsDirectoryPath:   filepath.Join(tmp, "p"),
		healthcheckPort:               healthcheckPort,
		deviceHealth:                  deviceHealth,
		cpuNUMANodes:                  1,
		cpusPerNUMANode:               2,
	}
	for _, dir := range []string{flags.cdiRoot, flags.kubeletRegistrarDirectoryPath, filepath.Join(flags.kubeletPluginsDirectoryPath, testDriverName)} {
		require.NoError(t, os.MkdirAll(dir, 0750))
	}

	// Capture what the driver reports as a fatal background error.
	var fatal error
	return &Config{
		flags:         flags,
		coreclient:    fakeclient.NewClientset(),
		cancelMainCtx: func(err error) { fatal = err },
		profile:       cpu.NewProfile(testNodeName, testDriverName, flags.cpuNUMANodes, flags.cpusPerNUMANode),
	}, &fatal
}

func TestNewDriverLifecycle(t *testing.T) {
	for name, deviceHealth := range map[string]bool{
		"device health enabled":  true,
		"device health disabled": false,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			config, _ := newDriverTestConfig(t, deviceHealth, 0)

			d, err := NewDriver(ctx, config)
			require.NoError(t, err)
			require.NotNil(t, d.helper)
			require.NotNil(t, d.healthcheck)
			assert.Equal(t, deviceHealth, d.deviceHealth)

			// The healthcheck talks to the registration and DRA sockets the
			// helper just opened, so it must report SERVING.
			resp, err := d.healthcheck.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "liveness"})
			require.NoError(t, err)
			assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.GetStatus())

			reports := make(chan kubeletplugin.DeviceHealthReport)
			done := make(chan error, 1)
			go func() { done <- d.WatchHealthStatus(ctx, reports) }()
			if deviceHealth {
				// Devices come from the profile, so the report must list them.
				report := receiveReport(t, reports)
				assert.Equal(t, []string{"numa-0"}, d.devices)
				assert.Equal(t, kubeletplugin.HealthStatusHealthy, healthOf(report, "numa-0"))
			} else {
				assert.Nil(t, d.simulator)
				assert.Empty(t, d.devices)
			}

			require.NoError(t, d.Shutdown(klog.FromContext(ctx)))

			// With health enabled Shutdown ends the in-flight stream; when
			// disabled the call returned ErrHealthNotSupported right away.
			select {
			case err := <-done:
				if deviceHealth {
					assert.NoError(t, err)
				} else {
					assert.ErrorIs(t, err, kubeletplugin.ErrHealthNotSupported)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("WatchHealthStatus did not return after Shutdown")
			}
			waitForHealthGoroutines(t, d)
		})
	}
}

func TestNewDriverWithoutHealthcheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config, _ := newDriverTestConfig(t, true, -1)

	d, err := NewDriver(ctx, config)
	require.NoError(t, err)
	assert.Nil(t, d.healthcheck, "a negative port disables the healthcheck service")
	require.NoError(t, d.Shutdown(klog.FromContext(ctx)))
}

func TestNewDriverErrors(t *testing.T) {
	for name, breakConfig := range map[string]func(t *testing.T, config *Config){
		"device state cannot be created": func(t *testing.T, config *Config) {
			// A regular file where the CDI root directory should be.
			require.NoError(t, os.RemoveAll(config.flags.cdiRoot))
			require.NoError(t, os.WriteFile(config.flags.cdiRoot, nil, 0600))
		},
		"kubeletplugin helper cannot start": func(_ *testing.T, config *Config) {
			config.flags.driverName = "" // rejected by kubeletplugin.Start
		},
		"healthcheck port in use": func(t *testing.T, config *Config) {
			lis, err := net.Listen("tcp", ":0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = lis.Close() })
			addr, ok := lis.Addr().(*net.TCPAddr)
			require.True(t, ok)
			config.flags.healthcheckPort = addr.Port
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			config, _ := newDriverTestConfig(t, true, 0)
			breakConfig(t, config)

			_, err := NewDriver(ctx, config)
			require.Error(t, err)
		})
	}
}

func TestHealthcheckNotServingWithoutPlugin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// No kubeletplugin helper is started, so the sockets do not exist.
	config, _ := newDriverTestConfig(t, true, 0)

	h, err := startHealthcheck(ctx, config)
	require.NoError(t, err)
	require.NotNil(t, h)
	defer h.Stop(klog.FromContext(ctx))

	for _, service := range []string{"", "liveness"} {
		resp, err := h.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: service})
		require.NoError(t, err)
		assert.Equal(t, grpc_health_v1.HealthCheckResponse_NOT_SERVING, resp.GetStatus(), "service %q", service)
	}

	_, err = h.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "bogus"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestStartHealthcheckFailsOnUsedPort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config, _ := newDriverTestConfig(t, true, 0)

	// Occupy a port on the wildcard address, which is what the healthcheck
	// binds, and ask it to use the same one.
	lis, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer func() { _ = lis.Close() }()
	addr, ok := lis.Addr().(*net.TCPAddr)
	require.True(t, ok)
	config.flags.healthcheckPort = addr.Port

	_, err = startHealthcheck(ctx, config)
	require.Error(t, err)
}

func TestPrepareAndUnprepareResourceClaims(t *testing.T) {
	ctx := context.Background()
	config, _ := newDriverTestConfig(t, true, -1)
	state, err := NewDeviceState(config)
	require.NoError(t, err)
	d := &driver{state: state, poolName: testNodeName}

	claim := func(uid types.UID, device string) *resourceapi.ResourceClaim {
		return &resourceapi.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{UID: uid, Namespace: "default", Name: string(uid)},
			Status: resourceapi.ResourceClaimStatus{
				Allocation: &resourceapi.AllocationResult{
					Devices: resourceapi.DeviceAllocationResult{
						Results: []resourceapi.DeviceRequestAllocationResult{{
							Request: "cpus",
							Driver:  testDriverName,
							Pool:    testNodeName,
							Device:  device,
						}},
					},
				},
			},
		}
	}

	good := claim("claim-good", "numa-0")
	bad := claim("claim-bad", "numa-9") // not a device of this driver

	results, err := d.PrepareResourceClaims(ctx, []*resourceapi.ResourceClaim{good, bad})
	require.NoError(t, err)
	require.Len(t, results, 2)

	require.NoError(t, results[good.UID].Err)
	require.Len(t, results[good.UID].Devices, 1)
	assert.Equal(t, "numa-0", results[good.UID].Devices[0].DeviceName)
	assert.Equal(t, testNodeName, results[good.UID].Devices[0].PoolName)
	assert.Equal(t, []string{"cpus"}, results[good.UID].Devices[0].Requests)
	assert.NotEmpty(t, results[good.UID].Devices[0].CDIDeviceIDs)

	require.Error(t, results[bad.UID].Err)
	assert.Empty(t, results[bad.UID].Devices)

	unprepare := func() map[types.UID]error {
		errs, err := d.UnprepareResourceClaims(ctx, []kubeletplugin.NamespacedObject{
			{UID: good.UID, NamespacedName: types.NamespacedName{Namespace: good.Namespace, Name: good.Name}},
		})
		require.NoError(t, err)
		require.Len(t, errs, 1)
		return errs
	}

	// A checkpoint that cannot be decoded must surface as a per-claim error.
	checkpoint, err := os.ReadFile(state.checkpointPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(state.checkpointPath, []byte("{not json"), 0600))
	require.Error(t, unprepare()[good.UID])

	// With the checkpoint restored, unprepare succeeds.
	require.NoError(t, os.WriteFile(state.checkpointPath, checkpoint, 0600))
	assert.NoError(t, unprepare()[good.UID])
}

func TestHandleError(t *testing.T) {
	var fatal error
	d := &driver{cancelCtx: func(err error) { fatal = err }}
	ctx := context.Background()

	d.HandleError(ctx, fmt.Errorf("transient: %w", kubeletplugin.ErrRecoverable), "recoverable")
	assert.NoError(t, fatal, "recoverable errors must not stop the driver")

	boom := errors.New("boom")
	d.HandleError(ctx, boom, "fatal")
	require.ErrorIs(t, fatal, boom, "fatal errors must cancel the main context")

	// A driver without a cancel func must not panic.
	(&driver{}).HandleError(ctx, boom, "fatal")
}
