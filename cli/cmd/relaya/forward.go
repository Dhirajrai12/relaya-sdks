package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RawEvent is an event exactly as it arrived at Relaya (GET …/events/{id}/raw).
type RawEvent struct {
	ID          string            `json:"id"`
	WebhookID   string            `json:"webhook_id"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
	ContentType string            `json:"content_type"`
	Headers     map[string]string `json:"headers"`
	Body        []byte            `json:"body_base64"`
	ReceivedAt  time.Time         `json:"received_at"`
	Simulated   bool              `json:"simulated"`
}

// Headers that describe the hop to Relaya, not the provider's request, or that
// Relaya sets itself: never forwarded.
var dropHeaders = map[string]bool{
	"host": true, "content-length": true, "connection": true, "keep-alive": true, "transfer-encoding": true,
	"accept-encoding": true, "te": true, "upgrade": true, "proxy-connection": true, "cookie": true,
	"x-forwarded-for": true, "x-forwarded-proto": true, "x-forwarded-host": true, "x-forwarded-port": true,
	"x-real-ip": true, "x-original-url": true, "x-arr-log-id": true, "x-arr-ssl": true, "max-forwards": true,
}

// buildForward turns a stored event into the request a Relaya destination would
// get: the provider's original body and headers (so the local app's provider
// signature check still passes), plus Relaya's delivery headers signed with secret.
func buildForward(ctx context.Context, target string, ev RawEvent, secret string, extra http.Header, now time.Time) (*http.Request, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(ev.Body))
	if err != nil {
		return nil, "", err
	}
	for k, v := range ev.Headers {
		lk := strings.ToLower(k)
		if dropHeaders[lk] || strings.HasPrefix(lk, "relaya-") || strings.HasPrefix(lk, "x-arr-") || v == "[REDACTED]" {
			continue
		}
		req.Header.Set(k, v)
	}
	if ev.ContentType != "" {
		req.Header.Set("Content-Type", ev.ContentType)
	}
	deliveryID := "cli_" + randomHex(12)
	req.Header.Set("User-Agent", "Relaya-CLI/"+version)
	req.Header.Set("Relaya-Event-Id", ev.ID)
	req.Header.Set("Relaya-Delivery-Id", deliveryID)
	req.Header.Set("Relaya-Attempt", "1")
	req.Header.Set("Relaya-Event-Type", ev.Type)
	req.Header.Set("Idempotency-Key", deliveryID)
	req.Header.Set("Relaya-Signature", sign([]byte(secret), now, ev.Body))
	if ev.Simulated {
		req.Header.Set("Relaya-Simulated", "true")
	}
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	return req, deliveryID, nil
}

// sign is Relaya's delivery signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<body>")>.
func sign(secret []byte, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

// forwardResult is what the local server answered.
type forwardResult struct {
	Status   int
	Body     string
	Duration time.Duration
	Err      error
}

func forward(hc *http.Client, req *http.Request) forwardResult {
	start := time.Now()
	resp, err := hc.Do(req)
	res := forwardResult{Duration: time.Since(start)}
	if err != nil {
		res.Err = explain(err, req.URL.Host)
		return res
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
	res.Status, res.Body = resp.StatusCode, strings.TrimSpace(string(b))
	return res
}

// explain makes the usual local mistakes obvious.
func explain(err error, host string) error {
	var op *net.OpError
	switch {
	case errors.As(err, &op) && op.Op == "dial":
		return fmt.Errorf("nothing is listening at %s (is your server running?)", host)
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Client.Timeout"):
		return fmt.Errorf("%s took longer than the timeout to answer", host)
	}
	return err
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newSecret is a signing secret in the format Relaya destinations use.
func newSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "rsec_" + base64.RawURLEncoding.EncodeToString(b)
}
