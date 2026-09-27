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

## Your users' accounts: connect, call, sync

Let your users connect their Zoho, HubSpot, Google or Shiprocket accounts; Relaya keeps their tokens fresh, calls the APIs for you and turns changes into events. Add the app once under **Connections** in the dashboard, then:

```python
# 1. A one-time link for one of your users; open it with connect.js (Relaya.connect(url)) or redirect them
link = relaya.connections.create_link("zoho", end_user_id=str(user.id))

# 2. Call Zoho as that user: Relaya adds and renews the token, and retries what is safe to retry
conn = relaya.connections.find("zoho", str(user.id))
res = relaya.proxy(conn["id"]).get("/crm/v2/Leads", params={"per_page": 10})
if res.ok:
    print(res.data)
# Zoho's own errors come back in res (res.ok / res.status); RelayaError means Relaya couldn't make the
# call, e.g. e.code == "connection_broken": send the user a new link.

# Another API host of the same provider:
relaya.proxy(google_conn).get(f"/v4/spreadsheets/{sheet_id}/values/Sheet1", base_url="https://sheets.googleapis.com")

# 3. New and changed records as events (zoho.lead.created / .updated), delivered like any other event
relaya.syncs.create(conn["id"], "zoho.crm_records", {"module": "Leads"}, interval_minutes=15)
```

Also: `relaya.integrations`, `relaya.connections.token(id)` (a fresh token and `api_base` to call the provider yourself), `relaya.proxy_calls.list()`, `relaya.syncs.models()` / `.runs(id)` / `.run(id)`.

## Development

```sh
PYTHONPATH=src python -m unittest discover -s tests
RELAYA_IT_API_URL=http://127.0.0.1:18080 PYTHONPATH=src python -m unittest tests.test_integration   # against a running dev stack
```
