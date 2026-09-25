package relaya

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func fakeAPI(t *testing.T, routes map[string]http.HandlerFunc) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		if h, ok := routes[r.Method+" "+r.URL.Path]; ok {
			h(w, r)
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"code":"not_found","message":"no route"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func writeJSON(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }

func TestOrgLookupOnceAndAuth(t *testing.T) {
	srv, seen := fakeAPI(t, map[string]http.HandlerFunc{
		"GET /v1/me": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"api_key": map[string]string{"org_id": "org1"}})
		},
		"GET /v1/orgs/org1/projects": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"data": []map[string]string{{"id": "p1"}}})
		},
	})
	c := New("rk_test", WithBaseURL(srv.URL+"/"))
	ctx := context.Background()
	for range 2 {
		ps, err := c.Projects.List(ctx)
		if err != nil || len(ps) != 1 || ps[0].ID != "p1" {
			t.Fatal(ps, err)
		}
	}
	meCalls := 0
	for _, r := range *seen {
		if r.URL.Path == "/v1/me" {
			meCalls++
		}
		if r.Header.Get("Authorization") != "Bearer rk_test" {
			t.Fatal("missing auth header")
		}
	}
	if meCalls != 1 {
		t.Fatalf("me called %d times", meCalls)
	}
}

func TestEventFiltersAndPaging(t *testing.T) {
	srv, seen := fakeAPI(t, map[string]http.HandlerFunc{
		"GET /v1/orgs/o/events": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("cursor") == "c2" {
				writeJSON(w, map[string]any{"data": []map[string]string{{"id": "e3"}}, "next_cursor": nil})
				return
			}
			writeJSON(w, map[string]any{"data": []map[string]string{{"id": "e1"}, {"id": "e2"}}, "next_cursor": "c2"})
		},
	})
	c := New("rk", WithBaseURL(srv.URL), WithOrgID("o"))
	var ids []string
	for e, err := range c.Events.All(context.Background(), EventFilters{ContractStatus: "breaking", Since: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Limit: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	if len(ids) != 3 || ids[2] != "e3" {
		t.Fatal(ids)
	}
	q := (*seen)[0].URL.Query()
	if q.Get("contract_status") != "breaking" || q.Get("since") != "2026-09-01T00:00:00Z" || q.Get("limit") != "2" || q.Has("type") {
		t.Fatal(q)
	}
}

func TestErrorsAndRetries(t *testing.T) {
	var gets, posts atomic.Int32
	srv, _ := fakeAPI(t, map[string]http.HandlerFunc{
		"GET /v1/orgs/o/incidents": func(w http.ResponseWriter, r *http.Request) {
			if gets.Add(1) < 3 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(503)
				return
			}
			writeJSON(w, map[string]any{"data": []any{}})
		},
		"POST /v1/orgs/o/incidents/i1/replay": func(w http.ResponseWriter, r *http.Request) {
			posts.Add(1)
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
		},
	})
	c := New("rk", WithBaseURL(srv.URL), WithOrgID("o"))
	ctx := context.Background()
	if _, err := c.Incidents.List(ctx, "open"); err != nil {
		t.Fatal("GET should be retried:", err)
	}
	_, err := c.Incidents.Replay(ctx, "i1")
	var e *Error
	if !errors.As(err, &e) || e.Status != 503 || posts.Load() != 1 {
		t.Fatalf("POST must not be retried: %v, %d calls", err, posts.Load())
	}
	if _, err := c.Webhooks.Get(ctx, "nope"); !IsNotFound(err) {
		t.Fatal("want not found:", err)
	}
	dead := New("rk", WithBaseURL("http://127.0.0.1:1"), WithOrgID("o"), WithMaxRetries(0))
	if _, err := dead.Projects.List(ctx); !errors.As(err, &e) || e.Code != "network_error" {
		t.Fatal("want network_error:", err)
	}
}
