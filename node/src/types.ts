// Mirrors the Relaya API's JSON shapes. Field names are exactly what the API returns.

export type Role = 'owner' | 'admin' | 'member'

export interface User {
  id: string
  email: string
  name: string
}

export interface Org {
  id: string
  name: string
  slug: string
  plan: string
  role?: Role
  created_at: string
}

export interface Session {
  token: string
  expires_at: string
  user: User
}

export interface Member {
  user_id: string
  email: string
  name: string
  role: Role
  created_at: string
}

export interface Project {
  id: string
  org_id: string
  name: string
  slug: string
  created_at: string
}

export interface Webhook {
  id: string
  org_id: string
  project_id: string
  name: string
  provider: string
  ingest_url: string
  has_signing_secret: boolean
  signature_header?: string
  status: 'active' | 'paused'
  created_at: string
  updated_at: string
}

export type EventStatus = 'received' | 'rejected'
export type SignatureResult = 'valid' | 'invalid' | 'missing' | 'not_configured'

export interface EventSummary {
  id: string
  project_id: string
  webhook_id: string
  dedup_key: string
  type: string
  status: EventStatus
  signature: SignatureResult
  content_type: string
  payload_size: number
  received_at: string
  /** Forwarding state across destinations. */
  delivery: EventDeliveryState
  contract_status: EventContractStatus
}

export interface EventDetail extends EventSummary {
  deliveries: Delivery[]
  violations: Violation[]
  contract_id: string | null
  headers: Record<string, string>
  source_ip: string | null
  payload_json?: unknown
  payload_text?: string
  payload_base64?: string
}

export interface ApiKey {
  id: string
  name: string
  prefix: string
  role: Role
  last_used_at: string | null
  revoked_at: string | null
  created_at: string
}

export interface AuditEntry {
  id: number
  actor_type: 'user' | 'api_key' | 'system'
  actor_id: string
  action: string
  target_type: string
  target_id: string
  reason: string
  result: string
  metadata: Record<string, unknown>
  at: string
}

export interface List<T> {
  data: T[]
}

export interface Page<T> extends List<T> {
  next_cursor: string | null
}

export interface HourBucket {
  hour: string
  received: number
  rejected: number
}

export interface WebhookHealth {
  webhook_id: string
  received: number
  rejected: number
  last_received_at: string | null
}

export interface EventStats {
  hours: HourBucket[]
  totals: { received: number; rejected: number }
  webhooks: WebhookHealth[]
  forwarded: { succeeded: number; failed: number; in_progress: number }
  contracts: { open_incidents: number; breaking_24h: number; suspicious_24h: number }
}

export type EventDeliveryState = 'none' | 'pending' | 'delivered' | 'failed'
export type DeliveryStatus = 'pending' | 'in_flight' | 'retrying' | 'succeeded' | 'failed'

export interface Destination {
  id: string
  webhook_id: string
  name: string
  url: string
  enabled: boolean
  timeout_ms: number
  max_attempts: number
  /** Only these event types are forwarded here; empty means all. */
  event_types: string[]
  created_at: string
  updated_at: string
  stats: {
    succeeded_24h: number
    failed_24h: number
    retrying: number
    pending: number
    last_success_at: string | null
  }
}

export interface Delivery {
  id: string
  event_id: string
  webhook_id: string
  destination_id: string
  destination_name: string
  destination_url: string
  status: DeliveryStatus
  attempts: number
  max_attempts: number
  next_attempt_at: string | null
  last_status_code: number | null
  last_error: string
  last_attempt_at: string | null
  completed_at: string | null
  created_at: string
}

export interface DeliveryAttempt {
  attempt: number
  started_at: string
  duration_ms: number
  status_code: number | null
  error: string
  response_body: string
  outcome: 'succeeded' | 'retry' | 'failed'
}

export interface TestDeliveryResult {
  ok: boolean
  status_code: number
  duration_ms: number
  response_body: string
  error: string
}

export type EventContractStatus = 'none' | 'pending' | 'learning' | 'ok' | 'compatible' | 'suspicious' | 'breaking'
export type ContractState = 'learning' | 'proposed' | 'active'

export interface Contract {
  id: string
  webhook_id: string
  webhook_name: string
  event_type: string
  status: ContractState
  samples: number
  min_samples: number
  active_version: number | null
  fingerprint: string
  field_count: number
  critical_count: number
  new_fields: number
  suspicious_24h: number
  breaking_24h: number
  open_incidents: number
  first_seen_at: string
  last_seen_at: string
}

export interface ContractField {
  path: string
  types: string[] | null
  required: boolean
  enum?: string[]
  critical: boolean
  in_version: boolean
  observed_types: string[] | null
  observed_seen: number
}

export interface ContractVersion {
  version: number
  fingerprint: string
  critical_count: number
  created_by: string
  created_at: string
}

export interface Violation {
  event_id: string
  severity: 'suspicious' | 'breaking'
  kind: string
  path: string
  expected: string
  actual: string
  created_at: string
}

export interface ContractDetail {
  contract: Contract
  fields: ContractField[]
  observed_samples: number
  new_fields: Record<string, { count: number; first_seen: string; types: string }>
  versions: ContractVersion[]
  violations: Violation[]
}

export interface Incident {
  id: string
  webhook_id: string
  webhook_name: string
  contract_id: string
  event_type: string
  kind: string
  path: string
  severity: string
  status: 'open' | 'resolved'
  title: string
  expected: string
  actual: string
  event_count: number
  first_seen_at: string
  last_seen_at: string
  sample_event_id: string | null
  resolved_at: string | null
  resolution: string
  resolved_by: string
  replay: Replay | null
}

export interface Replay {
  id: string
  status: 'running' | 'completed'
  total: number
  succeeded: number
  failed: number
  created_by: string
  created_at: string
  completed_at: string | null
}

export interface ReplayPlan {
  events: number
  will_send: number
  destinations: {
    destination_id: string
    destination_name: string
    destination_url: string
    enabled: boolean
    deliveries: number
    already_succeeded: number
    in_flight: number
  }[]
}

export type AlertKind =
  | 'incident_opened'
  | 'incident_resolved'
  | 'destination_failing'
  | 'destination_recovered'
  | 'signature_failures'
  | 'connection_broken'
  | 'connection_recovered'
  | 'sync_failing'
  | 'sync_recovered'
export type AlertChannelType = 'slack' | 'email' | 'webhook'

export interface AlertChannel {
  id: string
  type: AlertChannelType
  name: string
  target: string
  events: AlertKind[]
  enabled: boolean
  created_at: string
  sent_7d: number
  failed_7d: number
}

export interface AlertSettings {
  kinds: AlertKind[]
  email_enabled: boolean
}

// ---- connections, proxy and syncs ------------------------------------------------

export interface Integration {
  id: string
  /** The name your code uses, e.g. "zoho". */
  key: string
  provider: string
  provider_name: string
  auth: 'oauth2' | 'login'
  name: string
  client_id: string
  has_client_secret: boolean
  scopes: string[]
  connections: number
  broken: number
  created_at: string
  updated_at: string
}

export interface Connection {
  id: string
  integration_id: string
  integration_key: string
  integration_name: string
  provider: string
  /** Your ID for the user or account that connected. */
  end_user_id: string
  /** "broken": the provider refused to renew access; the user must connect again. */
  status: 'active' | 'broken'
  expires_at: string | null
  last_refreshed_at: string | null
  refresh_failures: number
  last_error: string
  broken_at: string | null
  metadata: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface ConnectionToken {
  access_token: string
  token_type: string
  expires_at: string | null
  /** Where to call the provider's API with it, e.g. https://www.zohoapis.in. */
  api_base: string
  provider: string
  end_user_id: string
}

export interface ConnectLink {
  id: string
  /** Send the user here, or open it with connect.js (`Relaya.connect(url)`). Works once, for 30 minutes. */
  url: string
  expires_at: string
}

export interface ProxyResponse<T = unknown> {
  /** The provider's status code. */
  status: number
  ok: boolean
  headers: Headers
  /** Parsed JSON, or the text when the answer isn't JSON. */
  data: T
  /** How many times Relaya called the provider (retries, token renewal). */
  attempts: number
}

export interface ProxyCall {
  id: number
  connection_id: string
  end_user_id: string
  integration_name: string
  method: string
  host: string
  path: string
  status: number
  attempts: number
  duration_ms: number
  error: string
  created_at: string
}

export interface SyncModelField {
  key: string
  label: string
  help?: string
  placeholder?: string
  required: boolean
  options?: string[]
  default?: string
}

export interface SyncModel {
  /** e.g. "zoho.crm_records". */
  key: string
  provider: string
  name: string
  description: string
  fields: SyncModelField[]
  incremental: boolean
  verified: boolean
}

export interface Sync {
  id: string
  connection_id: string
  end_user_id: string
  integration_name: string
  provider: string
  /** The webhook its events are stored on; add destinations there to receive them. */
  webhook_id: string
  webhook_name: string
  model: string
  model_name: string
  config: Record<string, string>
  interval_minutes: number
  enabled: boolean
  emit_existing: boolean
  baseline_done: boolean
  running: boolean
  next_run_at: string
  last_run_at: string | null
  last_status: 'never' | 'ok' | 'error'
  last_error: string
  consecutive_failures: number
  records: number
  events: number
  created_at: string
}

export interface SyncRun {
  id: number
  started_at: string
  finished_at: string | null
  status: 'running' | 'ok' | 'error'
  fetched: number
  created: number
  updated: number
  error: string
}

export interface AlertLogEntry {
  id: number
  channel_id: string
  channel_name: string
  channel_type: AlertChannelType
  kind: AlertKind | 'test'
  title: string
  status: 'pending' | 'sent' | 'failed'
  attempts: number
  last_error: string
  created_at: string
  sent_at: string | null
}

// ---- outbound webhooks ------------------------------------------------------------

/** One of your customers, who receives webhooks from you. */
export interface OutboundApp {
  id: string
  /** Your ID for this customer. */
  uid: string
  name: string
  webhook_id: string
  endpoints: number
  messages_24h: number
  failed_24h: number
  created_at: string
}

/** A URL where one of your customers receives their events. */
export interface OutboundEndpoint {
  id: string
  url: string
  description: string
  /** Only these event types are sent here; empty means all. */
  event_types: string[]
  enabled: boolean
  created_at: string
  succeeded_24h: number
  failed_24h: number
  retrying: number
  last_success_at: string | null
}

export interface OutboundMessage {
  /** Also the webhook-id header your customer receives, the same on every retry. */
  id: string
  app: string
  event_type: string
  /** How many endpoints it was queued for. */
  endpoints: number
  /** True when the idempotency_key was seen before; nothing new was sent. */
  duplicate: boolean
}

export interface OutboundEventType {
  name: string
  description: string
  created_at?: string
}

export interface PortalLink {
  /** Open it for your customer; valid for 24 hours. */
  url: string
  expires_at: string
}
