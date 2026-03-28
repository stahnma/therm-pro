package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stahnma/therm-pro/internal/cook"
)

type webhookPayload struct {
	Text string `json:"text"`
}

// Client sends alert notifications to a Slack incoming webhook.
type Client struct {
	webhookURL string
	httpClient *http.Client
}

// NewClient creates a new Slack webhook client. If webhookURL is empty,
// SendAlert will silently no-op.
func NewClient(webhookURL string) *Client {
	return &Client{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// SendAlert posts a formatted alert message to the configured Slack webhook.
// If no webhook URL is configured, it returns nil without sending anything.
func (c *Client) SendAlert(alert cook.Alert, allTemps [4]float64) error {
	if c.webhookURL == "" {
		return nil
	}

	text := fmt.Sprintf("*%s*\nAll probes: 1=%.1f°F  2=%.1f°F  3=%.1f°F  4=%.1f°F",
		alert.Message, allTemps[0], allTemps[1], allTemps[2], allTemps[3])

	payload, _ := json.Marshal(webhookPayload{Text: text})
	resp, err := c.httpClient.Post(c.webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("slack webhook: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}
	return nil
}
