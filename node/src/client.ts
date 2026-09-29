import { RelayaError } from './errors.ts'
import type {
  AlertChannel,
  AlertLogEntry,
  ConnectLink,
  Connection,
  ConnectionToken,
  Contract,
  ContractDetail,
  Delivery,
  DeliveryAttempt,
  DeliveryStatus,
  Destination,
  EventContractStatus,
  EventDetail,
  EventStatus,
  EventSummary,
  Incident,
  Integration,
  List,
  Page,
  Project,
  ProxyCall,
  ProxyResponse,
  Replay,
  ReplayPlan,
  SignatureResult,
  Sync,
  SyncModel,
  SyncRun,
  TestDeliveryResult,
  Webhook,
  OutboundApp,
  OutboundEndpoint,
  OutboundEventType,
  OutboundMessage,
  PortalLink,
} from './types.ts'

/** Where the API lives until the product has its own domain. Override with `baseUrl` or RELAYA_BASE_URL. */
export const DEFAULT_BASE_URL = 'https://api.relaya.sbs'

export interface RelayaOptions {
  /** An API key (`rk_…`) from Settings → API keys. Defaults to the RELAYA_API_KEY environment variable. */
  apiKey?: string
  /** The org to act on. Optional with an API key (it belongs to one org); required with a session token. */
  orgId?: string
  /** Defaults to RELAYA_BASE_URL, then the hosted API. */
  baseUrl?: string
  /** Per-request timeout. Default 30 s. */
  timeoutMs?: number
  /** Retries for GET requests on network errors, 429 and 5xx. Default 2. */
  maxRetries?: number
  /** Custom fetch (tests, proxies). */
  fetch?: typeof fetch
}

export interface EventFilters {
  project_id?: string
  webhook_id?: string
  type?: string
  status?: EventStatus
  signature?: SignatureResult
  contract_status?: EventContractStatus
  dedup_key?: string
  /** Only events received at or after this time. */
  since?: Date | string
  /** Only events received before this time. */
  until?: Date | string
  /** Page size, 1–200. Default 50. */
  limit?: number
  cursor?: string
}

export interface DeliveryFilters {
  event_id?: string
  destination_id?: string
  webhook_id?: string
  status?: DeliveryStatus
}

type Query = Record<string, string | number | Date | undefined | null>

function env(name: string): string | undefined {
  return typeof process !== 'undefined' ? process.env?.[name] : undefined
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

/**
 * Relaya API client.
 *
 *   const relaya = new Relaya({ apiKey: process.env.RELAYA_API_KEY })
 *   for await (const e of relaya.events.iterate({ contract_status: 'breaking' })) console.log(e.type)
 */
export class Relaya {
  readonly baseUrl: string
  private readonly apiKey: string
  private readonly timeoutMs: number
  private readonly maxRetries: number
  private readonly fetchImpl: typeof fetch
  private orgPromise: Promise<string> | null

  constructor(opts: RelayaOptions = {}) {
    const apiKey = opts.apiKey ?? env('RELAYA_API_KEY')
    if (!apiKey) throw new TypeError('Relaya: pass { apiKey } or set RELAYA_API_KEY')
    this.apiKey = apiKey
    this.baseUrl = (opts.baseUrl ?? env('RELAYA_BASE_URL') ?? DEFAULT_BASE_URL).replace(/\/+$/, '')
    this.timeoutMs = opts.timeoutMs ?? 30_000
    this.maxRetries = opts.maxRetries ?? 2
    this.fetchImpl = opts.fetch ?? globalThis.fetch.bind(globalThis)
    this.orgPromise = opts.orgId ? Promise.resolve(opts.orgId) : null
  }

  /** The org this client acts on (looked up from the API key on first use). */
  orgId(): Promise<string> {
    this.orgPromise ??= this.request<{ api_key?: { org_id: string } }>('GET', '/v1/me').then(
      (me) => {
        if (!me.api_key) throw new TypeError('Relaya: pass { orgId } when using a session token instead of an API key')
        return me.api_key.org_id
      },
      (err) => {
        this.orgPromise = null // let the next call try again
        throw err
      },
    )
    return this.orgPromise
  }

  /** Low-level request against the API; paths start with /v1. Use the resource helpers where you can. */
  async request<T>(method: string, path: string, body?: unknown, query?: Query): Promise<T> {
    const url = new URL(this.baseUrl + path)
    for (const [k, v] of Object.entries(query ?? {})) {
      if (v !== undefined && v !== null && v !== '') url.searchParams.set(k, v instanceof Date ? v.toISOString() : String(v))
    }
    const retries = method === 'GET' ? this.maxRetries : 0
    for (let attempt = 0; ; attempt++) {
      let res: Response
      try {
        res = await this.fetchImpl(url, {
          method,
          headers: {
            Authorization: `Bearer ${this.apiKey}`,
            Accept: 'application/json',
            ...(body !== undefined && { 'Content-Type': 'application/json' }),
          },
          body: body === undefined ? undefined : JSON.stringify(body),
          signal: AbortSignal.timeout(this.timeoutMs),
        })
      } catch (err) {
        if (attempt < retries) {
          await sleep(backoff(attempt, null))
          continue
        }
        const timedOut = err instanceof Error && err.name === 'TimeoutError'
        throw new RelayaError(0, timedOut ? 'timeout' : 'network_error', timedOut ? `Request timed out after ${this.timeoutMs} ms` : `Could not reach Relaya: ${(err as Error).message}`)
      }
      if ((res.status === 429 || res.status >= 500) && attempt < retries) {
        await res.body?.cancel()
        await sleep(backoff(attempt, res.headers.get('retry-after')))
        continue
      }
      if (res.status === 204) return undefined as T
      const text = await res.text()
      let data: unknown = null
      try {
        data = text ? JSON.parse(text) : null
      } catch {
        // not JSON (e.g. a proxy error page)
      }
      if (!res.ok) {
        const e = (data as { error?: { code?: string; message?: string } } | null)?.error
        throw new RelayaError(res.status, e?.code ?? 'http_error', e?.message ?? `HTTP ${res.status}`, res.headers.get('x-request-id'))
      }
      return data as T
    }
  }

  private async org<T>(method: string, path: string, body?: unknown, query?: Query): Promise<T> {
    return this.request<T>(method, `/v1/orgs/${await this.orgId()}${path}`, body, query)
  }

  readonly projects = {
    list: () => this.org<List<Project>>('GET', '/projects'),
    get: (id: string) => this.org<Project>('GET', `/projects/${id}`),
    create: (input: { name: string }) => this.org<Project>('POST', '/projects', input),
  }

  readonly webhooks = {
    list: (filters: { project_id?: string } = {}) => this.org<List<Webhook>>('GET', '/webhooks', undefined, filters),
    get: (id: string) => this.org<Webhook>('GET', `/webhooks/${id}`),
    /** Creates an inbound webhook; give its `ingest_url` to the provider. */
    create: (input: { project_id: string; name: string; provider: string; signing_secret?: string; signature_header?: string }) =>
      this.org<Webhook>('POST', '/webhooks', input),
    update: (id: string, input: { name?: string; status?: 'active' | 'paused'; signing_secret?: string; signature_header?: string }) =>
      this.org<Webhook>('PATCH', `/webhooks/${id}`, input),
    /** Issues a new ingest URL; the old one stops working. */
    rotateUrl: (id: string) => this.org<Webhook>('POST', `/webhooks/${id}/rotate-url`),
    delete: (id: string) => this.org<void>('DELETE', `/webhooks/${id}`),
  }

  readonly events = {
    /** One page, newest first. Pass `next_cursor` back as `cursor` for the next page. */
    list: (filters: EventFilters = {}) => this.org<Page<EventSummary>>('GET', '/events', undefined, { ...filters }),
    /** Every matching event, newest first, fetching pages as you go. */
    iterate: (filters: Omit<EventFilters, 'cursor'> = {}): AsyncIterable<EventSummary> => {
      const list = this.events.list
      return {
        async *[Symbol.asyncIterator]() {
          let cursor: string | undefined
          do {
            const page = await list({ ...filters, cursor })
            yield* page.data
            cursor = page.next_cursor ?? undefined
          } while (cursor)
        },
      }
    },
    /** Full event: payload (sensitive fields masked), headers, deliveries and contract findings. */
    get: (id: string) => this.org<EventDetail>('GET', `/events/${id}`),
  }

  readonly destinations = {
    list: (webhookId: string) => this.org<List<Destination>>('GET', `/webhooks/${webhookId}/destinations`),
    /** Returns the signing secret once; store it where your endpoint can read it. */
    create: (webhookId: string, input: { name: string; url: string; max_attempts?: number; timeout_ms?: number; enabled?: boolean; event_types?: string[] }) =>
      this.org<{ destination: Destination; signing_secret: string }>('POST', `/webhooks/${webhookId}/destinations`, input),
    update: (id: string, input: { name?: string; url?: string; enabled?: boolean; max_attempts?: number; timeout_ms?: number; event_types?: string[] }) =>
      this.org<Destination>('PATCH', `/destinations/${id}`, input),
    delete: (id: string) => this.org<void>('DELETE', `/destinations/${id}`),
    rotateSecret: (id: string) => this.org<{ signing_secret: string }>('POST', `/destinations/${id}/rotate-secret`),
    /** Sends a signed test request now and reports what the endpoint answered. */
    test: (id: string) => this.org<TestDeliveryResult>('POST', `/destinations/${id}/test`),
  }

  readonly deliveries = {
    /** The latest 100 matching deliveries. */
    list: (filters: DeliveryFilters = {}) => this.org<List<Delivery>>('GET', '/deliveries', undefined, { ...filters }),
    get: (id: string) => this.org<{ delivery: Delivery; attempts: DeliveryAttempt[] }>('GET', `/deliveries/${id}`),
    /** Sends a failed delivery again now. */
    retry: (id: string) => this.org<Delivery>('POST', `/deliveries/${id}/retry`),
  }

  readonly contracts = {
    list: (filters: { webhook_id?: string } = {}) => this.org<List<Contract>>('GET', '/contracts', undefined, filters),
    get: (id: string) => this.org<ContractDetail>('GET', `/contracts/${id}`),
    /** Activates a new version: from what was observed, or the active one with new critical fields. */
    createVersion: (id: string, input: { critical_fields?: string[]; source?: 'observed' | 'active' } = {}) =>
      this.org<{ version: number }>('POST', `/contracts/${id}/versions`, input),
    /** Throws away what was learned and starts learning again. */
    relearn: (id: string) => this.org<void>('POST', `/contracts/${id}/relearn`),
  }

  readonly incidents = {
    list: (filters: { status?: 'open' | 'resolved' } = {}) => this.org<List<Incident>>('GET', '/incidents', undefined, filters),
    resolve: (id: string, resolution: string) => this.org<void>('POST', `/incidents/${id}/resolve`, { resolution }),
    /** Dry run: which events and deliveries a replay would resend. Changes nothing. */
    previewReplay: (id: string) => this.org<ReplayPlan>('GET', `/incidents/${id}/replay`),
    /** Resends the incident's deliveries. The incident resolves itself if all of them succeed. */
    replay: (id: string) => this.org<Replay>('POST', `/incidents/${id}/replay`, { confirm: true }),
  }

  readonly alerts = {
    channels: () => this.org<List<AlertChannel>>('GET', '/alert-channels'),
    log: () => this.org<List<AlertLogEntry>>('GET', '/alerts'),
  }

  // ---- connections: your users' accounts at other apps ----------------------------

  readonly integrations = {
    list: () => this.org<List<Integration>>('GET', '/integrations'),
    /** Zoho, HubSpot and Google need your OAuth app's client_id and client_secret; Shiprocket needs neither. */
    create: (input: { provider: string; key?: string; name?: string; client_id?: string; client_secret?: string; scopes?: string[] }) =>
      this.org<Integration>('POST', '/integrations', input),
    update: (id: string, input: { name?: string; client_id?: string; client_secret?: string; scopes?: string[] }) =>
      this.org<Integration>('PATCH', `/integrations/${id}`, input),
    /** Deletes its connections too. */
    delete: (id: string) => this.org<void>('DELETE', `/integrations/${id}`),
  }

  readonly connections = {
    /**
     * A one-time link (30 minutes) where your user connects their account.
     * Open it with connect.js (`await Relaya.connect(link.url)`) or redirect them to it.
     */
    createLink: (input: { integration: string; end_user_id: string; return_url?: string }) =>
      this.org<ConnectLink>('POST', '/connect-sessions', input),
    list: (filters: { integration?: string; end_user_id?: string; status?: 'active' | 'broken' } = {}) =>
      this.org<List<Connection>>('GET', '/connections', undefined, filters),
    get: (id: string) => this.org<Connection>('GET', `/connections/${id}`),
    /** The connection for one of your users, or null. */
    find: async (integration: string, endUserId: string) =>
      (await this.connections.list({ integration, end_user_id: endUserId })).data[0] ?? null,
    /** A working access token, renewed first when about to expire. Use it right away rather than storing it. */
    token: (id: string) => this.org<ConnectionToken>('GET', `/connections/${id}/token`),
    /** Renews the token now, e.g. to check the connection works. */
    refresh: (id: string) => this.org<{ connection: Connection; refreshed: boolean; error?: string }>('POST', `/connections/${id}/refresh`),
    delete: (id: string) => this.org<void>('DELETE', `/connections/${id}`),
  }

  /**
   * Calls the provider's API as the connected user; Relaya adds and renews the token.
   *
   *   const res = await relaya.proxy(connectionId).get('/crm/v2/Leads', { query: { per_page: 10 } })
   *   if (res.ok) console.log(res.data)
   *
   * The provider's answer comes back as is, errors included (check `ok`/`status`).
   * A RelayaError is thrown only when Relaya couldn't make the call (e.g. the
   * connection is broken, `code: 'connection_broken'`).
   */
  proxy(connectionId: string) {
    const call = <T = unknown>(method: string, path: string, opts: ProxyOptions = {}) => this.proxyRequest<T>(connectionId, method, path, opts)
    return {
      request: call,
      get: <T = unknown>(path: string, opts?: Omit<ProxyOptions, 'body'>) => call<T>('GET', path, opts),
      post: <T = unknown>(path: string, body?: unknown, opts?: Omit<ProxyOptions, 'body'>) => call<T>('POST', path, { ...opts, body }),
      put: <T = unknown>(path: string, body?: unknown, opts?: Omit<ProxyOptions, 'body'>) => call<T>('PUT', path, { ...opts, body }),
      patch: <T = unknown>(path: string, body?: unknown, opts?: Omit<ProxyOptions, 'body'>) => call<T>('PATCH', path, { ...opts, body }),
      delete: <T = unknown>(path: string, opts?: Omit<ProxyOptions, 'body'>) => call<T>('DELETE', path, opts),
    }
  }

  private async proxyRequest<T>(connectionId: string, method: string, path: string, opts: ProxyOptions): Promise<ProxyResponse<T>> {
    const url = new URL(`${this.baseUrl}/v1/orgs/${await this.orgId()}/connections/${connectionId}/proxy/${path.replace(/^\/+/, '')}`)
    for (const [k, v] of Object.entries(opts.query ?? {})) {
      if (v !== undefined && v !== null) url.searchParams.set(k, String(v))
    }
    const headers: Record<string, string> = { Authorization: `Bearer ${this.apiKey}`, Accept: 'application/json' }
    for (const [k, v] of Object.entries(opts.headers ?? {})) headers[`Relaya-Proxy-${k}`] = v
    if (opts.baseUrl) headers['Relaya-Proxy-Base-Url'] = opts.baseUrl
    let body: BodyInit | undefined
    if (opts.body !== undefined) {
      body = typeof opts.body === 'string' ? opts.body : JSON.stringify(opts.body)
      headers['Content-Type'] = opts.contentType ?? 'application/json'
    }
    let res: Response
    try {
      // One attempt: Relaya already retries what is safe to retry.
      res = await this.fetchImpl(url, { method, headers, body, signal: AbortSignal.timeout(opts.timeoutMs ?? 120_000) })
    } catch (err) {
      throw new RelayaError(0, 'network_error', `Could not reach Relaya: ${(err as Error).message}`)
    }
    const text = await res.text()
    let data: unknown = text
    try {
      data = text ? JSON.parse(text) : null
    } catch {
      // not JSON: keep the text
    }
    if (res.headers.get('relaya-proxy-error') === 'true' || (res.status === 401 && !res.headers.has('relaya-proxy-attempts'))) {
      const e = (data as { error?: { code?: string; message?: string } } | null)?.error
      throw new RelayaError(res.status, e?.code ?? 'proxy_error', e?.message ?? `HTTP ${res.status}`, res.headers.get('x-request-id'))
    }
    return { status: res.status, ok: res.ok, headers: res.headers, data: data as T, attempts: Number(res.headers.get('relaya-proxy-attempts') ?? 1) }
  }

  readonly proxyCalls = {
    /** The last 100 calls made through connections (no query strings or bodies). */
    list: (filters: { connection?: string } = {}) => this.org<List<ProxyCall>>('GET', '/proxy-calls', undefined, filters),
  }

  // ---- syncs: changes in connected apps become events ------------------------------

  readonly syncs = {
    /** What can be synced, per provider, and the settings each needs. */
    models: () => this.request<List<SyncModel>>('GET', '/v1/connect/sync-models'),
    list: () => this.org<List<Sync>>('GET', '/syncs'),
    /**
     * Starts syncing, e.g. { connection_id, model: 'zoho.crm_records', config: { module: 'Leads' } }.
     * Events land on a new webhook unless you pass webhook_id; add a destination there to receive them.
     */
    create: (input: {
      connection_id: string
      model: string
      config?: Record<string, string>
      interval_minutes?: number
      webhook_id?: string
      emit_existing?: boolean
    }) => this.org<Sync>('POST', '/syncs', input),
    /** A new config starts the sync over (fresh first run). */
    update: (id: string, input: { enabled?: boolean; interval_minutes?: number; config?: Record<string, string> }) =>
      this.org<Sync>('PATCH', `/syncs/${id}`, input),
    delete: (id: string) => this.org<void>('DELETE', `/syncs/${id}`),
    /** Runs it within seconds instead of waiting for the schedule. */
    run: (id: string) => this.org<Sync>('POST', `/syncs/${id}/run`),
    runs: (id: string) => this.org<List<SyncRun>>('GET', `/syncs/${id}/runs`),
  }

  // ---- outbound webhooks: send events to your own customers ---------------------------

  readonly outbound = {
    /**
     * Sends an event to every endpoint of that customer that takes its type. Relaya signs it
     * (Standard Webhooks), retries failures and logs every attempt. With an idempotency_key,
     * sending the same message again returns the first one (duplicate: true) instead of a copy.
     */
    send: (input: { app: string; event_type: string; payload: Record<string, unknown>; idempotency_key?: string }) =>
      this.org<OutboundMessage>('POST', '/outbound/messages', input),

    /** One app per customer; refer to it by your own uid (or its id) everywhere. */
    apps: {
      list: () => this.org<List<OutboundApp>>('GET', '/outbound/apps'),
      get: (app: string) => this.org<{ app: OutboundApp; endpoints: OutboundEndpoint[] }>('GET', `/outbound/apps/${enc(app)}`),
      create: (input: { uid: string; name?: string }) => this.org<OutboundApp>('POST', '/outbound/apps', input),
      /** Also deletes its endpoints and message history. */
      delete: (app: string) => this.org<void>('DELETE', `/outbound/apps/${enc(app)}`),
      /** A 24-hour link where the customer manages their endpoints and sees deliveries. */
      portalLink: (app: string) => this.org<PortalLink>('POST', `/outbound/apps/${enc(app)}/portal-link`),
    },

    /** Customers manage these themselves in the portal; these let you do it for them. */
    endpoints: {
      list: async (app: string) => (await this.outbound.apps.get(app)).endpoints,
      /** Returns the signing secret (whsec_…) the customer verifies requests with. */
      create: (app: string, input: { url: string; description?: string; event_types?: string[] }) =>
        this.org<{ endpoint: OutboundEndpoint; signing_secret: string }>('POST', `/outbound/apps/${enc(app)}/endpoints`, input),
      update: (app: string, id: string, input: { url?: string; description?: string; event_types?: string[]; enabled?: boolean }) =>
        this.org<OutboundEndpoint>('PATCH', `/outbound/apps/${enc(app)}/endpoints/${id}`, input),
      delete: (app: string, id: string) => this.org<void>('DELETE', `/outbound/apps/${enc(app)}/endpoints/${id}`),
      secret: (app: string, id: string) => this.org<{ signing_secret: string }>('GET', `/outbound/apps/${enc(app)}/endpoints/${id}/secret`),
      /** Sends a signed test event now and reports what the endpoint answered. */
      test: (app: string, id: string, eventType?: string) =>
        this.org<TestDeliveryResult>('POST', `/outbound/apps/${enc(app)}/endpoints/${id}/test`, undefined, { event_type: eventType }),
    },

    /** The catalog customers pick from in the portal. Types you send are added automatically. */
    eventTypes: {
      list: () => this.org<List<OutboundEventType>>('GET', '/outbound/event-types'),
      /** Adds it, or updates its description. */
      save: (input: { name: string; description?: string }) => this.org<OutboundEventType>('POST', '/outbound/event-types', input),
      delete: (name: string) => this.org<void>('DELETE', `/outbound/event-types/${enc(name)}`),
    },
  }
}

const enc = encodeURIComponent

export interface ProxyOptions {
  query?: Record<string, string | number | boolean | undefined | null>
  /** Sent to the provider (as Relaya-Proxy-<name>); your Relaya API key never is. */
  headers?: Record<string, string>
  /** JSON-encoded unless it is a string. */
  body?: unknown
  contentType?: string
  /** Another API host of the same provider, e.g. https://sheets.googleapis.com. */
  baseUrl?: string
  /** Default 120 s. */
  timeoutMs?: number
}

/** Exponential backoff with jitter; honours Retry-After (seconds) up to 30 s. */
function backoff(attempt: number, retryAfter: string | null): number {
  const s = Number(retryAfter)
  if (retryAfter && Number.isFinite(s) && s >= 0) return Math.min(s * 1000, 30_000)
  return Math.min(500 * 2 ** attempt, 8_000) * (0.8 + Math.random() * 0.4)
}
