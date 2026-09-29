package main

// End-to-end against a running Relaya (API + worker). Skipped unless RELAYA_IT_API_URL is set:
//
//	RELAYA_IT_API_URL=http://127.0.0.1:18080 go test ./...

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	relaya "github.com/Dhirajrai12/relaya-sdks/go"
)

// syncBuffer is an io.Writer safe to read while listen writes to it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	for until := time.Now().Add(20 * time.Second); !fn(); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(until) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestListenAndTrigger(t *testing.T) {
	api := os.Getenv("RELAYA_IT_API_URL")
	if api == "" {
		t.Skip("set RELAYA_IT_API_URL to run")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	post := func(path string, body any, token string, out any) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", api+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode >= 300 {
			t.Fatalf("%s: %v %v", path, err, res.Status)
		}
		defer res.Body.Close()
		json.NewDecoder(res.Body).Decode(out)
	}
	var session struct{ Token string }
	post("/v1/auth/signup", map[string]string{"email": fmt.Sprintf("cli-%d@example.com", time.Now().UnixNano()), "password": "cli-test-password-1", "org_name": "CLI"}, "", &session)
	var me struct{ Orgs []struct{ ID string } }
	if err := relaya.New(session.Token, relaya.WithBaseURL(api)).Do(ctx, "GET", "/v1/me", nil, nil, &me); err != nil {
		t.Fatal(err)
	}
	org := me.Orgs[0].ID
	var key struct{ Key string }
	post("/v1/orgs/"+org+"/api-keys", map[string]string{"name": "cli", "role": "admin"}, session.Token, &key)
	var proj struct{ ID string }
	post("/v1/orgs/"+org+"/projects", map[string]string{"name": "P"}, session.Token, &proj)
	var rzp, other struct{ ID string }
	post("/v1/orgs/"+org+"/webhooks", map[string]string{"project_id": proj.ID, "name": "Payments", "provider": "razorpay", "signing_secret": "rzp-secret"}, session.Token, &rzp)
	post("/v1/orgs/"+org+"/webhooks", map[string]string{"project_id": proj.ID, "name": "Store", "provider": "shopify", "signing_secret": "shp-secret"}, session.Token, &other)

	// Log in the way a developer would, into a throwaway config file.
	t.Setenv("RELAYA_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("RELAYA_API_KEY", "")
	var out bytes.Buffer
	if err := run(ctx, []string{"login", "--api-key", key.Key, "--base-url", api}, nil, &out); err != nil || !strings.Contains(out.String(), org) {
		t.Fatalf("login: %v %s", err, out.String())
	}
	cfg, _ := loadConfig()
	if cfg.ListenSecret == "" || cfg.OrgID != org {
		t.Fatalf("config: %+v", cfg)
	}
	out.Reset()
	if err := run(ctx, []string{"webhooks"}, nil, &out); err != nil || !strings.Contains(out.String(), "Payments") || !strings.Contains(out.String(), "shopify") {
		t.Fatalf("webhooks: %v %s", err, out.String())
	}

	// The developer's local server: checks Razorpay's signature and Relaya's.
	type got struct {
		header http.Header
		body   []byte
	}
	var mu sync.Mutex
	var received []got
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, got{r.Header.Clone(), b})
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer local.Close()

	logs := &syncBuffer{}
	ready := make(chan struct{})
	go listen(ctx, listenOpts{cfg: cfg, forwardTo: local.URL + "/webhooks", webhooks: []string{"Payments"}, events: []string{"payment.captured", "payment.failed"}, ready: ready}, logs)
	select {
	case <-ready:
	case <-time.After(20 * time.Second):
		t.Fatalf("listen never got ready: %s", logs.String())
	}

	// trigger sends a signed sample; listen forwards it to the local server.
	out.Reset()
	if err := run(ctx, []string{"trigger", "payment.captured", "--webhook", "Payments"}, nil, &out); err != nil || !strings.Contains(out.String(), "Sent payment.captured to Payments") {
		t.Fatalf("trigger: %v %s", err, out.String())
	}
	waitFor(t, "the forwarded event", func() bool { mu.Lock(); defer mu.Unlock(); return len(received) == 1 })
	r := received[0]
	m := hmac.New(sha256.New, []byte("rzp-secret"))
	m.Write(r.body)
	if r.header.Get("X-Razorpay-Signature") != hex.EncodeToString(m.Sum(nil)) {
		t.Fatalf("provider signature doesn't verify: %v", r.header)
	}
	if d, err := relaya.VerifyDelivery(r.body, r.header, relaya.Secret(cfg.ListenSecret)); err != nil || d.EventType != "payment.captured" {
		t.Fatalf("relaya signature: %v %+v", err, d)
	}
	waitFor(t, "the log line", func() bool {
		return strings.Contains(logs.String(), "payment.captured (simulated)") && strings.Contains(logs.String(), "200 OK")
	})

	// Filtered out: another type, and another webhook.
	if err := run(ctx, []string{"trigger", "refund.processed", "--webhook", "Payments"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"trigger", "orders/create", "--webhook", "Store"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"trigger", "payment.failed", "--webhook", "Payments"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second forwarded event", func() bool { mu.Lock(); defer mu.Unlock(); return len(received) == 2 })
	time.Sleep(time.Second)
	mu.Lock()
	n := len(received)
	mu.Unlock()
	if n != 2 || !strings.Contains(string(received[1].body), "payment.failed") {
		t.Fatalf("forwarded %d events", n)
	}

	// trigger without --webhook when there are two is refused with the choices.
	if err := run(ctx, []string{"trigger", "payment.captured"}, nil, &out); err == nil || !strings.Contains(err.Error(), `"Payments"`) {
		t.Fatalf("ambiguous trigger: %v", err)
	}
}
