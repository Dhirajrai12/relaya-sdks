# Relaya Java SDK

Verify requests that Relaya forwards to your endpoints, and call the Relaya API. Java 17+; depends only on Jackson.

```xml
<dependency>
  <groupId>io.github.dhirajrai12</groupId>
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

## Development

```sh
mvn test
RELAYA_IT_API_URL=http://127.0.0.1:18080 mvn test   # includes the test against a running dev stack
```
