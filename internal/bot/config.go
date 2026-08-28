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

		names[name] = struct{}{}
		tokens[token] = struct{}{}
		configs = append(configs, botConfig{
			Name:        name,
			Token:       token,
			WebhookPath: namedWebhookPath + name,
			Features:    features,
		})
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("bots is configured but contains no enabled bots")
	}

	return configs, nil
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
