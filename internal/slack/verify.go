package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
)

// VerifySlackRequest verifies a Slack request using the signing secret.
// body should be the raw request body bytes.
func VerifySlackRequest(r *http.Request, body []byte, signingSecret string) error {
	tsHeader := r.Header.Get("X-Slack-Request-Timestamp")
	if tsHeader == "" {
		return fmt.Errorf("missing timestamp header")
	}

	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	// Reject requests older than 5 minutes to prevent replay attacks
	if math.Abs(float64(time.Now().Unix()-ts)) > 300 {
		return fmt.Errorf("request timestamp too old")
	}

	// Compute expected signature
	baseString := fmt.Sprintf("v0:%s:%s", tsHeader, string(body))
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expected := fmt.Sprintf("v0=%x", mac.Sum(nil))

	actual := r.Header.Get("X-Slack-Signature")
	if !hmac.Equal([]byte(expected), []byte(actual)) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}
