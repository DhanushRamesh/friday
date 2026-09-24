package httpx_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
)

// body : A request shape to decode into.
type body struct {
	Prompt string `json:"prompt"`
}

// decode : Runs DecodeJSON over a request carrying the given body.
func decode(raw string, into any) error {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))
	return httpx.DecodeJSON(httptest.NewRecorder(), r, into)
}

func TestDecodeJSONAcceptsAWellFormedBody(t *testing.T) {
	var got body
	if err := decode(`{"prompt":"hello"}`, &got); err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if got.Prompt != "hello" {
		t.Errorf("prompt = %q, want hello", got.Prompt)
	}
}

func TestDecodeJSONRefusesUnusableBodies(t *testing.T) {
	cases := map[string]string{
		"empty":          ``,
		"not json":       `nonsense`,
		"unknown field":  `{"prompt":"hi","model":"gpt"}`,
		"two objects":    `{"prompt":"hi"}{"prompt":"again"}`,
		"wrong type":     `{"prompt":42}`,
		"array not body": `[{"prompt":"hi"}]`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var got body
			err := decode(raw, &got)
			if err == nil {
				t.Fatal("accepted a body it should have refused")
			}
			// The message reaches the caller and may be spoken aloud, so it
			// must read as a sentence rather than as a decoder's complaint.
			if msg := err.Error(); !strings.HasSuffix(msg, ".") {
				t.Errorf("message = %q, want a plain sentence", msg)
			}
		})
	}
}

// A body beyond the limit is refused rather than read into memory.
func TestDecodeJSONRefusesAnOversizedBody(t *testing.T) {
	huge := `{"prompt":"` + strings.Repeat("a", httpx.MaxRequestBody+1024) + `"}`

	var got body
	err := decode(huge, &got)
	if err == nil {
		t.Fatal("an oversized body was accepted")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("message = %q, want it to say the request is too large", err.Error())
	}
}

func TestWriteErrorCarriesTheMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteError(t.Context(), rec, http.StatusNotFound, "No such chat.")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var out httpx.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	if out.Error != "No such chat." {
		t.Errorf("error = %q, want the message", out.Error)
	}
}

// Fail logs the cause and tells the caller nothing about it.
func TestFailLogsTheCauseButDoesNotReturnIt(t *testing.T) {
	logs := &bytes.Buffer{}
	responder := httpx.Responder{Logger: newLogger(logs)}

	rec := httptest.NewRecorder()
	responder.Fail(t.Context(), rec, "reading chat",
		errTest{"dsn=user:password@tcp(db)/friday"})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("the cause reached the caller: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "password") {
		t.Errorf("the cause was not logged, so the failure is undiagnosable:\n%s", logs)
	}
	if !strings.Contains(logs.String(), "reading chat") {
		t.Error("the log does not say what was being done")
	}
}

// errTest : An error carrying text that must not escape to a caller.
type errTest struct{ msg string }

// Error : Returns the message.
func (e errTest) Error() string { return e.msg }

// newLogger : A JSON logger writing into buf.
func newLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}
