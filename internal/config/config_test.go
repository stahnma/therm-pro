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
	if cfg.RegistrationPIN != "" {
		t.Errorf("expected default registration_pin empty, got %s", cfg.RegistrationPIN)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected default log_level 'info', got %s", cfg.LogLevel)
	}
}

func TestRegistrationPINEnvOverride(t *testing.T) {
	t.Setenv("THERM_PRO_REGISTRATION_PIN", "1234")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RegistrationPIN != "1234" {
		t.Errorf("expected registration_pin '1234', got %s", cfg.RegistrationPIN)
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
	t.Setenv("THERM_PRO_SLACK_WEBHOOK", "https://hooks.example.com/test")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.Slack.Webhook != "https://hooks.example.com/test" {
		t.Errorf("expected slack webhook override, got %s", cfg.Slack.Webhook)
	}
}

func TestYAMLFile(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(yamlPath, []byte("port: 7070\nregistration_pin: \"5678\"\n"), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 7070 {
		t.Errorf("expected port 7070, got %d", cfg.Port)
	}
	if cfg.RegistrationPIN != "5678" {
		t.Errorf("expected registration_pin '5678', got %s", cfg.RegistrationPIN)
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
	envContent := "THERM_PRO_REGISTRATION_PIN=9999\nTHERM_PRO_SLACK_WEBHOOK=https://hooks.example.com/dotenv\n"
	os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RegistrationPIN != "9999" {
		t.Errorf("expected registration_pin '9999' from .env, got %s", cfg.RegistrationPIN)
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

func TestDataDirEnvOverride(t *testing.T) {
	t.Setenv("THERM_PRO_DATA_DIR", "/var/lib/therm-pro")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DataDir != "/var/lib/therm-pro" {
		t.Errorf("expected data_dir '/var/lib/therm-pro', got %s", cfg.DataDir)
	}
}

func TestDataDirExplicitArgTakesPrecedence(t *testing.T) {
	t.Setenv("THERM_PRO_DATA_DIR", "/should/be/ignored")
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DataDir != dir {
		t.Errorf("expected data_dir %q, got %s", dir, cfg.DataDir)
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
