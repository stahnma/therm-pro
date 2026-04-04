package systemd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderUnit(t *testing.T) {
	opts := Options{
		BinPath: "/usr/local/bin/therm-pro-server",
		User:    "therm-pro",
		Port:    8088,
		DataDir: "/var/lib/therm-pro",
	}
	out, err := renderUnit(opts)
	if err != nil {
		t.Fatalf("renderUnit: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "ExecStart=/usr/local/bin/therm-pro-server") {
		t.Errorf("missing ExecStart, got:\n%s", s)
	}
	if !strings.Contains(s, "User=therm-pro") {
		t.Errorf("missing User, got:\n%s", s)
	}
	if !strings.Contains(s, "Environment=PORT=8088") {
		t.Errorf("missing PORT, got:\n%s", s)
	}
	if !strings.Contains(s, "WorkingDirectory=/var/lib/therm-pro") {
		t.Errorf("missing WorkingDirectory, got:\n%s", s)
	}
	if !strings.Contains(s, "Environment=THERM_PRO_DATA_DIR=/var/lib/therm-pro") {
		t.Errorf("missing THERM_PRO_DATA_DIR, got:\n%s", s)
	}
}

func TestInstallDryRun(t *testing.T) {
	opts := Options{
		BinPath: "/usr/local/bin/therm-pro-server",
		User:    "therm-pro",
		Port:    8088,
		DataDir: "/var/lib/therm-pro",
		DryRun:  true,
	}
	actions, err := Install(opts, "/tmp/fake-binary", "/tmp/fake-datadir")
	if err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}
	if len(actions) == 0 {
		t.Fatal("expected actions, got none")
	}
	joined := strings.Join(actions, "\n")
	for _, want := range []string{"copy", "useradd", "mkdir", "chown", "write", "daemon-reload", "enable"} {
		if !strings.Contains(strings.ToLower(joined), want) {
			t.Errorf("missing action containing %q in:\n%s", want, joined)
		}
	}
}

func TestInstallDryRunCopiesFirmware(t *testing.T) {
	srcDir := t.TempDir()
	fwDir := filepath.Join(srcDir, "firmware")
	os.MkdirAll(fwDir, 0755)
	os.WriteFile(filepath.Join(fwDir, "firmware.bin"), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(fwDir, "version.json"), []byte("{}"), 0644)

	opts := Options{
		BinPath: "/usr/local/bin/therm-pro-server",
		User:    "therm-pro",
		Port:    8088,
		DataDir: "/var/lib/therm-pro",
		DryRun:  true,
	}
	actions, err := Install(opts, "/tmp/fake-binary", srcDir)
	if err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}
	joined := strings.Join(actions, "\n")
	if !strings.Contains(joined, "firmware.bin") {
		t.Errorf("expected firmware.bin copy action, got:\n%s", joined)
	}
	if !strings.Contains(joined, "version.json") {
		t.Errorf("expected version.json copy action, got:\n%s", joined)
	}
}

func TestInstallDryRunSkipsMissingFirmware(t *testing.T) {
	srcDir := t.TempDir() // no firmware subdir

	opts := Options{
		BinPath: "/usr/local/bin/therm-pro-server",
		User:    "therm-pro",
		Port:    8088,
		DataDir: "/var/lib/therm-pro",
		DryRun:  true,
	}
	actions, err := Install(opts, "/tmp/fake-binary", srcDir)
	if err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}
	joined := strings.Join(actions, "\n")
	if strings.Contains(joined, "firmware") {
		t.Errorf("expected no firmware actions, got:\n%s", joined)
	}
}

func TestRenderUnitCustomPort(t *testing.T) {
	opts := Options{
		BinPath: "/opt/bin/therm-pro-server",
		User:    "therm-pro",
		Port:    9090,
		DataDir: "/var/lib/therm-pro",
	}
	out, err := renderUnit(opts)
	if err != nil {
		t.Fatalf("renderUnit: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Environment=PORT=9090") {
		t.Errorf("expected PORT=9090, got:\n%s", s)
	}
	if !strings.Contains(s, "ExecStart=/opt/bin/therm-pro-server") {
		t.Errorf("expected ExecStart=/opt/bin/therm-pro-server, got:\n%s", s)
	}
}

func TestDefaultBinPath(t *testing.T) {
	tests := []struct {
		prefix string
		want   string
	}{
		{"", "/usr/local/bin/therm-pro-server"},
		{"/usr/local", "/usr/local/bin/therm-pro-server"},
		{"/usr", "/usr/bin/therm-pro-server"},
		{"/opt", "/opt/bin/therm-pro-server"},
	}
	for _, tt := range tests {
		got := DefaultBinPath(tt.prefix)
		if got != tt.want {
			t.Errorf("DefaultBinPath(%q) = %q, want %q", tt.prefix, got, tt.want)
		}
	}
}
