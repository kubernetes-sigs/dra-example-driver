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
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"

	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"
)

// Server serves Prometheus metrics over HTTP.
type Server struct {
	httpServer *http.Server
	addr       string
	wg         sync.WaitGroup
}

// StartServer starts an HTTP server that exposes Prometheus metrics at /metrics.
// When port is negative, the server is not started and (nil, nil) is returned.
func StartServer(ctx context.Context, port int) (*Server, error) {
	log := klog.FromContext(ctx)

	if port < 0 {
		return nil, nil
	}

	addr := net.JoinHostPort("", strconv.Itoa(port))
	mux := http.NewServeMux()
	mux.Handle("/metrics", legacyregistry.HandlerWithReset())

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for metrics server at %s: %w", addr, err)
	}

	server := &Server{
		httpServer: &http.Server{
			Handler: mux,
		},
		addr: listener.Addr().String(),
	}

	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		log.Info("starting metrics server", "addr", listener.Addr().String())
		if err := server.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Error(err, "failed to serve metrics", "addr", addr)
		}
	}()

	return server, nil
}

// Addr returns the address the metrics server is listening on.
func (s *Server) Addr() string {
	if s == nil {
		return ""
	}
	return s.addr
}

// Stop gracefully shuts down the metrics server.
func (s *Server) Stop(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown metrics server: %w", err)
	}
	s.wg.Wait()
	return nil
}
