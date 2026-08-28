package bot

import (
	"testing"

	telegram "github.com/go-telegram/bot"
)

func TestBotRegistrySeparatesLegacyAndNamedBots(t *testing.T) {
	legacyClient, err := telegram.New("1:legacy", telegram.WithSkipGetMe())
	if err != nil {
		t.Fatalf("telegram.New() legacy error = %v", err)
	}
	namedClient, err := telegram.New("2:named", telegram.WithSkipGetMe())
	if err != nil {
		t.Fatalf("telegram.New() named error = %v", err)
	}

	registry := &botRegistry{}
	registry.replace([]runningBot{
		{config: botConfig{Name: "default", Legacy: true}, client: legacyClient},
		{config: botConfig{Name: "assistant"}, client: namedClient},
	})

	actualLegacy, ok := registry.get("")
	if !ok || actualLegacy != legacyClient {
		t.Fatalf("registry.get(legacy) = (%p, %t), want (%p, true)", actualLegacy, ok, legacyClient)
	}
	actualNamed, ok := registry.get("assistant")
	if !ok || actualNamed != namedClient {
		t.Fatalf("registry.get(assistant) = (%p, %t), want (%p, true)", actualNamed, ok, namedClient)
	}
	if _, ok := registry.get("missing"); ok {
		t.Fatal("registry.get(missing) found an unregistered bot")
	}
}
