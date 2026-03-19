// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

// Package testutil contains lightweight helpers shared by tests.
package testutil

import (
	"context"
	"net"
	"net/http"
)

// LocalServer is a minimal HTTP server wrapper for test code.
type LocalServer struct {
	URL      string
	listener net.Listener
	server   *http.Server
}

// NewLocalServer starts an HTTP server bound to localhost on an ephemeral port.
func NewLocalServer(handler http.Handler) (*LocalServer, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: handler}
	local := &LocalServer{
		URL:      "http://" + listener.Addr().String(),
		listener: listener,
		server:   server,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	return local, nil
}

// Client returns a plain HTTP client suitable for talking to the local test server.
func (s *LocalServer) Client() *http.Client {
	return &http.Client{}
}

// Close shuts down the local test server.
func (s *LocalServer) Close() {
	_ = s.server.Shutdown(context.Background())
}
