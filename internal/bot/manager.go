package bot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"go.orx.me/xbot/internal/conf"
	"go.orx.me/xbot/internal/dao"
)

const (
	// DefaultBotName is the bot created from conf.TelegramBotToken. It keeps
	// serving the legacy /v1/webhook path.
	DefaultBotName = "default"

	SourceConfig = "config"
	SourceAPI    = "api"
)

var (
	ErrInvalidName  = errors.New("invalid bot name, allowed: [a-zA-Z0-9_-]{1,32}")
	ErrInvalidToken = errors.New("token is required")
	ErrStartFailed  = errors.New("failed to start bot")
	ErrReadOnly     = errors.New("bot is defined in config file and cannot be modified via api")

	nameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)
)

type instance struct {
	name   string
	token  string
	source string
	bot    *bot.Bot
	cancel context.CancelFunc
}

// Manager owns the running telegram bots and keeps them in sync with the
// config file and the stored (api managed) bot configs.
type Manager struct {
	// opMu serializes mutations (start/stop/create/update/delete).
	opMu sync.Mutex

	mu   sync.RWMutex
	bots map[string]*instance
}

var manager = &Manager{bots: map[string]*instance{}}

// GetManager returns the global bot manager.
func GetManager() *Manager { return manager }

// Init starts the default bot, config file bots and api managed bots.
func Init() error {
	return manager.Init(context.Background())
}

func (m *Manager) Init(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if conf.Conf.TelegramBotToken != "" {
		// no webhook secret for the default bot to stay compatible with
		// the already registered webhook.
		inst, err := m.start(ctx, DefaultBotName, conf.Conf.TelegramBotToken, "", SourceConfig)
		if err != nil {
			return err
		}
		m.put(inst)
	}

	for _, c := range conf.Conf.Bots {
		if !c.Enable || c.Name == "" || c.Name == DefaultBotName {
			continue
		}
		inst, err := m.start(ctx, c.Name, c.Token, derivedSecret(c.Token), SourceConfig)
		if err != nil {
			slog.Error("start config bot failed", "name", c.Name, "error", err)
			continue
		}
		m.put(inst)
	}

	stored, err := dao.ListBots(ctx)
	if err != nil {
		slog.Error("load stored bots failed", "error", err)
		return nil
	}
	for _, c := range stored {
		if !c.Enable || m.has(c.Name) {
			continue
		}
		inst, err := m.start(ctx, c.Name, c.Token, c.WebhookSecret, SourceAPI)
		if err != nil {
			slog.Error("start stored bot failed", "name", c.Name, "error", err)
			continue
		}
		m.put(inst)
	}
	return nil
}

// Handler returns the webhook handler of a running bot.
func (m *Manager) Handler(name string) (http.Handler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.bots[name]
	if !ok {
		return nil, false
	}
	return inst.bot.WebhookHandler(), true
}

// BotInfo is the public view of a bot. The token is never exposed in full.
type BotInfo struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	Enable    bool   `json:"enable"`
	Running   bool   `json:"running"`
	Token     string `json:"token"`
	Webhook   string `json:"webhook"`
	CreatedAt int64  `json:"created_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

// List returns config file bots followed by api managed bots.
func (m *Manager) List(ctx context.Context) ([]*BotInfo, error) {
	stored, err := dao.ListBots(ctx)
	if err != nil {
		return nil, err
	}

	var infos []*BotInfo
	seen := map[string]bool{}
	if conf.Conf.TelegramBotToken != "" {
		seen[DefaultBotName] = true
		infos = append(infos, m.configInfo(DefaultBotName, conf.Conf.TelegramBotToken, true))
	}
	for _, c := range conf.Conf.Bots {
		if c.Name == "" || seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		infos = append(infos, m.configInfo(c.Name, c.Token, c.Enable))
	}
	sort.SliceStable(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })

	for _, c := range stored {
		if seen[c.Name] {
			continue
		}
		infos = append(infos, m.storedInfo(c))
	}
	return infos, nil
}

// Get returns a single bot.
func (m *Manager) Get(ctx context.Context, name string) (*BotInfo, error) {
	if c, ok := configBot(name); ok {
		return m.configInfo(c.Name, c.Token, c.Enable), nil
	}
	c, err := dao.GetBotConfig(ctx, name)
	if err != nil {
		return nil, err
	}
	return m.storedInfo(c), nil
}

// Create validates the token with telegram, registers the webhook and stores
// the bot.
func (m *Manager) Create(ctx context.Context, name, token string, enable bool) (*BotInfo, error) {
	if !nameRegexp.MatchString(name) {
		return nil, ErrInvalidName
	}
	if strings.TrimSpace(token) == "" {
		return nil, ErrInvalidToken
	}
	if _, ok := configBot(name); ok {
		return nil, ErrReadOnly
	}

	m.opMu.Lock()
	defer m.opMu.Unlock()

	if _, err := dao.GetBotConfig(ctx, name); err == nil {
		return nil, dao.ErrBotExists
	} else if !errors.Is(err, dao.ErrBotNotFound) {
		return nil, err
	}

	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	cfg := &dao.BotConfig{Name: name, Token: token, WebhookSecret: secret, Enable: enable}

	var inst *instance
	if enable {
		if inst, err = m.start(ctx, name, token, secret, SourceAPI); err != nil {
			return nil, err
		}
	}
	if err := dao.CreateBotConfig(ctx, cfg); err != nil {
		if inst != nil {
			inst.stop(false)
		}
		return nil, err
	}
	if inst != nil {
		m.put(inst)
	}
	return m.storedInfo(cfg), nil
}

// Update changes the token and/or enable flag of an api managed bot.
func (m *Manager) Update(ctx context.Context, name string, token *string, enable *bool) (*BotInfo, error) {
	if _, ok := configBot(name); ok {
		return nil, ErrReadOnly
	}
	if token != nil && strings.TrimSpace(*token) == "" {
		return nil, ErrInvalidToken
	}

	m.opMu.Lock()
	defer m.opMu.Unlock()

	cfg, err := dao.GetBotConfig(ctx, name)
	if err != nil {
		return nil, err
	}
	oldToken, oldEnable := cfg.Token, cfg.Enable
	if token != nil {
		cfg.Token = *token
	}
	if enable != nil {
		cfg.Enable = *enable
	}

	changed := cfg.Token != oldToken || cfg.Enable != oldEnable
	running := m.has(name)

	var newInst *instance
	if changed || (cfg.Enable && !running) {
		if cfg.Enable {
			if newInst, err = m.start(ctx, name, cfg.Token, cfg.WebhookSecret, SourceAPI); err != nil {
				return nil, err
			}
		}
	}

	if err := dao.UpdateBotConfig(ctx, cfg); err != nil {
		if newInst != nil {
			newInst.stop(false)
		}
		return nil, err
	}

	if changed || (cfg.Enable && !running) {
		old := m.take(name)
		if newInst != nil {
			m.put(newInst)
		}
		if old != nil {
			// only remove the webhook when it is not re-registered for the
			// same bot by the new instance.
			old.stop(newInst == nil || old.token != newInst.token)
		}
	}
	return m.storedInfo(cfg), nil
}

// Delete stops the bot, removes its webhook and deletes the stored config.
func (m *Manager) Delete(ctx context.Context, name string) error {
	if _, ok := configBot(name); ok {
		return ErrReadOnly
	}

	m.opMu.Lock()
	defer m.opMu.Unlock()

	if err := dao.DeleteBotConfig(ctx, name); err != nil {
		return err
	}
	if old := m.take(name); old != nil {
		old.stop(true)
	}
	return nil
}

// start creates a bot, registers handlers and the webhook, and starts
// processing updates. It does not add the instance to the manager.
func (m *Manager) start(ctx context.Context, name, token, secret, source string) (*instance, error) {
	opts := []bot.Option{bot.WithDefaultHandler(defaultHandler)}
	if secret != "" {
		opts = append(opts, bot.WithWebhookSecretToken(secret))
	}
	b, err := bot.New(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: create bot %q: %v", ErrStartFailed, name, err)
	}
	registerHandlers(b)

	resp, err := b.SetWebhook(ctx, &bot.SetWebhookParams{
		URL:         webhookURL(name),
		SecretToken: secret,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: set webhook for bot %q: %v", ErrStartFailed, name, err)
	}
	slog.Info("set webhook success", "name", name, "resp", resp)

	runCtx, cancel := context.WithCancel(context.Background())
	go b.StartWebhook(runCtx)
	return &instance{name: name, token: token, source: source, bot: b, cancel: cancel}, nil
}

func (i *instance) stop(deleteWebhook bool) {
	i.cancel()
	if !deleteWebhook {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := i.bot.DeleteWebhook(ctx, &bot.DeleteWebhookParams{}); err != nil {
		slog.Error("delete webhook failed", "name", i.name, "error", err)
	}
}

func (m *Manager) put(i *instance) {
	m.mu.Lock()
	m.bots[i.name] = i
	m.mu.Unlock()
}

func (m *Manager) take(name string) *instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.bots[name]
	delete(m.bots, name)
	return i
}

func (m *Manager) has(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.bots[name]
	return ok
}

func (m *Manager) configInfo(name, token string, enable bool) *BotInfo {
	return &BotInfo{
		Name:    name,
		Source:  SourceConfig,
		Enable:  enable,
		Running: m.has(name),
		Token:   maskToken(token),
		Webhook: webhookURL(name),
	}
}

func (m *Manager) storedInfo(c *dao.BotConfig) *BotInfo {
	return &BotInfo{
		Name:      c.Name,
		Source:    SourceAPI,
		Enable:    c.Enable,
		Running:   m.has(c.Name),
		Token:     maskToken(c.Token),
		Webhook:   webhookURL(c.Name),
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// configBot looks up a bot defined in the config file (including default).
func configBot(name string) (conf.Bot, bool) {
	if name == DefaultBotName {
		return conf.Bot{Name: DefaultBotName, Token: conf.Conf.TelegramBotToken, Enable: true}, true
	}
	for _, c := range conf.Conf.Bots {
		if c.Name == name {
			return c, true
		}
	}
	return conf.Bot{}, false
}

func webhookURL(name string) string {
	if name == DefaultBotName {
		return fmt.Sprintf("%s/v1/webhook", conf.Conf.Host)
	}
	return fmt.Sprintf("%s/v1/webhook/%s", conf.Conf.Host, name)
}

// maskToken keeps only the bot id part of "<id>:<secret>".
func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if id, _, ok := strings.Cut(token, ":"); ok {
		return id + ":****"
	}
	return "****"
}

func randomSecret() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func derivedSecret(token string) string {
	sum := sha256.Sum256([]byte("xbot-webhook:" + token))
	return hex.EncodeToString(sum[:16])
}
