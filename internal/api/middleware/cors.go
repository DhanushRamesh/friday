package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// corsMaxAge : How long a browser may remember the answer to a preflight.
//
// Authorization is not one of the handful of headers a browser will send
// cross-origin without asking first, so without this every call would cost
// two round trips instead of one.
const corsMaxAge = "600"

// corsHeaders : The request headers a client is allowed to send.
const corsHeaders = "Authorization, Content-Type, Accept"

// corsMethods : The methods the API uses.
const corsMethods = "GET, POST, DELETE, OPTIONS"

// CrossOrigin : Returns middleware that answers a browser's cross-origin
// checks, or a pass-through when enabled is false.
//
// It exists only for development. In production the web UI is served by
// The server itself, so the browser never makes a cross-origin request and this
// is switched off; during development `flutter run` serves the UI from its
// own port so that hot reload works, and without this the browser refuses
// every call.
//
// Only a loopback origin is ever allowed, and the origin is echoed back
// rather than answered with a wildcard: a wildcard cannot carry credentials,
// and every call here carries a bearer token.
func CrossOrigin(enabled bool) func(http.Handler) http.Handler {
	if !enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// The answer differs by origin, so a cache must not serve one
			// origin's response to another.
			w.Header().Add("Vary", "Origin")

			if origin == "" || !isLoopbackOrigin(origin) {
				// Not a cross-origin request, or not one from a machine the
				// developer is sitting at. A preflight still has to be
				// answered, but without permission.
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", corsMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsHeaders)
				w.Header().Set("Access-Control-Max-Age", corsMaxAge)
				// The preflight is a question about a later request, not a
				// request itself, so it never reaches a handler.
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isLoopbackOrigin : Reports whether an Origin header names this machine.
//
// Checked by parsing rather than by prefix: "http://localhost.attacker.com"
// starts with the same characters as a local origin and is not one.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	host := u.Hostname()
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
