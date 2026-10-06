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

package helm

import (
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/engine"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"sigs.k8s.io/yaml"
)

func TestDriverIdentity(t *testing.T) {
	for _, profile := range []string{"gpu", "cpu", "net"} {
		for _, override := range []string{"", "custom.example.org"} {
			t.Run(profile+"/"+override, func(t *testing.T) {
				expected := profile + ".dra-example-driver.sigs.k8s.io"
				if override != "" {
					expected = override
				}
				chart, err := loader.Load("../../deployments/helm/dra-example-driver")
				require.NoError(t, err)
				values, err := chartutil.ToRenderValues(chart, map[string]any{
					"deviceProfile": profile,
					"driverName":    override,
					"webhook":       map[string]any{"enabled": true},
					"controller":    map[string]any{"plugins": []any{"binding-conditions"}},
				}, common.ReleaseOptions{Name: "test", Namespace: "driver-test", IsInstall: true}, common.DefaultCapabilities)
				require.NoError(t, err)
				rendered, err := engine.Render(chart, values)
				require.NoError(t, err)
				var dc resourcev1.DeviceClass
				require.NoError(t, yaml.Unmarshal([]byte(rendered["dra-example-driver/templates/deviceclass.yaml"]), &dc))
				assert.Equal(t, expected, dc.Name)
				require.Len(t, dc.Spec.Selectors, 1)
				require.NotNil(t, dc.Spec.Selectors[0].CEL)
				assert.Equal(t, "device.driver == '"+expected+"'", dc.Spec.Selectors[0].CEL.Expression)
				var ds appsv1.DaemonSet
				require.NoError(t, yaml.Unmarshal([]byte(rendered["dra-example-driver/templates/kubeletplugin.yaml"]), &ds))
				assert.Equal(t, expected, pluginContainerEnv(t, ds)["DRIVER_NAME"])
				for _, component := range []string{"controller", "webhook"} {
					var deployment appsv1.Deployment
					require.NoError(t, yaml.Unmarshal([]byte(rendered["dra-example-driver/templates/"+component+"-deployment.yaml"]), &deployment))
					require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
					assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Args, "--driver-name="+expected)
				}
			})
		}
	}
}

func TestDriverSocketPathLimit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		driverName string
		pluginsDir string
		wantError  string
	}{
		{name: "default"},
		{name: "107 byte path", driverName: strings.Repeat("a", 24) + ".example.org"},
		{name: "108 byte path", driverName: strings.Repeat("a", 25) + ".example.org", wantError: "DRA socket path is 108 bytes"},
		{name: "short directory permits longer name", driverName: strings.Repeat("a", 25) + ".example.org", pluginsDir: "/plugins"},
		{name: "long directory at limit", pluginsDir: "/var/lib/kubelet/pluginsxx"},
		{name: "long directory over limit", pluginsDir: "/var/lib/kubelet/pluginsxxx", wantError: "DRA socket path is 108 bytes"},
		{name: "clean directory before checking", driverName: strings.Repeat("a", 24) + ".example.org", pluginsDir: "/var/lib/kubelet/plugins/./"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chart, err := loader.Load("../../deployments/helm/dra-example-driver")
			require.NoError(t, err)
			overrides := map[string]any{"driverName": tc.driverName}
			if tc.pluginsDir != "" {
				overrides["kubeletPlugin"] = map[string]any{"kubeletPluginsDirectoryPath": tc.pluginsDir}
			}
			values, err := chartutil.ToRenderValues(chart, overrides, common.ReleaseOptions{
				Name: "test", Namespace: "driver-test", IsInstall: true,
			}, common.DefaultCapabilities)
			require.NoError(t, err)
			_, err = engine.Render(chart, values)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.ErrorContains(t, err, "Shorten driverName or kubeletPlugin.kubeletPluginsDirectoryPath")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRegistrationSocketPathLimit(t *testing.T) {
	const podUID = types.UID("f358c92b-e974-4f29-a84f-6c64bec0949d")
	for _, tc := range []struct {
		name          string
		driverLength  int
		registryDir   string
		wantPathBytes int
	}{
		{name: "full UID at limit", driverLength: 27, registryDir: kubeletplugin.KubeletRegistryDir, wantPathBytes: 107},
		{name: "full UID over limit", driverLength: 28, registryDir: kubeletplugin.KubeletRegistryDir, wantPathBytes: 108},
		{name: "UID hash fallback", driverLength: 29, registryDir: kubeletplugin.KubeletRegistryDir, wantPathBytes: 84},
		{name: "UID hash at limit", driverLength: 34, registryDir: "/" + strings.Repeat("r", 50), wantPathBytes: 107},
		{name: "UID hash over limit", driverLength: 34, registryDir: "/" + strings.Repeat("r", 51), wantPathBytes: 108},
		{name: "driver and UID hash fallback", driverLength: 34, registryDir: "/" + strings.Repeat("r", 52), wantPathBytes: 89},
		{name: "shortest filename at limit", driverLength: 34, registryDir: "/" + strings.Repeat("r", 70), wantPathBytes: 107},
		{name: "shortest filename over limit", driverLength: 34, registryDir: "/" + strings.Repeat("r", 71), wantPathBytes: 108},
		{name: "directory too long", driverLength: 34, registryDir: "/" + strings.Repeat("r", 72), wantPathBytes: 109},
		{name: "clean registry path", driverLength: 27, registryDir: kubeletplugin.KubeletRegistryDir + "/./", wantPathBytes: 107},
	} {
		t.Run(tc.name, func(t *testing.T) {
			driverName := strings.Repeat("a", tc.driverLength-12) + ".example.org"
			// Check the chart against the filename that the installed helper actually selects.
			filename := kubeletplugin.RollingUpdateRegistrarSocketFile(tc.registryDir, driverName, podUID)
			socketBytes := len(path.Join(tc.registryDir, filename))
			require.Equal(t, tc.wantPathBytes, socketBytes)
			chart, err := loader.Load("../../deployments/helm/dra-example-driver")
			require.NoError(t, err)
			values, err := chartutil.ToRenderValues(chart, map[string]any{
				"driverName":    driverName,
				"kubeletPlugin": map[string]any{"kubeletRegistrarDirectoryPath": tc.registryDir},
			}, common.ReleaseOptions{Name: "test", Namespace: "driver-test", IsInstall: true}, common.DefaultCapabilities)
			require.NoError(t, err)
			_, err = engine.Render(chart, values)
			if socketBytes > 107 {
				require.ErrorContains(t, err, "Registration socket path is "+strconv.Itoa(socketBytes)+" bytes")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDeviceHealthPodWatchRBAC(t *testing.T) {
	for _, tc := range []struct {
		name    string
		values  map[string]any
		enabled bool
	}{
		{name: "default", enabled: true},
		{name: "enabled", values: map[string]any{"deviceHealth": true}, enabled: true},
		{name: "disabled", values: map[string]any{"deviceHealth": false}},
		{name: "disabled with simulation", values: map[string]any{"deviceHealth": false, "simulateHealthChanges": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chart, err := loader.Load("../../deployments/helm/dra-example-driver")
			require.NoError(t, err)
			overrides := map[string]any{}
			if tc.values != nil {
				overrides["kubeletPlugin"] = tc.values
			}
			values, err := chartutil.ToRenderValues(chart, overrides, common.ReleaseOptions{
				Name: "test", Namespace: "driver-test", IsInstall: true,
			}, common.DefaultCapabilities)
			require.NoError(t, err)
			rendered, err := engine.Render(chart, values)
			require.NoError(t, err)

			// The env var names must match what the kubeletplugin binary reads
			// (see the EnvVars of the --device-health and
			// --simulate-health-changes flags in cmd/dra-example-kubeletplugin).
			pluginPath := "dra-example-driver/templates/kubeletplugin.yaml"
			require.Contains(t, rendered, pluginPath)
			var ds appsv1.DaemonSet
			require.NoError(t, yaml.Unmarshal([]byte(rendered[pluginPath]), &ds))
			env := pluginContainerEnv(t, ds)
			assert.Equal(t, strconv.FormatBool(tc.enabled), env["DEVICE_HEALTH"])
			simulate, _ := tc.values["simulateHealthChanges"].(bool)
			assert.Equal(t, strconv.FormatBool(simulate), env["SIMULATE_HEALTH_CHANGES"])
			assert.NotContains(t, env, "HEALTH_SERVICE", "stale env var name; the driver reads DEVICE_HEALTH")

			rolePath := "dra-example-driver/templates/role.yaml"
			bindingPath := "dra-example-driver/templates/rolebinding.yaml"
			require.Contains(t, rendered, rolePath)
			require.Contains(t, rendered, bindingPath)
			if !tc.enabled {
				assert.Empty(t, strings.TrimSpace(rendered[rolePath]))
				assert.Empty(t, strings.TrimSpace(rendered[bindingPath]))
				return
			}

			var role rbacv1.Role
			require.NoError(t, yaml.Unmarshal([]byte(rendered[rolePath]), &role))
			assert.Equal(t, "Role", role.Kind)
			assert.Equal(t, "driver-test", role.Namespace)
			assert.Equal(t, []rbacv1.PolicyRule{{
				APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"watch"},
			}}, role.Rules)

			var binding rbacv1.RoleBinding
			require.NoError(t, yaml.Unmarshal([]byte(rendered[bindingPath]), &binding))
			assert.Equal(t, "RoleBinding", binding.Kind)
			assert.Equal(t, role.Namespace, binding.Namespace)
			assert.Equal(t, rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name,
			}, binding.RoleRef)
			assert.Equal(t, []rbacv1.Subject{{
				Kind: "ServiceAccount", Name: "test-dra-example-driver-service-account", Namespace: role.Namespace,
			}}, binding.Subjects)
		})
	}
}

// pluginContainerEnv returns the literal env vars of the "plugin" container in
// the rendered DaemonSet, keyed by name.
func pluginContainerEnv(t *testing.T, ds appsv1.DaemonSet) map[string]string {
	t.Helper()
	var plugin *corev1.Container
	for i := range ds.Spec.Template.Spec.Containers {
		if ds.Spec.Template.Spec.Containers[i].Name == "plugin" {
			plugin = &ds.Spec.Template.Spec.Containers[i]
		}
	}
	require.NotNil(t, plugin, "plugin container not found in rendered DaemonSet")
	env := map[string]string{}
	for _, e := range plugin.Env {
		if e.ValueFrom == nil {
			env[e.Name] = e.Value
		}
	}
	return env
}
