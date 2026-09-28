package relaya

import (
	"encoding/json"
	"time"
)

// Types mirror the API's JSON. Rarely used nested shapes are left as json.RawMessage.

type List[T any] struct {
	Data []T `json:"data"`
}

type Project struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

type Webhook struct {
	ID               string    `json:"id"`
	OrgID            string    `json:"org_id"`
	ProjectID        string    `json:"project_id"`
	Name             string    `json:"name"`
	Provider         string    `json:"provider"`
	IngestURL        string    `json:"ingest_url"`
	HasSigningSecret bool      `json:"has_signing_secret"`
	SignatureHeader  string    `json:"signature_header,omitempty"`
	Status           string    `json:"status"` // active, paused
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type EventSummary struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	WebhookID      string    `json:"webhook_id"`
	DedupKey       string    `json:"dedup_key"`
	Type           string    `json:"type"`
	Status         string    `json:"status"`    // received, rejected
	Signature      string    `json:"signature"` // valid, invalid, missing, not_configured
	ContentType    string    `json:"content_type"`
	PayloadSize    int       `json:"payload_size"`
	ReceivedAt     time.Time `json:"received_at"`
	Delivery       string    `json:"delivery"`        // none, pending, delivered, failed
	ContractStatus string    `json:"contract_status"` // none, pending, learning, ok, compatible, suspicious, breaking
}

type EventPage struct {
	Data       []EventSummary `json:"data"`
	NextCursor *string        `json:"next_cursor"`
}

type EventDetail struct {
	EventSummary
	Deliveries    []DeliveryRecord  `json:"deliveries"`
	Violations    []Violation       `json:"violations"`
	ContractID    *string           `json:"contract_id"`
	Headers       map[string]string `json:"headers"`
	SourceIP      *string           `json:"source_ip"`
	PayloadJSON   json.RawMessage   `json:"payload_json,omitempty"` // sensitive fields masked
	PayloadText   *string           `json:"payload_text,omitempty"`
	PayloadBase64 *string           `json:"payload_base64,omitempty"`
}

// DeliveryRecord is a delivery as the API reports it (Delivery is a verified incoming request).
type DeliveryRecord struct {
	ID              string     `json:"id"`
	EventID         string     `json:"event_id"`
	WebhookID       string     `json:"webhook_id"`
	DestinationID   string     `json:"destination_id"`
	DestinationName string     `json:"destination_name"`
	DestinationURL  string     `json:"destination_url"`
	Status          string     `json:"status"` // pending, in_flight, retrying, succeeded, failed
	Attempts        int        `json:"attempts"`
	MaxAttempts     int        `json:"max_attempts"`
	NextAttemptAt   *time.Time `json:"next_attempt_at"`
	LastStatusCode  *int       `json:"last_status_code"`
	LastError       string     `json:"last_error"`
	LastAttemptAt   *time.Time `json:"last_attempt_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

type DeliveryAttempt struct {
	Attempt      int       `json:"attempt"`
	StartedAt    time.Time `json:"started_at"`
	DurationMS   int       `json:"duration_ms"`
	StatusCode   *int      `json:"status_code"`
	Error        string    `json:"error"`
	ResponseBody string    `json:"response_body"`
	Outcome      string    `json:"outcome"` // succeeded, retry, failed
}

type DeliveryWithAttempts struct {
	Delivery DeliveryRecord    `json:"delivery"`
	Attempts []DeliveryAttempt `json:"attempts"`
}

type Destination struct {
	ID          string    `json:"id"`
	WebhookID   string    `json:"webhook_id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Enabled     bool      `json:"enabled"`
	TimeoutMS   int       `json:"timeout_ms"`
	MaxAttempts int       `json:"max_attempts"`
	EventTypes  []string  `json:"event_types"` // only these are forwarded; empty means all
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Stats       struct {
		Succeeded24h  int        `json:"succeeded_24h"`
		Failed24h     int        `json:"failed_24h"`
		Retrying      int        `json:"retrying"`
		Pending       int        `json:"pending"`
		LastSuccessAt *time.Time `json:"last_success_at"`
	} `json:"stats"`
}

type CreatedDestination struct {
	Destination   Destination `json:"destination"`
	SigningSecret string      `json:"signing_secret"` // shown once
}

type TestResult struct {
	OK           bool   `json:"ok"`
	StatusCode   int    `json:"status_code"`
	DurationMS   int    `json:"duration_ms"`
	ResponseBody string `json:"response_body"`
	Error        string `json:"error"`
}

type Contract struct {
	ID            string    `json:"id"`
	WebhookID     string    `json:"webhook_id"`
	WebhookName   string    `json:"webhook_name"`
	EventType     string    `json:"event_type"`
	Status        string    `json:"status"` // learning, proposed, active
	Samples       int       `json:"samples"`
	MinSamples    int       `json:"min_samples"`
	ActiveVersion *int      `json:"active_version"`
	Fingerprint   string    `json:"fingerprint"`
	FieldCount    int       `json:"field_count"`
	CriticalCount int       `json:"critical_count"`
	NewFields     int       `json:"new_fields"`
	Suspicious24h int       `json:"suspicious_24h"`
	Breaking24h   int       `json:"breaking_24h"`
	OpenIncidents int       `json:"open_incidents"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}

type ContractField struct {
	Path          string   `json:"path"`
	Types         []string `json:"types"`
	Required      bool     `json:"required"`
	Enum          []string `json:"enum,omitempty"`
	Critical      bool     `json:"critical"`
	InVersion     bool     `json:"in_version"`
	ObservedTypes []string `json:"observed_types"`
	ObservedSeen  int      `json:"observed_seen"`
}

type ContractVersion struct {
	Version       int       `json:"version"`
	Fingerprint   string    `json:"fingerprint"`
	CriticalCount int       `json:"critical_count"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
}

type Violation struct {
	EventID   string    `json:"event_id"`
	Severity  string    `json:"severity"` // suspicious, breaking
	Kind      string    `json:"kind"`
	Path      string    `json:"path"`
	Expected  string    `json:"expected"`
	Actual    string    `json:"actual"`
	CreatedAt time.Time `json:"created_at"`
}

type ContractDetail struct {
	Contract        Contract          `json:"contract"`
	Fields          []ContractField   `json:"fields"`
	ObservedSamples int               `json:"observed_samples"`
	NewFields       json.RawMessage   `json:"new_fields"`
	Versions        []ContractVersion `json:"versions"`
	Violations      []Violation       `json:"violations"`
}

type Incident struct {
	ID            string     `json:"id"`
	WebhookID     string     `json:"webhook_id"`
	WebhookName   string     `json:"webhook_name"`
	ContractID    string     `json:"contract_id"`
	EventType     string     `json:"event_type"`
	Kind          string     `json:"kind"`
	Path          string     `json:"path"`
	Severity      string     `json:"severity"`
	Status        string     `json:"status"` // open, resolved
	Title         string     `json:"title"`
	Expected      string     `json:"expected"`
	Actual        string     `json:"actual"`
	EventCount    int        `json:"event_count"`
	FirstSeenAt   time.Time  `json:"first_seen_at"`
	LastSeenAt    time.Time  `json:"last_seen_at"`
	SampleEventID *string    `json:"sample_event_id"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	Resolution    string     `json:"resolution"`
	ResolvedBy    string     `json:"resolved_by"`
	Replay        *Replay    `json:"replay"`
}

type Replay struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"` // running, completed
	Total       int        `json:"total"`
	Succeeded   int        `json:"succeeded"`
	Failed      int        `json:"failed"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type ReplayPlan struct {
	Events       int `json:"events"`
	WillSend     int `json:"will_send"`
	Destinations []struct {
		DestinationID    string `json:"destination_id"`
		DestinationName  string `json:"destination_name"`
		DestinationURL   string `json:"destination_url"`
		Enabled          bool   `json:"enabled"`
		Deliveries       int    `json:"deliveries"`
		AlreadySucceeded int    `json:"already_succeeded"`
		InFlight         int    `json:"in_flight"`
	} `json:"destinations"`
}

type AlertChannel struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // slack, email, webhook
	Name      string    `json:"name"`
	Target    string    `json:"target"`
	Events    []string  `json:"events"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	Sent7d    int       `json:"sent_7d"`
	Failed7d  int       `json:"failed_7d"`
}

type AlertLogEntry struct {
	ID          int64      `json:"id"`
	ChannelID   string     `json:"channel_id"`
	ChannelName string     `json:"channel_name"`
	ChannelType string     `json:"channel_type"`
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Status      string     `json:"status"` // pending, sent, failed
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error"`
	CreatedAt   time.Time  `json:"created_at"`
	SentAt      *time.Time `json:"sent_at"`
}
