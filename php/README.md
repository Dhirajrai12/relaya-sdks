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

## Your users' accounts: connect, call, sync

Let your users connect their Zoho, HubSpot, Google or Shiprocket accounts; Relaya keeps their tokens fresh, calls the APIs for you and turns changes into events. Add the app once under **Connections** in the dashboard, then:

```php
// 1. A one-time link for one of your users; open it with connect.js (Relaya.connect(url)) or redirect them
$link = $relaya->connections->createLink('zoho', (string) $user->id);

// 2. Call Zoho as that user: Relaya adds and renews the token, and retries what is safe to retry
$conn = $relaya->connections->find('zoho', (string) $user->id);
$res = $relaya->proxy($conn['id'])->get('/crm/v2/Leads', ['query' => ['per_page' => 10]]);
if ($res->ok) {
    print_r($res->data);
}
// Zoho's own errors come back in $res ($res->ok / $res->status); RelayaException means Relaya couldn't
// make the call, e.g. errorCode 'connection_broken': send the user a new link.

// 3. New and changed records as events (zoho.lead.created / .updated), delivered like any other event
$relaya->syncs->create($conn['id'], 'zoho.crm_records', ['module' => 'Leads'], ['interval_minutes' => 15]);
```

Also: `$relaya->integrations`, `$relaya->connections->token($id)` (a fresh token and `api_base` to call the provider yourself), `$relaya->proxyCalls->list()`, `$relaya->syncs->models()` / `->runs($id)` / `->run($id)`.

## Send webhooks to your customers

If your product sends webhooks to its own customers, Relaya can do the sending: it signs each message with [Standard Webhooks](https://www.standardwebhooks.com), retries failures for up to a day and logs every attempt. Each customer manages their own endpoints in a hosted portal.

```php
// When a customer signs up: one app per customer, keyed by your own ID for them
$relaya->outbound->apps->create((string) $customer->id, $customer->name);

// Whenever something happens. With an idempotency key, sending the same message twice sends it once.
$msg = $relaya->outbound->send((string) $customer->id, 'invoice.paid', ['invoice_id' => $invoice->id, 'amount' => $invoice->amount], "{$invoice->id}-paid");
// $msg['endpoints']: how many endpoints it went to; $msg['id'] is the webhook-id header they receive

// A "Webhooks" button in your product: a 24-hour portal link where the customer adds endpoints,
// picks event types, sees deliveries and re-sends failures
$url = $relaya->outbound->apps->portalLink((string) $customer->id)['url'];
```

Or manage endpoints for them: `$relaya->outbound->endpoints->create($app, $url, ['event_types' => [...]])` (returns the `whsec_…` signing secret), `->list`, `->update`, `->delete`, `->secret`, `->test`; event types in `$relaya->outbound->eventTypes`. Your customers verify requests with any Standard Webhooks library (`composer require standard-webhooks/standard-webhooks`).

## Development

Run these from the repository root (Packagist reads `composer.json` there):

```sh
composer install
vendor/bin/phpunit
RELAYA_IT_API_URL=http://127.0.0.1:18080 vendor/bin/phpunit   # includes the test against a running dev stack
```
