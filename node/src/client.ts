import { RelayaError } from './errors.ts'
import type {
  AlertChannel,
  AlertLogEntry,
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
  List,
  Page,
  Project,
  Replay,
  ReplayPlan,
  SignatureResult,
  TestDeliveryResult,
  Webhook,
} from './types.ts'

/** Where the API lives until the product has its own domain. Override with `baseUrl` or RELAYA_BASE_URL. */
export const DEFAULT_BASE_URL = 'https://server.aegonassett.com/api'

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
    create: (webhookId: string, input: { name: string; url: string; max_attempts?: number; timeout_ms?: number; enabled?: boolean }) =>
      this.org<{ destination: Destination; signing_secret: string }>('POST', `/webhooks/${webhookId}/destinations`, input),
    update: (id: string, input: { name?: string; url?: string; enabled?: boolean; max_attempts?: number; timeout_ms?: number }) =>
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
}

/** Exponential backoff with jitter; honours Retry-After (seconds) up to 30 s. */
function backoff(attempt: number, retryAfter: string | null): number {
  const s = Number(retryAfter)
  if (retryAfter && Number.isFinite(s) && s >= 0) return Math.min(s * 1000, 30_000)
  return Math.min(500 * 2 ** attempt, 8_000) * (0.8 + Math.random() * 0.4)
}
