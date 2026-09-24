// Package api : Assembles the server's HTTP interface from its modules.
//
// Nothing is served from here. Each group of endpoints lives in its own
// package below this one — authn, clients, sessions, chats, assist, health —
// holding its own handlers and wire types, and this package's only job is to build
// them from one set of dependencies, decide the middleware they sit behind,
// and mount them on one router.
//
// Keeping the assembly in one place is what makes the shape of the API
// readable: routes() below is the whole surface, and a module cannot quietly
// register an endpoint somewhere else.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/DhanushRamesh/personal-assistant/internal/api/assist"
	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/chats"
	"github.com/DhanushRamesh/personal-assistant/internal/api/clients"
	"github.com/DhanushRamesh/personal-assistant/internal/api/health"
	"github.com/DhanushRamesh/personal-assistant/internal/api/middleware"
	"github.com/DhanushRamesh/personal-assistant/internal/api/sessions"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// DefaultRequestTimeout : The per-request deadline applied when Options does
// not name one.
const DefaultRequestTimeout = 30 * time.Second

// The interfaces the modules require, republished here so that a caller
// building a Server does not have to import the module that defines each one.
type (
	// Pinger : The part of a database handle that the readiness check
	// requires.
	Pinger = health.Pinger
	// Runner : The part of the chat runner that the API requires.
	Runner = chats.Runner
	// Subscriber : Somewhere to listen for a chat's messages as they happen.
	Subscriber = chats.Subscriber
)

// Options : The dependencies and settings a Server is built from.
type Options struct {
	// Logger : Receives request records and handler errors. Required.
	Logger *slog.Logger
	// DB : Checked by the readiness endpoint. Required.
	DB Pinger
	// Chats : Stores and retrieves chats. Required.
	Chats chat.Repository
	// Runner : Executes chats. Required.
	Runner Runner
	// Events : Carries a chat's messages to clients listening for them.
	// Required for streaming.
	Events Subscriber

	// RequestTimeout : The per-request deadline. Zero selects
	// DefaultRequestTimeout.
	RequestTimeout time.Duration

	// AllowCrossOrigin : Whether a browser on this machine may call the API
	// from another origin.
	//
	// For development only, where `flutter run` serves the UI from its own
	// port so that hot reload works. In production the server serves the UI
	// itself, so every call is same-origin and this stays false.
	AllowCrossOrigin bool
}

// Server : the server's HTTP interface. It implements http.Handler.
type Server struct {
	logger           *slog.Logger
	requestTimeout   time.Duration
	allowCrossOrigin bool
	router           chi.Router

	health   *health.Handler
	authn    *authn.Handler
	clients  *clients.Handler
	sessions *sessions.Handler
	chats    *chats.Handler
	assist   *assist.Handler
}

// New : Builds a Server from opts and registers its routes.
func New(opts Options) *Server {
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}

	s := &Server{
		logger:           opts.Logger,
		requestTimeout:   opts.RequestTimeout,
		allowCrossOrigin: opts.AllowCrossOrigin,
		router:           chi.NewRouter(),

		health:   health.New(opts.Logger, opts.DB),
		authn:    authn.New(opts.Logger, opts.Chats),
		clients:  clients.New(opts.Logger, opts.Chats),
		sessions: sessions.New(opts.Logger, opts.Chats),
		chats:    chats.New(opts.Logger, opts.Chats, opts.Runner, opts.Events),
		assist:   assist.New(opts.Logger, opts.Chats, opts.Runner, opts.Events),
	}
	s.routes()
	return s
}

// ServeHTTP : Dispatches r to the matching route.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// routes : Registers the middleware stack and mounts every module.
//
// Middleware order is significant: RequestID must run before RequestContext,
// which must run before anything that logs, so that every record produced
// while serving a request carries its identifier.
func (s *Server) routes() {
	s.router.Use(chimw.RequestID)
	s.router.Use(chimw.RealIP)
	s.router.Use(middleware.RequestContext)
	s.router.Use(middleware.RequestLogger(s.logger))
	s.router.Use(middleware.Recoverer(s.logger))
	// After the logger, so that a preflight is recorded like any other
	// request rather than vanishing; before authentication, because a
	// preflight cannot carry a token and would be refused before it was
	// answered.
	s.router.Use(middleware.CrossOrigin(s.allowCrossOrigin))
	s.router.Use(chimw.Timeout(s.requestTimeout))

	// Open: probed by infrastructure, and the one call that issues a token.
	s.health.Mount(s.router)
	s.authn.Mount(s.router)

	// Everything else needs one. Grouped so that authentication is applied
	// once here rather than remembered by each module.
	s.router.Group(func(r chi.Router) {
		r.Use(s.authn.Require)
		s.clients.Mount(r)
		s.sessions.Mount(r)
		s.chats.Mount(r)
		s.assist.Mount(r)
	})
}
