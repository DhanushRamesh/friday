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

	"github.com/DhanushRamesh/friday/internal/events"
	"github.com/DhanushRamesh/friday/internal/task"
)

// DefaultRequestTimeout : The per-request deadline applied when Options does
// not name one.
const DefaultRequestTimeout = 30 * time.Second

// Pinger : The part of a database handle that the readiness check requires.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Runner : The part of the task runner that the API requires.
//
// Taking an interface rather than the concrete runner keeps the handlers
// testable without executing anything.
type Runner interface {
	// Submit : Starts running a stored task in the background.
	Submit(t *task.Task) error
	// Cancel : Stops a queued or running task, reporting whether one was
	// found.
	Cancel(id string) bool
}

// Subscriber : Somewhere to listen for a task's messages as they happen.
type Subscriber interface {
	// Subscribe : Returns a channel of a task's events and a function that
	// ends the subscription.
	Subscribe(taskID string) (<-chan events.Event, func())
}

// Options : The dependencies and settings a Server is built from.
type Options struct {
	// Logger : Receives request records and handler errors. Required.
	Logger *slog.Logger
	// DB : Checked by the readiness endpoint. Required.
	DB Pinger
	// Tasks : Stores and retrieves tasks. Required.
	Tasks task.Repository
	// Runner : Executes tasks. Required.
	Runner Runner
	// Events : Carries a task's messages to clients listening for them.
	// Required for streaming.
	Events Subscriber
	// RequestTimeout : The per-request deadline. Zero selects
	// DefaultRequestTimeout.
	RequestTimeout time.Duration
}

// Server : FRIDAY's HTTP interface. It implements http.Handler.
type Server struct {
	logger         *slog.Logger
	db             Pinger
	tasks          task.Repository
	runner         Runner
	events         Subscriber
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
		tasks:          opts.Tasks,
		runner:         opts.Runner,
		events:         opts.Events,
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

	s.router.Route("/v1/tasks", func(r chi.Router) {
		r.Post("/", s.handleCreateTask)
		r.Get("/", s.handleListTasks)
		r.Get("/{id}", s.handleGetTask)
		r.Get("/{id}/messages", s.handleGetTaskMessages)
		r.Get("/{id}/stream", s.handleStreamTask)
		r.Post("/{id}/cancel", s.handleCancelTask)
	})

	s.router.Route("/v1/conversations", func(r chi.Router) {
		r.Get("/", s.handleListConversations)
		r.Get("/{id}", s.handleGetConversation)
	})
}
