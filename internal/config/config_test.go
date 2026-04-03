package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8088 {
		t.Errorf("expected port 8088, got %d", cfg.Port)
	}
	if cfg.AllowedCIDR != "192.168.1.0/24" {
		t.Errorf("expected default CIDR, got %s", cfg.AllowedCIDR)
	}
	if cfg.TrustProxy {
		t.Error("expected trust_proxy false by default")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected default log_level 'info', got %s", cfg.LogLevel)
	}
}

func TestLogLevelEnvOverride(t *testing.T) {
	t.Setenv("THERM_PRO_LOG_LEVEL", "debug")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected log_level 'debug', got %s", cfg.LogLevel)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("THERM_PRO_PORT", "9090")
	t.Setenv("THERM_PRO_ALLOWED_CIDR", "10.0.0.0/8")
	t.Setenv("THERM_PRO_TRUST_PROXY", "true")
	t.Setenv("THERM_PRO_SLACK_WEBHOOK", "https://hooks.example.com/test")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.AllowedCIDR != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", cfg.AllowedCIDR)
	}
	if !cfg.TrustProxy {
		t.Error("expected trust_proxy true")
	}
	if cfg.Slack.Webhook != "https://hooks.example.com/test" {
		t.Errorf("expected slack webhook override, got %s", cfg.Slack.Webhook)
	}
}

func TestYAMLFile(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(yamlPath, []byte("port: 7070\nallowed_cidr: \"172.16.0.0/12\"\n"), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 7070 {
		t.Errorf("expected port 7070, got %d", cfg.Port)
	}
	if cfg.AllowedCIDR != "172.16.0.0/12" {
		t.Errorf("expected 172.16.0.0/12, got %s", cfg.AllowedCIDR)
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(yamlPath, []byte("port: 7070\n"), 0644)
	t.Setenv("THERM_PRO_PORT", "9999")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9999 {
		t.Errorf("expected env override 9999, got %d", cfg.Port)
	}
}

func TestDotEnvFile(t *testing.T) {
	dir := t.TempDir()
	envContent := "THERM_PRO_ALLOWED_CIDR=10.0.0.0/8\nTHERM_PRO_SLACK_WEBHOOK=https://hooks.example.com/dotenv\n"
	os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AllowedCIDR != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8 from .env, got %s", cfg.AllowedCIDR)
	}
	if cfg.Slack.Webhook != "https://hooks.example.com/dotenv" {
		t.Errorf("expected slack webhook from .env, got %s", cfg.Slack.Webhook)
	}
}

func TestEnvOverridesDotEnv(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("THERM_PRO_PORT=5555\n"), 0644)
	t.Setenv("THERM_PRO_PORT", "6666")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Real env vars should override .env file values
	if cfg.Port != 6666 {
		t.Errorf("expected env var 6666 to override .env 5555, got %d", cfg.Port)
	}
}

func TestLegacyPortEnv(t *testing.T) {
	t.Setenv("PORT", "3000")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("expected PORT=3000 override, got %d", cfg.Port)
	}
}
