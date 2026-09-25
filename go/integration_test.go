package relaya

// End-to-end against a running Relaya (API + ingest + worker). Skipped unless RELAYA_IT_API_URL is set:
//
//	RELAYA_IT_API_URL=http://127.0.0.1:18080 go test -run Integration
//
// The server must allow http://127.0.0.1 destinations (APP_ENV=dev) and use CONTRACT_MIN_SAMPLES=3.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"
)

func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	for until := time.Now().Add(15 * time.Second); !fn(); time.Sleep(150 * time.Millisecond) {
		if time.Now().After(until) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestIntegration(t *testing.T) {
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

	// Sign up and mint an API key (the dashboard's job).
	var session struct{ Token string }
	post("/v1/auth/signup", map[string]string{"email": fmt.Sprintf("go-sdk-%d@example.com", time.Now().UnixNano()), "password": "sdk-test-password-1", "org_name": "Go SDK"}, "", &session)
	boot := New(session.Token, WithBaseURL(api))
	var me struct{ Orgs []struct{ ID string } }
	if err := boot.Do(ctx, "GET", "/v1/me", nil, nil, &me); err != nil {
		t.Fatal(err)
	}
	var key struct{ Key string }
	post("/v1/orgs/"+me.Orgs[0].ID+"/api-keys", map[string]string{"name": "sdk", "role": "admin"}, session.Token, &key)

	c := New(key.Key, WithBaseURL(api))
	if org, err := c.OrgID(ctx); err != nil || org != me.Orgs[0].ID {
		t.Fatal(org, err)
	}

	// A customer endpoint verifying with the SDK middleware.
	var mu sync.Mutex
	var received []*Delivery
	failNext := 0
	opts := VerifyOptions{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		o := opts
		mu.Unlock()
		Middleware(o, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d, _ := DeliveryFromContext(r.Context())
			mu.Lock()
			defer mu.Unlock()
			received = append(received, d)
			if failNext > 0 {
				failNext--
				w.WriteHeader(500)
			}
		})).ServeHTTP(w, r)
	}))
	defer endpoint.Close()
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(received) }
	last := func() *Delivery { mu.Lock(); defer mu.Unlock(); return received[len(received)-1] }

	project, err := c.Projects.Create(ctx, "SDK")
	if err != nil {
		t.Fatal(err)
	}
	wh, err := c.Webhooks.Create(ctx, WebhookInput{ProjectID: project.ID, Name: "Payments", Provider: "generic"})
	if err != nil {
		t.Fatal(err)
	}
	dest, err := c.Destinations.Create(ctx, wh.ID, DestinationInput{Name: "My app", URL: endpoint.URL + "/hooks"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	opts = Secret(dest.SigningSecret)
	mu.Unlock()

	send := func(id string, body string) {
		req, _ := http.NewRequest("POST", wh.IngestURL, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Event-Id", id)
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("ingest %s: %v %v", id, err, res.Status)
		}
		res.Body.Close()
	}
	for i := 1; i <= 3; i++ {
		send(fmt.Sprintf("e%d", i), fmt.Sprintf(`{"type":"payment.captured","amount":%d}`, i*100))
	}

	waitFor(t, "3 deliveries", func() bool { return count() >= 3 })
	first := received[0]
	var payload struct{ Amount int }
	if first.IdempotencyKey != first.DeliveryID || first.Attempt != 1 || first.EventType != "payment.captured" || first.JSON(&payload) != nil || payload.Amount == 0 {
		t.Fatalf("%+v", first)
	}

	// Events: iterate across pages, get details.
	n := 0
	for e, err := range c.Events.All(ctx, EventFilters{WebhookID: wh.ID, Limit: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		_ = e
	}
	if n != 3 {
		t.Fatalf("iterated %d events", n)
	}
	detail, err := c.Events.Get(ctx, first.EventID)
	if err != nil || !bytes.Contains(detail.PayloadJSON, []byte("payment.captured")) {
		t.Fatal(detail, err)
	}

	// Deliveries: list, get with attempts.
	var ok []DeliveryRecord
	waitFor(t, "succeeded deliveries", func() bool {
		ok, _ = c.Deliveries.List(ctx, DeliveryFilters{WebhookID: wh.ID, Status: "succeeded"})
		return len(ok) == 3
	})
	one, err := c.Deliveries.Get(ctx, ok[0].ID)
	if err != nil || one.Attempts[0].Outcome != "succeeded" {
		t.Fatal(one, err)
	}

	// A failing endpoint, then a manual retry.
	mu.Lock()
	failNext = 1
	mu.Unlock()
	send("e4", `{"type":"payment.captured","amount":400}`)
	var retrying []DeliveryRecord
	waitFor(t, "a retrying delivery", func() bool {
		retrying, _ = c.Deliveries.List(ctx, DeliveryFilters{WebhookID: wh.ID, Status: "retrying"})
		return len(retrying) == 1
	})
	if _, err := c.Deliveries.Retry(ctx, retrying[0].ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the retry to succeed", func() bool {
		d, _ := c.Deliveries.Get(ctx, retrying[0].ID)
		return d != nil && d.Delivery.Status == "succeeded"
	})
	if l := last(); l.Attempt != 2 || l.IdempotencyKey != retrying[0].ID {
		t.Fatalf("%+v", l)
	}

	// Contract → incident → replay.
	var contract Contract
	waitFor(t, "a proposed contract", func() bool {
		cs, _ := c.Contracts.List(ctx, wh.ID)
		for _, x := range cs {
			if x.Status != "learning" {
				contract = x
				return true
			}
		}
		return false
	})
	if _, err := c.Contracts.CreateVersion(ctx, contract.ID, VersionInput{CriticalFields: []string{"amount"}, Source: "observed"}); err != nil {
		t.Fatal(err)
	}
	send("e5", `{"type":"payment.captured","amount":"500"}`)
	var incidents []Incident
	waitFor(t, "an open incident", func() bool {
		incidents, _ = c.Incidents.List(ctx, "open")
		return len(incidents) == 1
	})
	if !regexp.MustCompile(`amount changed type`).MatchString(incidents[0].Title) {
		t.Fatal(incidents[0].Title)
	}
	plan, err := c.Incidents.PreviewReplay(ctx, incidents[0].ID)
	if err != nil || plan.Events != 1 {
		t.Fatal(plan, err)
	}
	before := count()
	replay, err := c.Incidents.Replay(ctx, incidents[0].ID)
	if err != nil || replay.Total != 1 {
		t.Fatal(replay, err)
	}
	waitFor(t, "the replayed delivery", func() bool { return count() > before })
	if last().ReplayID != replay.ID {
		t.Fatalf("replay id %q, want %q", last().ReplayID, replay.ID)
	}
	waitFor(t, "the incident to resolve", func() bool {
		open, _ := c.Incidents.List(ctx, "open")
		return len(open) == 0
	})

	// Destination test and API errors.
	if r, err := c.Destinations.Test(ctx, dest.Destination.ID); err != nil || !r.OK {
		t.Fatal(r, err)
	}
	if _, err := c.Webhooks.Get(ctx, "00000000-0000-0000-0000-000000000000"); !IsNotFound(err) {
		t.Fatal("want 404:", err)
	}
	var e *Error
	if _, err := c.Deliveries.Retry(ctx, ok[0].ID); !asError(err, &e) || e.Status != 409 {
		t.Fatal("want 409:", err)
	}
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
