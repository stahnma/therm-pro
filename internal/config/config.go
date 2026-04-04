package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
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
	Port            int         `koanf:"port"`
	RegistrationPIN string      `koanf:"registration_pin"`
	Slack           SlackConfig `koanf:"slack"`
	DataDir         string      `koanf:"data_dir"`
	WebAuthnOrigin  string      `koanf:"webauthn_origin"`
	LogLevel        string      `koanf:"log_level"`
}

// Load reads config from defaults, then config.yaml in dataDir (if present),
// then .env in dataDir (if present), then environment variables.
// Pass empty dataDir to use ~/.therm-pro.
func Load(dataDir string) (*Config, error) {
	k := koanf.New(".")

	// 1. Defaults
	k.Load(confmap.Provider(map[string]interface{}{
		"port":            8088,
		"webauthn_origin": "http://localhost:8088",
		"log_level":       "info",
	}, "."), nil)

	// Resolve data dir: explicit arg > THERM_PRO_DATA_DIR > ~/.therm-pro
	if dataDir == "" {
		dataDir = os.Getenv("THERM_PRO_DATA_DIR")
	}
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".therm-pro")
	}

	// 2. YAML config file (optional)
	yamlPath := filepath.Join(dataDir, "config.yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		k.Load(file.Provider(yamlPath), yaml.Parser())
	}

	// 3. .env file (optional) — loads THERM_PRO_* vars into the process environment
	envPath := filepath.Join(dataDir, ".env")
	if _, err := os.Stat(envPath); err == nil {
		godotenv.Load(envPath)
	}

	// 4. Environment variables (THERM_PRO_ prefix)
	k.Load(env.Provider("THERM_PRO_", ".", func(s string) string {
		key := s[len("THERM_PRO_"):]
		switch key {
		case "SLACK_WEBHOOK":
			return "slack.webhook"
		case "SLACK_SIGNING_SECRET":
			return "slack.signing_secret"
		case "SLACK_BOT_TOKEN":
			return "slack.bot_token"
		case "WEBAUTHN_ORIGIN":
			return "webauthn_origin"
		default:
			return strings.ToLower(key)
		}
	}), nil)

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, err
	}
	cfg.DataDir = dataDir

	// 5. Legacy PORT env var (backwards compat)
	if p := os.Getenv("PORT"); p != "" {
		if pn, err := strconv.Atoi(p); err == nil {
			cfg.Port = pn
		}
	}

	return &cfg, nil
}
