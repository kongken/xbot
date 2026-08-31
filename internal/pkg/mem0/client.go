// Package mem0 is a focused HTTP client for the Mem0 memory REST API.
// It intentionally exposes only the operations xbot needs: creating
// (ingesting) memories and searching them.
package mem0

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.orx.me/xbot/internal/conf"
)

const (
	defaultMaxResponseBytes = 2 << 20 // 2 MiB
	defaultTimeout          = 10 * time.Second
	maxErrorDetailBytes     = 4096
	truncateErrorBytes      = 1000
	maxTokenRun             = 32
)

// Message is one chat message submitted to Mem0.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Memory is a single returned or created memory.
type Memory struct {
	ID     string  `json:"id"`
	Memory string  `json:"memory"`
	Event  string  `json:"event"`
	Score  float64 `json:"score"`
}

// AddRequest is the payload for POST /memories.
type AddRequest struct {
	Messages   []Message
	UserID     string
	AgentID    string
	RunID      string
	Metadata   map[string]any
	Infer      *bool
	Prompt     string
	Expiration string
}

type addPayload struct {
	Messages []Message      `json:"messages"`
	UserID   string         `json:"user_id,omitempty"`
	AgentID  string         `json:"agent_id,omitempty"`
	RunID    string         `json:"run_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Infer    *bool          `json:"infer,omitempty"`
	Prompt   string         `json:"prompt,omitempty"`
}

// AddResult wraps the Mem0 create response envelope.
type AddResult struct {
	Results []Memory `json:"results"`
}

// SearchRequest is the payload for POST /search.
type SearchRequest struct {
	Query       string
	Filters     map[string]string
	TopK        int
	ShowExpired bool
}

type searchPayload struct {
	Query       string            `json:"query"`
	Filters     map[string]string `json:"filters,omitempty"`
	TopK        int               `json:"top_k,omitempty"`
	ShowExpired bool              `json:"show_expired,omitempty"`
}

// SearchResult wraps the Mem0 search response envelope.
type SearchResult struct {
	Results []Memory `json:"results"`
}

// APIError describes a non-2xx or otherwise failed Mem0 API response.
type APIError struct {
	StatusCode int
	RequestID  string
	Detail     string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("mem0: request failed with status %d", e.StatusCode)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.RequestID != "" {
		msg += " (request-id: " + e.RequestID + ")"
	}
	return msg
}

// Client is a Mem0 REST client.
type Client struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

// New builds a Mem0 client from configuration.
func New(cfg conf.Mem0) (*Client, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" {
		return nil, errors.New("mem0: endpoint is required")
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return nil, fmt.Errorf("mem0: endpoint must be an http(s) URL, got %q", endpoint)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("mem0: apiKey is required")
	}

	timeout := cfg.RequestTimeout.TimeDuration()
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return &Client{
		endpoint: endpoint,
		apiKey:   strings.TrimSpace(cfg.APIKey),
		http: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// NewWithHTTPClient builds a client using an injected HTTP client (for tests).
func NewWithHTTPClient(endpoint, apiKey string, httpClient *http.Client) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		http:     httpClient,
	}
}

// Add creates memories for the given messages.
func (c *Client) Add(ctx context.Context, req AddRequest) (AddResult, error) {
	if len(req.Messages) == 0 {
		return AddResult{}, errors.New("mem0: Add requires at least one message")
	}
	if req.AgentID == "" && req.RunID == "" && req.UserID == "" {
		return AddResult{}, errors.New("mem0: Add requires user_id, agent_id, or run_id")
	}
	for _, message := range req.Messages {
		if strings.TrimSpace(message.Content) == "" {
			return AddResult{}, errors.New("mem0: Add rejects empty message content")
		}
	}

	payload := addPayload{
		Messages: req.Messages,
		UserID:   req.UserID,
		AgentID:  req.AgentID,
		RunID:    req.RunID,
		Metadata: req.Metadata,
		Prompt:   req.Prompt,
	}
	if req.Infer != nil {
		payload.Infer = req.Infer
	}

	var result AddResult
	if err := c.doJSON(ctx, http.MethodPost, "/memories", payload, &result); err != nil {
		return AddResult{}, err
	}
	return result, nil
}

// Search queries Mem0 for relevant memories.
func (c *Client) Search(ctx context.Context, req SearchRequest) ([]Memory, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("mem0: Search requires a query")
	}
	if len(req.Filters) == 0 {
		return nil, errors.New("mem0: Search requires filters")
	}

	payload := searchPayload(req)

	var result SearchResult
	if err := c.doJSON(ctx, http.MethodPost, "/search", payload, &result); err != nil {
		return nil, err
	}
	if result.Results == nil {
		result.Results = []Memory{}
	}
	return result.Results, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mem0: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mem0: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("mem0: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	requestID := resp.Header.Get("X-Request-ID")

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail := readLimited(resp.Body, maxErrorDetailBytes)
		detail = truncateRedact(detail)
		return &APIError{
			StatusCode: resp.StatusCode,
			RequestID:  requestID,
			Detail:     detail,
		}
	}

	limited := http.MaxBytesReader(nil, resp.Body, defaultMaxResponseBytes)
	if err := json.NewDecoder(limited).Decode(out); err != nil {
		if maxErr := new(http.MaxBytesError); errors.As(err, &maxErr) {
			return &APIError{StatusCode: resp.StatusCode, RequestID: requestID, Detail: "response body exceeded size limit"}
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			RequestID:  requestID,
			Detail:     "malformed JSON response: " + err.Error(),
		}
	}
	return nil
}

func readLimited(r io.Reader, limit int64) string {
	b, _ := io.ReadAll(io.LimitReader(r, limit))
	return string(b)
}

var replaceRun = []byte("x")

// truncateRedact hides credentials-like fragments (e.g. tokens in error detail).
func truncateRedact(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > truncateErrorBytes {
		s = s[:truncateErrorBytes] + "... (truncated)"
	}
	// Redact anything resembling a long opaque token so error messages never
	// leak secrets embedded in upstream responses.
	b := []byte(s)
	inQuote := false
	runLen := 0
	for i, ch := range b {
		if ch == '"' {
			inQuote = !inQuote
			runLen = 0
			continue
		}
		if !inQuote {
			continue
		}
		if isTokenChar(ch) {
			runLen++
			if runLen > maxTokenRun {
				b[i] = replaceRun[0]
			}
		} else {
			runLen = 0
		}
	}
	return string(b)
}

func isTokenChar(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' || b == '-' || b == '.'
}
