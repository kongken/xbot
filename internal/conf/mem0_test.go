package conf

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestMem0YAMLConfiguration(t *testing.T) {
	data := []byte(`
mem0:
  endpoint: https://mem0.example.com
  apiKey: secret-key
  infer: false
  batchSize: 50
  flushInterval: 1m30s
  maxBatchBytes: 4096
  requestTimeout: 5s
  maxAttempts: 3
  topK: 7
  userProfilesEnabled: false
bots:
  - name: assistant
    token: token-1
    enabled: true
    features: [assistant]
    memory:
      chatIDs:
        - -100123
        - -100456
`)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	mem0 := cfg.Mem0
	if mem0.Endpoint != "https://mem0.example.com" || mem0.APIKey != "secret-key" {
		t.Fatalf("unexpected mem0 connection config: %+v", mem0)
	}
	if mem0.Infer == nil || *mem0.Infer {
		t.Fatalf("infer = %v, want false", mem0.Infer)
	}
	if mem0.BatchSize != 50 {
		t.Fatalf("batchSize = %d, want 50", mem0.BatchSize)
	}
	if mem0.FlushInterval.TimeDuration().String() != "1m30s" {
		t.Fatalf("flushInterval = %s, want 1m30s", mem0.FlushInterval.TimeDuration())
	}
	if mem0.RequestTimeout.TimeDuration() != 5_000_000_000 {
		t.Fatalf("requestTimeout = %v, want 5s", mem0.RequestTimeout.TimeDuration())
	}
	if mem0.UserProfilesEnabled == nil || *mem0.UserProfilesEnabled {
		t.Fatalf("userProfilesEnabled = %v, want false", mem0.UserProfilesEnabled)
	}
	if mem0.MaxBatchBytes != 4096 || mem0.MaxAttempts != 3 || mem0.TopK != 7 {
		t.Fatalf("unexpected numeric config: %+v", mem0)
	}

	if len(cfg.Bots) != 1 {
		t.Fatalf("len(bots) = %d, want 1", len(cfg.Bots))
	}
	got := cfg.Bots[0].Memory.ChatIDs
	if len(got) != 2 || got[0] != -100123 || got[1] != -100456 {
		t.Fatalf("memory.chatIDs = %v, want [-100123 -100456]", got)
	}
}

func TestMem0EffectiveDefaults(t *testing.T) {
	effective := (Mem0{}).Effective()
	if effective.BatchSize != 20 {
		t.Fatalf("default batchSize = %d, want 20", effective.BatchSize)
	}
	if effective.FlushInterval.TimeDuration() != 30_000_000_000 {
		t.Fatalf("default flushInterval = %v, want 30s", effective.FlushInterval.TimeDuration())
	}
	if effective.MaxBatchBytes != 32768 {
		t.Fatalf("default maxBatchBytes = %d, want 32768", effective.MaxBatchBytes)
	}
	if effective.RequestTimeout.TimeDuration() != 10_000_000_000 {
		t.Fatalf("default requestTimeout = %v, want 10s", effective.RequestTimeout.TimeDuration())
	}
	if effective.MaxAttempts != 8 {
		t.Fatalf("default maxAttempts = %d, want 8", effective.MaxAttempts)
	}
	if effective.TopK != 10 {
		t.Fatalf("default topK = %d, want 10", effective.TopK)
	}
	if !effective.InferEnabled() {
		t.Fatal("infer should default to enabled")
	}
	if !effective.UserProfiles() {
		t.Fatal("user profiles should default to enabled")
	}
}

func TestMem0EffectiveKeepsExplicitValues(t *testing.T) {
	falseValue := false
	cfg := Mem0{
		Infer:               &falseValue,
		UserProfilesEnabled: &falseValue,
		BatchSize:           3,
		TopK:                2,
	}
	effective := cfg.Effective()
	if effective.InferEnabled() {
		t.Fatal("infer should stay disabled")
	}
	if effective.UserProfiles() {
		t.Fatal("user profiles should stay disabled")
	}
	if effective.BatchSize != 3 || effective.TopK != 2 {
		t.Fatalf("unexpected preserved values: %+v", effective)
	}
}

func TestMem0TimeoutDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name       string
		yaml       string
		wantSearch time.Duration
		wantWrite  time.Duration
	}{
		{"omitted", "{}", 10 * time.Second, 60 * time.Second},
		{"existing request timeout", "requestTimeout: 3s", 3 * time.Second, 60 * time.Second},
		{"write timeout only", "writeTimeout: 90s", 10 * time.Second, 90 * time.Second},
		{"both explicit", "requestTimeout: 7s\nwriteTimeout: 2m", 7 * time.Second, 2 * time.Minute},
		{"nonpositive", "requestTimeout: 0s\nwriteTimeout: -1s", 10 * time.Second, 60 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Mem0
			if err := yaml.Unmarshal([]byte(tc.yaml), &cfg); err != nil {
				t.Fatalf("decode config: %v", err)
			}
			effective := cfg.Effective()
			if got := effective.RequestTimeout.TimeDuration(); got != tc.wantSearch {
				t.Errorf("requestTimeout = %v, want %v", got, tc.wantSearch)
			}
			if got := effective.WriteTimeout.TimeDuration(); got != tc.wantWrite {
				t.Errorf("writeTimeout = %v, want %v", got, tc.wantWrite)
			}
		})
	}
}
