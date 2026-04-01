package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signRequest(body, signingSecret string, ts time.Time) (string, string) {
	timestamp := strconv.FormatInt(ts.Unix(), 10)
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, body)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	sig := fmt.Sprintf("v0=%x", mac.Sum(nil))
	return timestamp, sig
}

func TestVerifySlackRequest_Valid(t *testing.T) {
	secret := "test-secret-12345"
	body := "token=abc&command=%2Fbbq&text=&channel_id=C123"
	ts, sig := signRequest(body, secret, time.Now())

	req, _ := http.NewRequest("POST", "/slack/command", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)

	err := VerifySlackRequest(req, []byte(body), secret)
	if err != nil {
		t.Fatalf("expected valid request, got: %v", err)
	}
}

func TestVerifySlackRequest_BadSignature(t *testing.T) {
	body := "token=abc"
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req, _ := http.NewRequest("POST", "/slack/command", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", "v0=badsignature")

	err := VerifySlackRequest(req, []byte(body), "test-secret")
	if err == nil {
		t.Fatal("expected error for bad signature")
	}
}

func TestVerifySlackRequest_OldTimestamp(t *testing.T) {
	secret := "test-secret"
	body := "token=abc"
	oldTime := time.Now().Add(-10 * time.Minute)
	ts, sig := signRequest(body, secret, oldTime)

	req, _ := http.NewRequest("POST", "/slack/command", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)

	err := VerifySlackRequest(req, []byte(body), secret)
	if err == nil {
		t.Fatal("expected error for old timestamp")
	}
}
