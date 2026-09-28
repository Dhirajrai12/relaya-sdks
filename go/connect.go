package relaya

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ---- connections: your users' accounts at other apps ----

// Integration is a provider you set up once (your OAuth app for Zoho, HubSpot or Google; nothing for Shiprocket).
type Integration struct {
	ID              string    `json:"id"`
	Key             string    `json:"key"` // the name your code uses, e.g. "zoho"
	Provider        string    `json:"provider"`
	ProviderName    string    `json:"provider_name"`
	Auth            string    `json:"auth"` // oauth2 or login
	Name            string    `json:"name"`
	ClientID        string    `json:"client_id"`
	HasClientSecret bool      `json:"has_client_secret"`
	Scopes          []string  `json:"scopes"`
	Connections     int       `json:"connections"`
	Broken          int       `json:"broken"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// IntegrationInput creates or updates an integration. On update, empty fields are left unchanged.
type IntegrationInput struct {
	Provider     string   `json:"provider,omitempty"` // create only: zoho, hubspot, google, shiprocket
	Key          string   `json:"key,omitempty"`      // create only; defaults to the provider
	Name         string   `json:"name,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

// Connection is one of your users' accounts at a provider.
type Connection struct {
	ID              string         `json:"id"`
	IntegrationID   string         `json:"integration_id"`
	IntegrationKey  string         `json:"integration_key"`
	IntegrationName string         `json:"integration_name"`
	Provider        string         `json:"provider"`
	EndUserID       string         `json:"end_user_id"` // your ID for the user or account that connected
	Status          string         `json:"status"`      // active, or broken: the user must connect again
	ExpiresAt       *time.Time     `json:"expires_at"`
	LastRefreshedAt *time.Time     `json:"last_refreshed_at"`
	RefreshFailures int            `json:"refresh_failures"`
	LastError       string         `json:"last_error"`
	BrokenAt        *time.Time     `json:"broken_at"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// ConnectLink is a one-time link (30 minutes) where a user connects their account.
type ConnectLink struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"` // open it with connect.js (Relaya.connect(url)) or redirect the user
	ExpiresAt time.Time `json:"expires_at"`
}

// ConnectionToken is a working access token. Use it right away rather than storing it.
type ConnectionToken struct {
	AccessToken string     `json:"access_token"`
	TokenType   string     `json:"token_type"`
	ExpiresAt   *time.Time `json:"expires_at"`
	APIBase     string     `json:"api_base"` // where to call the provider with it
	Provider    string     `json:"provider"`
	EndUserID   string     `json:"end_user_id"`
}

// RefreshResult reports a token renewal.
type RefreshResult struct {
	Connection Connection `json:"connection"`
	Refreshed  bool       `json:"refreshed"`
	Error      string     `json:"error"`
}

// ConnectionFilters narrows Connections.List; empty fields are ignored.
type ConnectionFilters struct {
	Integration string
	EndUserID   string
	Status      string // active or broken
}

type IntegrationsService struct{ c *Client }

func (s *IntegrationsService) List(ctx context.Context) ([]Integration, error) {
	l, err := get[List[Integration]](ctx, s.c, "/integrations", nil)
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}

// Create sets a provider up. Zoho, HubSpot and Google need your OAuth app's ClientID and ClientSecret.
func (s *IntegrationsService) Create(ctx context.Context, in IntegrationInput) (*Integration, error) {
	return send[Integration](ctx, s.c, http.MethodPost, "/integrations", in)
}
func (s *IntegrationsService) Update(ctx context.Context, id string, in IntegrationInput) (*Integration, error) {
	return send[Integration](ctx, s.c, http.MethodPatch, "/integrations/"+id, in)
}

// Delete removes the integration and its connections.
func (s *IntegrationsService) Delete(ctx context.Context, id string) error {
	return s.c.org(ctx, http.MethodDelete, "/integrations/"+id, nil, nil, nil)
}

type ConnectionsService struct{ c *Client }

// CreateLink returns a one-time link where your user (endUserID: their ID in your system) connects their account.
// returnURL may be empty.
func (s *ConnectionsService) CreateLink(ctx context.Context, integration, endUserID, returnURL string) (*ConnectLink, error) {
	body := map[string]string{"integration": integration, "end_user_id": endUserID}
	if returnURL != "" {
		body["return_url"] = returnURL
	}
	return send[ConnectLink](ctx, s.c, http.MethodPost, "/connect-sessions", body)
}
func (s *ConnectionsService) List(ctx context.Context, f ConnectionFilters) ([]Connection, error) {
	l, err := get[List[Connection]](ctx, s.c, "/connections", values("integration", f.Integration, "end_user_id", f.EndUserID, "status", f.Status))
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}
func (s *ConnectionsService) Get(ctx context.Context, id string) (*Connection, error) {
	return get[Connection](ctx, s.c, "/connections/"+id, nil)
}

// Find returns the connection for one of your users, or nil when they haven't connected.
func (s *ConnectionsService) Find(ctx context.Context, integration, endUserID string) (*Connection, error) {
	l, err := s.List(ctx, ConnectionFilters{Integration: integration, EndUserID: endUserID})
	if err != nil || len(l) == 0 {
		return nil, err
	}
	return &l[0], nil
}

// Token returns a working access token, renewed first when about to expire.
func (s *ConnectionsService) Token(ctx context.Context, id string) (*ConnectionToken, error) {
	return get[ConnectionToken](ctx, s.c, "/connections/"+id+"/token", nil)
}

// Refresh renews the token now, e.g. to check the connection works.
func (s *ConnectionsService) Refresh(ctx context.Context, id string) (*RefreshResult, error) {
	return send[RefreshResult](ctx, s.c, http.MethodPost, "/connections/"+id+"/refresh", nil)
}
func (s *ConnectionsService) Delete(ctx context.Context, id string) error {
	return s.c.org(ctx, http.MethodDelete, "/connections/"+id, nil, nil, nil)
}

// ---- proxy: call the provider's API as the connected user ----

// ProxyOptions tunes a proxy call. All fields are optional.
type ProxyOptions struct {
	Query       url.Values
	Headers     map[string]string // sent to the provider (as Relaya-Proxy-<name>); your API key never is
	Body        any               // JSON-encoded unless it is a string or []byte
	ContentType string            // default application/json
	BaseURL     string            // another API host of the same provider, e.g. https://sheets.googleapis.com
	Timeout     time.Duration     // default 120 s
}

// ProxyResponse is the provider's answer, errors included: check OK or Status.
type ProxyResponse struct {
	Status   int
	OK       bool
	Header   http.Header
	Body     []byte
	Attempts int // how many times Relaya called the provider (retries, token renewal)
}

// JSON decodes the provider's answer into v.
func (r *ProxyResponse) JSON(v any) error { return json.Unmarshal(r.Body, v) }

// Proxy calls a provider's API as a connected user; Relaya adds and renews the token and retries what
// is safe to retry.
//
//	res, err := c.Proxy(connID).Get(ctx, "/crm/v2/Leads", &relaya.ProxyOptions{Query: url.Values{"per_page": {"10"}}})
//
// The provider's own errors come back in the response. An *Error is returned only when Relaya couldn't
// make the call, e.g. Code "connection_broken": send the user a new link.
func (c *Client) Proxy(connectionID string) *Proxy { return &Proxy{c: c, id: connectionID} }

type Proxy struct {
	c  *Client
	id string
}

func (p *Proxy) Get(ctx context.Context, path string, o *ProxyOptions) (*ProxyResponse, error) {
	return p.Request(ctx, http.MethodGet, path, o)
}
func (p *Proxy) Delete(ctx context.Context, path string, o *ProxyOptions) (*ProxyResponse, error) {
	return p.Request(ctx, http.MethodDelete, path, o)
}
func (p *Proxy) Post(ctx context.Context, path string, body any, o *ProxyOptions) (*ProxyResponse, error) {
	return p.Request(ctx, http.MethodPost, path, withBody(o, body))
}
func (p *Proxy) Put(ctx context.Context, path string, body any, o *ProxyOptions) (*ProxyResponse, error) {
	return p.Request(ctx, http.MethodPut, path, withBody(o, body))
}
func (p *Proxy) Patch(ctx context.Context, path string, body any, o *ProxyOptions) (*ProxyResponse, error) {
	return p.Request(ctx, http.MethodPatch, path, withBody(o, body))
}

func withBody(o *ProxyOptions, body any) *ProxyOptions {
	out := ProxyOptions{}
	if o != nil {
		out = *o
	}
	out.Body = body
	return &out
}

// Request makes one call. It is not retried here: Relaya already retries what is safe to retry.
func (p *Proxy) Request(ctx context.Context, method, path string, o *ProxyOptions) (*ProxyResponse, error) {
	if o == nil {
		o = &ProxyOptions{}
	}
	org, err := p.c.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	u := p.c.baseURL + "/v1/orgs/" + org + "/connections/" + p.id + "/proxy/" + strings.TrimLeft(path, "/")
	if len(o.Query) > 0 {
		u += "?" + o.Query.Encode()
	}
	var body io.Reader
	contentType := ""
	switch b := o.Body.(type) {
	case nil:
	case string:
		body = strings.NewReader(b)
	case []byte:
		body = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	if body != nil {
		contentType = o.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "relaya-go/"+Version)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range o.Headers {
		req.Header.Set("Relaya-Proxy-"+k, v)
	}
	if o.BaseURL != "" {
		req.Header.Set("Relaya-Proxy-Base-Url", o.BaseURL)
	}
	hc := *p.c.httpClient
	hc.Timeout = 0 // the context bounds the call; provider calls may take up to 110 s
	res, err := hc.Do(req)
	if err != nil {
		return nil, &Error{Code: "network_error", Message: "could not reach Relaya: " + err.Error()}
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 50<<20))
	if err != nil {
		return nil, &Error{Status: res.StatusCode, Code: "network_error", Message: err.Error()}
	}
	// Relaya's own errors (broken connection, host not allowed, provider unreachable) are marked;
	// a 401 without Relaya-Proxy-Attempts is Relaya refusing the API key.
	if res.Header.Get("Relaya-Proxy-Error") == "true" || (res.StatusCode == http.StatusUnauthorized && res.Header.Get("Relaya-Proxy-Attempts") == "") {
		e := &Error{Status: res.StatusCode, Code: "proxy_error", Message: http.StatusText(res.StatusCode), RequestID: res.Header.Get("X-Request-Id")}
		var wrapped struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if json.Unmarshal(data, &wrapped) == nil && wrapped.Error.Code != "" {
			e.Code, e.Message = wrapped.Error.Code, wrapped.Error.Message
		}
		return nil, e
	}
	attempts, _ := strconv.Atoi(res.Header.Get("Relaya-Proxy-Attempts"))
	if attempts == 0 {
		attempts = 1
	}
	return &ProxyResponse{Status: res.StatusCode, OK: res.StatusCode >= 200 && res.StatusCode < 300, Header: res.Header, Body: data, Attempts: attempts}, nil
}

// ProxyCall is one logged call made through a connection (no query strings or bodies).
type ProxyCall struct {
	ID              int64     `json:"id"`
	ConnectionID    string    `json:"connection_id"`
	EndUserID       string    `json:"end_user_id"`
	IntegrationName string    `json:"integration_name"`
	Method          string    `json:"method"`
	Host            string    `json:"host"`
	Path            string    `json:"path"`
	Status          int       `json:"status"`
	Attempts        int       `json:"attempts"`
	DurationMS      int       `json:"duration_ms"`
	Error           string    `json:"error"`
	CreatedAt       time.Time `json:"created_at"`
}

type ProxyCallsService struct{ c *Client }

// List returns the last 100 calls, for one connection when connectionID is not empty.
func (s *ProxyCallsService) List(ctx context.Context, connectionID string) ([]ProxyCall, error) {
	l, err := get[List[ProxyCall]](ctx, s.c, "/proxy-calls", values("connection", connectionID))
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}

// ---- syncs: new and changed records in connected apps become events ----

type SyncModelField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Help        string   `json:"help"`
	Placeholder string   `json:"placeholder"`
	Required    bool     `json:"required"`
	Options     []string `json:"options"`
	Default     string   `json:"default"`
}

// SyncModel is something that can be synced, e.g. "zoho.crm_records".
type SyncModel struct {
	Key         string           `json:"key"`
	Provider    string           `json:"provider"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Fields      []SyncModelField `json:"fields"`
	Incremental bool             `json:"incremental"`
	Verified    bool             `json:"verified"`
}

type Sync struct {
	ID                  string            `json:"id"`
	ConnectionID        string            `json:"connection_id"`
	EndUserID           string            `json:"end_user_id"`
	IntegrationName     string            `json:"integration_name"`
	Provider            string            `json:"provider"`
	WebhookID           string            `json:"webhook_id"` // its events are stored here; add destinations to receive them
	WebhookName         string            `json:"webhook_name"`
	Model               string            `json:"model"`
	ModelName           string            `json:"model_name"`
	Config              map[string]string `json:"config"`
	IntervalMinutes     int               `json:"interval_minutes"`
	Enabled             bool              `json:"enabled"`
	EmitExisting        bool              `json:"emit_existing"`
	BaselineDone        bool              `json:"baseline_done"`
	Running             bool              `json:"running"`
	NextRunAt           time.Time         `json:"next_run_at"`
	LastRunAt           *time.Time        `json:"last_run_at"`
	LastStatus          string            `json:"last_status"` // never, ok or error
	LastError           string            `json:"last_error"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
	Records             int               `json:"records"`
	Events              int               `json:"events"`
	CreatedAt           time.Time         `json:"created_at"`
}

type SyncRun struct {
	ID         int64      `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Status     string     `json:"status"` // running, ok or error
	Fetched    int        `json:"fetched"`
	Created    int        `json:"created"`
	Updated    int        `json:"updated"`
	Error      string     `json:"error"`
}

// SyncInput starts a sync. Events land on a new webhook unless WebhookID is set.
type SyncInput struct {
	ConnectionID    string            `json:"connection_id"`
	Model           string            `json:"model"`
	Config          map[string]string `json:"config"`
	IntervalMinutes int               `json:"interval_minutes,omitempty"` // default 15
	WebhookID       string            `json:"webhook_id,omitempty"`
	EmitExisting    bool              `json:"emit_existing,omitempty"` // also send events for records that already exist
}

// SyncUpdate changes a sync; nil fields are left unchanged. A new Config starts the sync over.
type SyncUpdate struct {
	Enabled         *bool             `json:"enabled,omitempty"`
	IntervalMinutes int               `json:"interval_minutes,omitempty"`
	Config          map[string]string `json:"config,omitempty"`
}

type SyncsService struct{ c *Client }

// Models lists what can be synced, per provider, and the settings each needs.
func (s *SyncsService) Models(ctx context.Context) ([]SyncModel, error) {
	var l List[SyncModel]
	if err := s.c.Do(ctx, http.MethodGet, "/v1/connect/sync-models", nil, nil, &l); err != nil {
		return nil, err
	}
	return l.Data, nil
}
func (s *SyncsService) List(ctx context.Context) ([]Sync, error) {
	l, err := get[List[Sync]](ctx, s.c, "/syncs", nil)
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}

// Create starts syncing, e.g. SyncInput{ConnectionID: id, Model: "zoho.crm_records", Config: map[string]string{"module": "Leads"}}.
func (s *SyncsService) Create(ctx context.Context, in SyncInput) (*Sync, error) {
	if in.Config == nil {
		in.Config = map[string]string{}
	}
	return send[Sync](ctx, s.c, http.MethodPost, "/syncs", in)
}
func (s *SyncsService) Update(ctx context.Context, id string, in SyncUpdate) (*Sync, error) {
	return send[Sync](ctx, s.c, http.MethodPatch, "/syncs/"+id, in)
}
func (s *SyncsService) Delete(ctx context.Context, id string) error {
	return s.c.org(ctx, http.MethodDelete, "/syncs/"+id, nil, nil, nil)
}

// Run runs it within seconds instead of waiting for the schedule.
func (s *SyncsService) Run(ctx context.Context, id string) (*Sync, error) {
	return send[Sync](ctx, s.c, http.MethodPost, "/syncs/"+id+"/run", nil)
}

// Runs returns the last 50 runs.
func (s *SyncsService) Runs(ctx context.Context, id string) ([]SyncRun, error) {
	l, err := get[List[SyncRun]](ctx, s.c, "/syncs/"+id+"/runs", nil)
	if err != nil {
		return nil, err
	}
	return l.Data, nil
}
