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
	"sigs.k8s.io/yaml"
)

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
