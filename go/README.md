# Relaya Go SDK

Verify requests that Relaya forwards to your endpoints, and call the Relaya API. Standard library only; Go 1.23+.

```sh
go get github.com/Dhirajrai12/relaya-sdks/go
```

```go
import relaya "github.com/Dhirajrai12/relaya-sdks/go"
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

## Development

```sh
go test ./...
RELAYA_IT_API_URL=http://127.0.0.1:18080 go test -run Integration ./...   # against a running dev stack
```
