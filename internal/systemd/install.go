package systemd

import (
	"bytes"
	"embed"
	"path/filepath"
	"text/template"
)

//go:embed unit.tmpl
var unitFS embed.FS

// Options configures the install.
type Options struct {
	BinPath string // full path to install the binary (derived from Prefix)
	User    string // system user to create/run as (default therm-pro)
	Port    int    // port for the Environment= line
	DataDir string // working/state directory (default /var/lib/therm-pro)
	DryRun  bool   // print plan without executing
}

// DefaultBinPath returns the install path for a given prefix (default /usr/local).
// e.g. prefix="/usr" -> "/usr/bin/therm-pro-server"
func DefaultBinPath(prefix string) string {
	if prefix == "" {
		prefix = "/usr/local"
	}
	return filepath.Join(prefix, "bin", "therm-pro-server")
}

func renderUnit(opts Options) (*bytes.Buffer, error) {
	tmpl, err := template.ParseFS(unitFS, "unit.tmpl")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, opts); err != nil {
		return nil, err
	}
	return &buf, nil
}
