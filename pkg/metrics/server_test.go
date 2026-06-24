/*
 * Copyright 2026 The Kubernetes Authors.
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

package metrics

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStartServerDisabled(t *testing.T) {
	t.Parallel()

	server, err := StartServer(context.Background(), -1)
	require.NoError(t, err)
	require.Nil(t, server)
}

func TestStartServerServesMetrics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server, err := StartServer(ctx, 0)
	require.NoError(t, err)
	require.NotNil(t, server)

	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, server.Stop(shutdownCtx))
	})

	addr := server.Addr()
	require.NotEmpty(t, addr)

	var resp *http.Response
	require.Eventually(t, func() bool {
		var err error
		resp, err = http.Get("http://" + addr + "/metrics")
		return err == nil
	}, 5*time.Second, 50*time.Millisecond)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "go_goroutines")
}
