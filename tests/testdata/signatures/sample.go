package server
// Package server implements the MCP server for ctx-saver.
// This is a sample fixture for signatures extraction tests.
package server

import (
	"context"
	"errors"
	"time"
)

// Config holds server configuration.
type Config struct {
	Addr    string
	Timeout time.Duration
}

// Server is the main server struct.
type Server struct {
	cfg *Config
}

// NewServer creates a new Server with the given config.
func NewServer(cfg *Config) *Server {
	return &Server{cfg: cfg}
}

// Start starts the server and blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	if s.cfg == nil {
		return errors.New("server: nil config")
	}
	<-ctx.Done()
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	return nil
}

// Set is a generic set type.
type Set[T comparable] map[T]struct{}

// NewSet returns an empty Set.
func NewSet[T comparable]() Set[T] {
	return make(Set[T])
}

// Add inserts a value into the set.
func (s Set[T]) Add(v T) {
	s[v] = struct{}{}
}

// Contains reports whether v is in the set.
func (s Set[T]) Contains(v T) bool {
	_, ok := s[v]
	return ok
}

// Handler is an embedded struct example.
type Handler struct {
	*Server
	name string
}

// Name returns the handler name.
func (h *Handler) Name() string {
	return h.name
}

const maxRetries = 3

var defaultTimeout = 30 * time.Second
