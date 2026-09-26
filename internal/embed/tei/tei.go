// Package tei embeds text with a text-embeddings-inference server.
package tei

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
)

// DefaultURL : Where the embedding server answers.
const DefaultURL = "http://127.0.0.1:8090"

// DefaultModel : The model the server is expected to be serving.
const DefaultModel = "BAAI/bge-base-en-v1.5"

// DefaultQueryPrefix : What bge models require in front of a search query.
//
// Stored text is embedded without it. Using the same text for both, or
// putting the prefix on the stored side, lowers every score without failing.
const DefaultQueryPrefix = "Represent this sentence for searching relevant passages: "

// DefaultTimeout : How long one call may take.
const DefaultTimeout = 30 * time.Second

// maxResponseBytes : The most that will be read from a reply, so a
// misbehaving endpoint cannot exhaust memory.
const maxResponseBytes = 32 << 20

// Config : What is needed to reach the embedding server.
type Config struct {
	// URL : Where it answers. Empty selects DefaultURL.
	URL string
	// Model : The model it serves, recorded beside every vector. Empty
	// selects DefaultModel.
	Model string
	// QueryPrefix : Put in front of a query and never in front of stored
	// text. Empty selects DefaultQueryPrefix; a single space disables it.
	QueryPrefix string
	// Timeout : How long a call may take. Zero selects DefaultTimeout.
	Timeout time.Duration
	// HTTP : The client to use. Optional.
	HTTP *http.Client
}

// Client : An embed.Embedder backed by a text-embeddings-inference server.
type Client struct {
	cfg  Config
	http *http.Client
}

// New : Builds a Client. It makes no call, so a server whose embedding
// container is not running still starts.
func New(cfg Config) *Client {
	if strings.TrimSpace(cfg.URL) == "" {
		cfg.URL = DefaultURL
	}
	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.QueryPrefix == "" {
		cfg.QueryPrefix = DefaultQueryPrefix
	}
	if strings.TrimSpace(cfg.QueryPrefix) == "" {
		cfg.QueryPrefix = ""
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}

	client := cfg.HTTP
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{cfg: cfg, http: client}
}

// Model : The model named in the configuration.
func (c *Client) Model() string { return c.cfg.Model }

// Available : Always true. Whether the server answers is found by calling it.
func (c *Client) Available() bool { return c != nil }

// Query : Embeds text that is being searched with.
func (c *Client) Query(ctx context.Context, text string) (embed.Vector, error) {
	out, err := c.post(ctx, []string{c.cfg.QueryPrefix + text})
	if err != nil {
		return nil, err
	}
	if len(out) != 1 {
		return nil, fmt.Errorf("tei: asked for 1 vector, got %d", len(out))
	}
	return out[0], nil
}

// Documents : Embeds text that is being stored.
func (c *Client) Documents(ctx context.Context, texts []string) ([]embed.Vector, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out, err := c.post(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(out) != len(texts) {
		return nil, fmt.Errorf("tei: asked for %d vectors, got %d", len(texts), len(out))
	}
	return out, nil
}

// Info : What the server reports about itself.
type Info struct {
	// ModelID : The model it is actually serving.
	ModelID string `json:"model_id"`
	// MaxInputLength : The most tokens it will accept in one input.
	MaxInputLength int `json:"max_input_length"`
}

// Check : Reads the server's own description of itself and reports a model
// other than the configured one, which would make stored vectors and new
// ones incomparable.
func (c *Client) Check(ctx context.Context) (Info, error) {
	var info Info

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.URL+"/info", nil)
	if err != nil {
		return info, fmt.Errorf("tei: building info request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return info, fmt.Errorf("tei: reading server info: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return info, fmt.Errorf("tei: reading server info: %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return info, fmt.Errorf("tei: server info was not usable: %w", err)
	}
	if info.ModelID != "" && info.ModelID != c.cfg.Model {
		return info, fmt.Errorf("tei: server is serving %s, not %s", info.ModelID, c.cfg.Model)
	}
	return info, nil
}

// embedRequest : The body of POST /embed.
type embedRequest struct {
	Inputs []string `json:"inputs"`
	// Normalize : Unit length, so similarity is a dot product.
	Normalize bool `json:"normalize"`
	// Truncate : Cut an over-long input rather than failing the call.
	Truncate bool `json:"truncate"`
}

// post : Sends texts to be embedded and returns their vectors.
func (c *Client) post(ctx context.Context, texts []string) ([]embed.Vector, error) {
	body, err := json.Marshal(embedRequest{Inputs: texts, Normalize: true, Truncate: true})
	if err != nil {
		return nil, fmt.Errorf("tei: building embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tei: building embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tei: embedding: %w", err)
	}
	defer resp.Body.Close()

	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("tei: reading embeddings: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tei: embedding: %s: %s", resp.Status, errorMessage(answer))
	}

	var out []embed.Vector
	if err := json.Unmarshal(answer, &out); err != nil {
		return nil, fmt.Errorf("tei: embeddings were not usable: %w", err)
	}
	for i, v := range out {
		if len(v) == 0 {
			return nil, fmt.Errorf("tei: vector %d came back empty", i)
		}
	}
	return out, nil
}

// errorMessage : What the server said went wrong, or the raw body.
func errorMessage(body []byte) string {
	var wire struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err == nil && wire.Error != "" {
		return wire.Error
	}
	return strings.TrimSpace(string(body))
}

// Ensure Client satisfies the interface it exists to provide.
var _ embed.Embedder = (*Client)(nil)
