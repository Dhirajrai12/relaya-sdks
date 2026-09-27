# relaya-node

Verify requests that Relaya forwards to your endpoints, and call the Relaya API.

- No runtime dependencies. Works on Node.js 20+, Bun, Deno and edge runtimes (it uses Web Crypto and `fetch`).
- ESM and CommonJS, with TypeScript types.

```sh
npm install relaya-node
```

## Receive events

Relaya signs every request it forwards with your destination's signing secret (shown once when you add the destination). Verify the signature before trusting the body.

### Express

Mount the middleware **before** `express.json()` for this route; it needs the raw body.

```js
import express from 'express'
import { relayaMiddleware } from 'relaya-node'

const app = express()

app.post('/webhooks/relaya', relayaMiddleware({ secret: process.env.RELAYA_SIGNING_SECRET }), async (req, res) => {
  const delivery = req.relaya
  if (await alreadyProcessed(delivery.idempotencyKey)) return res.sendStatus(200)

  const event = delivery.json()
  await handle(event)
  res.sendStatus(200)
})

app.use(express.json()) // other routes
```

Bad signatures get a `400` with `{ "error": "<reason>" }`, and your handler never runs.

### Next.js, Hono, Bun, Deno, Cloudflare Workers

```ts
// app/webhooks/relaya/route.ts
import { relayaHandler } from 'relaya-node'

export const POST = relayaHandler({
  secret: process.env.RELAYA_SIGNING_SECRET!,
  onDelivery: async (delivery) => {
    await handle(delivery.json())
  },
})
```

If `onDelivery` throws, Relaya gets a `500` and retries later with the same idempotency key.

### Anything else

```ts
import { verifyDelivery, WebhookVerificationError } from 'relaya-node'

try {
  const delivery = await verifyDelivery(rawBody, headers, { secret })
} catch (err) {
  if (err instanceof WebhookVerificationError) {
    // err.reason: missing_signature | malformed_signature | timestamp_out_of_range | signature_mismatch
  }
}
```

`rawBody` must be the exact bytes Relaya sent (a string, `Buffer`, `Uint8Array` or `ArrayBuffer`), not a re-serialised object. `headers` can be a Fetch `Headers`, Node's `req.headers` or a plain object.

### What a delivery gives you

| Field | |
|---|---|
| `idempotencyKey` | The same across retries and replays of one delivery. Dedupe on this. |
| `eventId`, `deliveryId` | Relaya's IDs, to look the event up in the dashboard or API. |
| `eventType` | e.g. `payment.captured`, when Relaya could tell. |
| `attempt` | 1, then 2, 3… on retries. |
| `replayId` | Set when the request is part of an incident replay. |
| `signedAt` | When Relaya signed it. |
| `rawBody`, `json()` | The provider's original body, unchanged. |

### Options

- `secret`: a string, or an array while you rotate secrets (any match passes).
- `toleranceSeconds`: how old a signature may be (default 300; `0` turns the check off). This stops captured requests from being replayed later.

### Alerts

Webhook alert channels are signed the same way:

```ts
import { verifyAlert } from 'relaya-node'
const alert = await verifyAlert(rawBody, headers, { secret }) // { type, title, body, link, ... }
```

## Call the API

Create an API key in **Settings → API keys**.

```ts
import { Relaya } from 'relaya-node'

const relaya = new Relaya({ apiKey: process.env.RELAYA_API_KEY })

// Every event that broke its contract this week, across pages
for await (const e of relaya.events.iterate({ contract_status: 'breaking', since: new Date(Date.now() - 7 * 864e5) })) {
  console.log(e.type, e.received_at)
}

// Retry failed deliveries
const failed = await relaya.deliveries.list({ status: 'failed' })
for (const d of failed.data) await relaya.deliveries.retry(d.id)

// Fix your endpoint, then replay an incident's events. It resolves itself once they all succeed.
const [incident] = (await relaya.incidents.list({ status: 'open' })).data
if (incident) {
  const plan = await relaya.incidents.previewReplay(incident.id) // dry run
  await relaya.incidents.replay(incident.id)
}
```

| Resource | Methods |
|---|---|
| `projects` | `list`, `get`, `create` |
| `webhooks` | `list`, `get`, `create`, `update`, `rotateUrl`, `delete` |
| `events` | `list` (one page), `iterate` (all pages), `get` |
| `destinations` | `list`, `create`, `update`, `delete`, `rotateSecret`, `test` |
| `deliveries` | `list`, `get` (with attempts), `retry` |
| `contracts` | `list`, `get`, `createVersion`, `relearn` |
| `incidents` | `list`, `resolve`, `previewReplay`, `replay` |
| `alerts` | `channels`, `log` |
| `integrations` | `list`, `create`, `update`, `delete` |
| `connections` | `createLink`, `list`, `get`, `find`, `token`, `refresh`, `delete` |
| `proxy(connectionId)` | `get`, `post`, `put`, `patch`, `delete`, `request` |
| `proxyCalls` | `list` |
| `syncs` | `models`, `list`, `create`, `update`, `delete`, `run`, `runs` |

Responses use the API's field names (`snake_case`). `relaya.request(method, path, body?, query?)` reaches anything else.

## Your users' accounts: connect, call, sync

Let your users connect their Zoho, HubSpot, Google or Shiprocket accounts. Relaya keeps their tokens fresh, calls the APIs for you and turns changes into events. Add the app once under **Connections** in the dashboard (your OAuth app's client ID and secret), then:

```ts
// 1. Backend: a one-time link for one of your users (their ID in your system)
app.post('/integrations/zoho/link', async (req, res) => {
  const link = await relaya.connections.createLink({ integration: 'zoho', end_user_id: req.user.id })
  res.json({ url: link.url })
})
```

```html
<!-- 2. Frontend: a popup, resolved once they've connected -->
<script src="https://server.aegonassett.com/connect.js"></script>
<script>
  connectButton.onclick = async () => {
    const { url } = await fetch('/integrations/zoho/link', { method: 'POST' }).then((r) => r.json())
    const { connectionId } = await Relaya.connect(url) // rejects with err.code: closed | expired | failed | popup_blocked
  }
</script>
```

```ts
// 3. Backend: call Zoho as that user. Relaya adds and renews the token, and retries what is safe to retry.
const conn = await relaya.connections.find('zoho', user.id)
const res = await relaya.proxy(conn!.id).get('/crm/v2/Leads', { query: { per_page: 10 } })
if (res.ok) console.log(res.data)
// Zoho's own errors come back in res (check res.ok / res.status); a RelayaError means Relaya couldn't
// make the call, e.g. code 'connection_broken': send the user a new link.

// Another API host of the same provider, or extra headers for the provider:
await relaya.proxy(googleConn).get(`/v4/spreadsheets/${sheetId}/values/Sheet1`, { baseUrl: 'https://sheets.googleapis.com' })

// 4. Get new and changed records as events, e.g. zoho.lead.created / zoho.lead.updated every 15 minutes.
//    They arrive at your destinations like any other event (verify them with relayaHandler above).
await relaya.syncs.create({ connection_id: conn!.id, model: 'zoho.crm_records', config: { module: 'Leads' }, interval_minutes: 15 })
```

Prefer to call the provider yourself? `await relaya.connections.token(id)` gives a fresh access token and the `api_base` to use.

**Options:** `apiKey` (or `RELAYA_API_KEY`), `baseUrl` (or `RELAYA_BASE_URL`), `orgId` (only needed with a session token), `timeoutMs` (default 30 s), `maxRetries` (default 2), `fetch`.

**Errors:** failed calls throw `RelayaError` with `status`, `code` (e.g. `not_found`, `bad_request`, `network_error`, `timeout`) and `requestId`. GET requests are retried on network errors, `429` and `5xx` (honouring `Retry-After`); other methods are never retried automatically.

## Development

```sh
npm install
npm test            # unit tests
npm run typecheck
npm run build       # dist/esm + dist/cjs
RELAYA_IT_API_URL=http://127.0.0.1:18080 node --test test/integration.test.ts   # against a running dev stack
```
