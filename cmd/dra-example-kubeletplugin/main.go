/*
 * Copyright 2023 The Kubernetes Authors.
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
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/urfave/cli/v2"

	"k8s.io/apimachinery/pkg/api/resource"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"sigs.k8s.io/dra-example-driver/internal/profiles"
	"sigs.k8s.io/dra-example-driver/internal/profiles/gpu"
	"sigs.k8s.io/dra-example-driver/pkg/flags"
)

const (
	DriverPluginCheckpointFile = "checkpoint.json"
)

type Flags struct {
	kubeClientConfig flags.KubeClientConfig
	loggingConfig    *flags.LoggingConfig

	nodeName                      string
	cdiRoot                       string
	numDevices                    int
	kubeletRegistrarDirectoryPath string
	kubeletPluginsDirectoryPath   string
	healthcheckPort               int
	profile                       string
	driverName                    string
	podUID                        string
	gpuPartitions                 int
	gpuNodeAllocatableResources   bool
	gpuNodeAllocatableCPU         string
	gpuNodeAllocatableMemory      string
}

type Config struct {
	flags         *Flags
	coreclient    coreclientset.Interface
	cancelMainCtx func(error)

	profile profiles.Profile
}

var validProfiles = map[string]func(flags Flags) (profiles.Profile, error){
	gpu.ProfileName: func(flags Flags) (profiles.Profile, error) {
		nodeAllocatable, err := buildNodeAllocatableResources(flags)
		if err != nil {
			return nil, err
		}
		if flags.gpuPartitions > 0 && nodeAllocatable.Enabled() {
			return nil, fmt.Errorf("--gpu-partitions cannot be combined with --gpu-node-allocatable-resources: native node-allocatable mappings are only emitted on standalone GPU devices")
		}
		return gpu.NewProfile(flags.nodeName, flags.numDevices, flags.gpuPartitions, nodeAllocatable), nil
	},
}

// buildNodeAllocatableResources parses the --gpu-node-allocatable-* flags. It
// returns a zero value (Enabled() == false) when the feature is disabled.
func buildNodeAllocatableResources(flags Flags) (gpu.NodeAllocatableResources, error) {
	if !flags.gpuNodeAllocatableResources {
		return gpu.NodeAllocatableResources{}, nil
	}
	cpu, err := parseNodeAllocatableQuantity("--gpu-node-allocatable-cpu", flags.gpuNodeAllocatableCPU)
	if err != nil {
		return gpu.NodeAllocatableResources{}, err
	}
	memory, err := parseNodeAllocatableQuantity("--gpu-node-allocatable-memory", flags.gpuNodeAllocatableMemory)
	if err != nil {
		return gpu.NodeAllocatableResources{}, err
	}
	if cpu == nil && memory == nil {
		return gpu.NodeAllocatableResources{}, fmt.Errorf("--gpu-node-allocatable-resources is enabled but both --gpu-node-allocatable-cpu and --gpu-node-allocatable-memory are empty")
	}
	return gpu.NodeAllocatableResources{CPU: cpu, Memory: memory}, nil
}

// parseNodeAllocatableQuantity parses a node-allocatable resource quantity
// flag. An empty string disables the mapping for that resource; a zero or
// negative quantity is rejected.
func parseNodeAllocatableQuantity(flagName, value string) (*resource.Quantity, error) {
	if value == "" {
		return nil, nil
	}
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return nil, fmt.Errorf("invalid value for %s %q: %w", flagName, value, err)
	}
	if q.Sign() <= 0 {
		return nil, fmt.Errorf("invalid value for %s %q: must be a positive quantity", flagName, value)
	}
	return &q, nil
}

var validProfileNames = func() []string {
	var valid []string
	for profileName := range validProfiles {
		valid = append(valid, profileName)
	}
	return valid
}()

func (c Config) DriverPluginPath() string {
	return filepath.Join(c.flags.kubeletPluginsDirectoryPath, c.flags.driverName)
}

func main() {
	if err := newApp().Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newApp() *cli.App {
	flags := &Flags{
		loggingConfig: flags.NewLoggingConfig(),
	}
	cliFlags := []cli.Flag{
		&cli.StringFlag{
			Name:        "node-name",
			Usage:       "The name of the node to be worked on.",
			Required:    true,
			Destination: &flags.nodeName,
			EnvVars:     []string{"NODE_NAME"},
		},
		&cli.StringFlag{
			Name:        "cdi-root",
			Usage:       "Absolute path to the directory where CDI files will be generated.",
			Value:       "/etc/cdi",
			Destination: &flags.cdiRoot,
			EnvVars:     []string{"CDI_ROOT"},
		},
		&cli.IntFlag{
			Name:        "num-devices",
			Usage:       "The number of devices to be generated. Only relevant for the " + gpu.ProfileName + " profile.",
			Value:       8,
			Destination: &flags.numDevices,
			EnvVars:     []string{"NUM_DEVICES"},
		},
		&cli.StringFlag{
			Name:        "kubelet-registrar-directory-path",
			Usage:       "Absolute path to the directory where kubelet stores plugin registrations.",
			Value:       kubeletplugin.KubeletRegistryDir,
			Destination: &flags.kubeletRegistrarDirectoryPath,
			EnvVars:     []string{"KUBELET_REGISTRAR_DIRECTORY_PATH"},
		},
		&cli.StringFlag{
			Name:        "kubelet-plugins-directory-path",
			Usage:       "Absolute path to the directory where kubelet stores plugin data.",
			Value:       kubeletplugin.KubeletPluginsDir,
			Destination: &flags.kubeletPluginsDirectoryPath,
			EnvVars:     []string{"KUBELET_PLUGINS_DIRECTORY_PATH"},
		},
		&cli.IntFlag{
			Name:        "healthcheck-port",
			Usage:       "Port to start a gRPC healthcheck service. When positive, a literal port number. When zero, a random port is allocated. When negative, the healthcheck service is disabled.",
			Value:       -1,
			Destination: &flags.healthcheckPort,
			EnvVars:     []string{"HEALTHCHECK_PORT"},
		},
		&cli.StringFlag{
			Name:        "device-profile",
			Usage:       fmt.Sprintf("Name of the device profile. Valid values are %q.", validProfileNames),
			Value:       gpu.ProfileName,
			Destination: &flags.profile,
			EnvVars:     []string{"DEVICE_PROFILE"},
		},
		&cli.StringFlag{
			Name:        "driver-name",
			Usage:       "Name of the DRA driver. Its default is derived from the device profile.",
			Destination: &flags.driverName,
			EnvVars:     []string{"DRIVER_NAME"},
		},
		&cli.StringFlag{
			Name:        "pod-uid",
			Usage:       "UID of the pod (used for seamless upgrades to create unique socket names).",
			Destination: &flags.podUID,
			EnvVars:     []string{"POD_UID"},
		},
		&cli.IntFlag{
			Name:        "gpu-partitions",
			Usage:       "Number of partitions per GPU. When set to a value greater than 0, GPUs are exposed with shared counters allowing flexible partitioning (DRAPartitionableDevices feature).",
			Value:       0,
			Destination: &flags.gpuPartitions,
			EnvVars:     []string{"GPU_PARTITIONS"},
		},
		&cli.BoolFlag{
			Name:        "gpu-node-allocatable-resources",
			Usage:       "When set, every GPU device is published with NodeAllocatableResourceMappings so the scheduler charges --gpu-node-allocatable-cpu / --gpu-node-allocatable-memory against the node's standard CPU/memory (feature gate DRANodeAllocatableResources). Mutually exclusive with --gpu-partitions.",
			Destination: &flags.gpuNodeAllocatableResources,
			EnvVars:     []string{"GPU_NODE_ALLOCATABLE_RESOURCES"},
		},
		&cli.StringFlag{
			Name:        "gpu-node-allocatable-cpu",
			Usage:       "Per-GPU CPU quantity charged on the node when --gpu-node-allocatable-resources is enabled.",
			Value:       "2",
			Destination: &flags.gpuNodeAllocatableCPU,
			EnvVars:     []string{"GPU_NODE_ALLOCATABLE_CPU"},
		},
		&cli.StringFlag{
			Name:        "gpu-node-allocatable-memory",
			Usage:       "Per-GPU memory quantity charged on the node when --gpu-node-allocatable-resources is enabled.",
			Value:       "4Gi",
			Destination: &flags.gpuNodeAllocatableMemory,
			EnvVars:     []string{"GPU_NODE_ALLOCATABLE_MEMORY"},
		},
	}
	cliFlags = append(cliFlags, flags.kubeClientConfig.Flags()...)
	cliFlags = append(cliFlags, flags.loggingConfig.Flags()...)

	app := &cli.App{
		Name:            "dra-example-kubeletplugin",
		Usage:           "dra-example-kubeletplugin implements a DRA driver plugin.",
		ArgsUsage:       " ",
		HideHelpCommand: true,
		Flags:           cliFlags,
		Before: func(c *cli.Context) error {
			if c.Args().Len() > 0 {
				return fmt.Errorf("arguments not supported: %v", c.Args().Slice())
			}
			return flags.loggingConfig.Apply()
		},
		Action: func(c *cli.Context) error {
			ctx := c.Context
			clientSets, err := flags.kubeClientConfig.NewClientSets()
			if err != nil {
				return fmt.Errorf("create client: %w", err)
			}

			if flags.driverName == "" {
				flags.driverName = flags.profile + ".example.com"
			}

			newProfile, ok := validProfiles[flags.profile]
			if !ok {
				return fmt.Errorf("invalid device profile %q, valid profiles are %q", flags.profile, validProfileNames)
			}

			profile, err := newProfile(*flags)
			if err != nil {
				return err
			}

			config := &Config{
				flags:      flags,
				coreclient: clientSets.Core,
				profile:    profile,
			}

			return RunPlugin(ctx, config)
		},
	}

	return app
}

func RunPlugin(ctx context.Context, config *Config) error {
	logger := klog.FromContext(ctx)

	err := os.MkdirAll(config.DriverPluginPath(), 0750)
	if err != nil {
		return err
	}

	info, err := os.Stat(config.flags.cdiRoot)
	switch {
	case err != nil && os.IsNotExist(err):
		err := os.MkdirAll(config.flags.cdiRoot, 0750)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("path for cdi file generation is not a directory: '%v'", err)
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()
	ctx, cancel := context.WithCancelCause(ctx)
	config.cancelMainCtx = cancel

	driver, err := NewDriver(ctx, config)
	if err != nil {
		return err
	}

	<-ctx.Done()
	// restore default signal behavior as soon as possible in case graceful
	// shutdown gets stuck.
	stop()
	if err := context.Cause(ctx); err != nil && !errors.Is(err, context.Canceled) {
		// A canceled context is the normal case here when the process receives
		// a signal. Only log the error for more interesting cases.
		logger.Error(err, "error from context")
	}

	err = driver.Shutdown(logger)
	if err != nil {
		logger.Error(err, "Unable to cleanly shutdown driver")
	}

	return nil
}
