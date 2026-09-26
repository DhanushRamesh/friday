package tei_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/embed/tei"
)

// embedCall : The body of an /embed call, as the server received it.
type embedCall struct {
	Inputs    []string `json:"inputs"`
	Normalize bool     `json:"normalize"`
	Truncate  bool     `json:"truncate"`
}

// server : A text-embeddings-inference that records what it was sent.
type server struct {
	inputs atomic.Value
	model  string
	status int
	body   string
}

func (s *server) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			model := s.model
			if model == "" {
				model = tei.DefaultModel
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model_id": model, "max_input_length": 512,
			})

		case "/embed":
			var body embedCall
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.inputs.Store(body)

			if s.status >= 400 {
				w.WriteHeader(s.status)
				_, _ = w.Write([]byte(s.body))
				return
			}

			out := make([][]float32, 0, len(body.Inputs))
			for range body.Inputs {
				out = append(out, []float32{1, 0, 0})
			}
			_ = json.NewEncoder(w).Encode(out)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func client(t *testing.T, s *server) *tei.Client {
	t.Helper()
	httpSrv := httptest.NewServer(s.handler())
	t.Cleanup(httpSrv.Close)
	return tei.New(tei.Config{URL: httpSrv.URL})
}

// A query carries the model's instruction prefix and stored text does not.
// Getting this the wrong way round lowers every score without failing.
func TestOnlyAQueryIsPrefixed(t *testing.T) {
	s := &server{}
	c := client(t, s)

	if _, err := c.Query(context.Background(), "what did the roofer charge"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	sent := s.inputs.Load().(embedCall)
	if !strings.HasPrefix(sent.Inputs[0], tei.DefaultQueryPrefix) {
		t.Errorf("query was sent as %q, want the prefix", sent.Inputs[0])
	}

	if _, err := c.Documents(context.Background(), []string{"the roofer quoted 40000"}); err != nil {
		t.Fatalf("Documents: %v", err)
	}
	sent = s.inputs.Load().(embedCall)
	if strings.HasPrefix(sent.Inputs[0], tei.DefaultQueryPrefix) {
		t.Errorf("stored text was sent as %q, want no prefix", sent.Inputs[0])
	}
}

// Vectors are asked for normalized, so similarity is a dot product, and
// truncated, so one long memory does not fail the call.
func TestVectorsAreAskedForNormalizedAndTruncated(t *testing.T) {
	s := &server{}
	c := client(t, s)

	if _, err := c.Documents(context.Background(), []string{"anything"}); err != nil {
		t.Fatalf("Documents: %v", err)
	}
	sent := s.inputs.Load().(embedCall)
	if !sent.Normalize {
		t.Error("vectors were not asked for normalized")
	}
	if !sent.Truncate {
		t.Error("over-long input was not asked to be truncated")
	}
}

// One vector comes back per text, in order.
func TestOneVectorPerText(t *testing.T) {
	c := client(t, &server{})

	got, err := c.Documents(context.Background(), []string{"one", "two", "three"})
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d vectors, want 3", len(got))
	}
}

// Embedding nothing is not worth a round trip.
func TestNoTextIsNoCall(t *testing.T) {
	s := &server{}
	c := client(t, s)

	got, err := c.Documents(context.Background(), nil)
	if err != nil || got != nil {
		t.Errorf("Documents(nil) = %v, %v, want nil, nil", got, err)
	}
	if s.inputs.Load() != nil {
		t.Error("an empty request reached the server")
	}
}

// A server serving a different model would produce vectors that cannot be
// compared with the stored ones, and nothing about that fails on its own.
func TestADifferentModelIsReported(t *testing.T) {
	c := client(t, &server{model: "some/other-model"})

	_, err := c.Check(context.Background())
	if err == nil {
		t.Fatal("Check passed, want the wrong model reported")
	}
	if !strings.Contains(err.Error(), "some/other-model") {
		t.Errorf("error = %v, want it to name what is being served", err)
	}
}

// The right model passes.
func TestTheExpectedModelPasses(t *testing.T) {
	c := client(t, &server{})

	info, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if info.ModelID != tei.DefaultModel {
		t.Errorf("model = %q, want %q", info.ModelID, tei.DefaultModel)
	}
}

// What the server said went wrong is repeated, rather than a status code.
func TestTheServersOwnErrorIsReported(t *testing.T) {
	c := client(t, &server{
		status: http.StatusBadRequest,
		body:   `{"error":"input is too long","error_type":"validation"}`,
	})

	_, err := c.Query(context.Background(), "anything")
	if err == nil {
		t.Fatal("Query succeeded, want the failure")
	}
	if !strings.Contains(err.Error(), "input is too long") {
		t.Errorf("error = %v, want what the server said", err)
	}
}

// Nothing is called when the client is built, so a server whose embedding
// container is not running still starts.
func TestBuildingItCallsNothing(t *testing.T) {
	c := tei.New(tei.Config{URL: "http://127.0.0.1:1"})
	if c.Model() != tei.DefaultModel {
		t.Errorf("model = %q, want the default", c.Model())
	}
}

// A single space turns the prefix off, for a model that does not want one.
func TestASpaceDisablesThePrefix(t *testing.T) {
	s := &server{}
	httpSrv := httptest.NewServer(s.handler())
	t.Cleanup(httpSrv.Close)

	c := tei.New(tei.Config{URL: httpSrv.URL, QueryPrefix: " "})
	if _, err := c.Query(context.Background(), "plain"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	sent := s.inputs.Load().(embedCall)
	if sent.Inputs[0] != "plain" {
		t.Errorf("query was sent as %q, want it unprefixed", sent.Inputs[0])
	}
}

var _ embed.Embedder = (*tei.Client)(nil)
