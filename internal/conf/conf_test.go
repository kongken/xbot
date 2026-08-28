package conf

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBotYAMLConfiguration(t *testing.T) {
	data := []byte(`bots:
  - name: assistant
    token: token-1
    enabled: true
    features:
      - assistant
      - utility
`)

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if len(config.Bots) != 1 {
		t.Fatalf("len(config.Bots) = %d, want 1", len(config.Bots))
	}

	configuredBot := config.Bots[0]
	if configuredBot.Name != "assistant" || configuredBot.Token != "token-1" || !configuredBot.Enabled {
		t.Fatalf("unexpected bot config: %+v", configuredBot)
	}
	if len(configuredBot.Features) != 2 || configuredBot.Features[0] != "assistant" || configuredBot.Features[1] != "utility" {
		t.Fatalf("features = %v, want [assistant utility]", configuredBot.Features)
	}
}
