# Relaya Go SDK

Verify requests that Relaya forwards to your endpoints, and call the Relaya API. Standard library only; Go 1.23+.

```sh
go get github.com/relayaa/relaya-sdks/go
```

```go
import relaya "github.com/relayaa/relaya-sdks/go"
```

## Receive events

Relaya signs every request it forwards with your destination's signing secret (shown once when you add the destination).

```go
secret := relaya.Secret(os.Getenv("RELAYA_SIGNING_SECRET"))

http.Handle("/webhooks/relaya", relaya.Middleware(secret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	d, _ := relaya.DeliveryFromContext(r.Context())
	if alreadyProcessed(d.IdempotencyKey) {
		return
	}
	var event struct{ Type string }
	d.JSON(&event)
	// … handle it; return a 5xx to have Relaya retry
})))
```

Bad signatures get `400 {"error": "<reason>"}` and never reach your handler. Without the middleware:

```go
d, err := relaya.VerifyRequest(r, secret) // or VerifyDelivery(body, r.Header, secret)
var ve *relaya.VerificationError
if errors.As(err, &ve) { /* ve.Reason: missing_signature, malformed_signature, timestamp_out_of_range, signature_mismatch */ }
```

| `Delivery` field | |
|---|---|
| `IdempotencyKey` | The same across retries and replays of one delivery. Dedupe on this. |
| `EventID`, `DeliveryID` | Relaya's IDs. |
| `EventType` | e.g. `payment.captured`, when Relaya could tell. |
| `Attempt` | 1, then 2, 3… on retries. |
| `ReplayID` | Set when the request is part of an incident replay. |
| `Body`, `JSON(v)` | The provider's original body, unchanged. |

Rotating secrets: `relaya.VerifyOptions{Secrets: []string{oldSecret, newSecret}}`. `Tolerance` sets the maximum signature age (default 5 minutes; negative disables).

Alert channels of type webhook are signed the same way: `relaya.VerifyAlert(body, r.Header, secret)`.

## Call the API

```go
c := relaya.New(os.Getenv("RELAYA_API_KEY"))
ctx := context.Background()

for e, err := range c.Events.All(ctx, relaya.EventFilters{ContractStatus: "breaking", Since: time.Now().Add(-7 * 24 * time.Hour)}) {
	if err != nil { return err }
	fmt.Println(e.Type, e.ReceivedAt)
}

failed, _ := c.Deliveries.List(ctx, relaya.DeliveryFilters{Status: "failed"})
for _, d := range failed {
	c.Deliveries.Retry(ctx, d.ID)
}

open, _ := c.Incidents.List(ctx, "open")
if len(open) > 0 {
	plan, _ := c.Incidents.PreviewReplay(ctx, open[0].ID) // dry run
	c.Incidents.Replay(ctx, open[0].ID)                   // resolves itself once every delivery succeeds
	_ = plan
}
```

Services: `Projects`, `Webhooks`, `Events` (`List`, `All`, `Get`), `Destinations`, `Deliveries`, `Contracts`, `Incidents`, `Alerts`. `c.Do(ctx, method, path, query, body, &out)` reaches anything else.

Options: `WithBaseURL` (or `RELAYA_BASE_URL`), `WithOrgID` (only with a session token), `WithHTTPClient`, `WithMaxRetries` (default 2; GET only, on network errors, 429 and 5xx).

Errors are `*relaya.Error` with `Status`, `Code` and `RequestID`; `relaya.IsNotFound(err)` checks for 404.

## Your users' accounts: connect, call, sync

Let your users connect their Zoho, HubSpot, Google or Shiprocket accounts; Relaya keeps their tokens fresh, calls the APIs for you and turns changes into events. Add the app once under **Connections** in the dashboard, then:

```go
// 1. A one-time link for one of your users; open it with connect.js (Relaya.connect(url)) or redirect them
link, err := c.Connections.CreateLink(ctx, "zoho", user.ID, "")

// 2. Call Zoho as that user: Relaya adds and renews the token, and retries what is safe to retry
conn, err := c.Connections.Find(ctx, "zoho", user.ID) // nil until they have connected
res, err := c.Proxy(conn.ID).Get(ctx, "/crm/v2/Leads", &relaya.ProxyOptions{Query: url.Values{"per_page": {"10"}}})
if err == nil && res.OK {
	var leads struct{ Data []map[string]any }
	res.JSON(&leads)
}
// Zoho's own errors come back in res (res.OK / res.Status); a *relaya.Error means Relaya couldn't make the
// call, e.g. Code "connection_broken": send the user a new link.

// 3. New and changed records as events (zoho.lead.created / .updated), delivered like any other event
c.Syncs.Create(ctx, relaya.SyncInput{ConnectionID: conn.ID, Model: "zoho.crm_records", Config: map[string]string{"module": "Leads"}, IntervalMinutes: 15})
```

Also: `c.Integrations`, `c.Connections.Token(ctx, id)` (a fresh token and `APIBase` to call the provider yourself), `c.ProxyCalls.List`, `c.Syncs.Models` / `Runs` / `Run`. `ProxyOptions.BaseURL` reaches another API host of the same provider (e.g. `https://sheets.googleapis.com`).

## Send webhooks to your customers

If your product sends webhooks to its own customers, Relaya can do the sending: it signs each message with [Standard Webhooks](https://www.standardwebhooks.com), retries failures for up to a day and logs every attempt. Each customer manages their own endpoints in a hosted portal.

```go
// When a customer signs up: one app per customer, keyed by your own ID for them
c.Outbound.Apps.Create(ctx, customer.ID, customer.Name)

// Whenever something happens. With an IdempotencyKey, sending the same message twice sends it once.
msg, err := c.Outbound.Send(ctx, relaya.Message{
	App:            customer.ID,
	EventType:      "invoice.paid",
	Payload:        map[string]any{"invoice_id": inv.ID, "amount": inv.Amount},
	IdempotencyKey: inv.ID + "-paid",
})
// msg.Endpoints: how many endpoints it went to; msg.ID is the webhook-id header they receive

// A "Webhooks" button in your product: a 24-hour portal link where the customer adds endpoints,
// picks event types, sees deliveries and re-sends failures
link, err := c.Outbound.Apps.PortalLink(ctx, customer.ID)
```

Or manage endpoints for them: `c.Outbound.Endpoints.Create(ctx, app, relaya.EndpointInput{URL: ..., EventTypes: ...})` (returns the `whsec_…` signing secret), `List`, `Update`, `Delete`, `Secret`, `Test`; event types in `c.Outbound.EventTypes`. Your customers verify requests with any Standard Webhooks library (`github.com/standard-webhooks/standard-webhooks/libraries/go`).

## Development

```sh
go test ./...
RELAYA_IT_API_URL=http://127.0.0.1:18080 go test -run Integration ./...   # against a running dev stack
```
