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

package flags

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v2"
)

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: test
`

// runApp parses args with the given flags, like the driver binaries do.
func runApp(t *testing.T, flags []cli.Flag, args ...string) {
	t.Helper()
	app := &cli.App{Flags: flags, Action: func(*cli.Context) error { return nil }}
	if err := app.Run(append([]string{"test"}, args...)); err != nil {
		t.Fatalf("parse flags %v: %v", args, err)
	}
}

func TestKubeClientConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", "")

	var k KubeClientConfig
	runApp(t, k.Flags(), "--kubeconfig", path, "--kube-api-qps", "20", "--kube-api-burst", "40")
	config, err := k.NewClientSetConfig()
	if err != nil {
		t.Fatalf("NewClientSetConfig() error = %v", err)
	}
	if config.Host != "https://127.0.0.1:6443" || config.QPS != 20 || config.Burst != 40 {
		t.Errorf("config = host %q qps %v burst %d, want the kubeconfig server, 20 and 40", config.Host, config.QPS, config.Burst)
	}
	clientSets, err := k.NewClientSets()
	if err != nil || clientSets.Core == nil || clientSets.Dynamic == nil {
		t.Errorf("NewClientSets() = %+v, %v; want both clients", clientSets, err)
	}

	missing := KubeClientConfig{KubeConfig: filepath.Join(t.TempDir(), "missing")}
	if _, err := missing.NewClientSets(); err == nil {
		t.Error("NewClientSets() with a missing kubeconfig: want error")
	}
}

func TestKubeClientConfigInCluster(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	var k KubeClientConfig
	if _, err := k.NewClientSetConfig(); err == nil {
		t.Error("NewClientSetConfig() outside a cluster without kubeconfig: want error")
	}
}

func TestLoggingConfig(t *testing.T) {
	l := NewLoggingConfig()
	flags := l.Flags()
	names := map[string]bool{}
	for _, f := range flags {
		names[f.Names()[0]] = true
	}
	for _, want := range []string{"v", "logging-format", "feature-gates"} {
		if !names[want] {
			t.Errorf("logging flags lack %q", want)
		}
	}
	runApp(t, flags, "-v", "2", "--logging-format", "json")
	if err := l.Apply(); err != nil {
		t.Errorf("Apply() error = %v", err)
	}
	if got := l.config.Format; got != "json" {
		t.Errorf("logging format = %q, want json", got)
	}
}
