# systemd Install Subcommand Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an `install` subcommand that copies the binary, creates a service user, and registers a systemd system unit.

**Architecture:** Add an `internal/systemd` package that handles binary installation, user creation, unit file generation, and systemd registration. Route `os.Args[1] == "install"` in `main.go` before the existing server startup. The unit file is rendered from an embedded `text/template`. All operations require root and are idempotent. A `--dry-run` flag prints actions without executing.

**Tech Stack:** Go stdlib (`os/exec`, `text/template`, `embed`), no new dependencies.

---

### Task 1: Create the systemd unit template

**Files:**
- Create: `internal/systemd/unit.tmpl`

**Step 1: Write the template file**

This is a `text/template` version of the existing `contrib/therm-pro-server.service`, parameterized:

```ini
[Unit]
Description=Therm-Pro Temperature Monitoring Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User={{.User}}
Group={{.User}}
ExecStart={{.BinPath}}
Restart=on-failure
RestartSec=5

Environment=PORT={{.Port}}

StateDirectory=therm-pro
WorkingDirectory={{.DataDir}}
HOME={{.DataDir}}

[Install]
WantedBy=multi-user.target
```

**Step 2: Commit**

```bash
git add internal/systemd/unit.tmpl
git commit -m "feat: add systemd unit file template"
```

---

### Task 2: Implement the install package — unit rendering

**Files:**
- Create: `internal/systemd/install.go`
- Test: `internal/systemd/install_test.go`

**Step 1: Write the failing test for unit rendering**

```go
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
```

**Step 2: Run test to verify it fails**

Run: `make test`
Expected: FAIL — `renderUnit` not defined.

**Step 3: Write minimal implementation**

`internal/systemd/install.go`:

```go
package systemd

import (
	"bytes"
	"embed"
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
```

**Step 4: Run test to verify it passes**

Run: `make test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/systemd/install.go internal/systemd/install_test.go
git commit -m "feat: implement systemd unit rendering with template"
```

---

### Task 3: Implement the install actions

**Files:**
- Modify: `internal/systemd/install.go`
- Test: `internal/systemd/install_test.go`

**Step 1: Write the failing test for dry-run**

Dry-run mode exercises the full `Install()` path without touching the system. It returns the list of actions it *would* take.

```go
func TestInstallDryRun(t *testing.T) {
	opts := Options{
		BinPath: "/usr/local/bin/therm-pro-server",
		User:    "therm-pro",
		Port:    8088,
		DataDir: "/var/lib/therm-pro",
		DryRun:  true,
	}
	actions, err := Install(opts, "/tmp/fake-binary")
	if err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}
	if len(actions) == 0 {
		t.Fatal("expected actions, got none")
	}
	// Should describe copying binary, creating user, writing unit, reloading
	joined := strings.Join(actions, "\n")
	for _, want := range []string{"copy", "useradd", "mkdir", "write", "daemon-reload", "enable"} {
		if !strings.Contains(strings.ToLower(joined), want) {
			t.Errorf("missing action containing %q in:\n%s", want, joined)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `make test`
Expected: FAIL — `Install` not defined.

**Step 3: Write the Install function**

Add to `internal/systemd/install.go`:

```go
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
	// Check if user exists already
	if err := exec.Command("id", user).Run(); err == nil {
		return nil // already exists
	}
	return exec.Command("useradd", "--system", "--home-dir", homeDir, "--shell", "/usr/sbin/nologin", user).Run()
}

func createDataDir(dir, user string) error {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	return exec.Command("chown", "-R", user+":"+user, dir).Run()
}
```

**Step 4: Run test to verify it passes**

Run: `make test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/systemd/install.go internal/systemd/install_test.go
git commit -m "feat: implement Install() with dry-run support"
```

---

### Task 4: Wire up the subcommand in main.go

**Files:**
- Modify: `cmd/therm-pro-server/main.go`

**Step 1: Add install routing before the existing server startup**

Insert at the top of `main()`, before `config.Load`:

```go
if len(os.Args) > 1 && os.Args[1] == "install" {
    runInstall()
    return
}
```

Add the `runInstall` function:

```go
func runInstall() {
	dryRun := false
	prefix := "/usr/local"
	for i, arg := range os.Args[2:] {
		switch {
		case arg == "--dry-run":
			dryRun = true
		case arg == "--prefix" && i+1 < len(os.Args[2:]):
			prefix = os.Args[2+i+1]
		case strings.HasPrefix(arg, "--prefix="):
			prefix = strings.TrimPrefix(arg, "--prefix=")
		}
	}

	if os.Geteuid() != 0 && !dryRun {
		fmt.Fprintln(os.Stderr, "error: install must be run as root (try sudo)")
		os.Exit(1)
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot determine binary path: %v\n", err)
		os.Exit(1)
	}

	opts := systemd.Options{
		BinPath: systemd.DefaultBinPath(prefix),
		User:    "therm-pro",
		Port:    8088,
		DataDir: "/var/lib/therm-pro",
		DryRun:  dryRun,
	}

	actions, err := systemd.Install(opts, self)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if dryRun {
		fmt.Println("Dry run — actions that would be taken:")
	}
	for _, a := range actions {
		fmt.Printf("  %s\n", a)
	}
	if !dryRun {
		fmt.Println("\nInstalled. Start with: systemctl start therm-pro-server")
	}
}
```

Add the import for `"github.com/stahnma/therm-pro/internal/systemd"`.

**Step 2: Build to verify it compiles**

Run: `make build`
Expected: compiles without errors.

**Step 3: Verify dry-run works**

Run: `./bin/therm-pro-server install --dry-run`
Expected: prints the list of actions without requiring root, binary path defaults to `/usr/local/bin/therm-pro-server`.

Run: `./bin/therm-pro-server install --dry-run --prefix=/opt`
Expected: binary path shows `/opt/bin/therm-pro-server`.

**Step 4: Commit**

```bash
git add cmd/therm-pro-server/main.go
git commit -m "feat: wire up 'install' subcommand in main"
```

---

### Task 5: Manual integration test

This cannot be automated in CI (requires root + systemd). Verify on a real system:

**Step 1: Build**

Run: `make build`

**Step 2: Run the install**

Run: `sudo ./bin/therm-pro-server install`

**Step 3: Verify**

```bash
id therm-pro                                    # user exists
ls -la /usr/local/bin/therm-pro-server           # binary installed
cat /etc/systemd/system/therm-pro-server.service # unit file correct
systemctl is-enabled therm-pro-server            # "enabled"
sudo systemctl start therm-pro-server
systemctl status therm-pro-server                # active (running)
curl http://localhost:8088/api/health             # responds
sudo systemctl stop therm-pro-server
```

**Step 4: Final commit (if any fixups needed)**

---

### Task 6: Clean up and close

**Step 1: Verify all tests pass**

Run: `make test`
Expected: PASS

**Step 2: Final commit and note in README if desired**

Add a usage note to README.md under an "Installation" section:

```markdown
## Installation

Build and install as a systemd service:

    make build
    sudo ./bin/therm-pro-server install

This will:
- Copy the binary to `/usr/local/bin/therm-pro-server`
- Create a `therm-pro` system user
- Create `/var/lib/therm-pro` data directory
- Install and enable a systemd unit

To install to a different prefix (e.g. `/usr` or `/opt`):

    sudo ./bin/therm-pro-server install --prefix=/usr

Start the service:

    sudo systemctl start therm-pro-server

Preview without making changes:

    ./bin/therm-pro-server install --dry-run
```

```bash
git add README.md
git commit -m "docs: add systemd installation instructions to README"
```
