package slack

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/stahnma/therm-pro/internal/chart"
	"github.com/stahnma/therm-pro/internal/cook"
)

// CommandHandler handles Slack slash command requests.
type CommandHandler struct {
	signingSecret string
	client        *Client
	session       *cook.Session
}

// NewCommandHandler creates a handler for /slack/command.
func NewCommandHandler(signingSecret, botToken string, session *cook.Session) *CommandHandler {
	c := NewClient("", botToken)
	return &CommandHandler{
		signingSecret: signingSecret,
		client:        c,
		session:       session,
	}
}

// ServeHTTP handles the incoming slash command POST from Slack.
func (h *CommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := VerifySlackRequest(r, body, h.signingSecret); err != nil {
		log.Printf("slack command: verification failed: %v", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	channelID := parseFormValue(string(body), "channel_id")
	if channelID == "" {
		http.Error(w, "missing channel_id", http.StatusBadRequest)
		return
	}

	// Build status text
	statusText := FormatStatusText(h.session)

	// Generate chart
	h.session.RLock()
	history := make([]cook.Reading, len(h.session.History))
	copy(history, h.session.History)
	probes := h.session.Probes
	h.session.RUnlock()

	pngData, err := chart.RenderSessionChart(history, probes)
	if err != nil {
		log.Printf("slack command: chart render failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"response_type":"in_channel","text":%q}`, statusText)
		return
	}

	// Upload chart and post to channel
	if err := h.client.UploadFileAndPost(pngData, channelID, statusText); err != nil {
		log.Printf("slack command: file upload failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"response_type":"in_channel","text":%q}`, statusText)
		return
	}

	// Return empty 200 — the message was posted via API
	w.WriteHeader(http.StatusOK)
}

// FormatStatusText builds the BBQ status message text.
func FormatStatusText(session *cook.Session) string {
	session.RLock()
	defer session.RUnlock()

	elapsed := time.Since(session.Started).Round(time.Minute)
	hours := int(elapsed.Hours())
	minutes := int(elapsed.Minutes()) % 60

	var b strings.Builder
	fmt.Fprintf(&b, "*BBQ Status* (session started %dh %dm ago)\n", hours, minutes)

	battery := 0
	if len(session.History) > 0 {
		battery = session.History[len(session.History)-1].Battery
	}

	for i := 0; i < cook.NumProbes; i++ {
		p := session.Probes[i]
		if p.Connected {
			fmt.Fprintf(&b, "  %s (Probe %d):  %.1f°F\n", p.Label, p.ID, p.CurrentTemp)
		} else {
			fmt.Fprintf(&b, "  %s (Probe %d):  disconnected\n", p.Label, p.ID)
		}
	}

	fmt.Fprintf(&b, "  Battery: %d%%", battery)
	return b.String()
}

// parseFormValue extracts a value from a URL-encoded form body.
func parseFormValue(body, key string) string {
	for _, pair := range strings.Split(body, "&") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 && parts[0] == key {
			return parts[1]
		}
	}
	return ""
}
