// Package helminstall provides functions for installing Kubernetes Helm charts, specifically for the DRA example driver.
package helminstall

import (
	"fmt"
	"log"
	"os"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
)

// InstallOptions contains all options for chart installation
type InstallOptions struct {
	// Namespace to install the chart into
	Namespace string
	// ReleaseName for the Helm installation
	ReleaseName string
	// ChartPath for local chart installation (only used with LocalChart=true)
	ChartPath string
	// ChartURL is the full chart URL for repository-based installation
	ChartURL string
	// ChartVersion is the version of the chart to install (empty for latest)
	ChartVersion string
	// Kubeconfig is the path to kubeconfig file
	Kubeconfig string
	// CreateNamespace determines if namespace should be created if it doesn't exist
	CreateNamespace bool
	// ValuesFile is the path to custom values file
	ValuesFile string
	// EnableValidationPolicy enables ValidatingAdmissionPolicy (disabled by default)
	EnableValidationPolicy bool
	// LocalChart determines if a local chart should be used instead of repository
	LocalChart bool
	// Logger for logging messages (if nil, log package will be used)
	Logger func(string, ...interface{})
}

// DefaultInstallOptions returns the default install options
func DefaultInstallOptions() *InstallOptions {
	return &InstallOptions{
		Namespace:              "dra-example-driver",
		ReleaseName:            "dra-example-driver",
		ChartPath:              "deployments/helm/dra-example-driver",
		ChartURL:               "oci://registry.k8s.io/dra-example-driver/charts/dra-example-driver",
		CreateNamespace:        true,
		EnableValidationPolicy: false,
		LocalChart:             false,
		Logger:                 log.Printf,
	}
}

// logf logs a formatted message if logger is set
func (o *InstallOptions) logf(format string, args ...interface{}) {
	if o.Logger != nil {
		o.Logger(format, args...)
	}
}

// InstallChart installs a Helm chart using the provided options
func InstallChart(opts *InstallOptions) (*release.Release, error) {
	if opts == nil {
		opts = DefaultInstallOptions()
	}

	// Enable OCI support
	os.Setenv("HELM_EXPERIMENTAL_OCI", "1")

	// Set up Helm environment
	settings := cli.New()

	// Override kubeconfig if specified
	if opts.Kubeconfig != "" {
		settings.KubeConfig = opts.Kubeconfig
	}

	// Create the action configuration
	actionConfig := new(action.Configuration)

	// Initialize Registry client
	registryClient, regErr := registry.NewClient(
		registry.ClientOptDebug(settings.Debug),
		registry.ClientOptWriter(os.Stdout),
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
	)
	if regErr != nil {
		return nil, fmt.Errorf("failed to create registry client: %v", regErr)
	}

	actionConfig.RegistryClient = registryClient

	var err error
	if err = actionConfig.Init(settings.RESTClientGetter(), opts.Namespace, os.Getenv("HELM_DRIVER"), opts.logf); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %v", err)
	}

	// Check if release exists
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	_, histErr := histClient.Run(opts.ReleaseName)

	// Get values from values file
	vals, err := getValues(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get values: %v", err)
	}

	// Set validation policy enabled/disabled
	if vals == nil {
		vals = make(map[string]interface{})
	}

	webhook, ok := vals["webhook"].(map[string]interface{})
	if !ok {
		webhook = make(map[string]interface{})
		vals["webhook"] = webhook
	}

	webhook["enabled"] = opts.EnableValidationPolicy
	opts.logf("ValidatingAdmissionPolicy (webhook.enabled) is %s", map[bool]string{true: "enabled", false: "disabled"}[opts.EnableValidationPolicy])

	// Load the chart
	chartRequested, err := loadChart(opts, settings)
	if err != nil {
		return nil, err
	}

	// Install or upgrade the chart
	return installOrUpgradeChart(actionConfig, chartRequested, vals, opts, histErr)
}

// getValues loads and merges values from the values file
func getValues(opts *InstallOptions) (map[string]interface{}, error) {
	valueOpts := &values.Options{}
	if opts.ValuesFile != "" {
		valueOpts.ValueFiles = []string{opts.ValuesFile}
	}

	return valueOpts.MergeValues(nil)
}

// loadChart loads a chart from local path, HTTP repository, or OCI registry
func loadChart(opts *InstallOptions, settings *cli.EnvSettings) (*chart.Chart, error) {
	if opts.LocalChart {
		opts.logf("Using local chart at %s", opts.ChartPath)
		return loader.Load(opts.ChartPath)
	}

	isOCI := strings.HasPrefix(opts.ChartURL, "oci://")
	if isOCI {
		return loadOCIChart(opts, settings)
	}

	return loadHTTPChart(opts, settings)
}

// loadOCIChart loads a chart from an OCI registry
func loadOCIChart(opts *InstallOptions, settings *cli.EnvSettings) (*chart.Chart, error) {
	opts.logf("Using OCI chart URL: %s", opts.ChartURL)

	install := action.NewInstall(new(action.Configuration))
	install.ChartPathOptions.Version = opts.ChartVersion

	localChartPath, err := install.ChartPathOptions.LocateChart(opts.ChartURL, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate OCI chart: %v", err)
	}

	opts.logf("Downloaded chart to: %s", localChartPath)
	return loader.Load(localChartPath)
}

// loadHTTPChart loads a chart from an HTTP repository
func loadHTTPChart(opts *InstallOptions, settings *cli.EnvSettings) (*chart.Chart, error) {
	parts := strings.Split(opts.ChartURL, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid chart URL format. Expected format: https://example.com/repo/chartname")
	}

	chartName := parts[len(parts)-1]
	repoURL := strings.TrimSuffix(opts.ChartURL, "/"+chartName)

	opts.logf("Using HTTP repository: %s with chart: %s", repoURL, chartName)

	c := repo.Entry{
		Name: chartName,
		URL:  repoURL,
	}

	r, err := repo.NewChartRepository(&c, getter.All(settings))
	if err != nil {
		return nil, fmt.Errorf("failed to create chart repository: %v", err)
	}

	if _, err = r.DownloadIndexFile(); err != nil {
		return nil, fmt.Errorf("failed to download repository index: %v", err)
	}

	client := action.NewInstall(new(action.Configuration))
	client.Namespace = opts.Namespace
	client.ReleaseName = opts.ReleaseName
	client.ChartPathOptions.Version = opts.ChartVersion
	client.ChartPathOptions.RepoURL = repoURL

	localChartPath, err := client.ChartPathOptions.LocateChart(fmt.Sprintf("%s/%s", chartName, chartName), settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart: %v", err)
	}

	opts.logf("Found chart at %s", localChartPath)
	return loader.Load(localChartPath)
}

// installOrUpgradeChart installs or upgrades a chart based on whether it already exists
func installOrUpgradeChart(
	actionConfig *action.Configuration,
	chartRequested *chart.Chart,
	vals map[string]interface{},
	opts *InstallOptions,
	histErr error,
) (*release.Release, error) {
	if histErr != nil {
		opts.logf("Installing chart for the first time")
		client := action.NewInstall(actionConfig)
		client.Namespace = opts.Namespace
		client.ReleaseName = opts.ReleaseName
		client.CreateNamespace = opts.CreateNamespace

		return client.Run(chartRequested, vals)
	}

	opts.logf("Upgrading existing chart")
	client := action.NewUpgrade(actionConfig)
	client.Namespace = opts.Namespace

	return client.Run(opts.ReleaseName, chartRequested, vals)
}
