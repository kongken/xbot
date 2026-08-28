package bot

import (
	"strings"
	"testing"

	"go.orx.me/xbot/internal/conf"
)

func TestResolveBotConfigsLegacy(t *testing.T) {
	configs, err := resolveBotConfigs(&conf.Config{TelegramBotToken: " legacy-token "})
	if err != nil {
		t.Fatalf("resolveBotConfigs() error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("resolveBotConfigs() returned %d configs, want 1", len(configs))
	}

	config := configs[0]
	if config.Name != "default" || config.Token != "legacy-token" || config.WebhookPath != legacyWebhookPath {
		t.Fatalf("unexpected legacy config: %+v", config)
	}
	if !config.Legacy {
		t.Fatal("legacy config is not marked as legacy")
	}
	assertFeatures(t, config.Features, featureAssistant, featurePoll, featureUtility)
}

func TestResolveBotConfigsUsesEnabledNamedBots(t *testing.T) {
	configs, err := resolveBotConfigs(&conf.Config{
		TelegramBotToken: "ignored-legacy-token",
		Bots: []conf.Bot{
			{Name: "disabled", Token: "", Enabled: false},
			{Name: "assistant_bot", Token: " token-1 ", Enabled: true, Features: []string{"Assistant", "utility"}},
			{Name: "poll-bot", Token: "token-2", Enabled: true, Features: []string{"all"}},
		},
	})
	if err != nil {
		t.Fatalf("resolveBotConfigs() error = %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("resolveBotConfigs() returned %d configs, want 2", len(configs))
	}

	first := configs[0]
	if first.Name != "assistant_bot" || first.Token != "token-1" || first.WebhookPath != namedWebhookPath+"assistant_bot" {
		t.Fatalf("unexpected first config: %+v", first)
	}
	if first.Legacy {
		t.Fatal("named bot is marked as legacy")
	}
	assertFeatures(t, first.Features, featureAssistant, featureUtility)
	assertFeatures(t, configs[1].Features, featureAssistant, featurePoll, featureUtility)
}

func TestResolveBotConfigsRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		config      *conf.Config
		wantErrPart string
	}{
		{
			name:        "missing legacy token",
			config:      &conf.Config{},
			wantErrPart: "telegramBotToken is required",
		},
		{
			name: "no enabled bots",
			config: &conf.Config{Bots: []conf.Bot{
				{Name: "disabled", Token: "token", Features: []string{"all"}},
			}},
			wantErrPart: "no enabled bots",
		},
		{
			name: "invalid name",
			config: &conf.Config{Bots: []conf.Bot{
				{Name: "invalid/name", Token: "token", Enabled: true, Features: []string{"all"}},
			}},
			wantErrPart: "bots[0].name",
		},
		{
			name: "empty token",
			config: &conf.Config{Bots: []conf.Bot{
				{Name: "valid", Enabled: true, Features: []string{"all"}},
			}},
			wantErrPart: "empty token",
		},
		{
			name: "duplicate name",
			config: &conf.Config{Bots: []conf.Bot{
				{Name: "same", Token: "token-1", Enabled: true, Features: []string{"all"}},
				{Name: "same", Token: "token-2", Enabled: true, Features: []string{"all"}},
			}},
			wantErrPart: "duplicate enabled bot name",
		},
		{
			name: "duplicate token",
			config: &conf.Config{Bots: []conf.Bot{
				{Name: "first", Token: "same-token", Enabled: true, Features: []string{"all"}},
				{Name: "second", Token: "same-token", Enabled: true, Features: []string{"all"}},
			}},
			wantErrPart: "reuses another enabled bot token",
		},
		{
			name: "empty features",
			config: &conf.Config{Bots: []conf.Bot{
				{Name: "valid", Token: "token", Enabled: true},
			}},
			wantErrPart: "features must not be empty",
		},
		{
			name: "unknown feature after all",
			config: &conf.Config{Bots: []conf.Bot{
				{Name: "valid", Token: "token", Enabled: true, Features: []string{"all", "unknown"}},
			}},
			wantErrPart: "unknown feature",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveBotConfigs(test.config)
			if err == nil {
				t.Fatal("resolveBotConfigs() error = nil")
			}
			if !strings.Contains(err.Error(), test.wantErrPart) {
				t.Fatalf("resolveBotConfigs() error = %q, want it to contain %q", err, test.wantErrPart)
			}
		})
	}
}

func assertFeatures(t *testing.T, actual featureSet, expected ...feature) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("features = %v, want %v", actual.names(), expected)
	}
	for _, value := range expected {
		if !actual.has(value) {
			t.Fatalf("features = %v, missing %q", actual.names(), value)
		}
	}
}
