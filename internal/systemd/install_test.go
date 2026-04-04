package systemd

import (
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
