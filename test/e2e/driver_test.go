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
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/registry"
	"helm.sh/helm/v4/pkg/strvals"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
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

	// diagnosticsLogLines is the number of trailing log lines collected from
	// each driver container when a spec fails.
	diagnosticsLogLines = 20
)

// DriverConfig captures per-test driver instance parameters that the caller
// may set. installDriver returns an installedDriver with the resolved values
// that downstream consumers (e.g. deployManifest) need.
type DriverConfig struct {
	// DriverName overrides the auto-generated DRA driver name. Tests that
	// share static testdata (e.g. the webhook tests) pin this. Defaults to
	// the auto-generated release name + ".example.com".
	DriverName string

	// ExtendedResourceName advertises the DeviceClass under a KEP-5004 extended resource name. Defaults to "" (disabled).
	ExtendedResourceName string

	// NumDevices is the number of mock GPUs advertised per node. Defaults to defaultDriverNumDevices.
	NumDevices int

	// WebhookEnabled toggles the chart's validating webhook subchart. Defaults to false.
	WebhookEnabled bool

	// ExtraValues are dot-notation helm overrides applied after the defaults above.
	ExtraValues map[string]string

	// GPUDeviceStatus, when true, instructs the driver to publish per-device
	// attributes (e.g. uuid, model, driverVersion) into
	// ResourceClaim.status.devices[].data.
	GPUDeviceStatus bool
}

// installedDriver is what installDriver returns: the identity bits downstream
// consumers (deployManifest, tests) need to substitute into demo manifests
// and assertions.
type installedDriver struct {
	DriverName           string
	ExtendedResourceName string
}

// installDriver installs the DRA example driver via Helm and registers
// DeferCleanup callbacks for diagnostics + uninstall.
func installDriver(ctx context.Context, cfg DriverConfig) installedDriver {
	GinkgoHelper()

	releaseName := "dra-" + rand.String(6)
	namespace := "dra-" + rand.String(6)
	if cfg.DriverName == "" {
		cfg.DriverName = releaseName + ".example.com"
	}
	if cfg.NumDevices == 0 {
		cfg.NumDevices = defaultDriverNumDevices
	}

	// Register cleanup before any cluster state is created. DeferCleanup is
	// LIFO so driver-log diagnostics run first.
	DeferCleanup(uninstallDriver, releaseName, namespace, NodeTimeout(driverUninstallTimeout))
	DeferCleanup(dumpDriverDiagnostics, releaseName, namespace, NodeTimeout(30*time.Second))

	registryClient, err := registry.NewClient()
	Expect(err).NotTo(HaveOccurred(), "Failed to create OCI registry client")
	actionCfg := newHelmActionConfig(namespace, registryClient)

	install := action.NewInstall(actionCfg)
	install.ReleaseName = releaseName
	install.Namespace = namespace
	install.CreateNamespace = true
	install.WaitStrategy = kube.LegacyStrategy
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
		namespace, releaseName, cfg.DriverName, cfg.WebhookEnabled)

	_, err = install.RunWithContext(ctx, chrt, buildHelmValues(cfg, namespace))
	Expect(err).NotTo(HaveOccurred(),
		"Failed to install helm release %s/%s", namespace, releaseName)

	waitForDriverReady(ctx, namespace, cfg.DriverName, cfg.WebhookEnabled)
	return installedDriver{
		DriverName:           cfg.DriverName,
		ExtendedResourceName: cfg.ExtendedResourceName,
	}
}

// buildHelmValues builds the values map for installing the chart per cfg.
// Test-supplied ExtraValues are applied last so they can override any default.
func buildHelmValues(cfg DriverConfig, namespace string) map[string]any {
	GinkgoHelper()
	values := map[string]any{
		"driverName":        cfg.DriverName,
		"namespaceOverride": namespace,
		"gpuDeviceStatus":   cfg.GPUDeviceStatus,
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
func uninstallDriver(ctx context.Context, releaseName, namespace string) {
	actionCfg := newHelmActionConfig(namespace, nil)
	uninstall := action.NewUninstall(actionCfg)
	uninstall.WaitStrategy = kube.LegacyStrategy
	uninstall.Timeout = driverUninstallTimeout
	uninstall.IgnoreNotFound = true

	if _, err := uninstall.Run(releaseName); err != nil {
		fmt.Fprintf(GinkgoWriter,
			"Warning: failed to uninstall release %s/%s: %v\n",
			namespace, releaseName, err)
	}

	deletePolicy := metav1.DeletePropagationForeground
	err := clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		fmt.Fprintf(GinkgoWriter,
			"Warning: failed to delete namespace %s: %v\n", namespace, err)
		return
	}

	Eventually(func() bool {
		_, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}).WithContext(ctx).WithTimeout(driverUninstallTimeout).WithPolling(2*time.Second).Should(BeTrue(),
		"Timed out waiting for driver namespace %s to terminate", namespace)
}

// dumpDriverDiagnostics writes recent driver pod logs to GinkgoWriter when
// the current spec has failed. Intended for use as a DeferCleanup callback.
func dumpDriverDiagnostics(ctx context.Context, releaseName, namespace string) {
	if !CurrentSpecReport().Failed() {
		return
	}
	fmt.Fprintf(GinkgoWriter,
		"\n=== Driver diagnostics for release %s/%s ===\n", namespace, releaseName)

	driverPods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: driverPodSelector,
	})
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "Failed to list driver pods: %v\n", err)
		return
	}
	tailLines := int64(diagnosticsLogLines)
	for _, pod := range driverPods.Items {
		for _, c := range pod.Spec.Containers {
			logs, err := readPodLogs(ctx, namespace, pod.Name, c.Name, &tailLines)
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
// ResourceSlices have been published, and (when webhookEnabled) the webhook
// is serving. The DaemonSet check ties readiness to the current install so
// stale cluster state can't false-positive.
func waitForDriverReady(ctx context.Context, namespace, driverName string, webhookEnabled bool) {
	GinkgoHelper()

	Eventually(func(g Gomega, ctx context.Context) {
		dsList, err := clientset.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: driverPodSelector,
		})
		g.Expect(err).NotTo(HaveOccurred(),
			"Failed to list driver DaemonSets in %s", namespace)
		g.Expect(dsList.Items).NotTo(BeEmpty(),
			"No driver DaemonSet yet in %s", namespace)
		for _, ds := range dsList.Items {
			g.Expect(ds.Status.NumberReady).To(BeNumerically(">=", 1),
				"DaemonSet %s/%s has %d ready pods, want >=1",
				ds.Namespace, ds.Name, ds.Status.NumberReady)
			g.Expect(ds.Status.NumberReady).To(Equal(ds.Status.DesiredNumberScheduled),
				"DaemonSet %s/%s only %d of %d pods ready",
				ds.Namespace, ds.Name, ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)
		}

		_, err = clientset.ResourceV1().DeviceClasses().Get(ctx, driverName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred(),
			"DeviceClass %s not yet created", driverName)

		slices, err := clientset.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{
			FieldSelector: "spec.driver=" + driverName,
		})
		g.Expect(err).NotTo(HaveOccurred(),
			"Failed to list ResourceSlices for driver %s", driverName)
		g.Expect(slices.Items).NotTo(BeEmpty(),
			"No ResourceSlices yet published for driver %s", driverName)
	}).WithContext(ctx).WithTimeout(driverInstallTimeout).WithPolling(2 * time.Second).Should(Succeed())

	if webhookEnabled {
		verifyWebhook(ctx, driverName)
	}
}

// newHelmActionConfig initializes a Helm action.Configuration scoped to the
// given install namespace. registryClient is required for OCI chart pulls and
// may be nil for actions (e.g. uninstall) that operate only on existing releases.
func newHelmActionConfig(namespace string, registryClient *registry.Client) *action.Configuration {
	GinkgoHelper()
	settings := cli.New()
	settings.SetNamespace(namespace)
	cfg := action.NewConfiguration(action.ConfigurationSetLogger(
		slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{Level: slog.LevelInfo}),
	))
	cfg.RegistryClient = registryClient
	err := cfg.Init(settings.RESTClientGetter(), namespace, "secret")
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
