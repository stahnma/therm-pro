// cmd/therm-pro-server/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/stahnma/therm-pro/internal/api"
	"github.com/stahnma/therm-pro/internal/config"
	"github.com/stahnma/therm-pro/internal/consul"
)

// GitCommit is set at build time via -ldflags.
var GitCommit = "dev"

func main() {
	cfg, err := config.Load("")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	srv := api.NewServer(cfg, GitCommit)
	mux := srv.Routes()

	if err := consul.Register(cfg.Port); err != nil {
		log.Printf("WARNING: consul registration failed: %v", err)
	}

	httpSrv := &http.Server{Addr: ":" + strconv.Itoa(cfg.Port), Handler: mux}

	go func() {
		log.Printf("therm-pro-server listening on :%d", cfg.Port)
		if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	consul.Deregister()
	httpSrv.Shutdown(context.Background())
}
