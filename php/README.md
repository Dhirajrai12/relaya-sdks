# Relaya PHP SDK

Verify requests that Relaya forwards to your endpoints, and call the Relaya API. Includes Laravel middleware. PHP 8.1+ with `ext-curl`.

```sh
composer require dhirajrai12/relaya-php
```

## Receive events

Relaya signs every request it forwards with your destination's signing secret (shown once when you add the destination).

### Laravel

```php
// config/services.php
'relaya' => ['signing_secret' => env('RELAYA_SIGNING_SECRET')],

// routes/api.php
use Illuminate\Http\Request;
use Relaya\Laravel\VerifyRelayaSignature;

Route::post('/webhooks/relaya', function (Request $request) {
    $delivery = $request->attributes->get('relaya'); // Relaya\Delivery

    if (Cache::add("relaya:{$delivery->idempotencyKey}", true, now()->addDay())) {
        ProcessPayment::dispatch($delivery->json());
    }
    return response()->noContent();
})->middleware(VerifyRelayaSignature::class);
```

Bad signatures get `400 {"error": "<reason>"}` and never reach your route. For a second destination with its own secret: `->middleware(VerifyRelayaSignature::class.':services.relaya.orders_secret')`. If the route lives in `routes/web.php`, exclude it from CSRF.

### Plain PHP

```php
use Relaya\Webhook;
use Relaya\Exception\WebhookVerificationException;

try {
    $delivery = Webhook::fromGlobals(getenv('RELAYA_SIGNING_SECRET'));
} catch (WebhookVerificationException $e) {
    http_response_code(400);
    exit(json_encode(['error' => $e->reason]));
}
$event = $delivery->json();
```

Or with your framework's raw body and headers: `Webhook::verifyDelivery($rawBody, $headers, $secret)`.

| `Delivery` property | |
|---|---|
| `idempotencyKey` | The same across retries and replays of one delivery. Dedupe on this. |
| `eventId`, `deliveryId` | Relaya's IDs. |
| `eventType` | e.g. `payment.captured`, when Relaya could tell. |
| `attempt` | 1, then 2, 3… on retries. |
| `replayId` | Set when the request is part of an incident replay. |
| `body`, `json()` | The provider's original body, unchanged. |

`$e->reason` is one of `missing_signature`, `malformed_signature`, `timestamp_out_of_range`, `signature_mismatch`. Pass an array of secrets while rotating. Webhook alert channels: `Webhook::verifyAlert($body, $headers, $secret)`.

## Call the API

```php
$relaya = new Relaya\Client(getenv('RELAYA_API_KEY'));

foreach ($relaya->events->iterate(['contract_status' => 'breaking', 'since' => new DateTime('-7 days')]) as $event) {
    echo $event['type'], ' ', $event['received_at'], PHP_EOL;
}

foreach ($relaya->deliveries->list(['status' => 'failed']) as $d) {
    $relaya->deliveries->retry($d['id']);
}

foreach ($relaya->incidents->list('open') as $incident) {
    $plan = $relaya->incidents->previewReplay($incident['id']); // dry run
    $relaya->incidents->replay($incident['id']);               // resolves itself once every delivery succeeds
}
```

Resources: `projects`, `webhooks`, `events` (`list`, `iterate`, `get`), `destinations`, `deliveries`, `contracts`, `incidents`, `alerts`. Responses are arrays with the API's field names. `$relaya->request($method, $path, $body, $query)` reaches anything else.

Options (second constructor argument): `base_url` (or `RELAYA_BASE_URL`), `org_id` (only with a session token), `timeout` (30), `max_retries` (2; GET only, on network errors, 429 and 5xx).

Errors throw `Relaya\Exception\RelayaException` with `status`, `errorCode` and `requestId`.

## Development

Run these from the repository root (Packagist reads `composer.json` there):

```sh
composer install
vendor/bin/phpunit
RELAYA_IT_API_URL=http://127.0.0.1:18080 vendor/bin/phpunit   # includes the test against a running dev stack
```
