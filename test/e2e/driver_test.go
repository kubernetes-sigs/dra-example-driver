//go:build e2e

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

package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/storage/driver"
	"helm.sh/helm/v3/pkg/strvals"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// chartPathEnvVar names the env var honored by both setup-e2e.sh and the
	// test code to locate the Helm chart (local path or "oci://..." URL).
	chartPathEnvVar = "HELM_CHART_PATH"

	// driverInstallTimeout bounds Helm wait plus driver readiness polling.
	driverInstallTimeout = 3 * time.Minute

	// driverUninstallTimeout bounds the per-test Helm uninstall + namespace delete.
	driverUninstallTimeout = 60 * time.Second

	// defaultDriverNumDevices is the smallest count that satisfies every demo
	// manifest. Tests that need more can override via DriverConfig.NumDevices.
	defaultDriverNumDevices = 2

	// maxDriverNameLen is the longest driver name that fits within Linux's
	// 108-byte UNIX_PATH_MAX after the kubelet appends its registrar socket
	// prefix and per-pod UID suffix.
	maxDriverNameLen = 28

	// diagnosticsLogLines is the number of trailing log lines collected from
	// each driver container when a spec fails.
	diagnosticsLogLines = 20
)

// DriverConfig captures per-test driver instance parameters and the resolved
// values returned from installDriver.
type DriverConfig struct {
	// ReleaseName is the Helm release name and (by default) the install namespace. Must be a valid DNS-1123 label.
	ReleaseName string

	// Namespace is the install namespace. Defaults to ReleaseName.
	Namespace string

	// DriverName is the DRA driver name registered with kubelet. Defaults to a release-derived unique value.
	DriverName string

	// ExtendedResourceName advertises the DeviceClass under a KEP-5004 extended resource name. Defaults to "" (disabled).
	ExtendedResourceName string

	// NumDevices is the number of mock GPUs advertised per node. Defaults to defaultDriverNumDevices.
	NumDevices int

	// WebhookEnabled toggles the chart's validating webhook subchart. Defaults to false.
	WebhookEnabled bool

	// ExtraValues are dot-notation helm overrides applied after the defaults above.
	ExtraValues map[string]string
}

// installDriver installs the DRA example driver via Helm and registers
// DeferCleanup callbacks for diagnostics + uninstall. The returned config has
// all defaults filled in for callers to pass to deployManifest.
func installDriver(ctx context.Context, cfg DriverConfig) DriverConfig {
	GinkgoHelper()

	cfg = withDriverDefaults(cfg)

	// Register cleanup before any cluster state is created. DeferCleanup is
	// LIFO so driver-log diagnostics run first.
	DeferCleanup(uninstallDriver, cfg, NodeTimeout(driverUninstallTimeout))
	DeferCleanup(dumpDriverDiagnostics, cfg, NodeTimeout(30*time.Second))

	registryClient, err := registry.NewClient()
	Expect(err).NotTo(HaveOccurred(), "Failed to create OCI registry client")
	actionCfg := newHelmActionConfig(cfg.Namespace, registryClient)

	install := action.NewInstall(actionCfg)
	install.ReleaseName = cfg.ReleaseName
	install.Namespace = cfg.Namespace
	install.CreateNamespace = true
	install.Wait = true
	install.Timeout = driverInstallTimeout

	chartRef := os.Getenv(chartPathEnvVar)
	if chartRef == "" {
		chartRef = filepath.Join(rootDir, "deployments", "helm", "dra-example-driver")
	}
	settings := cli.New()
	chartPath, err := install.LocateChart(chartRef, settings)
	Expect(err).NotTo(HaveOccurred(), "Failed to locate helm chart")
	chrt, err := loader.Load(chartPath)
	Expect(err).NotTo(HaveOccurred(), "Failed to load helm chart from %s", chartPath)

	fmt.Fprintf(GinkgoWriter,
		"Installing driver release %s/%s (driverName=%s, webhook=%v)\n",
		cfg.Namespace, cfg.ReleaseName, cfg.DriverName, cfg.WebhookEnabled)

	runUpgradeOrInstall(ctx, actionCfg, install, chrt, buildHelmValues(cfg))

	waitForDriverReady(ctx, cfg)
	return cfg
}

// runUpgradeOrInstall emulates `helm upgrade --install`: if a release with
// the configured name already exists it is upgraded in place, otherwise a
// fresh install is performed.
func runUpgradeOrInstall(ctx context.Context, actionCfg *action.Configuration, install *action.Install, chrt *chart.Chart, values map[string]any) {
	GinkgoHelper()
	history := action.NewHistory(actionCfg)
	history.Max = 1
	if _, err := history.Run(install.ReleaseName); errors.Is(err, driver.ErrReleaseNotFound) {
		_, err = install.RunWithContext(ctx, chrt, values)
		Expect(err).NotTo(HaveOccurred(),
			"Failed to install helm release %s/%s", install.Namespace, install.ReleaseName)
		return
	} else {
		Expect(err).NotTo(HaveOccurred(),
			"Failed to query history for release %s/%s", install.Namespace, install.ReleaseName)
	}

	upgrade := action.NewUpgrade(actionCfg)
	upgrade.Namespace = install.Namespace
	upgrade.Install = true
	upgrade.Wait = install.Wait
	upgrade.Timeout = install.Timeout
	_, err := upgrade.RunWithContext(ctx, install.ReleaseName, chrt, values)
	Expect(err).NotTo(HaveOccurred(),
		"Failed to upgrade helm release %s/%s", install.Namespace, install.ReleaseName)
}

// withDriverDefaults validates the release name and fills in unset fields.
func withDriverDefaults(cfg DriverConfig) DriverConfig {
	GinkgoHelper()
	Expect(validation.IsDNS1123Label(cfg.ReleaseName)).To(BeEmpty(),
		"DriverConfig.ReleaseName %q must be a valid DNS-1123 label", cfg.ReleaseName)
	if cfg.Namespace == "" {
		cfg.Namespace = cfg.ReleaseName
	}
	if cfg.DriverName == "" {
		cfg.DriverName = cfg.ReleaseName + ".example.com"
	}
	Expect(len(cfg.DriverName)).To(BeNumerically("<=", maxDriverNameLen),
		"DriverName %q is %d bytes long; the kubelet plugin's registrar socket path requires <=%d bytes. Shorten DriverConfig.ReleaseName.",
		cfg.DriverName, len(cfg.DriverName), maxDriverNameLen)
	if cfg.NumDevices == 0 {
		cfg.NumDevices = defaultDriverNumDevices
	}
	return cfg
}

// buildHelmValues builds the values map for installing the chart per cfg.
// Test-supplied ExtraValues are applied last so they can override any default.
func buildHelmValues(cfg DriverConfig) map[string]any {
	GinkgoHelper()
	values := map[string]any{
		"driverName":        cfg.DriverName,
		"namespaceOverride": cfg.Namespace,
		"kubeletPlugin": map[string]any{
			"numDevices": cfg.NumDevices,
		},
		"webhook": map[string]any{
			"enabled": cfg.WebhookEnabled,
		},
	}
	if cfg.ExtendedResourceName != "" {
		values["deviceClass"] = map[string]any{
			"extendedResourceName": cfg.ExtendedResourceName,
		}
	}
	for k, v := range cfg.ExtraValues {
		Expect(strvals.ParseInto(k+"="+v, values)).To(Succeed(),
			"Invalid helm value %s=%s", k, v)
	}
	return values
}

// uninstallDriver removes the Helm release and waits for the install
// namespace to terminate. Safe to register before install runs.
func uninstallDriver(ctx context.Context, cfg DriverConfig) {
	actionCfg := newHelmActionConfig(cfg.Namespace, nil)
	uninstall := action.NewUninstall(actionCfg)
	uninstall.Wait = true
	uninstall.Timeout = driverUninstallTimeout
	uninstall.IgnoreNotFound = true

	if _, err := uninstall.Run(cfg.ReleaseName); err != nil {
		fmt.Fprintf(GinkgoWriter,
			"Warning: failed to uninstall release %s/%s: %v\n",
			cfg.Namespace, cfg.ReleaseName, err)
	}

	deletePolicy := metav1.DeletePropagationForeground
	err := clientset.CoreV1().Namespaces().Delete(ctx, cfg.Namespace, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		fmt.Fprintf(GinkgoWriter,
			"Warning: failed to delete namespace %s: %v\n", cfg.Namespace, err)
		return
	}

	Eventually(func() bool {
		_, err := clientset.CoreV1().Namespaces().Get(ctx, cfg.Namespace, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}).WithContext(ctx).WithTimeout(driverUninstallTimeout).WithPolling(2*time.Second).Should(BeTrue(),
		"Timed out waiting for driver namespace %s to terminate", cfg.Namespace)
}

// dumpDriverDiagnostics writes recent driver pod logs to GinkgoWriter when
// the current spec has failed. Intended for use as a DeferCleanup callback.
func dumpDriverDiagnostics(ctx context.Context, cfg DriverConfig) {
	if !CurrentSpecReport().Failed() {
		return
	}
	fmt.Fprintf(GinkgoWriter,
		"\n=== Driver diagnostics for release %s/%s ===\n", cfg.Namespace, cfg.ReleaseName)

	driverPods, err := clientset.CoreV1().Pods(cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: driverPodSelector,
	})
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "Failed to list driver pods: %v\n", err)
		return
	}
	tailLines := int64(diagnosticsLogLines)
	for _, pod := range driverPods.Items {
		for _, c := range pod.Spec.Containers {
			logs, err := readPodLogs(ctx, cfg.Namespace, pod.Name, c.Name, &tailLines)
			if err != nil {
				fmt.Fprintf(GinkgoWriter,
					"Driver pod %s, container %s: failed to get logs: %v\n",
					pod.Name, c.Name, err)
				continue
			}
			fmt.Fprintf(GinkgoWriter,
				"Driver pod %s, container %s (last %d lines):\n%s\n",
				pod.Name, c.Name, tailLines, logs)
		}
	}
}

// readPodLogs streams the tail of a container's logs into a string.
func readPodLogs(ctx context.Context, namespace, pod, container string, tailLines *int64) (string, error) {
	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: container,
		TailLines: tailLines,
	}).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	return string(data), err
}

// waitForDriverReady polls for signals that Helm --wait does not cover: the
// kubelet plugin DaemonSet has ready pods, the DeviceClass exists,
// ResourceSlices have been published, and (when enabled) the webhook is
// serving. The DaemonSet check ties readiness to the current install so
// stale cluster state can't false-positive.
func waitForDriverReady(ctx context.Context, cfg DriverConfig) {
	GinkgoHelper()

	Eventually(func(g Gomega, ctx context.Context) {
		dsList, err := clientset.AppsV1().DaemonSets(cfg.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: driverPodSelector,
		})
		g.Expect(err).NotTo(HaveOccurred(),
			"Failed to list driver DaemonSets in %s", cfg.Namespace)
		g.Expect(dsList.Items).NotTo(BeEmpty(),
			"No driver DaemonSet yet in %s", cfg.Namespace)
		for _, ds := range dsList.Items {
			g.Expect(ds.Status.NumberReady).To(BeNumerically(">=", 1),
				"DaemonSet %s/%s has %d ready pods, want >=1",
				ds.Namespace, ds.Name, ds.Status.NumberReady)
			g.Expect(ds.Status.NumberReady).To(Equal(ds.Status.DesiredNumberScheduled),
				"DaemonSet %s/%s only %d of %d pods ready",
				ds.Namespace, ds.Name, ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)
		}

		_, err = clientset.ResourceV1().DeviceClasses().Get(ctx, cfg.DriverName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred(),
			"DeviceClass %s not yet created", cfg.DriverName)

		slices, err := clientset.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{
			FieldSelector: "spec.driver=" + cfg.DriverName,
		})
		g.Expect(err).NotTo(HaveOccurred(),
			"Failed to list ResourceSlices for driver %s", cfg.DriverName)
		g.Expect(slices.Items).NotTo(BeEmpty(),
			"No ResourceSlices yet published for driver %s", cfg.DriverName)
	}).WithContext(ctx).WithTimeout(driverInstallTimeout).WithPolling(2 * time.Second).Should(Succeed())

	if cfg.WebhookEnabled {
		verifyWebhook(ctx, cfg.DriverName)
	}
}

// newHelmActionConfig initializes a Helm action.Configuration scoped to the
// given install namespace. registryClient is required for OCI chart pulls and
// may be nil for actions (e.g. uninstall) that operate only on existing releases.
func newHelmActionConfig(namespace string, registryClient *registry.Client) *action.Configuration {
	GinkgoHelper()
	settings := cli.New()
	settings.SetNamespace(namespace)
	cfg := &action.Configuration{RegistryClient: registryClient}
	err := cfg.Init(settings.RESTClientGetter(), namespace, "secret",
		func(format string, v ...any) {
			fmt.Fprintf(GinkgoWriter, "[helm] "+format+"\n", v...)
		})
	Expect(err).NotTo(HaveOccurred(), "Failed to init helm action config")
	return cfg
}

// verifyWebhook waits until the validating webhook is serving for the given
// DeviceClass by creating a dry-run ResourceClaim until it succeeds.
func verifyWebhook(ctx context.Context, deviceClassName string) {
	GinkgoHelper()
	fmt.Fprintf(GinkgoWriter, "Waiting for webhook for DeviceClass %s to be available\n", deviceClassName)

	testClaim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			// ResourceClaim names use DNS-1123 subdomain validation, which
			// allows the dots in the driver name (e.g. "gpu.example.com").
			Name:      "webhook-test-" + deviceClassName,
			Namespace: "default",
		},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{{
					Name: "gpu",
					Exactly: &resourceapi.ExactDeviceRequest{
						DeviceClassName: deviceClassName,
					},
				}},
			},
		},
	}

	Eventually(func() error {
		_, err := clientset.ResourceV1().ResourceClaims("default").Create(
			ctx, testClaim,
			metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}},
		)
		if err != nil {
			return fmt.Errorf("webhook not ready: %w", err)
		}
		return nil
	}, "30s", "1s").WithContext(ctx).Should(Succeed())
}
