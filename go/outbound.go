package relaya

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// ---- outbound webhooks: send events to your own customers ----

// OutboundApp is one of your customers, who receives webhooks from you.
type OutboundApp struct {
	ID          string    `json:"id"`
	UID         string    `json:"uid"` // your ID for this customer
	Name        string    `json:"name"`
	WebhookID   string    `json:"webhook_id"`
	Endpoints   int       `json:"endpoints"`
	Messages24h int       `json:"messages_24h"`
	Failed24h   int       `json:"failed_24h"`
	CreatedAt   time.Time `json:"created_at"`
}

// OutboundEndpoint is a URL where one of your customers receives their events.
type OutboundEndpoint struct {
	ID            string     `json:"id"`
	URL           string     `json:"url"`
	Description   string     `json:"description"`
	EventTypes    []string   `json:"event_types"` // only these are sent; empty means all
	Enabled       bool       `json:"enabled"`
	CreatedAt     time.Time  `json:"created_at"`
	Succeeded24h  int        `json:"succeeded_24h"`
	Failed24h     int        `json:"failed_24h"`
	Retrying      int        `json:"retrying"`
	LastSuccessAt *time.Time `json:"last_success_at"`
}

// CreatedEndpoint carries the signing secret (whsec_…) the customer verifies requests with.
type CreatedEndpoint struct {
	Endpoint      OutboundEndpoint `json:"endpoint"`
	SigningSecret string           `json:"signing_secret"`
}

// OutboundAppDetail is an app with its endpoints.
type OutboundAppDetail struct {
	App       OutboundApp        `json:"app"`
	Endpoints []OutboundEndpoint `json:"endpoints"`
}

// Message is what to send. Payload must encode to a JSON object.
type Message struct {
	App       string `json:"app"`
	EventType string `json:"event_type"`
	Payload   any    `json:"payload"`
	// IdempotencyKey makes sending the same message again return the first one instead of a copy.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// SentMessage reports a sent message.
type SentMessage struct {
	ID        string `json:"id"` // also the webhook-id header the customer receives, the same on every retry
	App       string `json:"app"`
	EventType string `json:"event_type"`
	Endpoints int    `json:"endpoints"` // how many endpoints it was queued for
	Duplicate bool   `json:"duplicate"` // the idempotency key was seen before; nothing new was sent
}

// EndpointInput creates or updates an endpoint. On update, empty fields and a nil EventTypes are left unchanged.
type EndpointInput struct {
	URL         string   `json:"url,omitempty"`
	Description string   `json:"description,omitempty"`
	EventTypes  []string `json:"event_types"` // only these are sent; an empty (non-nil) slice means all
	Enabled     *bool    `json:"enabled,omitempty"`
}

type OutboundEventType struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
}

// PortalLink is a 24-hour link where a customer manages their endpoints and sees deliveries.
type PortalLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// OutboundService sends webhooks to your customers. Relaya signs them (Standard Webhooks),
// retries failures and logs every attempt; each customer manages their endpoints in a portal.
type OutboundService struct {
	c          *Client
	Apps       *OutboundAppsService
	Endpoints  *OutboundEndpointsService
	EventTypes *OutboundEventTypesService
}

func newOutbound(c *Client) *OutboundService {
	return &OutboundService{c: c, Apps: &OutboundAppsService{c}, Endpoints: &OutboundEndpointsService{c}, EventTypes: &OutboundEventTypesService{c}}
}

// Send sends an event to every endpoint of that customer that takes its type.
func (s *OutboundService) Send(ctx context.Context, m Message) (*SentMessage, error) {
	return send[SentMessage](ctx, s.c, http.MethodPost, "/outbound/messages", m)
}

// OutboundAppsService manages apps: one per customer, referred to by your own uid (or its id).
type OutboundAppsService struct{ c *Client }

func appPath(app string) string { return "/outbound/apps/" + url.PathEscape(app) }

func (s *OutboundAppsService) List(ctx context.Context) ([]OutboundApp, error) {
	l, err := get[List[OutboundApp]](ctx, s.c, "/outbound/apps", nil)
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}

func (s *OutboundAppsService) Get(ctx context.Context, app string) (*OutboundAppDetail, error) {
	return get[OutboundAppDetail](ctx, s.c, appPath(app), nil)
}

// Create adds an app for a customer; name defaults to uid.
func (s *OutboundAppsService) Create(ctx context.Context, uid, name string) (*OutboundApp, error) {
	return send[OutboundApp](ctx, s.c, http.MethodPost, "/outbound/apps", map[string]string{"uid": uid, "name": name})
}

// Delete removes the app with its endpoints and message history.
func (s *OutboundAppsService) Delete(ctx context.Context, app string) error {
	return s.c.org(ctx, http.MethodDelete, appPath(app), nil, nil, nil)
}

// PortalLink returns a 24-hour link for the customer's portal.
func (s *OutboundAppsService) PortalLink(ctx context.Context, app string) (*PortalLink, error) {
	return send[PortalLink](ctx, s.c, http.MethodPost, appPath(app)+"/portal-link", nil)
}

// OutboundEndpointsService manages a customer's endpoints for them (they can also do it in the portal).
type OutboundEndpointsService struct{ c *Client }

func (s *OutboundEndpointsService) List(ctx context.Context, app string) ([]OutboundEndpoint, error) {
	d, err := s.c.Outbound.Apps.Get(ctx, app)
	if err != nil {
		return nil, err
	}
	return d.Endpoints, nil
}

func (s *OutboundEndpointsService) Create(ctx context.Context, app string, in EndpointInput) (*CreatedEndpoint, error) {
	return send[CreatedEndpoint](ctx, s.c, http.MethodPost, appPath(app)+"/endpoints", in)
}

func (s *OutboundEndpointsService) Update(ctx context.Context, app, id string, in EndpointInput) (*OutboundEndpoint, error) {
	return send[OutboundEndpoint](ctx, s.c, http.MethodPatch, appPath(app)+"/endpoints/"+id, in)
}

func (s *OutboundEndpointsService) Delete(ctx context.Context, app, id string) error {
	return s.c.org(ctx, http.MethodDelete, appPath(app)+"/endpoints/"+id, nil, nil, nil)
}

func (s *OutboundEndpointsService) Secret(ctx context.Context, app, id string) (string, error) {
	r, err := get[struct {
		SigningSecret string `json:"signing_secret"`
	}](ctx, s.c, appPath(app)+"/endpoints/"+id+"/secret", nil)
	if err != nil {
		return "", err
	}
	return r.SigningSecret, nil
}

// Test sends a signed test event now (eventType may be empty) and reports what the endpoint answered.
func (s *OutboundEndpointsService) Test(ctx context.Context, app, id, eventType string) (*TestResult, error) {
	var out TestResult
	if err := s.c.org(ctx, http.MethodPost, appPath(app)+"/endpoints/"+id+"/test", values("event_type", eventType), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// OutboundEventTypesService is the catalog customers pick from in the portal. Types you send are added automatically.
type OutboundEventTypesService struct{ c *Client }

func (s *OutboundEventTypesService) List(ctx context.Context) ([]OutboundEventType, error) {
	l, err := get[List[OutboundEventType]](ctx, s.c, "/outbound/event-types", nil)
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}

// Save adds the type, or updates its description.
func (s *OutboundEventTypesService) Save(ctx context.Context, name, description string) (*OutboundEventType, error) {
	return send[OutboundEventType](ctx, s.c, http.MethodPost, "/outbound/event-types", map[string]string{"name": name, "description": description})
}

func (s *OutboundEventTypesService) Delete(ctx context.Context, name string) error {
	return s.c.org(ctx, http.MethodDelete, "/outbound/event-types/"+url.PathEscape(name), nil, nil, nil)
}
