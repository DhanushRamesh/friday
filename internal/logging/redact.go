package logging

import (
	"log/slog"
	"strings"
)

// Redacted : The placeholder substituted for a redacted value.
const Redacted = "[REDACTED]"

// sensitiveKeys : Holds the attribute names whose values are replaced with
// Redacted.
//
// Keys match exactly rather than by substring, so that names merely
// containing a sensitive word, such as token_count, are left intact. A value
// whose key is not listed here can be protected with Secret instead.
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

// redactor : Returns a slog.HandlerOptions.ReplaceAttr function that redacts
// the built-in sensitive keys together with extra.
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

// Secret : A string that renders as Redacted when logged or formatted.
//
// It protects a credential whose attribute key is not itself recognised as
// sensitive:
//
//	slog.Any("gitlab_pat", logging.Secret(pat))
//
// The underlying value is reachable only through Reveal.
type Secret string

// LogValue : Implements slog.LogValuer and returns Redacted.
func (s Secret) LogValue() slog.Value { return slog.StringValue(Redacted) }

// String : Implements fmt.Stringer and returns Redacted, so that %s and %v do
// not expose the value either.
func (s Secret) String() string { return Redacted }

// Reveal : Returns the underlying string.
func (s Secret) Reveal() string { return string(s) }
