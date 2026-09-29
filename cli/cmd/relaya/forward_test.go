package main

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	relaya "github.com/Dhirajrai12/relaya-sdks/go"
)

func TestBuildForward(t *testing.T) {
	ev := RawEvent{
		ID: "11111111-2222-3333-4444-555555555555", Type: "payment.captured", ContentType: "application/json", Simulated: true,
		Body: []byte(`{"event":"payment.captured"}`),
		Headers: map[string]string{
			"x-razorpay-signature": "abc123", "x-razorpay-event-id": "evt_1", "host": "server.example", "content-length": "28",
			"x-forwarded-for": "1.2.3.4", "authorization": "[REDACTED]", "relaya-simulated": "true", "x-arr-ssl": "x", "user-agent": "Razorpay-Webhook/v1",
		},
	}
	extra := http.Header{"X-Tunnel": {"dev"}}
	now := time.Now()
	req, deliveryID, err := buildForward(context.Background(), "http://localhost:3000/hooks", ev, "rsec_local", extra, now)
	if err != nil {
		t.Fatal(err)
	}
	h := req.Header
	// The provider's own headers go through, so its signature check still works.
	if h.Get("X-Razorpay-Signature") != "abc123" || h.Get("X-Razorpay-Event-Id") != "evt_1" || h.Get("Content-Type") != "application/json" {
		t.Fatalf("provider headers: %v", h)
	}
	// Hop and masked headers don't.
	for _, k := range []string{"Host", "Content-Length", "X-Forwarded-For", "Authorization", "X-Arr-Ssl"} {
		if h.Get(k) != "" {
			t.Errorf("%s forwarded", k)
		}
	}
	if h.Get("X-Tunnel") != "dev" || h.Get("Relaya-Simulated") != "true" || !strings.HasPrefix(h.Get("User-Agent"), "Relaya-CLI/") {
		t.Fatalf("headers: %v", h)
	}
	// Relaya's delivery headers verify with the Go SDK, like a real destination's.
	d, err := relaya.VerifyDelivery(ev.Body, h, relaya.Secret("rsec_local"))
	if err != nil {
		t.Fatal(err)
	}
	if d.EventID != ev.ID || d.EventType != "payment.captured" || d.DeliveryID != deliveryID || d.IdempotencyKey != deliveryID || d.Attempt != 1 {
		t.Fatalf("delivery: %+v", d)
	}
	if _, err := relaya.VerifyDelivery(ev.Body, h, relaya.Secret("rsec_other")); err == nil {
		t.Fatal("verified with another secret")
	}
}

func TestTargetsAndErrors(t *testing.T) {
	for in, want := range map[string]string{
		"3000":                        "http://localhost:3000",
		"3000/webhooks":               "http://localhost:3000/webhooks",
		"localhost:3000/webhooks":     "http://localhost:3000/webhooks",
		"http://127.0.0.1:8080/hooks": "http://127.0.0.1:8080/hooks",
		"https://dev.example.test/in": "https://dev.example.test/in",
	} {
		if got, err := normalizeTarget(in); err != nil || got != want {
			t.Errorf("%q → %q, %v", in, got, err)
		}
	}
	if _, err := normalizeTarget("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
	if got := streamURL("https://server.example/api/", "org1"); got != "wss://server.example/api/v1/orgs/org1/stream" {
		t.Error(got)
	}
	if got := streamURL("http://127.0.0.1:18080", "o"); got != "ws://127.0.0.1:18080/v1/orgs/o/stream" {
		t.Error(got)
	}

	// Nothing listening locally: a plain explanation.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	req, _ := http.NewRequest("POST", "http://"+addr+"/hooks", nil)
	if res := forward(&http.Client{Timeout: 2 * time.Second}, req); res.Err == nil || !strings.Contains(res.Err.Error(), "nothing is listening") {
		t.Fatalf("closed port: %v", res.Err)
	}
}
