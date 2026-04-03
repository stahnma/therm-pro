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
