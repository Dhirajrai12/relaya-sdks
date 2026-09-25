import assert from 'node:assert/strict'
import { test } from 'node:test'

import { Relaya, RelayaError } from '../src/index.ts'

type Call = { method: string; url: URL; body: unknown; auth: string | null }

/** A fake API: routes "METHOD /path" to a response factory and records calls. */
function fakeApi(routes: Record<string, (call: Call, n: number) => Response>) {
  const calls: Call[] = []
  const counts: Record<string, number> = {}
  const fetchImpl = (async (input: URL | string, init?: RequestInit) => {
    const url = new URL(String(input))
    const call = { method: init?.method ?? 'GET', url, body: init?.body ? JSON.parse(String(init.body)) : undefined, auth: new Headers(init?.headers).get('authorization') }
    calls.push(call)
    const key = `${call.method} ${url.pathname}`
    counts[key] = (counts[key] ?? 0) + 1
    const route = routes[key]
    if (!route) return Response.json({ error: { code: 'not_found', message: `no route ${key}` } }, { status: 404 })
    return route(call, counts[key])
  }) as typeof fetch
  return { calls, fetchImpl }
}

const me = () => Response.json({ api_key: { id: 'k', org_id: 'org1', role: 'admin' } })

test('looks the org up from the API key once, and sends the key', async () => {
  const api = fakeApi({ 'GET /api/v1/me': me, 'GET /api/v1/orgs/org1/projects': () => Response.json({ data: [{ id: 'p1' }] }) })
  const r = new Relaya({ apiKey: 'rk_test', baseUrl: 'https://relaya.test/api/', fetch: api.fetchImpl })
  assert.equal((await r.projects.list()).data[0]?.id, 'p1')
  await r.projects.list()
  assert.equal(api.calls.filter((c) => c.url.pathname === '/api/v1/me').length, 1)
  assert.ok(api.calls.every((c) => c.auth === 'Bearer rk_test'))
})

test('skips the lookup when orgId is given; encodes filters and dates', async () => {
  const api = fakeApi({ 'GET /api/v1/orgs/o9/events': () => Response.json({ data: [], next_cursor: null }) })
  const r = new Relaya({ apiKey: 'rk', orgId: 'o9', baseUrl: 'https://relaya.test/api', fetch: api.fetchImpl })
  await r.events.list({ contract_status: 'breaking', since: new Date('2026-09-01T00:00:00Z'), limit: 10, type: undefined })
  const q = api.calls[0]!.url.searchParams
  assert.equal(q.get('contract_status'), 'breaking')
  assert.equal(q.get('since'), '2026-09-01T00:00:00.000Z')
  assert.equal(q.get('limit'), '10')
  assert.equal(q.has('type'), false)
})

test('iterate follows next_cursor across pages', async () => {
  const api = fakeApi({
    'GET /api/v1/orgs/o/events': (c) =>
      c.url.searchParams.get('cursor') === 'c2'
        ? Response.json({ data: [{ id: 'e3' }], next_cursor: null })
        : Response.json({ data: [{ id: 'e1' }, { id: 'e2' }], next_cursor: 'c2' }),
  })
  const r = new Relaya({ apiKey: 'rk', orgId: 'o', baseUrl: 'https://relaya.test/api', fetch: api.fetchImpl })
  const ids: string[] = []
  for await (const e of r.events.iterate({ type: 'x' })) ids.push(e.id)
  assert.deepEqual(ids, ['e1', 'e2', 'e3'])
  assert.equal(api.calls[1]!.url.searchParams.get('type'), 'x')
})

test('turns API errors into RelayaError', async () => {
  const api = fakeApi({})
  const r = new Relaya({ apiKey: 'rk', orgId: 'o', baseUrl: 'https://relaya.test/api', fetch: api.fetchImpl })
  await assert.rejects(r.webhooks.get('nope'), (e) => e instanceof RelayaError && e.status === 404 && e.code === 'not_found')
})

test('retries GETs on 503 (honouring Retry-After) but never POSTs', async () => {
  const api = fakeApi({
    'GET /api/v1/orgs/o/incidents': (_, n) => (n < 3 ? new Response('busy', { status: 503, headers: { 'Retry-After': '0' } }) : Response.json({ data: [] })),
    'POST /api/v1/orgs/o/incidents/i1/replay': () => new Response('busy', { status: 503, headers: { 'Retry-After': '0' } }),
  })
  const r = new Relaya({ apiKey: 'rk', orgId: 'o', baseUrl: 'https://relaya.test/api', fetch: api.fetchImpl })
  assert.deepEqual(await r.incidents.list({ status: 'open' }), { data: [] })
  await assert.rejects(r.incidents.replay('i1'), (e) => e instanceof RelayaError && e.status === 503 && e.code === 'http_error')
  assert.equal(api.calls.filter((c) => c.method === 'POST').length, 1)
  assert.deepEqual(api.calls.find((c) => c.method === 'POST')!.body, { confirm: true })
})

test('reports network failures with code network_error', async () => {
  const r = new Relaya({ apiKey: 'rk', orgId: 'o', maxRetries: 0, baseUrl: 'https://relaya.test/api', fetch: (async () => { throw new TypeError('fetch failed') }) as typeof fetch })
  await assert.rejects(r.projects.list(), (e) => e instanceof RelayaError && e.code === 'network_error' && e.status === 0)
})

test('needs an API key', () => {
  const saved = process.env.RELAYA_API_KEY
  delete process.env.RELAYA_API_KEY
  try {
    assert.throws(() => new Relaya(), /RELAYA_API_KEY/)
  } finally {
    if (saved !== undefined) process.env.RELAYA_API_KEY = saved
  }
})
