// cmd/therm-pro-server/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/stahnma/therm-pro/internal/api"
	"github.com/stahnma/therm-pro/internal/consul"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8088"
	}

	slackWebhook := os.Getenv("THERM_PRO_SLACK_WEBHOOK")

	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".therm-pro")
	sessionPath := filepath.Join(dataDir, "session.json")
	firmwareDir := filepath.Join(dataDir, "firmware")

	srv := api.NewServer(":"+port, slackWebhook, sessionPath, firmwareDir)
	mux := srv.Routes()

	// Register with local Consul agent
	portNum, _ := strconv.Atoi(port)
	if err := consul.Register(portNum); err != nil {
		log.Printf("WARNING: consul registration failed: %v", err)
	}

	httpSrv := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		log.Printf("therm-pro-server listening on :%s", port)
		if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	consul.Deregister()
	httpSrv.Shutdown(context.Background())
}
