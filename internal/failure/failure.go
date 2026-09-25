// Package failure turns what went wrong into what to say about it.
//
// Two audiences want different things from the same failure. Somebody waiting
// for an answer wants one sentence telling them whether to try again; whoever
// is fixing it wants the exact words the service used, status code and all.
// Writing one line for both produces a sentence that is either useless aloud
// or leaks internals into a room.
//
// So a failure carries both: a [Code], which chooses a fixed sentence, and a
// detail, which is whatever the service actually said. The sentence is spoken
// and shown; the detail waits behind "more info" until asked for.
package failure

import (
	"fmt"
	"net/http"
	"strings"
)

// Code : What kind of failure it was.
//
// Fixed, and small on purpose. A code exists when it changes what the person
// should do — wait, try again, tell somebody — and not merely because two
// failures came from different lines of code.
type Code string

const (
	// Unreachable : The service could not be contacted at all.
	Unreachable Code = "unreachable"
	// Timeout : It was contacted and did not answer in time.
	Timeout Code = "timeout"
	// Unauthorised : Credentials were refused, so they need renewing.
	Unauthorised Code = "unauthorised"
	// Forbidden : The credentials are known but not allowed to do this.
	Forbidden Code = "forbidden"
	// RateLimited : Too many requests too quickly.
	RateLimited Code = "rate_limited"
	// Unavailable : The service is up but not serving.
	Unavailable Code = "unavailable"
	// BadRequest : The service refused what was sent.
	BadRequest Code = "bad_request"
	// TooLong : The conversation no longer fits.
	TooLong Code = "too_long"
	// BadResponse : It answered, and the answer could not be used.
	BadResponse Code = "bad_response"
	// Unexpected : Anything with no better description. The fallback.
	Unexpected Code = "unexpected"
)

// sentences : What is said aloud for each code.
//
// Written to be heard, so no code, no status number and no jargon. Each one
// says what happened and whether waiting will help, because that is the only
// decision the listener has.
var sentences = map[Code]string{
	Unreachable:  "I could not reach the service that answers this.",
	Timeout:      "The service did not answer in time.",
	Unauthorised: "I am not allowed to reach the service. Its credentials need renewing.",
	Forbidden:    "I am not permitted to do that.",
	RateLimited:  "The service is busy. Ask me again in a moment.",
	Unavailable:  "The service is unavailable at the moment. Try again shortly.",
	BadRequest:   "The service would not accept that request.",
	TooLong:      "This conversation has grown too long. Start a new session.",
	BadResponse:  "The service answered with something I could not use.",
	Unexpected:   "The service could not complete the request.",
}

// Sentence : What to say for a code.
//
// An unknown code is not an error worth failing over — it is already being
// reported because something else went wrong — so it falls back to
// [Unexpected] and the caller logs it.
func Sentence(code Code) string {
	if s, ok := sentences[code]; ok {
		return s
	}
	return sentences[Unexpected]
}

// Known : Whether a code has a sentence of its own.
//
// Separate from [Sentence] so a caller can log the unknown one rather than
// silently reporting it as unexpected.
func Known(code Code) bool {
	_, ok := sentences[code]
	return ok
}

// byStatus : The code an HTTP status means.
//
// Only statuses that say something distinct are here. Everything else is
// [Unexpected], which is honest: a 418 from a model service tells the person
// nothing beyond that it did not work.
var byStatus = map[int]Code{
	http.StatusBadRequest:            BadRequest,
	http.StatusUnauthorized:          Unauthorised,
	http.StatusForbidden:             Forbidden,
	http.StatusNotFound:              BadRequest,
	http.StatusRequestTimeout:        Timeout,
	http.StatusRequestEntityTooLarge: TooLong,
	http.StatusTooManyRequests:       RateLimited,
	http.StatusInternalServerError:   Unexpected,
	http.StatusBadGateway:            Unreachable,
	http.StatusServiceUnavailable:    Unavailable,
	http.StatusGatewayTimeout:        Timeout,
}

// FromHTTP : The code an HTTP status maps to.
func FromHTTP(status int) Code {
	if code, ok := byStatus[status]; ok {
		return code
	}
	return Unexpected
}

// Error : A failure, in both the forms anybody needs it.
type Error struct {
	// Code : Which kind it was, and so which sentence is said.
	Code Code
	// Detail : What the service actually said, kept exactly.
	//
	// Never spoken unasked. It is shown behind "more info", and it is given
	// to the model so that being asked what precisely failed can be
	// answered instead of guessed at.
	Detail string
	// Status : The HTTP status, when there was one. Zero otherwise.
	Status int
	// Cause : The underlying error, for errors.Is and errors.As.
	Cause error
}

// New : A failure with a code and the detail behind it.
func New(code Code, detail string, cause error) *Error {
	return &Error{Code: code, Detail: strings.TrimSpace(detail), Cause: cause}
}

// FromStatus : A failure described by the status the service returned.
func FromStatus(status int, detail string, cause error) *Error {
	return &Error{
		Code:   FromHTTP(status),
		Detail: strings.TrimSpace(detail),
		Status: status,
		Cause:  cause,
	}
}

// Sentence : What to say aloud about this failure.
func (e *Error) Sentence() string { return Sentence(e.Code) }

// Full : The detail together with the status, for a log or a "more info".
//
// Empty when there is nothing to add, so a caller can tell the difference
// between a failure that explains itself and one that does not.
func (e *Error) Full() string {
	switch {
	case e.Detail != "" && e.Status != 0:
		return fmt.Sprintf("%s (HTTP %d)", e.Detail, e.Status)
	case e.Detail != "":
		return e.Detail
	case e.Status != 0:
		return fmt.Sprintf("HTTP %d", e.Status)
	default:
		return ""
	}
}

// Error : Describes the failure for a log.
func (e *Error) Error() string {
	if full := e.Full(); full != "" {
		return fmt.Sprintf("%s: %s", e.Code, full)
	}
	return string(e.Code)
}

// Unwrap : The underlying error, so errors.Is reaches it.
func (e *Error) Unwrap() error { return e.Cause }
