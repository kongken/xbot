package bot

import (
	"fmt"
	"regexp"
	"strings"

	"go.orx.me/xbot/internal/conf"
)

const (
	featureAssistant feature = "assistant"
	featurePoll      feature = "poll"
	featureUtility   feature = "utility"
	featureAll       feature = "all"

	legacyWebhookPath = "/v1/webhook"
	namedWebhookPath  = "/v1/webhooks/"
)

var (
	botNamePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	supportedFeatures = []feature{featureAssistant, featurePoll, featureUtility}
)

type feature string

type featureSet map[feature]struct{}

func (s featureSet) has(value feature) bool {
	_, ok := s[value]
	return ok
}

func (s featureSet) names() []string {
	names := make([]string, 0, len(s))
	for _, value := range supportedFeatures {
		if s.has(value) {
			names = append(names, string(value))
		}
	}
	return names
}

type botConfig struct {
	Name        string
	Token       string
	WebhookPath string
	Features    featureSet
	Legacy      bool
	// MemoryChats lists the group/supergroup Chat IDs to ingest into Mem0.
	MemoryChats []int64
}

func resolveBotConfigs(config *conf.Config) ([]botConfig, error) {
	if len(config.Bots) == 0 {
		token := strings.TrimSpace(config.TelegramBotToken)
		if token == "" {
			return nil, fmt.Errorf("telegramBotToken is required when bots is empty")
		}

		return []botConfig{
			{
				Name:        "default",
				Token:       token,
				WebhookPath: legacyWebhookPath,
				Features:    allFeatures(),
				Legacy:      true,
			},
		}, nil
	}

	configs := make([]botConfig, 0, len(config.Bots))
	names := make(map[string]struct{}, len(config.Bots))
	tokens := make(map[string]struct{}, len(config.Bots))
	memoryOwners := make(map[int64]string)

	for index, configuredBot := range config.Bots {
		if !configuredBot.Enabled {
			continue
		}

		name := strings.TrimSpace(configuredBot.Name)
		if !botNamePattern.MatchString(name) {
			return nil, fmt.Errorf(
				"bots[%d].name must be 1-64 letters, numbers, underscores, or hyphens and start with a letter or number",
				index,
			)
		}
		if _, exists := names[name]; exists {
			return nil, fmt.Errorf("duplicate enabled bot name %q", name)
		}

		token := strings.TrimSpace(configuredBot.Token)
		if token == "" {
			return nil, fmt.Errorf("bot %q has an empty token", name)
		}
		if _, exists := tokens[token]; exists {
			return nil, fmt.Errorf("bot %q reuses another enabled bot token", name)
		}

		features, err := parseFeatures(configuredBot.Features)
		if err != nil {
			return nil, fmt.Errorf("bot %q: %w", name, err)
		}

		memoryChats, err := validateMemoryChats(name, configuredBot.Memory.ChatIDs, memoryOwners, features)
		if err != nil {
			return nil, err
		}

		names[name] = struct{}{}
		tokens[token] = struct{}{}
		configs = append(configs, botConfig{
			Name:        name,
			Token:       token,
			WebhookPath: namedWebhookPath + name,
			Features:    features,
			MemoryChats: memoryChats,
		})
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("bots is configured but contains no enabled bots")
	}

	if hasMemoryBots(configs) {
		mem0 := config.Mem0
		if strings.TrimSpace(mem0.Endpoint) == "" {
			return nil, fmt.Errorf("mem0.endpoint is required when a bot enables memory.chatIDs")
		}
		if !strings.HasPrefix(mem0.Endpoint, "http://") && !strings.HasPrefix(mem0.Endpoint, "https://") {
			return nil, fmt.Errorf("mem0.endpoint must be an http(s) URL, got %q", mem0.Endpoint)
		}
		if strings.TrimSpace(mem0.APIKey) == "" {
			return nil, fmt.Errorf("mem0.apiKey is required when a bot enables memory.chatIDs")
		}
	}

	return configs, nil
}

// validateMemoryChats checks per-bot Chat ID uniqueness and cross-bot ownership.
func validateMemoryChats(
	botName string,
	chatIDs []int64,
	owners map[int64]string,
	features featureSet,
) ([]int64, error) {
	seen := make(map[int64]struct{}, len(chatIDs))
	validated := make([]int64, 0, len(chatIDs))
	for _, chatID := range chatIDs {
		if _, dup := seen[chatID]; dup {
			return nil, fmt.Errorf("bot %q lists chatID %d more than once", botName, chatID)
		}
		if owner, exists := owners[chatID]; exists {
			return nil, fmt.Errorf("chatID %d is assigned to both %q and %q", chatID, owner, botName)
		}
		seen[chatID] = struct{}{}
		owners[chatID] = botName
		validated = append(validated, chatID)
	}

	if len(validated) > 0 && !features.has(featureAssistant) {
		return nil, fmt.Errorf("bot %q enables memory.chatIDs but not the assistant feature", botName)
	}
	return validated, nil
}

func hasMemoryBots(configs []botConfig) bool {
	for _, config := range configs {
		if len(config.MemoryChats) > 0 {
			return true
		}
	}
	return false
}

func parseFeatures(values []string) (featureSet, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("features must not be empty (supported: %s)", supportedFeatureNames())
	}

	features := make(featureSet, len(values))
	hasAll := false
	for _, rawValue := range values {
		value := feature(strings.ToLower(strings.TrimSpace(rawValue)))
		switch value {
		case featureAll:
			hasAll = true
		case featureAssistant, featurePoll, featureUtility:
			features[value] = struct{}{}
		default:
			return nil, fmt.Errorf("unknown feature %q (supported: %s)", rawValue, supportedFeatureNames())
		}
	}

	if hasAll {
		return allFeatures(), nil
	}
	return features, nil
}

func allFeatures() featureSet {
	features := make(featureSet, len(supportedFeatures))
	for _, value := range supportedFeatures {
		features[value] = struct{}{}
	}
	return features
}

func supportedFeatureNames() string {
	names := make([]string, 0, len(supportedFeatures)+1)
	for _, value := range supportedFeatures {
		names = append(names, string(value))
	}
	names = append(names, string(featureAll))
	return strings.Join(names, ", ")
}
