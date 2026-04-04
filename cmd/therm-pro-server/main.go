// cmd/therm-pro-server/main.go
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/stahnma/therm-pro/internal/api"
	"github.com/stahnma/therm-pro/internal/config"
	"github.com/stahnma/therm-pro/internal/consul"
	"github.com/stahnma/therm-pro/internal/systemd"
)

// GitCommit is set at build time via -ldflags.
var GitCommit = "dev"

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// statusWriter captures the HTTP status code for logging.
type statusWriter struct {
	http.ResponseWriter
	status  int
	written bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.written {
		w.status = code
		w.written = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.status = 200
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

// Hijack implements http.Hijacker for websocket support.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support Hijack")
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		slog.Debug("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start),
			"remote_addr", r.RemoteAddr)
	})
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "install" {
		runInstall()
		return
	}

	cfg, err := config.Load("")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Initialize structured logging.
	level := parseLogLevel(cfg.LogLevel)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	slog.Info("starting server", "port", cfg.Port, "commit", GitCommit, "log_level", cfg.LogLevel)

	srv := api.NewServer(cfg, GitCommit)
	mux := srv.Routes()

	if err := consul.Register(cfg.Port); err != nil {
		slog.Warn("consul registration failed", "error", err)
	}

	httpSrv := &http.Server{Addr: ":" + strconv.Itoa(cfg.Port), Handler: requestLogger(mux)}

	go func() {
		slog.Info("listening", "addr", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down")
	consul.Deregister()
	httpSrv.Shutdown(context.Background())
}

func runInstall() {
	// Load config to pick up port from config.yaml / .env / env vars.
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load config, using defaults: %v\n", err)
		cfg = &config.Config{Port: 8088}
	}

	fs := flag.NewFlagSet("install", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s install [flags]\n\nInstall therm-pro-server as a systemd service.\n\nFlags:\n", os.Args[0])
		fs.PrintDefaults()
	}
	dryRun := fs.Bool("dry-run", false, "print actions without executing")
	prefix := fs.String("prefix", "/usr/local", "installation prefix (binary goes in <prefix>/bin/)")
	port := fs.Int("port", cfg.Port, "port for the service")
	fs.Parse(os.Args[2:])

	if os.Geteuid() != 0 && !*dryRun {
		fmt.Fprintln(os.Stderr, "error: install must be run as root (try sudo)")
		os.Exit(1)
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot determine binary path: %v\n", err)
		os.Exit(1)
	}

	opts := systemd.Options{
		BinPath: systemd.DefaultBinPath(*prefix),
		User:    "therm-pro",
		Port:    *port,
		DataDir: "/var/lib/therm-pro",
		DryRun:  *dryRun,
	}

	actions, err := systemd.Install(opts, self)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Println("Dry run — actions that would be taken:")
	}
	for _, a := range actions {
		fmt.Printf("  %s\n", a)
	}
	if !*dryRun {
		fmt.Println("\nInstalled. Start with: systemctl start therm-pro-server")
	}
}
