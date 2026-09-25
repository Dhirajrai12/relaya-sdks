package relaya

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Headers Relaya sets on every forwarded request.
const (
	HeaderSignature      = "Relaya-Signature"
	HeaderIdempotencyKey = "Idempotency-Key"
	HeaderEventID        = "Relaya-Event-Id"
	HeaderDeliveryID     = "Relaya-Delivery-Id"
	HeaderAttempt        = "Relaya-Attempt"
	HeaderReplay         = "Relaya-Replay"
	HeaderEventType      = "Relaya-Event-Type"
)

// DefaultTolerance is how old a signature may be before it is rejected.
const DefaultTolerance = 5 * time.Minute

// MaxBodyBytes caps how much VerifyRequest and Middleware read.
const MaxBodyBytes = 10 << 20

// Reasons a request fails verification (VerificationError.Reason).
const (
	ReasonMissingSignature    = "missing_signature"
	ReasonMalformedSignature  = "malformed_signature"
	ReasonTimestampOutOfRange = "timestamp_out_of_range"
	ReasonSignatureMismatch   = "signature_mismatch"
)

// VerificationError means a request claiming to come from Relaya failed the signature check.
type VerificationError struct {
	Reason  string
	Message string
}

func (e *VerificationError) Error() string { return "relaya: " + e.Message }

// VerifyOptions configures signature checks.
type VerifyOptions struct {
	// Secrets are the destination's signing secrets. Pass several while rotating: any match passes.
	Secrets []string
	// Tolerance is the maximum signature age. Zero means DefaultTolerance; negative disables the check.
	Tolerance time.Duration
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Secret is a shorthand for VerifyOptions with one secret and the default tolerance.
func Secret(s string) VerifyOptions { return VerifyOptions{Secrets: []string{s}} }

// VerifySignature checks a Relaya-Signature header
// (t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<body>")>) and returns when it was signed.
func VerifySignature(body []byte, header string, opts VerifyOptions) (time.Time, error) {
	if len(opts.Secrets) == 0 {
		return time.Time{}, errors.New("relaya: a signing secret is required")
	}
	if header == "" {
		return time.Time{}, &VerificationError{ReasonMissingSignature, "the Relaya-Signature header is missing"}
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			if v != "" {
				sigs = append(sigs, strings.ToLower(v))
			}
		}
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return time.Time{}, &VerificationError{ReasonMalformedSignature, "the Relaya-Signature header is malformed"}
	}
	signedAt := time.Unix(unix, 0)

	tol := opts.Tolerance
	if tol == 0 {
		tol = DefaultTolerance
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	if tol > 0 {
		if age := now().Sub(signedAt); age > tol || age < -tol {
			return time.Time{}, &VerificationError{ReasonTimestampOutOfRange, fmt.Sprintf("the signature is older than %s (or from the future)", tol)}
		}
	}

	for _, secret := range opts.Secrets {
		if secret == "" {
			continue
		}
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(ts))
		m.Write([]byte("."))
		m.Write(body)
		expected := hex.EncodeToString(m.Sum(nil))
		for _, s := range sigs {
			if hmac.Equal([]byte(s), []byte(expected)) {
				return signedAt, nil
			}
		}
	}
	return time.Time{}, &VerificationError{ReasonSignatureMismatch, "the signature does not match: check the signing secret and that you pass the raw body"}
}

// Delivery is a verified request forwarded by Relaya.
type Delivery struct {
	// IdempotencyKey is the same across retries and replays of one delivery: dedupe on it.
	IdempotencyKey string
	DeliveryID     string
	EventID        string
	// EventType is the type Relaya detected (e.g. "payment.captured"), or "".
	EventType string
	// Attempt is 1 for the first try, then 2, 3… on retries.
	Attempt int
	// ReplayID is set when the request is part of an incident replay.
	ReplayID string
	SignedAt time.Time
	// Body is the provider's original body, byte for byte.
	Body []byte
}

// JSON decodes the body into v.
func (d *Delivery) JSON(v any) error { return json.Unmarshal(d.Body, v) }

// VerifyDelivery checks the signature and returns the delivery's details.
func VerifyDelivery(body []byte, h http.Header, opts VerifyOptions) (*Delivery, error) {
	signedAt, err := VerifySignature(body, h.Get(HeaderSignature), opts)
	if err != nil {
		return nil, err
	}
	attempt, _ := strconv.Atoi(h.Get(HeaderAttempt))
	if attempt < 1 {
		attempt = 1
	}
	d := &Delivery{
		IdempotencyKey: h.Get(HeaderIdempotencyKey),
		DeliveryID:     h.Get(HeaderDeliveryID),
		EventID:        h.Get(HeaderEventID),
		EventType:      h.Get(HeaderEventType),
		Attempt:        attempt,
		ReplayID:       h.Get(HeaderReplay),
		SignedAt:       signedAt,
		Body:           body,
	}
	if d.IdempotencyKey == "" {
		d.IdempotencyKey = d.DeliveryID
	}
	return d, nil
}

// VerifyRequest reads and verifies r's body. The body stays readable afterwards.
func VerifyRequest(r *http.Request, opts VerifyOptions) (*Delivery, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("relaya: reading body: %w", err)
	}
	if len(body) > MaxBodyBytes {
		return nil, fmt.Errorf("relaya: body larger than %d bytes", MaxBodyBytes)
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	return VerifyDelivery(body, r.Header, opts)
}

type ctxKey struct{}

// Middleware verifies each request before calling next; get the result with DeliveryFromContext.
// Requests with a bad signature get 400 {"error": "<reason>"}.
func Middleware(opts VerifyOptions, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, err := VerifyRequest(r, opts)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			var ve *VerificationError
			if errors.As(err, &ve) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, `{"error":%q}`, ve.Reason)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":"unreadable_body"}`)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, d)))
	})
}

// DeliveryFromContext returns the delivery Middleware verified.
func DeliveryFromContext(ctx context.Context) (*Delivery, bool) {
	d, ok := ctx.Value(ctxKey{}).(*Delivery)
	return d, ok
}

// Alert is what a webhook alert channel receives.
type Alert struct {
	Type    string    `json:"type"` // incident_opened, incident_resolved, destination_failing, destination_recovered, signature_failures, test
	Title   string    `json:"title"`
	Body    string    `json:"body"`
	Link    string    `json:"link"`
	OrgID   string    `json:"org_id"`
	AlertID int64     `json:"alert_id"`
	SentAt  time.Time `json:"sent_at"`
}

// VerifyAlert checks an alert's signature and decodes it.
func VerifyAlert(body []byte, h http.Header, opts VerifyOptions) (*Alert, error) {
	if _, err := VerifySignature(body, h.Get(HeaderSignature), opts); err != nil {
		return nil, err
	}
	var a Alert
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("relaya: decoding alert: %w", err)
	}
	return &a, nil
}
