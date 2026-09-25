# relaya (Python)

Verify requests that Relaya forwards to your endpoints, and call the Relaya API. No dependencies; Python 3.9+.

```sh
pip install relaya
```

## Receive events

Relaya signs every request it forwards with your destination's signing secret (shown once when you add the destination). Always pass the **raw** body.

### Django

```python
from django.http import HttpResponse, JsonResponse
from django.views.decorators.csrf import csrf_exempt
from django.views.decorators.http import require_POST
from relaya import verify_delivery, WebhookVerificationError

@csrf_exempt
@require_POST
def relaya_webhook(request):
    try:
        delivery = verify_delivery(request.body, request.headers, settings.RELAYA_SIGNING_SECRET)
    except WebhookVerificationError as e:
        return JsonResponse({"error": e.reason}, status=400)
    if already_processed(delivery.idempotency_key):
        return HttpResponse()
    handle(delivery.json())
    return HttpResponse()
```

### Flask

```python
@app.post("/webhooks/relaya")
def relaya_webhook():
    try:
        delivery = verify_delivery(request.get_data(), request.headers, os.environ["RELAYA_SIGNING_SECRET"])
    except WebhookVerificationError as e:
        return {"error": e.reason}, 400
    handle(delivery.json())
    return "", 200
```

### FastAPI

```python
@app.post("/webhooks/relaya")
async def relaya_webhook(request: Request):
    try:
        delivery = verify_delivery(await request.body(), request.headers, os.environ["RELAYA_SIGNING_SECRET"])
    except WebhookVerificationError as e:
        raise HTTPException(400, e.reason)
    await handle(delivery.json())
```

Return a 5xx and Relaya retries later with the same idempotency key.

| `Delivery` field | |
|---|---|
| `idempotency_key` | The same across retries and replays of one delivery. Dedupe on this. |
| `event_id`, `delivery_id` | Relaya's IDs. |
| `event_type` | e.g. `payment.captured`, when Relaya could tell. |
| `attempt` | 1, then 2, 3… on retries. |
| `replay_id` | Set when the request is part of an incident replay. |
| `body`, `json()` | The provider's original body, unchanged. |

`e.reason` is one of `missing_signature`, `malformed_signature`, `timestamp_out_of_range`, `signature_mismatch`. Pass a list of secrets while rotating; `tolerance=` sets the maximum signature age in seconds (default 300, `0` disables). Webhook alert channels: `verify_alert(body, headers, secret)`.

## Call the API

```python
from datetime import datetime, timedelta, timezone
from relaya import Relaya

relaya = Relaya()  # reads RELAYA_API_KEY

for event in relaya.events.iterate(contract_status="breaking", since=datetime.now(timezone.utc) - timedelta(days=7)):
    print(event["type"], event["received_at"])

for d in relaya.deliveries.list(status="failed"):
    relaya.deliveries.retry(d["id"])

for incident in relaya.incidents.list("open"):
    plan = relaya.incidents.preview_replay(incident["id"])  # dry run
    relaya.incidents.replay(incident["id"])                 # resolves itself once every delivery succeeds
```

Resources: `projects`, `webhooks`, `events` (`list`, `iterate`, `get`), `destinations`, `deliveries`, `contracts`, `incidents`, `alerts`. Responses are dicts with the API's field names. `relaya.request(method, path, body, params)` reaches anything else.

Options: `api_key` (or `RELAYA_API_KEY`), `base_url` (or `RELAYA_BASE_URL`), `org_id` (only with a session token), `timeout` (30 s), `max_retries` (2; GET only, on network errors, 429 and 5xx).

Errors raise `RelayaError` with `status`, `code` and `request_id`.

## Development

```sh
PYTHONPATH=src python -m unittest discover -s tests
RELAYA_IT_API_URL=http://127.0.0.1:18080 PYTHONPATH=src python -m unittest tests.test_integration   # against a running dev stack
```
