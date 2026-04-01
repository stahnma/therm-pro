package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/stahnma/therm-pro/internal/cook"
)

const defaultSlackAPIBase = "https://slack.com"

type webhookPayload struct {
	Text string `json:"text"`
}

// Client sends alert notifications to a Slack incoming webhook and supports
// file uploads via the Slack API.
type Client struct {
	webhookURL   string
	botToken     string
	slackAPIBase string
	httpClient   *http.Client
}

// NewClient creates a new Slack webhook client. An optional bot token can be
// provided to enable file-upload capabilities. If webhookURL is empty,
// SendAlert will silently no-op.
func NewClient(webhookURL string, botToken ...string) *Client {
	tok := ""
	if len(botToken) > 0 {
		tok = botToken[0]
	}
	return &Client{
		webhookURL:   webhookURL,
		botToken:     tok,
		slackAPIBase: defaultSlackAPIBase,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
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

// UploadFileAndPost uploads a PNG file to Slack and posts it to the given
// channel with the provided message text. It uses the three-step Slack file
// upload flow: getUploadURLExternal, upload, completeUploadExternal.
func (c *Client) UploadFileAndPost(pngData []byte, channelID, messageText string) error {
	if c.botToken == "" {
		return fmt.Errorf("slack file upload: bot token is required")
	}

	// Step 1: Get an upload URL from Slack.
	getURLEndpoint := fmt.Sprintf("%s/api/files.getUploadURLExternal?filename=chart.png&length=%d", c.slackAPIBase, len(pngData))
	req, err := http.NewRequest(http.MethodGet, getURLEndpoint, nil)
	if err != nil {
		return fmt.Errorf("slack file upload: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack file upload: getUploadURLExternal: %w", err)
	}
	defer resp.Body.Close()

	var uploadResp struct {
		OK        bool   `json:"ok"`
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return fmt.Errorf("slack file upload: decode getUploadURLExternal: %w", err)
	}
	if !uploadResp.OK {
		return fmt.Errorf("slack file upload: getUploadURLExternal returned ok=false")
	}

	// Step 2: Upload the file data to the provided URL.
	uploadResp2, err := c.httpClient.Post(uploadResp.UploadURL, "application/octet-stream", bytes.NewReader(pngData))
	if err != nil {
		return fmt.Errorf("slack file upload: upload: %w", err)
	}
	uploadResp2.Body.Close()

	// Step 3: Complete the upload and share to channel.
	completeBody, _ := json.Marshal(map[string]interface{}{
		"files":           []map[string]string{{"id": uploadResp.FileID}},
		"channel_id":      channelID,
		"initial_comment": messageText,
	})
	completeReq, err := http.NewRequest(http.MethodPost, c.slackAPIBase+"/api/files.completeUploadExternal", bytes.NewReader(completeBody))
	if err != nil {
		return fmt.Errorf("slack file upload: %w", err)
	}
	completeReq.Header.Set("Authorization", "Bearer "+c.botToken)
	completeReq.Header.Set("Content-Type", "application/json")

	completeResp, err := c.httpClient.Do(completeReq)
	if err != nil {
		return fmt.Errorf("slack file upload: completeUploadExternal: %w", err)
	}
	defer completeResp.Body.Close()

	var completeResult struct {
		OK bool `json:"ok"`
	}
	body, _ := io.ReadAll(completeResp.Body)
	if err := json.Unmarshal(body, &completeResult); err != nil {
		return fmt.Errorf("slack file upload: decode completeUploadExternal: %w", err)
	}
	if !completeResult.OK {
		return fmt.Errorf("slack file upload: completeUploadExternal returned ok=false")
	}

	return nil
}
