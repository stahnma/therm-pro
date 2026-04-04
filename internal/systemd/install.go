package systemd

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const (
	unitPath    = "/etc/systemd/system/therm-pro-server.service"
	serviceName = "therm-pro-server"
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

// Install performs the full systemd installation. srcBinary is the path to the
// currently-running binary. Returns a list of action descriptions.
func Install(opts Options, srcBinary string) ([]string, error) {
	var actions []string
	do := func(desc string, fn func() error) error {
		actions = append(actions, desc)
		if opts.DryRun {
			return nil
		}
		return fn()
	}

	// 1. Copy binary
	if err := do(fmt.Sprintf("copy %s -> %s", srcBinary, opts.BinPath), func() error {
		return copyFile(srcBinary, opts.BinPath, 0755)
	}); err != nil {
		return actions, fmt.Errorf("copy binary: %w", err)
	}

	// 2. Create system user (idempotent)
	if err := do(fmt.Sprintf("useradd --system --home-dir %s --shell /usr/sbin/nologin %s (skip if exists)", opts.DataDir, opts.User), func() error {
		return createUser(opts.User, opts.DataDir)
	}); err != nil {
		return actions, fmt.Errorf("create user: %w", err)
	}

	// 3. Create data directory
	if err := do(fmt.Sprintf("mkdir -p %s (owned by %s)", opts.DataDir, opts.User), func() error {
		return createDataDir(opts.DataDir, opts.User)
	}); err != nil {
		return actions, fmt.Errorf("create data dir: %w", err)
	}

	// 4. Render and write unit file
	unit, err := renderUnit(opts)
	if err != nil {
		return actions, fmt.Errorf("render unit: %w", err)
	}
	if err := do(fmt.Sprintf("write %s", unitPath), func() error {
		return os.WriteFile(unitPath, unit.Bytes(), 0644)
	}); err != nil {
		return actions, fmt.Errorf("write unit: %w", err)
	}

	// 5. Reload and enable
	if err := do("systemctl daemon-reload", func() error {
		return exec.Command("systemctl", "daemon-reload").Run()
	}); err != nil {
		return actions, fmt.Errorf("daemon-reload: %w", err)
	}
	if err := do(fmt.Sprintf("systemctl enable %s", serviceName), func() error {
		return exec.Command("systemctl", "enable", serviceName).Run()
	}); err != nil {
		return actions, fmt.Errorf("enable: %w", err)
	}

	return actions, nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func createUser(user, homeDir string) error {
	if err := exec.Command("id", user).Run(); err == nil {
		return nil
	}
	return exec.Command("useradd", "--system", "--home-dir", homeDir, "--shell", "/usr/sbin/nologin", user).Run()
}

func createDataDir(dir, user string) error {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	return exec.Command("chown", "-R", user+":"+user, dir).Run()
}
