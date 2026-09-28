package relaya

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
)

func TestConnectionsLinkFindToken(t *testing.T) {
	var linkBody map[string]string
	srv, seen := fakeAPI(t, map[string]http.HandlerFunc{
		"POST /v1/orgs/o/connect-sessions": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&linkBody)
			w.WriteHeader(201)
			writeJSON(w, map[string]any{"id": "s1", "url": "https://relaya.test/connect/cs_x"})
		},
		"GET /v1/orgs/o/connections": func(w http.ResponseWriter, r *http.Request) {
			data := []map[string]string{}
			if r.URL.Query().Get("end_user_id") == "u1" {
				data = append(data, map[string]string{"id": "c1", "end_user_id": "u1"})
			}
			writeJSON(w, map[string]any{"data": data})
		},
		"GET /v1/orgs/o/connections/c1/token": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"access_token": "at", "api_base": "https://www.zohoapis.in"})
		},
	})
	c := New("rk", WithBaseURL(srv.URL), WithOrgID("o"))
	ctx := context.Background()

	link, err := c.Connections.CreateLink(ctx, "zoho", "u1", "")
	if err != nil || link.URL != "https://relaya.test/connect/cs_x" {
		t.Fatal(link, err)
	}
	if linkBody["integration"] != "zoho" || linkBody["end_user_id"] != "u1" || linkBody["return_url"] != "" {
		t.Fatalf("link body %v", linkBody)
	}
	if conn, err := c.Connections.Find(ctx, "zoho", "u1"); err != nil || conn == nil || conn.ID != "c1" {
		t.Fatal(conn, err)
	}
	if conn, err := c.Connections.Find(ctx, "zoho", "nobody"); err != nil || conn != nil {
		t.Fatal(conn, err)
	}
	if q := (*seen)[1].URL.Query(); q.Get("integration") != "zoho" {
		t.Fatalf("query %v", q)
	}
	if tok, err := c.Connections.Token(ctx, "c1"); err != nil || tok.APIBase != "https://www.zohoapis.in" {
		t.Fatal(tok, err)
	}
}

func TestProxy(t *testing.T) {
	type call struct {
		path, query, body string
		header            http.Header
	}
	var calls []call
	srv, _ := fakeAPI(t, map[string]http.HandlerFunc{})
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.URL.Path, r.URL.RawQuery, string(b), r.Header.Clone()})
		switch r.URL.Path {
		case "/v1/orgs/o/connections/c1/proxy/crm/v2/Leads":
			w.Header().Set("Relaya-Proxy-Attempts", "2")
			writeJSON(w, map[string]any{"data": []map[string]string{{"id": "1"}}})
		case "/v1/orgs/o/connections/c1/proxy/missing":
			w.Header().Set("Relaya-Proxy-Attempts", "1")
			w.WriteHeader(404)
			writeJSON(w, map[string]string{"code": "INVALID_URL_PATTERN"})
		default:
			w.Header().Set("Relaya-Proxy-Error", "true")
			w.WriteHeader(409)
			writeJSON(w, map[string]any{"error": map[string]string{"code": "connection_broken", "message": "the user must connect again"}})
		}
	})
	c := New("rk", WithBaseURL(srv.URL), WithOrgID("o"))
	ctx := context.Background()
	zoho := c.Proxy("c1")

	res, err := zoho.Get(ctx, "/crm/v2/Leads", &ProxyOptions{Query: url.Values{"per_page": {"10"}}, Headers: map[string]string{"orgId": "42"}})
	if err != nil || !res.OK || res.Attempts != 2 {
		t.Fatal(res, err)
	}
	var leads struct{ Data []struct{ ID string } }
	if err := res.JSON(&leads); err != nil || leads.Data[0].ID != "1" {
		t.Fatal(leads, err)
	}
	if h := calls[0].header; calls[0].query != "per_page=10" || h.Get("Relaya-Proxy-OrgId") != "42" || h.Get("Authorization") != "Bearer rk" {
		t.Fatalf("first call %+v", calls[0])
	}

	if _, err := zoho.Post(ctx, "/crm/v2/Leads", map[string]any{"data": []map[string]string{{"Last_Name": "Rao"}}}, &ProxyOptions{BaseURL: "https://www.zohoapis.in"}); err != nil {
		t.Fatal(err)
	}
	if c := calls[1]; c.body != `{"data":[{"Last_Name":"Rao"}]}` || c.header.Get("Content-Type") != "application/json" || c.header.Get("Relaya-Proxy-Base-Url") != "https://www.zohoapis.in" {
		t.Fatalf("post %+v", c)
	}

	missing, err := zoho.Get(ctx, "/missing", nil)
	if err != nil || missing.OK || missing.Status != 404 {
		t.Fatal(missing, err)
	}

	_, err = zoho.Get(ctx, "/anything", nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != "connection_broken" || e.Status != 409 {
		t.Fatalf("broken: %v", err)
	}
}

func TestSyncs(t *testing.T) {
	var created map[string]any
	srv, _ := fakeAPI(t, map[string]http.HandlerFunc{
		"GET /v1/connect/sync-models": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"data": []map[string]string{{"key": "zoho.crm_records"}}})
		},
		"POST /v1/orgs/o/syncs": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(201)
			writeJSON(w, map[string]any{"id": "sy1", "model": created["model"]})
		},
		"POST /v1/orgs/o/syncs/sy1/run": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(202)
			writeJSON(w, map[string]any{"id": "sy1", "running": true})
		},
	})
	c := New("rk", WithBaseURL(srv.URL), WithOrgID("o"))
	ctx := context.Background()

	if m, err := c.Syncs.Models(ctx); err != nil || m[0].Key != "zoho.crm_records" {
		t.Fatal(m, err)
	}
	s, err := c.Syncs.Create(ctx, SyncInput{ConnectionID: "c1", Model: "zoho.crm_records", Config: map[string]string{"module": "Leads"}, IntervalMinutes: 15})
	if err != nil || s.ID != "sy1" {
		t.Fatal(s, err)
	}
	if created["connection_id"] != "c1" || created["interval_minutes"] != float64(15) || created["config"].(map[string]any)["module"] != "Leads" {
		t.Fatalf("created %v", created)
	}
	if s, err := c.Syncs.Run(ctx, "sy1"); err != nil || !s.Running {
		t.Fatal(s, err)
	}
}

// Against a live Relaya: everything short of a real provider account.
func TestConnectIntegration(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	in, err := c.Integrations.Create(ctx, IntegrationInput{Provider: "shiprocket"})
	if err != nil || in.Key != "shiprocket" {
		t.Fatal(in, err)
	}
	if l, err := c.Integrations.List(ctx); err != nil || len(l) != 1 {
		t.Fatal(l, err)
	}
	link, err := c.Connections.CreateLink(ctx, "shiprocket", "user-1", "")
	if err != nil || link.URL == "" {
		t.Fatal(link, err)
	}
	if conn, err := c.Connections.Find(ctx, "shiprocket", "user-1"); err != nil || conn != nil {
		t.Fatal("no connection yet:", conn, err)
	}
	if m, err := c.Syncs.Models(ctx); err != nil || len(m) == 0 {
		t.Fatal(m, err)
	}
	if l, err := c.Syncs.List(ctx); err != nil || len(l) != 0 {
		t.Fatal(l, err)
	}
	if l, err := c.ProxyCalls.List(ctx, ""); err != nil || len(l) != 0 {
		t.Fatal(l, err)
	}
	_, err = c.Proxy("00000000-0000-0000-0000-000000000000").Get(ctx, "/v1/external/orders", nil)
	var e *Error
	if !errors.As(err, &e) || e.Status != 404 {
		t.Fatalf("unknown connection: %v", err)
	}
	if err := c.Integrations.Delete(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
}
