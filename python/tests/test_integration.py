"""End-to-end against a running Relaya (API + ingest + worker). Skipped unless RELAYA_IT_API_URL is set:

    RELAYA_IT_API_URL=http://127.0.0.1:18080 python -m unittest tests.test_integration

The server must allow http://127.0.0.1 destinations (APP_ENV=dev) and use CONTRACT_MIN_SAMPLES=3.
"""

import json
import os
import threading
import time
import unittest
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from relaya import Relaya, RelayaError, WebhookVerificationError, verify_delivery

API = os.environ.get("RELAYA_IT_API_URL")


def wait_for(what, fn, timeout=15):
    until = time.time() + timeout
    while True:
        v = fn()
        if v:
            return v
        if time.time() > until:
            raise AssertionError(f"timed out waiting for {what}")
        time.sleep(0.15)


def post(path, body, token=None):
    req = urllib.request.Request(API + path, data=json.dumps(body).encode(), method="POST", headers={"Content-Type": "application/json", **({"Authorization": f"Bearer {token}"} if token else {})})
    with urllib.request.urlopen(req) as r:
        return json.loads(r.read())


@unittest.skipUnless(API, "set RELAYA_IT_API_URL to run")
class IntegrationTest(unittest.TestCase):
    def test_sdk_against_live_relaya(self):
        session = post("/v1/auth/signup", {"email": f"py-sdk-{time.time_ns()}@example.com", "password": "sdk-test-password-1", "org_name": "Python SDK"})
        org_id = Relaya(session["token"], base_url=API).request("GET", "/v1/me")["orgs"][0]["id"]
        key = post(f"/v1/orgs/{org_id}/api-keys", {"name": "sdk", "role": "admin"}, session["token"])

        relaya = Relaya(key["key"], base_url=API)
        self.assertEqual(relaya.org_id, org_id)

        # A customer endpoint verifying with the SDK.
        state = {"secret": "", "fail_next": 0, "received": []}

        class Endpoint(BaseHTTPRequestHandler):
            def log_message(self, *a):
                pass

            def do_POST(self):
                body = self.rfile.read(int(self.headers["Content-Length"]))
                try:
                    d = verify_delivery(body, self.headers, state["secret"])
                except WebhookVerificationError as e:
                    self.send_response(400)
                    self.end_headers()
                    self.wfile.write(e.reason.encode())
                    return
                state["received"].append(d)
                failing = state["fail_next"] > 0
                state["fail_next"] -= 1
                self.send_response(500 if failing else 200)
                self.end_headers()

        server = ThreadingHTTPServer(("127.0.0.1", 0), Endpoint)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        received = state["received"]

        project = relaya.projects.create("SDK")
        wh = relaya.webhooks.create(project["id"], "Payments", "generic")
        dest = relaya.destinations.create(wh["id"], "My app", f"http://127.0.0.1:{server.server_address[1]}/hooks")
        state["secret"] = dest["signing_secret"]

        def send(event_id, body):
            req = urllib.request.Request(wh["ingest_url"], data=json.dumps(body).encode(), method="POST", headers={"Content-Type": "application/json", "X-Event-Id": event_id})
            with urllib.request.urlopen(req) as r:
                self.assertEqual(r.status, 200)

        for i in range(1, 4):
            send(f"e{i}", {"type": "payment.captured", "amount": 100 * i})

        wait_for("3 deliveries", lambda: len(received) >= 3)
        first = received[0]
        self.assertEqual(first.idempotency_key, first.delivery_id)
        self.assertEqual(first.attempt, 1)
        self.assertEqual(first.event_type, "payment.captured")
        self.assertIsInstance(first.json()["amount"], int)

        events = list(relaya.events.iterate(webhook_id=wh["id"], limit=2))
        self.assertEqual(len(events), 3)
        self.assertEqual(relaya.events.get(first.event_id)["payload_json"]["type"], "payment.captured")

        ok = wait_for("succeeded deliveries", lambda: (lambda l: len(l) == 3 and l)(relaya.deliveries.list(webhook_id=wh["id"], status="succeeded")))
        self.assertEqual(relaya.deliveries.get(ok[0]["id"])["attempts"][0]["outcome"], "succeeded")

        # Failing endpoint, then a manual retry.
        state["fail_next"] = 1
        send("e4", {"type": "payment.captured", "amount": 400})
        retrying = wait_for("a retrying delivery", lambda: relaya.deliveries.list(webhook_id=wh["id"], status="retrying"))[0]
        relaya.deliveries.retry(retrying["id"])
        wait_for("the retry to succeed", lambda: relaya.deliveries.get(retrying["id"])["delivery"]["status"] == "succeeded")
        self.assertEqual(received[-1].attempt, 2)
        self.assertEqual(received[-1].idempotency_key, retrying["id"])

        # Contract -> incident -> replay.
        contract = wait_for("a proposed contract", lambda: next((c for c in relaya.contracts.list(wh["id"]) if c["status"] != "learning"), None))
        self.assertGreaterEqual(relaya.contracts.create_version(contract["id"], ["amount"], "observed"), 1)
        send("e5", {"type": "payment.captured", "amount": "500"})
        incident = wait_for("an open incident", lambda: relaya.incidents.list("open"))[0]
        self.assertIn("amount changed type", incident["title"])
        self.assertEqual(relaya.incidents.preview_replay(incident["id"])["events"], 1)
        before = len(received)
        replay = relaya.incidents.replay(incident["id"])
        self.assertEqual(replay["total"], 1)
        wait_for("the replayed delivery", lambda: len(received) > before)
        self.assertEqual(received[-1].replay_id, replay["id"])
        wait_for("the incident to resolve", lambda: relaya.incidents.list("open") == [])

        # Destination test and API errors.
        self.assertTrue(relaya.destinations.test(dest["destination"]["id"])["ok"])
        with self.assertRaises(RelayaError) as cm:
            relaya.webhooks.get("00000000-0000-0000-0000-000000000000")
        self.assertEqual(cm.exception.status, 404)
        with self.assertRaises(RelayaError) as cm:
            relaya.deliveries.retry(ok[0]["id"])
        self.assertEqual(cm.exception.status, 409)

    def test_outbound_against_live_relaya(self):
        import base64
        import hashlib
        import hmac

        session = post("/v1/auth/signup", {"email": f"py-out-{time.time_ns()}@example.com", "password": "sdk-test-password-1", "org_name": "SDK outbound"})
        req = urllib.request.Request(API + "/v1/me", headers={"Authorization": f"Bearer {session['token']}"})
        with urllib.request.urlopen(req) as r:
            org_id = json.loads(r.read())["orgs"][0]["id"]
        key = post(f"/v1/orgs/{org_id}/api-keys", {"name": "sdk", "role": "admin"}, session["token"])
        relaya = Relaya(key["key"], base_url=API)

        got = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
                got.append((dict(self.headers), body))
                self.send_response(200)
                self.end_headers()

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        url = f"http://127.0.0.1:{server.server_address[1]}/hooks"
        try:
            relaya.outbound.apps.create("customer-1", "Customer One")
            ep = relaya.outbound.endpoints.create("customer-1", url, event_types=["invoice.paid"])
            self.assertTrue(ep["signing_secret"].startswith("whsec_"))
            m = relaya.outbound.send("customer-1", "invoice.paid", {"invoice": "in_1"}, idempotency_key="in_1")
            self.assertEqual(m["endpoints"], 1)
            again = relaya.outbound.send("customer-1", "invoice.paid", {"invoice": "in_1"}, idempotency_key="in_1")
            self.assertTrue(again["duplicate"])
            self.assertEqual(again["id"], m["id"])

            headers, body = wait_for("the message", lambda: got and got[0])
            h = {k.lower(): v for k, v in headers.items()}
            key_bytes = base64.b64decode(ep["signing_secret"][len("whsec_"):])
            want = base64.b64encode(hmac.new(key_bytes, f"{h['webhook-id']}.{h['webhook-timestamp']}.".encode() + body, hashlib.sha256).digest()).decode()
            self.assertEqual(h["webhook-id"], m["id"])
            self.assertIn(f"v1,{want}", h["webhook-signature"].split(" "))
            self.assertEqual(json.loads(body)["data"]["invoice"], "in_1")

            self.assertTrue(relaya.outbound.endpoints.test("customer-1", ep["endpoint"]["id"])["ok"])
            self.assertIn("/portal#ps_", relaya.outbound.apps.portal_link("customer-1")["url"])
            self.assertTrue(any(t["name"] == "invoice.paid" for t in relaya.outbound.event_types.list()))
            relaya.outbound.apps.delete("customer-1")
            with self.assertRaises(RelayaError) as cm:
                relaya.outbound.apps.get("customer-1")
            self.assertEqual(cm.exception.status, 404)
        finally:
            server.shutdown()


if __name__ == "__main__":
    unittest.main()
