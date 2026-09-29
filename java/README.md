# Relaya Java SDK

Verify requests that Relaya forwards to your endpoints, and call the Relaya API. Java 17+; depends only on Jackson.

```xml
<dependency>
  <groupId>io.github.relayaa</groupId>
  <artifactId>relaya-java</artifactId>
  <version>0.1.0</version>
</dependency>
```

## Receive events

Relaya signs every request it forwards with your destination's signing secret (shown once when you add the destination). Take the body as raw bytes.

### Spring Boot

```java
@RestController
class RelayaWebhookController {
    @Value("${relaya.signing-secret}") String secret;

    @PostMapping("/webhooks/relaya")
    ResponseEntity<?> receive(@RequestBody byte[] body, @RequestHeader HttpHeaders headers) {
        Delivery delivery;
        try {
            delivery = Webhook.verifyDelivery(body, headers, secret);
        } catch (WebhookVerificationException e) {
            return ResponseEntity.badRequest().body(Map.of("error", e.reason()));
        }
        if (alreadyProcessed(delivery.idempotencyKey())) return ResponseEntity.ok().build();
        handle(delivery.json(PaymentEvent.class));
        return ResponseEntity.ok().build();
    }
}
```

Servlet: `Webhook.verifyDelivery(body, request::getHeader, Webhook.secret(secret))`.

| `Delivery` component | |
|---|---|
| `idempotencyKey()` | The same across retries and replays of one delivery. Dedupe on this. |
| `eventId()`, `deliveryId()` | Relaya's IDs. |
| `eventType()` | e.g. `payment.captured`, when Relaya could tell. |
| `attempt()` | 1, then 2, 3… on retries. |
| `replayId()` | Set when the request is part of an incident replay. |
| `body()`, `json()`, `json(Class)` | The provider's original body, unchanged. |

`e.reason()` is one of `missing_signature`, `malformed_signature`, `timestamp_out_of_range`, `signature_mismatch`. Rotating secrets: `new Webhook.Options().secret(oldSecret).secret(newSecret)`; `.tolerance(Duration)` sets the maximum signature age (default 5 minutes; `Duration.ZERO` disables). Webhook alert channels: `Webhook.verifyAlert(body, headers, options)`.

## Call the API

```java
Relaya relaya = Relaya.builder().apiKey(System.getenv("RELAYA_API_KEY")).build();

relaya.events()
      .stream(new EventFilters().contractStatus("breaking").since(Instant.now().minus(Duration.ofDays(7))))
      .forEach(e -> System.out.println(e.type() + " " + e.receivedAt()));

for (DeliveryRecord d : relaya.deliveries().list(null, "failed")) {
    relaya.deliveries().retry(d.id());
}

for (Incident incident : relaya.incidents().list("open")) {
    ReplayPlan plan = relaya.incidents().previewReplay(incident.id()); // dry run
    relaya.incidents().replay(incident.id());                          // resolves itself once every delivery succeeds
}
```

Resources: `projects()`, `webhooks()`, `events()` (`list`, `iterate`, `stream`, `get`), `destinations()`, `deliveries()`, `contracts()`, `incidents()`, `alerts()`. Responses are records in `io.relaya.model`. `relaya.request(method, path, body, query, type)` reaches anything else.

Builder: `apiKey` (or `RELAYA_API_KEY`), `baseUrl` (or `RELAYA_BASE_URL`), `orgId` (only with a session token), `timeout` (30 s), `maxRetries` (2; GET only, on network errors, 429 and 5xx).

Errors throw `RelayaException` (unchecked) with `status()`, `code()` and `requestId()`.

## Your users' accounts: connect, call, sync

Let your users connect their Zoho, HubSpot, Google or Shiprocket accounts; Relaya keeps their tokens fresh, calls the APIs for you and turns changes into events. Add the app once under **Connections** in the dashboard, then:

```java
// 1. A one-time link for one of your users; open it with connect.js (Relaya.connect(url)) or redirect them
ConnectLink link = relaya.connections().createLink("zoho", user.id());

// 2. Call Zoho as that user: Relaya adds and renews the token, and retries what is safe to retry
Connection conn = relaya.connections().find("zoho", user.id()).orElseThrow();
ProxyResponse res = relaya.proxy(conn.id()).get("/crm/v2/Leads", new ProxyOptions().query("per_page", 10));
if (res.ok()) {
    System.out.println(res.json().get("data"));
}
// Zoho's own errors come back in res (res.ok() / res.status()); RelayaException means Relaya couldn't make
// the call, e.g. code() "connection_broken": send the user a new link.

// 3. New and changed records as events (zoho.lead.created / .updated), delivered like any other event
relaya.syncs().create(conn.id(), "zoho.crm_records", Map.of("module", "Leads"), Map.of("interval_minutes", 15));
```

Also: `relaya.integrations()`, `relaya.connections().token(id)` (a fresh token and `apiBase` to call the provider yourself), `relaya.proxyCalls().list(null)`, `relaya.syncs().models()` / `runs(id)` / `run(id)`. `new ProxyOptions().baseUrl(...)` reaches another API host of the same provider.

## Send webhooks to your customers

If your product sends webhooks to its own customers, Relaya can do the sending: it signs each message with [Standard Webhooks](https://www.standardwebhooks.com), retries failures for up to a day and logs every attempt. Each customer manages their own endpoints in a hosted portal.

```java
// When a customer signs up: one app per customer, keyed by your own ID for them
relaya.outbound().apps().create(customer.id(), customer.name());

// Whenever something happens. With an idempotency key, sending the same message twice sends it once.
SentMessage msg = relaya.outbound().send(customer.id(), "invoice.paid",
        Map.of("invoice_id", invoice.id(), "amount", invoice.amount()), invoice.id() + "-paid");
// msg.endpoints(): how many endpoints it went to; msg.id() is the webhook-id header they receive

// A "Webhooks" button in your product: a 24-hour portal link where the customer adds endpoints,
// picks event types, sees deliveries and re-sends failures
String url = relaya.outbound().apps().portalLink(customer.id()).url();
```

Or manage endpoints for them: `relaya.outbound().endpoints().create(app, url, List.of("invoice.paid"))` (returns the `whsec_…` signing secret), `list`, `update`, `delete`, `secret`, `test`; event types in `relaya.outbound().eventTypes()`. Your customers verify requests with any Standard Webhooks library (`com.standardwebhooks:standardwebhooks`).

## Development

```sh
mvn test
RELAYA_IT_API_URL=http://127.0.0.1:18080 mvn test   # includes the test against a running dev stack
```
