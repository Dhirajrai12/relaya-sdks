import { WebhookVerificationError } from './errors.ts'

/** Header names Relaya sets on every forwarded request. */
export const Headers = {
  signature: 'relaya-signature',
  idempotencyKey: 'idempotency-key',
  eventId: 'relaya-event-id',
  deliveryId: 'relaya-delivery-id',
  attempt: 'relaya-attempt',
  replay: 'relaya-replay',
  eventType: 'relaya-event-type',
} as const

/** Reject signatures older (or newer) than this by default, to stop replayed requests. */
export const DEFAULT_TOLERANCE_SECONDS = 300

export type RawBody = string | Uint8Array | ArrayBuffer

/** Anything header-like: a Fetch `Headers`, Node's `req.headers`, or a plain object. */
export type HeadersLike =
  | { get(name: string): string | null }
  | Record<string, string | string[] | undefined>

export interface VerifyOptions {
  /** Signing secret(s). Pass several while rotating: any match is accepted. */
  secret: string | string[]
  /** Maximum age of the signature in seconds. Default 300. */
  toleranceSeconds?: number
  /** Override "now" (tests). */
  now?: Date
}

/** A verified request forwarded by Relaya to your endpoint. */
export interface VerifiedDelivery {
  /** Stable across retries and replays of the same delivery: dedupe on this. */
  idempotencyKey: string
  deliveryId: string
  eventId: string
  /** Event type Relaya detected, e.g. "payment.captured", if any. */
  eventType: string | null
  /** 1 for the first try, then 2, 3… on retries. */
  attempt: number
  /** Set when the request is part of an incident replay. */
  replayId: string | null
  /** When Relaya signed the request. */
  signedAt: Date
  /** The provider's original body, byte for byte. */
  rawBody: string
  /** The body parsed as JSON. Throws if it isn't JSON. */
  json<T = unknown>(): T
}

/** A verified alert sent to a webhook alert channel. */
export interface AlertPayload {
  type: 'incident_opened' | 'incident_resolved' | 'destination_failing' | 'destination_recovered' | 'signature_failures' | 'test'
  title: string
  body: string
  link: string
  org_id: string
  alert_id: number
  sent_at: string
}

const encoder = new TextEncoder()
const decoder = new TextDecoder()

function toBytes(body: RawBody): Uint8Array {
  if (typeof body === 'string') return encoder.encode(body)
  if (body instanceof Uint8Array) return body
  if (body instanceof ArrayBuffer) return new Uint8Array(body)
  throw new TypeError(
    'Relaya: the request body must be the raw string or bytes. If a JSON body parser already ran, use express.raw() for this route.',
  )
}

function header(headers: HeadersLike, name: string): string | null {
  if (typeof (headers as { get?: unknown }).get === 'function') return (headers as { get(n: string): string | null }).get(name)
  const rec = headers as Record<string, string | string[] | undefined>
  let v = rec[name]
  if (v === undefined) {
    const key = Object.keys(rec).find((k) => k.toLowerCase() === name)
    v = key === undefined ? undefined : rec[key]
  }
  if (Array.isArray(v)) return v[0] ?? null
  return v ?? null
}

function parseSignature(value: string): { timestamp: number; signatures: string[] } | null {
  let timestamp = NaN
  const signatures: string[] = []
  for (const part of value.split(',')) {
    const i = part.indexOf('=')
    if (i < 0) continue
    const k = part.slice(0, i).trim()
    const v = part.slice(i + 1).trim()
    if (k === 't') timestamp = Number(v)
    else if (k === 'v1' && v) signatures.push(v.toLowerCase())
  }
  if (!Number.isInteger(timestamp) || signatures.length === 0) return null
  return { timestamp, signatures }
}

async function hmacHex(secret: string, message: Uint8Array): Promise<string> {
  const key = await crypto.subtle.importKey('raw', encoder.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  const mac = new Uint8Array(await crypto.subtle.sign('HMAC', key, message as Uint8Array<ArrayBuffer>))
  let hex = ''
  for (const b of mac) hex += b.toString(16).padStart(2, '0')
  return hex
}

function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false
  let diff = 0
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i)
  return diff === 0
}

/**
 * Checks a `Relaya-Signature` header (`t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<body>")>`).
 * Throws WebhookVerificationError with a specific reason; returns the signing time.
 */
export async function verifySignature(body: RawBody, signatureHeader: string | null | undefined, opts: VerifyOptions): Promise<Date> {
  const secrets = (Array.isArray(opts.secret) ? opts.secret : [opts.secret]).filter(Boolean)
  if (secrets.length === 0) throw new TypeError('Relaya: a signing secret is required')
  if (!signatureHeader) throw new WebhookVerificationError('missing_signature', 'The Relaya-Signature header is missing')
  const parsed = parseSignature(signatureHeader)
  if (!parsed) throw new WebhookVerificationError('malformed_signature', 'The Relaya-Signature header is malformed')

  const tolerance = opts.toleranceSeconds ?? DEFAULT_TOLERANCE_SECONDS
  const now = Math.floor((opts.now ?? new Date()).getTime() / 1000)
  if (tolerance > 0 && Math.abs(now - parsed.timestamp) > tolerance) {
    throw new WebhookVerificationError('timestamp_out_of_range', `The signature is older than ${tolerance} seconds (or from the future)`)
  }

  const bytes = toBytes(body)
  const signed = new Uint8Array(encoder.encode(`${parsed.timestamp}.`).length + bytes.length)
  signed.set(encoder.encode(`${parsed.timestamp}.`))
  signed.set(bytes, signed.length - bytes.length)
  for (const secret of secrets) {
    const expected = await hmacHex(secret, signed)
    if (parsed.signatures.some((s) => timingSafeEqual(s, expected))) return new Date(parsed.timestamp * 1000)
  }
  throw new WebhookVerificationError('signature_mismatch', 'The signature does not match: check the signing secret and that you pass the raw body')
}

/** Like verifySignature, but returns true/false instead of throwing. */
export async function isValidSignature(body: RawBody, signatureHeader: string | null | undefined, opts: VerifyOptions): Promise<boolean> {
  try {
    await verifySignature(body, signatureHeader, opts)
    return true
  } catch (err) {
    if (err instanceof WebhookVerificationError) return false
    throw err
  }
}

/** Verifies a request forwarded by Relaya and returns its details. Throws WebhookVerificationError. */
export async function verifyDelivery(body: RawBody, headers: HeadersLike, opts: VerifyOptions): Promise<VerifiedDelivery> {
  const signedAt = await verifySignature(body, header(headers, Headers.signature), opts)
  const rawBody = typeof body === 'string' ? body : decoder.decode(toBytes(body))
  const deliveryId = header(headers, Headers.deliveryId) ?? ''
  return {
    idempotencyKey: header(headers, Headers.idempotencyKey) ?? deliveryId,
    deliveryId,
    eventId: header(headers, Headers.eventId) ?? '',
    eventType: header(headers, Headers.eventType),
    attempt: Number(header(headers, Headers.attempt) ?? 1) || 1,
    replayId: header(headers, Headers.replay),
    signedAt,
    rawBody,
    json: <T>() => JSON.parse(rawBody) as T,
  }
}

/** Verifies an alert sent to a webhook alert channel and returns it parsed. */
export async function verifyAlert(body: RawBody, headers: HeadersLike, opts: VerifyOptions): Promise<AlertPayload> {
  await verifySignature(body, header(headers, Headers.signature), opts)
  return JSON.parse(typeof body === 'string' ? body : decoder.decode(toBytes(body))) as AlertPayload
}

/**
 * For Fetch-style servers (Next.js route handlers, Hono, Bun, Deno, Cloudflare Workers):
 *
 *   export const POST = relayaHandler({ secret: process.env.RELAYA_SIGNING_SECRET!, onDelivery: async (d) => { … } })
 *
 * Answers 400 when the signature is bad, 200 when onDelivery succeeds, and 500 if it throws
 * (Relaya then retries with the same idempotency key).
 */
export function relayaHandler(opts: VerifyOptions & { onDelivery: (delivery: VerifiedDelivery, request: Request) => unknown }) {
  return async (request: Request): Promise<Response> => {
    let delivery: VerifiedDelivery
    try {
      delivery = await verifyDelivery(new Uint8Array(await request.arrayBuffer()), request.headers, opts)
    } catch (err) {
      if (err instanceof WebhookVerificationError) return Response.json({ error: err.reason }, { status: 400 })
      throw err
    }
    try {
      await opts.onDelivery(delivery, request)
    } catch (err) {
      console.error('Relaya: onDelivery failed; Relaya will retry', err)
      return Response.json({ error: 'handler_failed' }, { status: 500 })
    }
    return Response.json({ ok: true })
  }
}

interface NodeRequest {
  headers: Record<string, string | string[] | undefined>
  body?: unknown
  on(event: 'data', cb: (chunk: Uint8Array | string) => void): unknown
  on(event: 'end', cb: () => void): unknown
  on(event: 'error', cb: (err: Error) => void): unknown
}
interface NodeResponse {
  statusCode: number
  setHeader(name: string, value: string): unknown
  end(body?: string): unknown
}

function readBody(req: NodeRequest): Promise<Uint8Array> {
  if (req.body instanceof Uint8Array) return Promise.resolve(req.body) // express.raw() / Buffer
  if (typeof req.body === 'string') return Promise.resolve(encoder.encode(req.body))
  if (req.body !== undefined && req.body !== null && typeof req.body === 'object' && Object.keys(req.body).length > 0) {
    return Promise.reject(
      new TypeError('Relaya: the body was already parsed as JSON, so the signature cannot be checked. Mount this route before express.json(), or use express.raw({ type: "*/*" }).'),
    )
  }
  return new Promise((resolve, reject) => {
    const chunks: Uint8Array[] = []
    req.on('data', (c) => chunks.push(typeof c === 'string' ? encoder.encode(c) : c))
    req.on('error', reject)
    req.on('end', () => {
      const out = new Uint8Array(chunks.reduce((n, c) => n + c.length, 0))
      let at = 0
      for (const c of chunks) {
        out.set(c, at)
        at += c.length
      }
      resolve(out)
    })
  })
}

/**
 * Express / Connect middleware. Verifies the request and puts the result on `req.relaya`:
 *
 *   app.post('/webhooks/relaya', relayaMiddleware({ secret }), (req, res) => { req.relaya.json(); res.sendStatus(200) })
 *
 * Mount it before express.json() for this route (or use express.raw()). Bad signatures get a 400.
 */
export function relayaMiddleware(opts: VerifyOptions) {
  return (req: NodeRequest & { relaya?: VerifiedDelivery }, res: NodeResponse, next: (err?: unknown) => void) => {
    readBody(req)
      .then((body) => verifyDelivery(body, req.headers, opts))
      .then(
        (delivery) => {
          req.relaya = delivery
          next()
        },
        (err) => {
          if (!(err instanceof WebhookVerificationError)) return next(err)
          res.statusCode = 400
          res.setHeader('Content-Type', 'application/json')
          res.end(JSON.stringify({ error: err.reason }))
        },
      )
  }
}
