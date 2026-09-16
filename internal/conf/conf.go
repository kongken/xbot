package conf

import (
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration so YAML can express values as "30s" / "1m".
type Duration time.Duration

// Mem0 defaults applied by Effective when a value is unset.
const (
	defaultBatchSize      = 20
	defaultFlushInterval  = 30 * time.Second
	defaultMaxBatchBytes  = 32 * 1024
	defaultRequestTimeout = 10 * time.Second
	defaultWriteTimeout   = time.Minute
	defaultMaxAttempts    = 8
	defaultTopK           = 10
)

// UnmarshalYAML parses a duration string such as "30s".
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

// TimeDuration returns the wrapped time.Duration value.
func (d Duration) TimeDuration() time.Duration { return time.Duration(d) }

type OpenAI struct {
	Endpoint string   `yaml:"endpoint"`
	Key      string   `yaml:"key"`
	Model    string   `yaml:"model"`
	Keys     []string `yaml:"keys"`
}

type Config struct {
	TelegramBotToken string `yaml:"telegramBotToken"`

	ChatEndpoint string `yaml:"chatEndpoint"`

	SummaryModels []string `yaml:"summaryModels"`
	Host          string   `yaml:"host"`
	DBName        string   `yaml:"dbName"`
	OpenAI        OpenAI   `yaml:"openAI"`
	PictureVendor OpenAI   `yaml:"pictureVendor"`
	Bots          []Bot    `yaml:"bots"`
	S3            S3Config `yaml:"s3Config"`
	Mem0          Mem0     `yaml:"mem0"`

	MessageStorage string `yaml:"messageStorage"`
}

// Bot configures one Telegram identity and its enabled features.
type Bot struct {
	Name     string        `yaml:"name"`
	Token    string        `yaml:"token"`
	Enabled  bool          `yaml:"enabled"`
	Features []string      `yaml:"features"`
	Memory   BotMem0Config `yaml:"memory"`
}

// BotMem0Config configures group-memory ingestion for one bot.
type BotMem0Config struct {
	// ChatIDs lists the Telegram group/supergroup Chat IDs to ingest.
	ChatIDs []int64 `yaml:"chatIDs"`
}

// Mem0 configures the Mem0 REST API connection and batching behavior.
type Mem0 struct {
	Endpoint       string   `yaml:"endpoint"`
	APIKey         string   `yaml:"apiKey"`
	Infer          *bool    `yaml:"infer"`
	BatchSize      int      `yaml:"batchSize"`
	FlushInterval  Duration `yaml:"flushInterval"`
	MaxBatchBytes  int      `yaml:"maxBatchBytes"`
	RequestTimeout Duration `yaml:"requestTimeout"` // Search timeout; writes use WriteTimeout.
	WriteTimeout   Duration `yaml:"writeTimeout"`
	MaxAttempts    int      `yaml:"maxAttempts"`
	TopK           int      `yaml:"topK"`
	// UserProfilesEnabled writes a per-sender projection used by /memory profile.
	UserProfilesEnabled *bool `yaml:"userProfilesEnabled"`
}

// Effective returns a normalized copy of Mem0 with bounded defaults applied.
func (m Mem0) Effective() Mem0 {
	if m.BatchSize <= 0 {
		m.BatchSize = defaultBatchSize
	}
	if m.FlushInterval.TimeDuration() <= 0 {
		m.FlushInterval = Duration(defaultFlushInterval)
	}
	if m.MaxBatchBytes <= 0 {
		m.MaxBatchBytes = defaultMaxBatchBytes
	}
	if m.RequestTimeout.TimeDuration() <= 0 {
		m.RequestTimeout = Duration(defaultRequestTimeout)
	}
	if m.WriteTimeout.TimeDuration() <= 0 {
		m.WriteTimeout = Duration(defaultWriteTimeout)
	}
	if m.MaxAttempts <= 0 {
		m.MaxAttempts = defaultMaxAttempts
	}
	if m.TopK <= 0 {
		m.TopK = defaultTopK
	}
	// Infer defaults to true when omitted.
	if m.Infer == nil {
		t := true
		m.Infer = &t
	}
	// User profiles default to enabled (backs /memory profile).
	if m.UserProfilesEnabled == nil {
		t := true
		m.UserProfilesEnabled = &t
	}
	return m
}

// InferEnabled reports whether fact extraction is enabled.
func (m Mem0) InferEnabled() bool {
	return m.Infer == nil || *m.Infer
}

// UserProfiles reports whether the per-sender projection is enabled.
func (m Mem0) UserProfiles() bool {
	return m.UserProfilesEnabled == nil || *m.UserProfilesEnabled
}

type S3Config struct {
	Endpoint  string `yaml:"endpoint"`
	AccessKey string `yaml:"accessKey"`
	SecretKey string `yaml:"secretKey"`
	Bucket    string `yaml:"bucket"`
}

var (
	Conf = new(Config)
)

func (c *Config) Print() {}
