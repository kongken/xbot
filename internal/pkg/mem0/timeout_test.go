package mem0_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.orx.me/xbot/internal/conf"
	"go.orx.me/xbot/internal/pkg/mem0"
	"gopkg.in/yaml.v3"
)

func TestAddOutlivesConcurrentSearchTimeout(t *testing.T) {
	t.Run("configured write timeout", func(t *testing.T) {
		assertAddOutlivesSearchTimeout(t, "writeTimeout: 2s\n")
	})
	t.Run("default write timeout", func(t *testing.T) {
		assertAddOutlivesSearchTimeout(t, "")
	})
}

func assertAddOutlivesSearchTimeout(t *testing.T, writeConfig string) {
	t.Helper()
	addStarted := make(chan struct{})
	releaseAdd := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		switch r.URL.Path {
		case "/memories":
			close(addStarted)
			select {
			case <-releaseAdd:
				_, _ = io.WriteString(w, `{"results":[{"id":"m1","event":"ADD"}]}`)
			case <-r.Context().Done():
			}
		case "/search":
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	var cfg conf.Mem0
	data := "endpoint: " + server.URL + "\napiKey: test-key\nrequestTimeout: 100ms\n" + writeConfig
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	client, err := mem0.New(cfg)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	type addOutcome struct {
		result mem0.AddResult
		err    error
	}
	added := make(chan addOutcome, 1)
	go func() {
		result, err := client.Add(t.Context(), mem0.AddRequest{
			Messages: []mem0.Message{{Role: "user", Content: "test memory"}},
			AgentID:  "test-agent",
		})
		added <- addOutcome{result: result, err: err}
	}()
	select {
	case <-addStarted:
	case result := <-added:
		t.Fatalf("Add returned before reaching the server: %v", result.err)
	case <-time.After(5 * time.Second):
		t.Fatal("Add did not reach the server")
	}

	_, searchErr := client.Search(t.Context(), mem0.SearchRequest{
		Query:   "test",
		Filters: map[string]string{"agent_id": "test-agent"},
	})
	close(releaseAdd)
	if !errors.Is(searchErr, context.DeadlineExceeded) {
		t.Errorf("Search error = %v, want deadline exceeded", searchErr)
	}
	select {
	case result := <-added:
		if result.err != nil {
			t.Fatalf("Add error = %v, want success after the shorter search timeout", result.err)
		}
		if len(result.result.Results) != 1 || result.result.Results[0].ID != "m1" {
			t.Fatalf("Add result = %+v, want memory m1", result.result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Add did not finish after the server was released")
	}
}

func TestAddUsesConfiguredWriteTimeout(t *testing.T) {
	server, _ := delayedMemoryServer(t, 500*time.Millisecond)
	client, err := mem0.New(conf.Mem0{
		Endpoint:       server.URL,
		APIKey:         "test-key",
		RequestTimeout: conf.Duration(2 * time.Second),
		WriteTimeout:   conf.Duration(50 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	_, err = client.Add(t.Context(), mem0.AddRequest{
		Messages: []mem0.Message{{Role: "user", Content: "test memory"}},
		AgentID:  "test-agent",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Add error = %v, want write deadline exceeded before the server responds", err)
	}
}

func TestAddHonorsCallerContext(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		wantErr error
	}{
		{"canceled", 0, context.Canceled},
		{"earlier deadline", 100 * time.Millisecond, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, started := delayedMemoryServer(t, 2*time.Second)
			client, err := mem0.New(conf.Mem0{
				Endpoint:       server.URL,
				APIKey:         "test-key",
				RequestTimeout: conf.Duration(3 * time.Second),
				WriteTimeout:   conf.Duration(3 * time.Second),
			})
			if err != nil {
				t.Fatalf("New(): %v", err)
			}
			var ctx context.Context
			var cancel context.CancelFunc
			if tc.timeout > 0 {
				ctx, cancel = context.WithTimeout(t.Context(), tc.timeout)
			} else {
				ctx, cancel = context.WithCancel(t.Context())
			}
			defer cancel()

			finished := make(chan error, 1)
			go func() {
				_, err := client.Add(ctx, mem0.AddRequest{
					Messages: []mem0.Message{{Role: "user", Content: "test memory"}},
					AgentID:  "test-agent",
				})
				finished <- err
			}()
			select {
			case <-started:
			case err := <-finished:
				t.Fatalf("Add returned before reaching the server: %v", err)
			case <-time.After(time.Second):
				t.Fatal("Add did not reach the server")
			}
			if tc.timeout == 0 {
				cancel()
			}
			select {
			case err := <-finished:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Add error = %v, want %v", err, tc.wantErr)
				}
			case <-time.After(time.Second):
				t.Fatal("Add ignored the caller context and kept waiting")
			}
		})
	}
}

func delayedMemoryServer(t *testing.T, delay time.Duration) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		close(started)
		select {
		case <-time.After(delay):
			_, _ = io.WriteString(w, `{"results":[]}`)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	return server, started
}
