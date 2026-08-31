package mem0

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.orx.me/xbot/internal/conf"
)

type stubServer struct {
	method, path string
	headerKey    string
	headerValue  string
	status       int
	body         string
}

func (s *stubServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.method != "" && r.Method != s.method {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if s.path != "" && r.URL.Path != s.path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("X-Request-ID", "req-123")
		if s.headerKey != "" && r.Header.Get(s.headerKey) != s.headerValue {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(s.status)
		w.Write([]byte(s.body))
	}
}

func TestAdd_SendsExpectedRequest(t *testing.T) {
	var gotBody AddPayloadCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/memories" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("X-API-Key") != "secret-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results":[{"id":"m1","memory":"likes coffee","event":"ADD"}]}`))
	}))
	defer srv.Close()

	c := NewWithHTTPClient(srv.URL, "secret-key", srv.Client())
	infer := true
	result, err := c.Add(context.Background(), AddRequest{
		Messages: []Message{{Role: "user", Content: "I like coffee"}},
		UserID:   "telegram-user:1",
		AgentID:  "xbot:assistant",
		RunID:    "telegram-chat:-100",
		Infer:    &infer,
		Prompt:   "extract",
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].ID != "m1" || result.Results[0].Event != "ADD" {
		t.Fatalf("unexpected results: %+v", result.Results)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Content != "I like coffee" {
		t.Fatalf("unexpected messages: %+v", gotBody.Messages)
	}
	if gotBody.AgentID != "xbot:assistant" || gotBody.RunID != "telegram-chat:-100" || gotBody.UserID != "telegram-user:1" {
		t.Fatalf("unexpected ids: %+v", gotBody)
	}
	if gotBody.Infer == nil || !*gotBody.Infer {
		t.Fatalf("infer = %v, want true", gotBody.Infer)
	}
}

func TestSearch_SendsFilters(t *testing.T) {
	var got SearchCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/search" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results":[{"id":"m1","memory":"likes coffee","score":0.9}]}`))
	}))
	defer srv.Close()

	c := NewWithHTTPClient(srv.URL, "secret-key", srv.Client())
	memories, err := c.Search(context.Background(), SearchRequest{
		Query: "coffee",
		Filters: map[string]string{
			"user_id":  "telegram-user:1",
			"agent_id": "xbot:assistant",
			"run_id":   "telegram-chat:-100",
		},
		TopK:        5,
		ShowExpired: false,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(memories) != 1 || memories[0].Score != 0.9 {
		t.Fatalf("unexpected memories: %+v", memories)
	}
	if got.Query != "coffee" || got.TopK != 5 {
		t.Fatalf("unexpected search payload: %+v", got)
	}
	if got.Filters["user_id"] != "telegram-user:1" || got.Filters["agent_id"] != "xbot:assistant" {
		t.Fatalf("unexpected filters: %+v", got.Filters)
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	c := NewWithHTTPClient(srv.URL, "k", srv.Client())
	memories, err := c.Search(context.Background(), SearchRequest{Query: "q", Filters: map[string]string{"user_id": "u"}})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(memories) != 0 {
		t.Fatalf("memories = %v, want empty", memories)
	}
}

func TestClient_Non2xxReturnsTypedError(t *testing.T) {
	for _, tc := range []struct {
		status int
	}{
		{http.StatusUnauthorized},
		{http.StatusForbidden},
		{http.StatusTooManyRequests},
		{http.StatusInternalServerError},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-ID", "req-123")
				w.WriteHeader(tc.status)
				w.Write([]byte(`{"detail":"boom"}`))
			}))
			defer srv.Close()
			c := NewWithHTTPClient(srv.URL, "k", srv.Client())
			_, err := c.Search(context.Background(), SearchRequest{Query: "q", Filters: map[string]string{"user_id": "u"}})
			if err == nil {
				t.Fatal("expected error")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error type = %T, want *APIError", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.RequestID != "req-123" {
				t.Fatalf("request id = %q, want req-123", apiErr.RequestID)
			}
		})
	}
}

func TestClient_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results": not-json`))
	}))
	defer srv.Close()
	c := NewWithHTTPClient(srv.URL, "k", srv.Client())
	_, err := c.Search(context.Background(), SearchRequest{Query: "q", Filters: map[string]string{"user_id": "u"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Detail, "malformed JSON") {
		t.Fatalf("error = %v, want malformed JSON APIError", err)
	}
}

func TestClient_OversizedResponse(t *testing.T) {
	body := strings.Repeat("a", defaultMaxResponseBytes+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results":[{"memory":"` + body + `"}]}`))
	}))
	defer srv.Close()
	c := NewWithHTTPClient(srv.URL, "k", srv.Client())
	_, err := c.Search(context.Background(), SearchRequest{Query: "q", Filters: map[string]string{"user_id": "u"}})
	if err == nil {
		t.Fatal("expected size-limit error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Detail, "size limit") {
		t.Fatalf("error = %v, want size-limit APIError", err)
	}
}

func TestClient_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(600 * time.Millisecond)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := conf.Mem0{Endpoint: srv.URL, APIKey: "k"}
	cfg.RequestTimeout = conf.Duration(100 * time.Millisecond)
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = c.Search(context.Background(), SearchRequest{Query: "q", Filters: map[string]string{"user_id": "u"}})
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline") {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestClient_Cancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	c := NewWithHTTPClient(srv.URL, "k", srv.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Search(ctx, SearchRequest{Query: "q", Filters: map[string]string{"user_id": "u"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestClient_Validation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewWithHTTPClient(srv.URL, "k", srv.Client())

	if _, err := c.Add(context.Background(), AddRequest{AgentID: "a"}); err == nil {
		t.Fatal("expected error for empty messages")
	}
	if _, err := c.Add(context.Background(), AddRequest{Messages: []Message{{Content: "hi"}}}); err == nil {
		t.Fatal("expected error for missing ids")
	}
	if _, err := c.Search(context.Background(), SearchRequest{Query: "", Filters: map[string]string{"user_id": "u"}}); err == nil {
		t.Fatal("expected error for empty query")
	}
	if _, err := c.Search(context.Background(), SearchRequest{Query: "q"}); err == nil {
		t.Fatal("expected error for empty filters")
	}
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	if _, err := New(conf.Mem0{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := New(conf.Mem0{Endpoint: "not-a-url", APIKey: "k"}); err == nil {
		t.Fatal("expected error for non-http endpoint")
	}
	if _, err := New(conf.Mem0{Endpoint: "https://m.example.com"}); err == nil {
		t.Fatal("expected error for missing api key")
	}
}

type AddPayloadCapture struct {
	Messages []Message      `json:"messages"`
	UserID   string         `json:"user_id"`
	AgentID  string         `json:"agent_id"`
	RunID    string         `json:"run_id"`
	Infer    *bool          `json:"infer"`
	Metadata map[string]any `json:"metadata"`
}

type SearchCapture struct {
	Query   string            `json:"query"`
	Filters map[string]string `json:"filters"`
	TopK    int               `json:"top_k"`
}
