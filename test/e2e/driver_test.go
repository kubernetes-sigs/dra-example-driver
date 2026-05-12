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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

	chartPath := resolveChartPath()

	// Ensure the install namespace exists before Helm runs.
	_, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: cfg.Namespace},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(),
			"Failed to create driver namespace %s", cfg.Namespace)
	}

	// Register cleanup before install so partially-installed releases are
	// torn down. DeferCleanup is LIFO so driver-log diagnostics run first.
	DeferCleanup(uninstallDriver, cfg, NodeTimeout(driverUninstallTimeout))
	DeferCleanup(dumpDriverDiagnostics, cfg, NodeTimeout(30*time.Second))

	args := []string{
		"upgrade", "--install", cfg.ReleaseName, chartPath,
		"--namespace", cfg.Namespace,
		"--wait",
		"--timeout", driverInstallTimeout.String(),
	}
	args = append(args, helmValueArgs(cfg)...)

	fmt.Fprintf(GinkgoWriter,
		"Installing driver release %s/%s (driverName=%s, webhook=%v)\n",
		cfg.Namespace, cfg.ReleaseName, cfg.DriverName, cfg.WebhookEnabled)

	out, err := runHelm(ctx, args...)
	Expect(err).NotTo(HaveOccurred(),
		"Failed to install helm release %s/%s: %s",
		cfg.Namespace, cfg.ReleaseName, out)

	waitForDriverReady(ctx, cfg)
	return cfg
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

// helmValueArgs builds the --set arguments for installing the chart per cfg.
// Test-supplied ExtraValues are applied last so they can override any default.
func helmValueArgs(cfg DriverConfig) []string {
	args := []string{
		"--set", "driverName=" + cfg.DriverName,
		"--set", "namespaceOverride=" + cfg.Namespace,
		"--set", fmt.Sprintf("kubeletPlugin.numDevices=%d", cfg.NumDevices),
		"--set", fmt.Sprintf("webhook.enabled=%t", cfg.WebhookEnabled),
	}
	if cfg.ExtendedResourceName != "" {
		args = append(args, "--set", "deviceClass.extendedResourceName="+cfg.ExtendedResourceName)
	}
	for k, v := range cfg.ExtraValues {
		args = append(args, "--set", k+"="+v)
	}
	return args
}

// uninstallDriver removes the Helm release and waits for the install
// namespace to terminate. Safe to register before install runs.
func uninstallDriver(ctx context.Context, cfg DriverConfig) {
	out, err := runHelm(ctx, "uninstall", cfg.ReleaseName,
		"--namespace", cfg.Namespace,
		"--wait",
		"--timeout", driverUninstallTimeout.String(),
		"--ignore-not-found",
	)
	if err != nil {
		fmt.Fprintf(GinkgoWriter,
			"Warning: failed to uninstall release %s/%s: %v\n%s\n",
			cfg.Namespace, cfg.ReleaseName, err, out)
	}

	deletePolicy := metav1.DeletePropagationForeground
	err = clientset.CoreV1().Namespaces().Delete(ctx, cfg.Namespace, metav1.DeleteOptions{
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

	tailLines := int64(20)
	driverPods, err := clientset.CoreV1().Pods(cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: driverPodSelector,
	})
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "Failed to list driver pods: %v\n", err)
		return
	}
	for _, pod := range driverPods.Items {
		for _, c := range pod.Spec.Containers {
			stream, err := clientset.CoreV1().Pods(cfg.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: c.Name,
				TailLines: &tailLines,
			}).Stream(ctx)
			if err != nil {
				fmt.Fprintf(GinkgoWriter,
					"Driver pod %s, container %s: failed to get logs: %v\n",
					pod.Name, c.Name, err)
				continue
			}
			buf := new(bytes.Buffer)
			_, _ = io.Copy(buf, stream)
			stream.Close()
			fmt.Fprintf(GinkgoWriter,
				"Driver pod %s, container %s (last %d lines):\n%s\n",
				pod.Name, c.Name, tailLines, buf.String())
		}
	}
}

// waitForDriverReady polls for signals that Helm --wait does not cover:
// the DeviceClass exists, ResourceSlices have been published, and (when
// enabled) the webhook is serving.
func waitForDriverReady(ctx context.Context, cfg DriverConfig) {
	GinkgoHelper()

	Eventually(func(g Gomega, ctx context.Context) {
		_, err := clientset.ResourceV1().DeviceClasses().Get(ctx, cfg.DriverName, metav1.GetOptions{})
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

// resolveChartPath returns HELM_CHART_PATH (local path or "oci://..." URL)
// when set, otherwise the in-repo chart path.
func resolveChartPath() string {
	if p := os.Getenv(chartPathEnvVar); p != "" {
		return p
	}
	return filepath.Join(rootDir, "deployments", "helm", "dra-example-driver")
}

// runHelm executes a helm subcommand and returns its combined output.
func runHelm(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "helm", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
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
