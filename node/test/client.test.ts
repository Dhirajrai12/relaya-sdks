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

test('connections: link, find, token', async () => {
  const api = fakeApi({
    'POST /api/v1/orgs/o/connect-sessions': (c) => Response.json({ id: 's1', url: 'https://relaya.test/connect/cs_x', expires_at: 'z', echo: c.body }, { status: 201 }),
    'GET /api/v1/orgs/o/connections': (c) =>
      Response.json({ data: c.url.searchParams.get('end_user_id') === 'u1' ? [{ id: 'c1', end_user_id: 'u1' }] : [] }),
    'GET /api/v1/orgs/o/connections/c1/token': () => Response.json({ access_token: 'at', api_base: 'https://www.zohoapis.in' }),
  })
  const r = new Relaya({ apiKey: 'rk', orgId: 'o', baseUrl: 'https://relaya.test/api', fetch: api.fetchImpl })
  const link = await r.connections.createLink({ integration: 'zoho', end_user_id: 'u1' })
  assert.equal(link.url, 'https://relaya.test/connect/cs_x')
  assert.deepEqual(api.calls[0]!.body, { integration: 'zoho', end_user_id: 'u1' })
  assert.equal((await r.connections.find('zoho', 'u1'))?.id, 'c1')
  assert.equal(await r.connections.find('zoho', 'nobody'), null)
  assert.equal(api.calls[1]!.url.searchParams.get('integration'), 'zoho')
  assert.equal((await r.connections.token('c1')).api_base, 'https://www.zohoapis.in')
})

test('proxy: forwards path, query, body and headers; returns provider errors, throws Relaya errors', async () => {
  const seen: { url: URL; headers: Headers; body: string | null }[] = []
  const fetchImpl = (async (input: URL | string, init?: RequestInit) => {
    const url = new URL(String(input))
    seen.push({ url, headers: new Headers(init?.headers), body: (init?.body as string) ?? null })
    if (url.pathname.endsWith('/proxy/crm/v2/Leads')) {
      return Response.json({ data: [{ id: '1' }] }, { headers: { 'Relaya-Proxy-Attempts': '2' } })
    }
    if (url.pathname.endsWith('/proxy/missing')) {
      return Response.json({ code: 'INVALID_URL_PATTERN' }, { status: 404, headers: { 'Relaya-Proxy-Attempts': '1' } })
    }
    if (url.pathname.endsWith('/proxy/text')) return new Response('plain', { headers: { 'Relaya-Proxy-Attempts': '1' } })
    return Response.json({ error: { code: 'connection_broken', message: 'the user must connect again' } }, { status: 409, headers: { 'Relaya-Proxy-Error': 'true' } })
  }) as typeof fetch
  const r = new Relaya({ apiKey: 'rk', orgId: 'o', baseUrl: 'https://relaya.test/api', fetch: fetchImpl })
  const zoho = r.proxy('c1')

  const res = await zoho.get<{ data: { id: string }[] }>('/crm/v2/Leads', { query: { per_page: 10 }, headers: { orgId: '42' } })
  assert.equal(res.ok, true)
  assert.equal(res.attempts, 2)
  assert.equal(res.data.data[0]!.id, '1')
  const s = seen[0]!
  assert.equal(s.url.pathname, '/api/v1/orgs/o/connections/c1/proxy/crm/v2/Leads')
  assert.equal(s.url.searchParams.get('per_page'), '10')
  assert.equal(s.headers.get('relaya-proxy-orgid'), '42')
  assert.equal(s.headers.get('authorization'), 'Bearer rk')

  await zoho.post('/crm/v2/Leads', { data: [{ Last_Name: 'Rao' }] }, { baseUrl: 'https://www.zohoapis.in' })
  assert.equal(seen[1]!.body, '{"data":[{"Last_Name":"Rao"}]}')
  assert.equal(seen[1]!.headers.get('content-type'), 'application/json')
  assert.equal(seen[1]!.headers.get('relaya-proxy-base-url'), 'https://www.zohoapis.in')

  const missing = await zoho.get('/missing')
  assert.equal(missing.ok, false)
  assert.equal(missing.status, 404)
  assert.equal((await zoho.get('/text')).data, 'plain')

  await assert.rejects(zoho.get('/anything'), (e: unknown) => e instanceof RelayaError && e.code === 'connection_broken' && e.status === 409)
})

test('syncs: create and run', async () => {
  const api = fakeApi({
    'GET /api/v1/connect/sync-models': () => Response.json({ data: [{ key: 'zoho.crm_records' }] }),
    'POST /api/v1/orgs/o/syncs': (c) => Response.json({ id: 'sy1', ...(c.body as object) }, { status: 201 }),
    'POST /api/v1/orgs/o/syncs/sy1/run': () => Response.json({ id: 'sy1', running: true }, { status: 202 }),
  })
  const r = new Relaya({ apiKey: 'rk', orgId: 'o', baseUrl: 'https://relaya.test/api', fetch: api.fetchImpl })
  assert.equal((await r.syncs.models()).data[0]!.key, 'zoho.crm_records')
  const s = await r.syncs.create({ connection_id: 'c1', model: 'zoho.crm_records', config: { module: 'Leads' }, interval_minutes: 15 })
  assert.equal(s.id, 'sy1')
  assert.deepEqual(api.calls[1]!.body, { connection_id: 'c1', model: 'zoho.crm_records', config: { module: 'Leads' }, interval_minutes: 15 })
  assert.equal((await r.syncs.run('sy1')).running, true)
})
