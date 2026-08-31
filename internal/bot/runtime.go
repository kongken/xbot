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
	"go.orx.me/xbot/internal/dao"
	"go.orx.me/xbot/internal/pkg/mem0"
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
		client, err := newBotClient(config)
		if err != nil {
			return err
		}
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

// newBotClient builds a Telegram client for one bot, wiring up Mem0 ingestion
// when the bot is configured with memory Chats IDs.
func newBotClient(config botConfig) (*telegram.Bot, error) {
	var opts []telegram.Option
	var memory *memoryService

	if len(config.MemoryChats) > 0 {
		outbox := dao.GetMem0Outbox()
		if outbox == nil {
			return nil, fmt.Errorf("bot %q enables memory but the Mem0 outbox is unavailable", config.Name)
		}
		client, err := mem0.New(conf.Conf.Mem0.Effective())
		if err != nil {
			return nil, fmt.Errorf("bot %q: %w", config.Name, err)
		}
		memory = newMemoryService(config.Name, config.MemoryChats, outbox, client, conf.Conf.Mem0)
		opts = append(opts, telegram.WithMiddlewares(memory.middleware()))
	}

	opts = append(opts, telegram.WithDefaultHandler(newDefaultHandler(config.Features)))
	client, err := telegram.New(config.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("create bot %q: %w", config.Name, err)
	}

	registerFeatureHandlers(client, config.Features, memory)

	if memory != nil {
		go memory.runWorker(context.Background())
	}
	return client, nil
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
