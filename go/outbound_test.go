package relaya

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOutbound(t *testing.T) {
	var bodies []map[string]any
	record := func(r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
	}
	srv, seen := fakeAPI(t, map[string]http.HandlerFunc{
		"POST /v1/orgs/o/outbound/apps": func(w http.ResponseWriter, r *http.Request) {
			record(r)
			w.WriteHeader(201)
			writeJSON(w, map[string]any{"id": "a1", "uid": "cust:42"})
		},
		"GET /v1/orgs/o/outbound/apps/cust:42": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"app": map[string]string{"uid": "cust:42"}, "endpoints": []map[string]string{{"id": "ep1"}}})
		},
		"POST /v1/orgs/o/outbound/apps/cust:42/endpoints": func(w http.ResponseWriter, r *http.Request) {
			record(r)
			w.WriteHeader(201)
			writeJSON(w, map[string]any{"endpoint": map[string]string{"id": "ep1"}, "signing_secret": "whsec_x"})
		},
		"POST /v1/orgs/o/outbound/apps/cust:42/endpoints/ep1/test": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"ok": true})
		},
		"POST /v1/orgs/o/outbound/messages": func(w http.ResponseWriter, r *http.Request) {
			record(r)
			w.WriteHeader(202)
			writeJSON(w, map[string]any{"id": "m1", "endpoints": 1})
		},
		"POST /v1/orgs/o/outbound/apps/cust:42/portal-link": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(201)
			writeJSON(w, map[string]any{"url": "https://relaya.test/portal#ps_1"})
		},
	})
	c := New("rk", WithBaseURL(srv.URL), WithOrgID("o"))
	ctx := context.Background()

	if a, err := c.Outbound.Apps.Create(ctx, "cust:42", "Acme"); err != nil || a.ID != "a1" {
		t.Fatal(a, err)
	}
	ep, err := c.Outbound.Endpoints.Create(ctx, "cust:42", EndpointInput{URL: "https://acme.test/hooks", EventTypes: []string{"invoice.paid"}})
	if err != nil || ep.SigningSecret != "whsec_x" {
		t.Fatal(ep, err)
	}
	if eps, err := c.Outbound.Endpoints.List(ctx, "cust:42"); err != nil || len(eps) != 1 || eps[0].ID != "ep1" {
		t.Fatal(eps, err)
	}
	if r, err := c.Outbound.Endpoints.Test(ctx, "cust:42", "ep1", "invoice.paid"); err != nil || !r.OK {
		t.Fatal(r, err)
	}
	if got := (*seen)[len(*seen)-1].URL.Query().Get("event_type"); got != "invoice.paid" {
		t.Fatalf("event_type query %q", got)
	}
	m, err := c.Outbound.Send(ctx, Message{App: "cust:42", EventType: "invoice.paid", Payload: map[string]any{"id": "in_1"}, IdempotencyKey: "in_1"})
	if err != nil || m.ID != "m1" || m.Endpoints != 1 {
		t.Fatal(m, err)
	}
	if b := bodies[2]; b["app"] != "cust:42" || b["idempotency_key"] != "in_1" || b["payload"].(map[string]any)["id"] != "in_1" {
		t.Fatalf("message body %v", b)
	}
	if types := bodies[1]["event_types"].([]any); len(types) != 1 || types[0] != "invoice.paid" {
		t.Fatalf("endpoint body %v", bodies[1])
	}
	if l, err := c.Outbound.Apps.PortalLink(ctx, "cust:42"); err != nil || !strings.Contains(l.URL, "#ps_") {
		t.Fatal(l, err)
	}
}

func TestOutboundIntegration(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	// The customer's server, checking the Standard Webhooks signature by hand.
	type req struct {
		header http.Header
		body   []byte
	}
	var mu sync.Mutex
	var got []req
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, req{r.Header.Clone(), b})
		mu.Unlock()
	}))
	defer srv.Close()

	if _, err := c.Outbound.Apps.Create(ctx, "customer-1", "Customer One"); err != nil {
		t.Fatal(err)
	}
	ep, err := c.Outbound.Endpoints.Create(ctx, "customer-1", EndpointInput{URL: srv.URL + "/hooks", EventTypes: []string{"invoice.paid"}})
	if err != nil || !strings.HasPrefix(ep.SigningSecret, "whsec_") {
		t.Fatal(ep, err)
	}
	msg := Message{App: "customer-1", EventType: "invoice.paid", Payload: map[string]string{"invoice": "in_1"}, IdempotencyKey: "in_1"}
	m, err := c.Outbound.Send(ctx, msg)
	if err != nil || m.Endpoints != 1 {
		t.Fatal(m, err)
	}
	if again, err := c.Outbound.Send(ctx, msg); err != nil || !again.Duplicate || again.ID != m.ID {
		t.Fatal(again, err)
	}

	waitFor(t, "the message", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) > 0 })
	r := got[0]
	id, ts := r.header.Get("webhook-id"), r.header.Get("webhook-timestamp")
	k, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(ep.SigningSecret, "whsec_"))
	mac := hmac.New(sha256.New, k)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(r.body)
	want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if id != m.ID || !slices.Contains(strings.Fields(r.header.Get("webhook-signature")), want) {
		t.Fatalf("signature: id %s, got %q", id, r.header.Get("webhook-signature"))
	}

	if res, err := c.Outbound.Endpoints.Test(ctx, "customer-1", ep.Endpoint.ID, ""); err != nil || !res.OK {
		t.Fatal(res, err)
	}
	if l, err := c.Outbound.Apps.PortalLink(ctx, "customer-1"); err != nil || !strings.Contains(l.URL, "/portal#ps_") {
		t.Fatal(l, err)
	}
	if types, err := c.Outbound.EventTypes.List(ctx); err != nil || !slices.ContainsFunc(types, func(t OutboundEventType) bool { return t.Name == "invoice.paid" }) {
		t.Fatal(types, err)
	}
	if err := c.Outbound.Apps.Delete(ctx, "customer-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Outbound.Apps.Get(ctx, "customer-1"); !IsNotFound(err) {
		t.Fatalf("after delete: %v", err)
	}
}

// liveClient signs up on the Relaya at RELAYA_IT_API_URL and returns a client with a fresh admin API key.
func liveClient(t *testing.T) *Client {
	t.Helper()
	api := os.Getenv("RELAYA_IT_API_URL")
	if api == "" {
		t.Skip("set RELAYA_IT_API_URL to run")
	}
	ctx := context.Background()
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
	post("/v1/auth/signup", map[string]string{"email": fmt.Sprintf("go-sdk-%d@example.com", time.Now().UnixNano()), "password": "sdk-test-password-1", "org_name": "Go SDK"}, "", &session)
	var me struct{ Orgs []struct{ ID string } }
	if err := New(session.Token, WithBaseURL(api)).Do(ctx, "GET", "/v1/me", nil, nil, &me); err != nil {
		t.Fatal(err)
	}
	var key struct{ Key string }
	post("/v1/orgs/"+me.Orgs[0].ID+"/api-keys", map[string]string{"name": "sdk", "role": "admin"}, session.Token, &key)
	c := New(key.Key, WithBaseURL(api))
	return c
}
