package bot

import (
	"strings"
	"testing"

	"go.orx.me/xbot/internal/conf"
)

func TestResolveBotConfigsMemoryChats(t *testing.T) {
	configs, err := resolveBotConfigs(&conf.Config{
		Mem0: conf.Mem0{Endpoint: "https://mem0.example.com", APIKey: "k"},
		Bots: []conf.Bot{
			{
				Name: "assistant", Token: "t1", Enabled: true, Features: []string{"assistant"},
				Memory: conf.BotMem0Config{ChatIDs: []int64{-1001, -1002}},
			},
		},
	})
	if err != nil {
		t.Fatalf("resolveBotConfigs() error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	got := configs[0].MemoryChats
	if len(got) != 2 || got[0] != -1001 || got[1] != -1002 {
		t.Fatalf("MemoryChats = %v, want [-1001 -1002]", got)
	}
}

func TestResolveBotConfigsMemoryRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		config      *conf.Config
		wantErrPart string
	}{
		{
			name: "duplicate chat in one bot",
			config: &conf.Config{
				Mem0: conf.Mem0{Endpoint: "https://m.example.com", APIKey: "k"},
				Bots: []conf.Bot{{
					Name: "a", Token: "t", Enabled: true, Features: []string{"assistant"},
					Memory: conf.BotMem0Config{ChatIDs: []int64{-1001, -1001}},
				}},
			},
			wantErrPart: "more than once",
		},
		{
			name: "duplicate chat across bots",
			config: &conf.Config{
				Mem0: conf.Mem0{Endpoint: "https://m.example.com", APIKey: "k"},
				Bots: []conf.Bot{
					{Name: "a", Token: "t1", Enabled: true, Features: []string{"assistant"}, Memory: conf.BotMem0Config{ChatIDs: []int64{-1001}}},
					{Name: "b", Token: "t2", Enabled: true, Features: []string{"assistant"}, Memory: conf.BotMem0Config{ChatIDs: []int64{-1001}}},
				},
			},
			wantErrPart: "assigned to both",
		},
		{
			name: "memory without endpoint",
			config: &conf.Config{
				Mem0: conf.Mem0{APIKey: "k"},
				Bots: []conf.Bot{{
					Name: "a", Token: "t", Enabled: true, Features: []string{"assistant"},
					Memory: conf.BotMem0Config{ChatIDs: []int64{-1001}},
				}},
			},
			wantErrPart: "mem0.endpoint is required",
		},
		{
			name: "memory without api key",
			config: &conf.Config{
				Mem0: conf.Mem0{Endpoint: "https://m.example.com"},
				Bots: []conf.Bot{{
					Name: "a", Token: "t", Enabled: true, Features: []string{"assistant"},
					Memory: conf.BotMem0Config{ChatIDs: []int64{-1001}},
				}},
			},
			wantErrPart: "mem0.apiKey is required",
		},
		{
			name: "memory without assistant feature",
			config: &conf.Config{
				Mem0: conf.Mem0{Endpoint: "https://m.example.com", APIKey: "k"},
				Bots: []conf.Bot{{
					Name: "a", Token: "t", Enabled: true, Features: []string{"utility"},
					Memory: conf.BotMem0Config{ChatIDs: []int64{-1001}},
				}},
			},
			wantErrPart: "not the assistant feature",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveBotConfigs(test.config)
			if err == nil {
				t.Fatal("resolveBotConfigs() error = nil")
			}
			if !strings.Contains(err.Error(), test.wantErrPart) {
				t.Fatalf("error = %q, want it to contain %q", err, test.wantErrPart)
			}
		})
	}
}

func TestResolveBotConfigsMemoryDisabledRequiresNoCredentials(t *testing.T) {
	// Bots without memory chats must not require Mem0 credentials.
	configs, err := resolveBotConfigs(&conf.Config{
		Bots: []conf.Bot{
			{Name: "a", Token: "t1", Enabled: true, Features: []string{"assistant"}},
		},
	})
	if err != nil {
		t.Fatalf("resolveBotConfigs() error = %v", err)
	}
	if len(configs) != 1 || len(configs[0].MemoryChats) != 0 {
		t.Fatalf("unexpected configs: %+v", configs)
	}
}
