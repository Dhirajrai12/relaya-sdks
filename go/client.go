// Package relaya verifies requests Relaya forwards to your endpoints and calls the Relaya API.
//
// Receiving:
//
//	http.Handle("/webhooks/relaya", relaya.Middleware(relaya.Secret(os.Getenv("RELAYA_SIGNING_SECRET")),
//		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			d, _ := relaya.DeliveryFromContext(r.Context())
//			// dedupe on d.IdempotencyKey, then handle d.Body
//		})))
//
// Calling the API:
//
//	c := relaya.New(os.Getenv("RELAYA_API_KEY"))
//	for e, err := range c.Events.All(ctx, relaya.EventFilters{ContractStatus: "breaking"}) { … }
package relaya

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is where the API lives until the product has its own domain.
const DefaultBaseURL = "https://server.aegonassett.com/api"

// Error is an error response from the API.
type Error struct {
	Status    int    // HTTP status; 0 when no response arrived
	Code      string // e.g. not_found, bad_request, network_error
	Message   string
	RequestID string
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return "relaya: " + e.Message
	}
	return fmt.Sprintf("relaya: %d %s: %s", e.Status, e.Code, e.Message)
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// Client calls the Relaya API. Create it with New; it is safe for concurrent use.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	maxRetries int

	orgMu sync.Mutex
	orgID string

	Projects     *ProjectsService
	Webhooks     *WebhooksService
	Events       *EventsService
	Destinations *DestinationsService
	Deliveries   *DeliveriesService
	Contracts    *ContractsService
	Incidents    *IncidentsService
	Alerts       *AlertsService
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another API (default RELAYA_BASE_URL, then DefaultBaseURL).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithOrgID sets the org. Optional with an API key (it belongs to one org); required with a session token.
func WithOrgID(id string) Option { return func(c *Client) { c.orgID = id } }

// WithHTTPClient replaces the HTTP client (default: 30 s timeout).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithMaxRetries sets retries for GET requests on network errors, 429 and 5xx (default 2).
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// New creates a client. An empty apiKey falls back to RELAYA_API_KEY.
func New(apiKey string, opts ...Option) *Client {
	if apiKey == "" {
		apiKey = os.Getenv("RELAYA_API_KEY")
	}
	base := os.Getenv("RELAYA_BASE_URL")
	if base == "" {
		base = DefaultBaseURL
	}
	c := &Client{baseURL: strings.TrimRight(base, "/"), apiKey: apiKey, httpClient: &http.Client{Timeout: 30 * time.Second}, maxRetries: 2}
	for _, o := range opts {
		o(c)
	}
	c.Projects = &ProjectsService{c}
	c.Webhooks = &WebhooksService{c}
	c.Events = &EventsService{c}
	c.Destinations = &DestinationsService{c}
	c.Deliveries = &DeliveriesService{c}
	c.Contracts = &ContractsService{c}
	c.Incidents = &IncidentsService{c}
	c.Alerts = &AlertsService{c}
	return c
}

// OrgID returns the org the client acts on, looking it up from the API key on first use.
func (c *Client) OrgID(ctx context.Context) (string, error) {
	c.orgMu.Lock()
	defer c.orgMu.Unlock()
	if c.orgID != "" {
		return c.orgID, nil
	}
	var me struct {
		APIKey *struct {
			OrgID string `json:"org_id"`
		} `json:"api_key"`
	}
	if err := c.Do(ctx, http.MethodGet, "/v1/me", nil, nil, &me); err != nil {
		return "", err
	}
	if me.APIKey == nil {
		return "", errors.New("relaya: use WithOrgID when authenticating with a session token")
	}
	c.orgID = me.APIKey.OrgID
	return c.orgID, nil
}

// Do sends a request to path (starting with /v1) and decodes the JSON response into out (may be nil).
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if c.apiKey == "" {
		return errors.New("relaya: no API key (pass one to New or set RELAYA_API_KEY)")
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	retries := 0
	if method == http.MethodGet {
		retries = c.maxRetries
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "relaya-go/"+Version)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() == nil && attempt < retries {
				if sleep(ctx, backoff(attempt, "")) == nil {
					continue
				}
			}
			return &Error{Code: "network_error", Message: err.Error()}
		}
		data, readErr := io.ReadAll(io.LimitReader(res.Body, 50<<20))
		res.Body.Close()
		if (res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500) && attempt < retries {
			if sleep(ctx, backoff(attempt, res.Header.Get("Retry-After"))) == nil {
				continue
			}
		}
		if readErr != nil {
			return &Error{Status: res.StatusCode, Code: "network_error", Message: readErr.Error()}
		}
		if res.StatusCode >= 300 {
			e := &Error{Status: res.StatusCode, Code: "http_error", Message: http.StatusText(res.StatusCode), RequestID: res.Header.Get("X-Request-Id")}
			var wrapped struct {
				Error struct{ Code, Message string } `json:"error"`
			}
			if json.Unmarshal(data, &wrapped) == nil && wrapped.Error.Code != "" {
				e.Code, e.Message = wrapped.Error.Code, wrapped.Error.Message
			}
			return e
		}
		if out == nil || res.StatusCode == http.StatusNoContent || len(data) == 0 {
			return nil
		}
		return json.Unmarshal(data, out)
	}
}

func (c *Client) org(ctx context.Context, method, path string, query url.Values, body, out any) error {
	id, err := c.OrgID(ctx)
	if err != nil {
		return err
	}
	return c.Do(ctx, method, "/v1/orgs/"+id+path, query, body, out)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func backoff(attempt int, retryAfter string) time.Duration {
	if s, err := strconv.Atoi(retryAfter); err == nil && s >= 0 {
		return min(time.Duration(s)*time.Second, 30*time.Second)
	}
	d := min(500*time.Millisecond<<attempt, 8*time.Second)
	return time.Duration(float64(d) * (0.8 + rand.Float64()*0.4))
}

func get[T any](ctx context.Context, c *Client, path string, q url.Values) (*T, error) {
	var out T
	if err := c.org(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func send[T any](ctx context.Context, c *Client, method, path string, body any) (*T, error) {
	var out T
	if err := c.org(ctx, method, path, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func values(kv ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			q.Set(kv[i], kv[i+1])
		}
	}
	return q
}

// ---- resources ----

type ProjectsService struct{ c *Client }

func (s *ProjectsService) List(ctx context.Context) ([]Project, error) {
	l, err := get[List[Project]](ctx, s.c, "/projects", nil)
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}
func (s *ProjectsService) Get(ctx context.Context, id string) (*Project, error) {
	return get[Project](ctx, s.c, "/projects/"+id, nil)
}
func (s *ProjectsService) Create(ctx context.Context, name string) (*Project, error) {
	return send[Project](ctx, s.c, http.MethodPost, "/projects", map[string]string{"name": name})
}

type WebhooksService struct{ c *Client }

type WebhookInput struct {
	ProjectID       string `json:"project_id,omitempty"`
	Name            string `json:"name,omitempty"`
	Provider        string `json:"provider,omitempty"` // generic, razorpay, stripe, shopify, github
	SigningSecret   string `json:"signing_secret,omitempty"`
	SignatureHeader string `json:"signature_header,omitempty"`
}

type WebhookUpdate struct {
	Name            *string `json:"name,omitempty"`
	Status          *string `json:"status,omitempty"` // active, paused
	SigningSecret   *string `json:"signing_secret,omitempty"`
	SignatureHeader *string `json:"signature_header,omitempty"`
}

func (s *WebhooksService) List(ctx context.Context, projectID string) ([]Webhook, error) {
	l, err := get[List[Webhook]](ctx, s.c, "/webhooks", values("project_id", projectID))
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}
func (s *WebhooksService) Get(ctx context.Context, id string) (*Webhook, error) {
	return get[Webhook](ctx, s.c, "/webhooks/"+id, nil)
}

// Create makes an inbound webhook; give its IngestURL to the provider.
func (s *WebhooksService) Create(ctx context.Context, in WebhookInput) (*Webhook, error) {
	return send[Webhook](ctx, s.c, http.MethodPost, "/webhooks", in)
}
func (s *WebhooksService) Update(ctx context.Context, id string, in WebhookUpdate) (*Webhook, error) {
	return send[Webhook](ctx, s.c, http.MethodPatch, "/webhooks/"+id, in)
}

// RotateURL issues a new ingest URL; the old one stops working.
func (s *WebhooksService) RotateURL(ctx context.Context, id string) (*Webhook, error) {
	return send[Webhook](ctx, s.c, http.MethodPost, "/webhooks/"+id+"/rotate-url", nil)
}
func (s *WebhooksService) Delete(ctx context.Context, id string) error {
	return s.c.org(ctx, http.MethodDelete, "/webhooks/"+id, nil, nil, nil)
}

type EventsService struct{ c *Client }

// EventFilters narrows Events.List and Events.All. Zero values are ignored.
type EventFilters struct {
	ProjectID, WebhookID, Type, DedupKey string
	Status                               string // received, rejected
	Signature                            string // valid, invalid, missing, not_configured
	ContractStatus                       string // ok, compatible, suspicious, breaking, …
	Since, Until                         time.Time
	Limit                                int // 1–200, default 50
	Cursor                               string
}

func (f EventFilters) values() url.Values {
	q := values("project_id", f.ProjectID, "webhook_id", f.WebhookID, "type", f.Type, "dedup_key", f.DedupKey,
		"status", f.Status, "signature", f.Signature, "contract_status", f.ContractStatus, "cursor", f.Cursor)
	if !f.Since.IsZero() {
		q.Set("since", f.Since.UTC().Format(time.RFC3339))
	}
	if !f.Until.IsZero() {
		q.Set("until", f.Until.UTC().Format(time.RFC3339))
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	return q
}

// List returns one page, newest first. Pass NextCursor back as Cursor for the next page.
func (s *EventsService) List(ctx context.Context, f EventFilters) (*EventPage, error) {
	return get[EventPage](ctx, s.c, "/events", f.values())
}

// All yields every matching event, newest first, fetching pages as needed.
func (s *EventsService) All(ctx context.Context, f EventFilters) iter.Seq2[EventSummary, error] {
	return func(yield func(EventSummary, error) bool) {
		for {
			page, err := s.List(ctx, f)
			if err != nil {
				yield(EventSummary{}, err)
				return
			}
			for _, e := range page.Data {
				if !yield(e, nil) {
					return
				}
			}
			if page.NextCursor == nil || *page.NextCursor == "" {
				return
			}
			f.Cursor = *page.NextCursor
		}
	}
}

// Get returns the full event: payload (sensitive fields masked), headers, deliveries, contract findings.
func (s *EventsService) Get(ctx context.Context, id string) (*EventDetail, error) {
	return get[EventDetail](ctx, s.c, "/events/"+id, nil)
}

type DestinationsService struct{ c *Client }

type DestinationInput struct {
	Name        string `json:"name,omitempty"`
	URL         string `json:"url,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
	TimeoutMS   int    `json:"timeout_ms,omitempty"`
}

func (s *DestinationsService) List(ctx context.Context, webhookID string) ([]Destination, error) {
	l, err := get[List[Destination]](ctx, s.c, "/webhooks/"+webhookID+"/destinations", nil)
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}

// Create adds a destination. The signing secret is returned once; store it where your endpoint can read it.
func (s *DestinationsService) Create(ctx context.Context, webhookID string, in DestinationInput) (*CreatedDestination, error) {
	return send[CreatedDestination](ctx, s.c, http.MethodPost, "/webhooks/"+webhookID+"/destinations", in)
}
func (s *DestinationsService) Update(ctx context.Context, id string, in DestinationInput) (*Destination, error) {
	return send[Destination](ctx, s.c, http.MethodPatch, "/destinations/"+id, in)
}
func (s *DestinationsService) Delete(ctx context.Context, id string) error {
	return s.c.org(ctx, http.MethodDelete, "/destinations/"+id, nil, nil, nil)
}
func (s *DestinationsService) RotateSecret(ctx context.Context, id string) (string, error) {
	r, err := send[struct {
		SigningSecret string `json:"signing_secret"`
	}](ctx, s.c, http.MethodPost, "/destinations/"+id+"/rotate-secret", nil)
	if err != nil {
		return "", err
	}
	return r.SigningSecret, nil
}

// Test sends a signed test request now and reports what the endpoint answered.
func (s *DestinationsService) Test(ctx context.Context, id string) (*TestResult, error) {
	return send[TestResult](ctx, s.c, http.MethodPost, "/destinations/"+id+"/test", nil)
}

type DeliveriesService struct{ c *Client }

// DeliveryFilters narrows Deliveries.List. Zero values are ignored.
type DeliveryFilters struct {
	EventID, DestinationID, WebhookID string
	Status                            string // pending, in_flight, retrying, succeeded, failed
}

// List returns the latest 100 matching deliveries.
func (s *DeliveriesService) List(ctx context.Context, f DeliveryFilters) ([]DeliveryRecord, error) {
	l, err := get[List[DeliveryRecord]](ctx, s.c, "/deliveries",
		values("event_id", f.EventID, "destination_id", f.DestinationID, "webhook_id", f.WebhookID, "status", f.Status))
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}
func (s *DeliveriesService) Get(ctx context.Context, id string) (*DeliveryWithAttempts, error) {
	return get[DeliveryWithAttempts](ctx, s.c, "/deliveries/"+id, nil)
}

// Retry sends a failed or retrying delivery again now.
func (s *DeliveriesService) Retry(ctx context.Context, id string) (*DeliveryRecord, error) {
	return send[DeliveryRecord](ctx, s.c, http.MethodPost, "/deliveries/"+id+"/retry", nil)
}

type ContractsService struct{ c *Client }

func (s *ContractsService) List(ctx context.Context, webhookID string) ([]Contract, error) {
	l, err := get[List[Contract]](ctx, s.c, "/contracts", values("webhook_id", webhookID))
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}
func (s *ContractsService) Get(ctx context.Context, id string) (*ContractDetail, error) {
	return get[ContractDetail](ctx, s.c, "/contracts/"+id, nil)
}

// VersionInput: Source is "observed" (what was learned) or "active" (the current version with new critical fields).
type VersionInput struct {
	CriticalFields []string `json:"critical_fields,omitempty"`
	Source         string   `json:"source,omitempty"`
}

// CreateVersion activates a new contract version and returns its number.
func (s *ContractsService) CreateVersion(ctx context.Context, id string, in VersionInput) (int, error) {
	r, err := send[struct {
		Version int `json:"version"`
	}](ctx, s.c, http.MethodPost, "/contracts/"+id+"/versions", in)
	if err != nil {
		return 0, err
	}
	return r.Version, nil
}

// Relearn throws away what was learned and starts learning again.
func (s *ContractsService) Relearn(ctx context.Context, id string) error {
	return s.c.org(ctx, http.MethodPost, "/contracts/"+id+"/relearn", nil, nil, nil)
}

type IncidentsService struct{ c *Client }

// List returns incidents; status is "open", "resolved" or "" for the default (open).
func (s *IncidentsService) List(ctx context.Context, status string) ([]Incident, error) {
	l, err := get[List[Incident]](ctx, s.c, "/incidents", values("status", status))
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}
func (s *IncidentsService) Resolve(ctx context.Context, id, resolution string) error {
	return s.c.org(ctx, http.MethodPost, "/incidents/"+id+"/resolve", nil, map[string]string{"resolution": resolution}, nil)
}

// PreviewReplay is a dry run: which events and deliveries a replay would resend. It changes nothing.
func (s *IncidentsService) PreviewReplay(ctx context.Context, id string) (*ReplayPlan, error) {
	return get[ReplayPlan](ctx, s.c, "/incidents/"+id+"/replay", nil)
}

// Replay resends the incident's deliveries. The incident resolves itself if all of them succeed.
func (s *IncidentsService) Replay(ctx context.Context, id string) (*Replay, error) {
	return send[Replay](ctx, s.c, http.MethodPost, "/incidents/"+id+"/replay", map[string]bool{"confirm": true})
}

type AlertsService struct{ c *Client }

func (s *AlertsService) Channels(ctx context.Context) ([]AlertChannel, error) {
	l, err := get[List[AlertChannel]](ctx, s.c, "/alert-channels", nil)
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}
func (s *AlertsService) Log(ctx context.Context) ([]AlertLogEntry, error) {
	l, err := get[List[AlertLogEntry]](ctx, s.c, "/alerts", nil)
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}
