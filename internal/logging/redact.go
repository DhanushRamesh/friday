package logging

import (
	"log/slog"
	"strings"
)

// Redacted replaces the value of any attribute considered sensitive.
const Redacted = "[REDACTED]"

// sensitiveKeys are attribute names whose values are never written to logs.
//
// Matching is exact (case-insensitive) rather than by substring on purpose:
// FRIDAY logs model token counts, and a substring rule on "token" would redact
// token_count and tokens_used along with the credentials. Anything not named
// here should be wrapped in Secret at the call site instead.
var sensitiveKeys = map[string]struct{}{
	"access_token":        {},
	"api_key":             {},
	"apikey":              {},
	"auth":                {},
	"authorization":       {},
	"bearer":              {},
	"client_secret":       {},
	"cookie":              {},
	"credential":          {},
	"credentials":         {},
	"id_token":            {},
	"passwd":              {},
	"password":            {},
	"private_key":         {},
	"proxy-authorization": {},
	"refresh_token":       {},
	"secret":              {},
	"session_token":       {},
	"set-cookie":          {},
	"token":               {},
}

// redactor builds the ReplaceAttr hook, folding any caller-supplied keys into
// the built-in set.
func redactor(extra []string) func([]string, slog.Attr) slog.Attr {
	keys := sensitiveKeys
	if len(extra) > 0 {
		keys = make(map[string]struct{}, len(sensitiveKeys)+len(extra))
		for k := range sensitiveKeys {
			keys[k] = struct{}{}
		}
		for _, k := range extra {
			keys[strings.ToLower(strings.TrimSpace(k))] = struct{}{}
		}
	}

	return func(groups []string, attr slog.Attr) slog.Attr {
		if _, ok := keys[strings.ToLower(attr.Key)]; ok {
			attr.Value = slog.StringValue(Redacted)
		}
		return attr
	}
}

// Secret wraps a credential so it cannot be logged or printed by accident.
//
// Use it for values whose attribute key is not self-evidently sensitive:
//
//	slog.Any("gitlab_pat", logging.Secret(pat))  // logs [REDACTED]
//
// Call Reveal only where the value is actually used, never in a log call.
type Secret string

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(Redacted) }

// String implements fmt.Stringer, so %s and %v cannot leak the value either.
func (s Secret) String() string { return Redacted }

// Reveal returns the underlying value.
func (s Secret) Reveal() string { return string(s) }
