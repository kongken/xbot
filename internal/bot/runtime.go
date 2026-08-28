package bot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	telegram "github.com/go-telegram/bot"
	"go.orx.me/xbot/internal/conf"
)

type runningBot struct {
	config botConfig
	client *telegram.Bot
}

type botRegistry struct {
	mu         sync.RWMutex
	defaultBot *telegram.Bot
	namedBots  map[string]*telegram.Bot
}

var registry = &botRegistry{}

// Init creates configured bots, registers their features, and starts their webhook workers.
func Init() error {
	configs, err := resolveBotConfigs(conf.Conf)
	if err != nil {
		return fmt.Errorf("resolve bot configuration: %w", err)
	}

	runningBots := make([]runningBot, 0, len(configs))
	for _, config := range configs {
		client, err := telegram.New(
			config.Token,
			telegram.WithDefaultHandler(newDefaultHandler(config.Features)),
		)
		if err != nil {
			return fmt.Errorf("create bot %q: %w", config.Name, err)
		}

		registerFeatureHandlers(client, config.Features)
		runningBots = append(runningBots, runningBot{config: config, client: client})
	}

	for _, running := range runningBots {
		webhookURL := strings.TrimRight(conf.Conf.Host, "/") + running.config.WebhookPath
		response, err := running.client.SetWebhook(context.Background(), &telegram.SetWebhookParams{URL: webhookURL})
		if err != nil {
			slog.Error("set webhook error", "bot", running.config.Name, "error", err)
			continue
		}

		slog.Info(
			"set webhook success",
			"bot", running.config.Name,
			"features", running.config.Features.names(),
			"response", response,
		)
	}

	registry.replace(runningBots)
	for _, running := range runningBots {
		go running.client.StartWebhook(context.Background())
	}

	return nil
}

// WebhookHandler returns the webhook handler for a named bot, or the legacy bot when name is empty.
func WebhookHandler(name string) (http.Handler, bool) {
	client, ok := registry.get(name)
	if !ok {
		return nil, false
	}
	return client.WebhookHandler(), true
}

func (r *botRegistry) replace(runningBots []runningBot) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.defaultBot = nil
	r.namedBots = make(map[string]*telegram.Bot, len(runningBots))
	for _, running := range runningBots {
		if running.config.Legacy {
			r.defaultBot = running.client
			continue
		}
		r.namedBots[running.config.Name] = running.client
	}
}

func (r *botRegistry) get(name string) (*telegram.Bot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name == "" {
		return r.defaultBot, r.defaultBot != nil
	}
	client, ok := r.namedBots[name]
	return client, ok
}
