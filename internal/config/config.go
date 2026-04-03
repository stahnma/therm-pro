package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type SlackConfig struct {
	Webhook       string `koanf:"webhook"`
	SigningSecret string `koanf:"signing_secret"`
	BotToken      string `koanf:"bot_token"`
}

type Config struct {
	Port        int         `koanf:"port"`
	AllowedCIDR string      `koanf:"allowed_cidr"`
	TrustProxy  bool        `koanf:"trust_proxy"`
	Slack       SlackConfig `koanf:"slack"`
	DataDir     string      `koanf:"data_dir"`
}

// Load reads config from defaults, then config.yaml in dataDir (if present),
// then environment variables.
// Pass empty dataDir to use ~/.therm-pro.
func Load(dataDir string) (*Config, error) {
	k := koanf.New(".")

	// 1. Defaults
	k.Load(confmap.Provider(map[string]interface{}{
		"port":         8088,
		"allowed_cidr": "192.168.1.0/24",
		"trust_proxy":  false,
	}, "."), nil)

	// Resolve data dir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".therm-pro")
	}

	// 2. YAML config file (optional)
	yamlPath := filepath.Join(dataDir, "config.yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		k.Load(file.Provider(yamlPath), yaml.Parser())
	}

	// 3. Environment variables (THERM_PRO_ prefix)
	k.Load(env.Provider("THERM_PRO_", ".", func(s string) string {
		key := s[len("THERM_PRO_"):]
		switch key {
		case "SLACK_WEBHOOK":
			return "slack.webhook"
		case "SLACK_SIGNING_SECRET":
			return "slack.signing_secret"
		case "SLACK_BOT_TOKEN":
			return "slack.bot_token"
		default:
			return strings.ToLower(key)
		}
	}), nil)

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, err
	}
	cfg.DataDir = dataDir

	// 4. Legacy PORT env var (backwards compat)
	if p := os.Getenv("PORT"); p != "" {
		if pn, err := strconv.Atoi(p); err == nil {
			cfg.Port = pn
		}
	}

	return &cfg, nil
}
