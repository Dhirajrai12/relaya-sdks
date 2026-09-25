package relaya

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const secret = "whsec_test_secret"

var body = []byte(`{"type":"payment.captured","amount":100,"note":"héllo"}`)

// sign mirrors the Relaya worker: hex HMAC-SHA256(secret, "<t>.<body>").
func sign(b []byte, s string, t time.Time) string {
	m := hmac.New(sha256.New, []byte(s))
	fmt.Fprintf(m, "%d.", t.Unix())
	m.Write(b)
	return fmt.Sprintf("t=%d,v1=%s", t.Unix(), hex.EncodeToString(m.Sum(nil)))
}

func reason(err error) string {
	var ve *VerificationError
	if errors.As(err, &ve) {
		return ve.Reason
	}
	return fmt.Sprint(err)
}

func TestVerifySignature(t *testing.T) {
	now := time.Now()
	if _, err := VerifySignature(body, sign(body, secret, now), Secret(secret)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, header string
		body         []byte
		want         string
	}{
		{"wrong secret", sign(body, "other", now), body, ReasonSignatureMismatch},
		{"tampered", sign(body, secret, now), append([]byte{}, append(body, ' ')...), ReasonSignatureMismatch},
		{"missing", "", body, ReasonMissingSignature},
		{"no v1", "t=1", body, ReasonMalformedSignature},
		{"bad t", "t=x,v1=ab", body, ReasonMalformedSignature},
		{"old", sign(body, secret, now.Add(-6*time.Minute)), body, ReasonTimestampOutOfRange},
		{"future", sign(body, secret, now.Add(6*time.Minute)), body, ReasonTimestampOutOfRange},
	}
	for _, c := range cases {
		if _, err := VerifySignature(c.body, c.header, Secret(secret)); reason(err) != c.want {
			t.Errorf("%s: got %v, want %s", c.name, err, c.want)
		}
	}
}

func TestToleranceAndRotation(t *testing.T) {
	old := time.Now().Add(-10 * time.Minute)
	if _, err := VerifySignature(body, sign(body, secret, old), VerifyOptions{Secrets: []string{secret}, Tolerance: 15 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySignature(body, sign(body, secret, old), VerifyOptions{Secrets: []string{secret}, Tolerance: -1}); err != nil {
		t.Fatal("negative tolerance should disable the check:", err)
	}
	fixed := time.Unix(1_700_000_000, 0)
	at, err := VerifySignature(body, sign(body, secret, fixed), VerifyOptions{Secrets: []string{secret}, Now: func() time.Time { return fixed.Add(time.Second) }})
	if err != nil || !at.Equal(fixed) {
		t.Fatal(at, err)
	}
	if _, err := VerifySignature(body, sign(body, "new", time.Now()), VerifyOptions{Secrets: []string{"old", "new"}}); err != nil {
		t.Fatal("rotation:", err)
	}
	if _, err := VerifySignature(body, sign(body, secret, time.Now()), VerifyOptions{}); err == nil {
		t.Fatal("no secret should error")
	}
}

func TestVerifyDeliveryHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("relaya-signature", sign(body, secret, time.Now())) // any case
	h.Set("Idempotency-Key", "dlv_1")
	h.Set("Relaya-Delivery-Id", "dlv_1")
	h.Set("Relaya-Event-Id", "evt_1")
	h.Set("Relaya-Attempt", "3")
	h.Set("Relaya-Event-Type", "payment.captured")
	d, err := VerifyDelivery(body, h, Secret(secret))
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Note string }
	if d.IdempotencyKey != "dlv_1" || d.EventID != "evt_1" || d.Attempt != 3 || d.EventType != "payment.captured" || d.ReplayID != "" || d.JSON(&v) != nil || v.Note != "héllo" {
		t.Fatalf("%+v %+v", d, v)
	}
}

func TestMiddleware(t *testing.T) {
	var got *Delivery
	var bodyAfter string
	h := Middleware(Secret(secret), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = DeliveryFromContext(r.Context())
		b, _ := io.ReadAll(r.Body)
		bodyAfter = string(b)
	}))
	req := httptest.NewRequest("POST", "/hooks", strings.NewReader(string(body)))
	req.Header.Set(HeaderSignature, sign(body, secret, time.Now()))
	req.Header.Set(HeaderEventID, "evt_2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || got == nil || got.EventID != "evt_2" || bodyAfter != string(body) {
		t.Fatalf("code %d, delivery %+v, body %q", rec.Code, got, bodyAfter)
	}

	got = nil
	req = httptest.NewRequest("POST", "/hooks", strings.NewReader(string(body)))
	req.Header.Set(HeaderSignature, sign(body, "other", time.Now()))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 || got != nil || !strings.Contains(rec.Body.String(), ReasonSignatureMismatch) {
		t.Fatalf("bad signature: %d %s", rec.Code, rec.Body)
	}
}

func TestVerifyAlert(t *testing.T) {
	a := []byte(`{"type":"test","title":"Test alert","body":"b","link":"https://x","org_id":"o","alert_id":7,"sent_at":"2026-09-26T00:00:00Z"}`)
	h := http.Header{}
	h.Set(HeaderSignature, sign(a, secret, time.Now()))
	got, err := VerifyAlert(a, h, Secret(secret))
	if err != nil || got.Title != "Test alert" || got.AlertID != 7 {
		t.Fatal(got, err)
	}
}
