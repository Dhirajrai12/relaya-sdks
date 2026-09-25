import assert from 'node:assert/strict'
import { createHmac } from 'node:crypto'
import { Readable } from 'node:stream'
import { test } from 'node:test'

import {
  isValidSignature,
  relayaHandler,
  relayaMiddleware,
  verifyAlert,
  verifyDelivery,
  verifySignature,
  WebhookVerificationError,
} from '../src/index.ts'

const secret = 'whsec_test_secret'
const body = '{"type":"payment.captured","amount":100,"note":"héllo"}'

// Same algorithm as the Go worker: hex HMAC-SHA256(secret, "<t>.<body>").
function sign(b: string | Buffer, s = secret, t = Math.floor(Date.now() / 1000)) {
  return `t=${t},v1=${createHmac('sha256', s).update(`${t}.`).update(b).digest('hex')}`
}

const reason = (r: string) => (err: unknown) => err instanceof WebhookVerificationError && err.reason === r

test('accepts a valid signature over string and byte bodies', async () => {
  const h = sign(body)
  assert.ok(await verifySignature(body, h, { secret }))
  assert.ok(await verifySignature(new TextEncoder().encode(body), h, { secret }))
  assert.ok(await verifySignature(Buffer.from(body), h, { secret }))
  assert.equal(await isValidSignature(body, h, { secret }), true)
})

test('rejects wrong secret, tampered body, missing and malformed headers', async () => {
  await assert.rejects(verifySignature(body, sign(body, 'other'), { secret }), reason('signature_mismatch'))
  await assert.rejects(verifySignature(body + ' ', sign(body), { secret }), reason('signature_mismatch'))
  await assert.rejects(verifySignature(body, null, { secret }), reason('missing_signature'))
  await assert.rejects(verifySignature(body, 'v1=abc', { secret }), reason('malformed_signature'))
  await assert.rejects(verifySignature(body, 't=abc,v1=abc', { secret }), reason('malformed_signature'))
  assert.equal(await isValidSignature(body, sign(body, 'other'), { secret }), false)
})

test('enforces the timestamp tolerance', async () => {
  const old = Math.floor(Date.now() / 1000) - 301
  await assert.rejects(verifySignature(body, sign(body, secret, old), { secret }), reason('timestamp_out_of_range'))
  assert.ok(await verifySignature(body, sign(body, secret, old), { secret, toleranceSeconds: 600 }))
  assert.ok(await verifySignature(body, sign(body, secret, old), { secret, toleranceSeconds: 0 })) // 0 disables
  const t = 1_700_000_000
  assert.equal((await verifySignature(body, sign(body, secret, t), { secret, now: new Date(t * 1000 + 5000) })).getTime(), t * 1000)
})

test('accepts any of several secrets while rotating', async () => {
  assert.ok(await verifySignature(body, sign(body, 'new'), { secret: ['old', 'new'] }))
  await assert.rejects(verifySignature(body, sign(body, 'new'), { secret: [] }), TypeError)
})

test('refuses an already-parsed body with a helpful error', async () => {
  await assert.rejects(verifySignature(JSON.parse(body), sign(body), { secret }), /raw string or bytes/)
})

test('verifyDelivery reads Relaya headers from a plain object (any case) and Fetch Headers', async () => {
  const raw = {
    'Relaya-Signature': sign(body),
    'Idempotency-Key': 'dlv_1',
    'relaya-delivery-id': 'dlv_1',
    'relaya-event-id': 'evt_1',
    'relaya-attempt': '3',
    'relaya-event-type': 'payment.captured',
  }
  for (const headers of [raw, new Headers(raw)]) {
    const d = await verifyDelivery(body, headers, { secret })
    assert.equal(d.idempotencyKey, 'dlv_1')
    assert.equal(d.eventId, 'evt_1')
    assert.equal(d.attempt, 3)
    assert.equal(d.eventType, 'payment.captured')
    assert.equal(d.replayId, null)
    assert.equal(d.json<{ note: string }>().note, 'héllo')
  }
})

test('verifyAlert parses the alert', async () => {
  const alert = JSON.stringify({ type: 'test', title: 'Test alert', body: 'b', link: 'https://x', org_id: 'o', alert_id: 1, sent_at: '' })
  const a = await verifyAlert(alert, { 'relaya-signature': sign(alert) }, { secret })
  assert.equal(a.title, 'Test alert')
})

test('relayaHandler answers 200, 400 and 500', async () => {
  let seen = ''
  const handler = relayaHandler({ secret, onDelivery: (d) => void (seen = d.eventId) })
  const req = (sig: string) => new Request('https://x/', { method: 'POST', body, headers: { 'relaya-signature': sig, 'relaya-event-id': 'evt_9' } })
  assert.equal((await handler(req(sign(body)))).status, 200)
  assert.equal(seen, 'evt_9')
  const bad = await handler(req(sign(body, 'other')))
  assert.equal(bad.status, 400)
  assert.deepEqual(await bad.json(), { error: 'signature_mismatch' })

  const failing = relayaHandler({ secret, onDelivery: () => { throw new Error('db down') } })
  const orig = console.error
  console.error = () => {}
  try {
    assert.equal((await failing(req(sign(body)))).status, 500)
  } finally {
    console.error = orig
  }
})

function nodeReq(headers: Record<string, string>, chunks: string[], parsedBody?: unknown) {
  return Object.assign(Readable.from(chunks.map((c) => Buffer.from(c))), { headers, body: parsedBody })
}
function nodeRes() {
  const res = { statusCode: 200, headers: {} as Record<string, string>, sent: '', setHeader(n: string, v: string) { res.headers[n] = v }, end(b?: string) { res.sent = b ?? '' } }
  return res
}

test('relayaMiddleware reads the raw stream, sets req.relaya, and 400s bad signatures', async () => {
  const mw = relayaMiddleware({ secret })
  const run = (req: ReturnType<typeof nodeReq>, res = nodeRes()) =>
    new Promise<{ err?: unknown; res: ReturnType<typeof nodeRes> }>((resolve) => {
      mw(req, res, (err) => resolve({ err, res }))
      setTimeout(() => resolve({ res }), 200)
    })

  const ok = nodeReq({ 'relaya-signature': sign(body), 'relaya-event-id': 'evt_2' }, [body.slice(0, 10), body.slice(10)])
  const r1 = await run(ok)
  assert.equal(r1.err, undefined)
  assert.equal((ok as unknown as { relaya: { eventId: string } }).relaya.eventId, 'evt_2')

  const r2 = await run(nodeReq({ 'relaya-signature': sign(body, 'other') }, [body]))
  assert.equal(r2.res.statusCode, 400)
  assert.match(r2.res.sent, /signature_mismatch/)

  // Buffer from express.raw()
  const r3 = await run(nodeReq({ 'relaya-signature': sign(body) }, [], Buffer.from(body)))
  assert.equal(r3.err, undefined)

  // express.json() already ran: pass a clear error to next()
  const r4 = await run(nodeReq({ 'relaya-signature': sign(body) }, [], JSON.parse(body)))
  assert.match(String(r4.err), /already parsed/)
})
