// End-to-end against a running Relaya (API + ingest + worker). Skipped unless RELAYA_IT_API_URL is set, e.g.
//   RELAYA_IT_API_URL=http://127.0.0.1:18080 node --test test/integration.test.ts
// The server must allow http://127.0.0.1 destinations (APP_ENV=dev) and use CONTRACT_MIN_SAMPLES=3.
import assert from 'node:assert/strict'
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'
import { test } from 'node:test'

import { Relaya, RelayaError, relayaMiddleware, type VerifiedDelivery } from '../src/index.ts'

const API = process.env.RELAYA_IT_API_URL

async function waitFor<T>(what: string, fn: () => Promise<T | undefined | false> | T | undefined | false, ms = 15_000): Promise<T> {
  const until = Date.now() + ms
  for (;;) {
    const v = await fn()
    if (v) return v
    if (Date.now() > until) throw new Error(`timed out waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 150))
  }
}

test('SDK against a live Relaya', { skip: !API && 'set RELAYA_IT_API_URL to run' }, async () => {
  // Sign up and mint an API key (the dashboard's job; plain fetch here).
  const post = async (path: string, body: unknown, token?: string) => {
    const r = await fetch(API + path, { method: 'POST', headers: { 'Content-Type': 'application/json', ...(token && { Authorization: `Bearer ${token}` }) }, body: JSON.stringify(body) })
    assert.ok(r.ok, `${path}: ${r.status} ${await r.clone().text()}`)
    return r.json()
  }
  const session = await post('/v1/auth/signup', { email: `sdk-${Date.now()}@example.com`, password: 'sdk-test-password-1', org_name: 'SDK test' })
  const me = await (await fetch(API + '/v1/me', { headers: { Authorization: `Bearer ${session.token}` } })).json()
  const orgId: string = me.orgs[0].id
  const key = await post(`/v1/orgs/${orgId}/api-keys`, { name: 'sdk', role: 'admin' }, session.token)

  const relaya = new Relaya({ apiKey: key.key, baseUrl: API })
  assert.equal(await relaya.orgId(), orgId)

  // A customer endpoint verifying with the SDK middleware.
  let secret = ''
  let failNext = 0
  const received: VerifiedDelivery[] = []
  const verify = () => relayaMiddleware({ secret })
  const server = createServer((req: IncomingMessage & { relaya?: VerifiedDelivery }, res: ServerResponse) => {
    verify()(req, res, (err) => {
      if (err) {
        res.statusCode = 500
        return res.end(String(err))
      }
      received.push(req.relaya!)
      res.statusCode = failNext-- > 0 ? 500 : 200
      res.end('ok')
    })
  })
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r))
  const endpoint = `http://127.0.0.1:${(server.address() as AddressInfo).port}/hooks`

  try {
    const project = await relaya.projects.create({ name: 'SDK' })
    const wh = await relaya.webhooks.create({ project_id: project.id, name: 'Payments', provider: 'generic' })
    const dest = await relaya.destinations.create(wh.id, { name: 'My app', url: endpoint })
    secret = dest.signing_secret
    assert.match(secret, /\S{16,}/)

    const send = (id: string, body: unknown) =>
      fetch(wh.ingest_url, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Event-Id': id }, body: JSON.stringify(body) })
    for (let i = 1; i <= 3; i++) assert.equal((await send(`e${i}`, { type: 'payment.captured', amount: 100 * i })).status, 200)

    // Delivered, signed, and verified by the middleware.
    await waitFor('3 deliveries', () => received.length >= 3)
    const first = received[0]!
    assert.equal(first.idempotencyKey, first.deliveryId)
    assert.equal(first.attempt, 1)
    assert.equal(first.eventType, 'payment.captured')
    assert.equal(typeof first.json<{ amount: number }>().amount, 'number')

    // Events: pagination via iterate, details via get.
    const all = []
    for await (const e of relaya.events.iterate({ webhook_id: wh.id, limit: 2 })) all.push(e)
    assert.equal(all.length, 3)
    assert.ok(all.some((e) => e.id === first.eventId))
    const detail = await relaya.events.get(first.eventId)
    assert.equal((detail.payload_json as { type: string }).type, 'payment.captured')

    // Deliveries: list, get with attempts.
    const dl = await waitFor('succeeded deliveries', async () => {
      const l = await relaya.deliveries.list({ webhook_id: wh.id, status: 'succeeded' })
      return l.data.length === 3 && l
    })
    const one = await relaya.deliveries.get(dl.data[0]!.id)
    assert.equal(one.attempts[0]!.outcome, 'succeeded')

    // A failing endpoint, then a manual retry.
    failNext = 1
    await send('e4', { type: 'payment.captured', amount: 400 })
    const retrying = await waitFor('a retrying delivery', async () => (await relaya.deliveries.list({ webhook_id: wh.id, status: 'retrying' })).data[0])
    await relaya.deliveries.retry(retrying.id)
    await waitFor('the retry to succeed', async () => (await relaya.deliveries.get(retrying.id)).delivery.status === 'succeeded')
    assert.equal(received.at(-1)!.attempt, 2)
    assert.equal(received.at(-1)!.idempotencyKey, retrying.id)

    // Contracts → incident → replay, all through the SDK.
    const contract = await waitFor('a proposed contract', async () => (await relaya.contracts.list({ webhook_id: wh.id })).data.find((c) => c.status !== 'learning'))
    await relaya.contracts.createVersion(contract.id, { critical_fields: ['amount'], source: 'observed' })
    await send('e5', { type: 'payment.captured', amount: '500' })
    const incident = await waitFor('an open incident', async () => (await relaya.incidents.list({ status: 'open' })).data[0])
    assert.match(incident.title, /amount changed type/)
    const plan = await relaya.incidents.previewReplay(incident.id)
    assert.equal(plan.events, 1)
    const before = received.length
    const replay = await relaya.incidents.replay(incident.id)
    assert.equal(replay.total, 1)
    await waitFor('the replayed delivery', () => received.length > before)
    assert.equal(received.at(-1)!.replayId, replay.id)
    await waitFor('the incident to resolve after a verified replay', async () => (await relaya.incidents.list({ status: 'open' })).data.length === 0)

    // Destination test, and API errors.
    assert.equal((await relaya.destinations.test(dest.destination.id)).ok, true)
    await assert.rejects(relaya.webhooks.get('00000000-0000-0000-0000-000000000000'), (e) => e instanceof RelayaError && e.status === 404)
    await assert.rejects(relaya.deliveries.retry(dl.data[0]!.id), (e) => e instanceof RelayaError && e.status === 409)
  } finally {
    server.close()
  }
})
