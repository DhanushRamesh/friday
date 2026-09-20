// Package api : Serves FRIDAY's HTTP interface.
//
// A Server holds the dependencies its handlers need and exposes them as one
// http.Handler. Routing, middleware and handlers live here rather than in
// package main so that the whole interface can be exercised in tests without
// starting a process.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// DefaultRequestTimeout : The per-request deadline applied when Options does
// not name one.
const DefaultRequestTimeout = 30 * time.Second

// Pinger : The part of a database handle that the readiness check requires.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Options : The dependencies and settings a Server is built from.
type Options struct {
	// Logger : Receives request records and handler errors. Required.
	Logger *slog.Logger
	// DB : Checked by the readiness endpoint. Required.
	DB Pinger
	// RequestTimeout : The per-request deadline. Zero selects
	// DefaultRequestTimeout.
	RequestTimeout time.Duration
}

// Server : FRIDAY's HTTP interface. It implements http.Handler.
type Server struct {
	logger         *slog.Logger
	db             Pinger
	requestTimeout time.Duration
	router         chi.Router
}

// New : Builds a Server from opts and registers its routes.
func New(opts Options) *Server {
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}

	s := &Server{
		logger:         opts.Logger,
		db:             opts.DB,
		requestTimeout: opts.RequestTimeout,
		router:         chi.NewRouter(),
	}
	s.routes()
	return s
}

// ServeHTTP : Dispatches r to the matching route.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// routes : Registers the middleware stack and every endpoint.
//
// Middleware order is significant: RequestID must run before requestContext,
// which must run before anything that logs, so that every record produced
// while serving a request carries its identifier.
func (s *Server) routes() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(s.requestContext)
	s.router.Use(s.requestLogger)
	s.router.Use(s.recoverer)
	s.router.Use(middleware.Timeout(s.requestTimeout))

	s.router.Get("/health", s.handleHealth)
	s.router.Get("/ready", s.handleReady)
}
