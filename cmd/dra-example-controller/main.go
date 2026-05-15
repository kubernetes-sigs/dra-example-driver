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
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"sigs.k8s.io/dra-example-driver/cmd/dra-example-controller/plugins"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(resourceapi.AddToScheme(scheme))
}

const (
	PluginBindingConditions = "BindingConditions"
)

// PluginFactory creates a Plugin for the given driver name.
type PluginFactory func(driverName string) Plugin

// pluginRegistry maps plugin names to their factory functions.
// Add new plugins here.
var pluginRegistry = map[string]PluginFactory{
	PluginBindingConditions: func(driverName string) Plugin {
		return plugins.NewBindingConditionsPlugin(driverName)
	},
}

// pluginNames returns sorted plugin names for help text.
func pluginNames() []string {
	names := make([]string, 0, len(pluginRegistry))
	for name := range pluginRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// enablePlugins is a flag.Value that collects --enable-plugin values.
// Values can be specified as comma-separated or by repeating the flag.
type enablePlugins []string

func (e *enablePlugins) String() string { return strings.Join(*e, ",") }
func (e *enablePlugins) Set(v string) error {
	for _, name := range strings.Split(v, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := pluginRegistry[name]; !ok {
			return fmt.Errorf("unknown plugin %q (available: %s)", name, strings.Join(pluginNames(), ", "))
		}
		*e = append(*e, name)
	}
	return nil
}

func main() {
	var driverName string
	var enabled enablePlugins
	flag.StringVar(&driverName, "driver-name", "gpu.example.com", "The driver name to filter ResourceClaims by.")
	flag.Var(&enabled, "enable-plugin",
		fmt.Sprintf("Enable a plugin (can be specified multiple times). Available: %s", strings.Join(pluginNames(), ", ")))
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating manager: %v\n", err)
		os.Exit(1)
	}

	// Build the plugin list from flags.
	var plugins []Plugin
	for _, name := range enabled {
		plugins = append(plugins, pluginRegistry[name](driverName))
	}

	if err := NewClaimReconciler(mgr, driverName, plugins).SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up controller: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		fmt.Fprintf(os.Stderr, "Error running manager: %v\n", err)
		os.Exit(1)
	}
}
