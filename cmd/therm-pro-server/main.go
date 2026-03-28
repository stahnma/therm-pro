// cmd/therm-pro-server/main.go
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/stahnma/therm-pro/internal/api"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slackWebhook := os.Getenv("THERM_PRO_SLACK_WEBHOOK")

	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".therm-pro")
	sessionPath := filepath.Join(dataDir, "session.json")
	firmwareDir := filepath.Join(dataDir, "firmware")

	srv := api.NewServer(":"+port, slackWebhook, sessionPath, firmwareDir)
	mux := srv.Routes()

	log.Printf("therm-pro-server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
