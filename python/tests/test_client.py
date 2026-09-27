import json
import threading
import unittest
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

from relaya import Relaya, RelayaError


class FakeAPI:
    """A tiny HTTP server: routes "METHOD /path" to (status, body, headers) callables."""

    def __init__(self, routes):
        self.calls = []
        api = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def handle_any(self):
                u = urlparse(self.path)
                length = int(self.headers.get("Content-Length") or 0)
                body = json.loads(self.rfile.read(length)) if length else None
                call = {"method": self.command, "path": u.path, "query": parse_qs(u.query), "body": body, "auth": self.headers.get("Authorization"), "headers": {k.lower(): v for k, v in self.headers.items()}}
                api.calls.append(call)
                route = routes.get(f"{self.command} {u.path}")
                n = sum(1 for c in api.calls if c["method"] == self.command and c["path"] == u.path)
                status, payload, headers = route(call, n) if route else (404, {"error": {"code": "not_found", "message": "no route"}}, {})
                self.send_response(status)
                for k, v in headers.items():
                    self.send_header(k, v)
                data = b"" if payload is None else json.dumps(payload).encode()
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = handle_any

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"

    def close(self):
        self.server.shutdown()


class ClientTest(unittest.TestCase):
    def api(self, routes):
        a = FakeAPI(routes)
        self.addCleanup(a.close)
        return a

    def test_org_lookup_once_and_auth(self):
        a = self.api({
            "GET /v1/me": lambda c, n: (200, {"api_key": {"org_id": "org1"}}, {}),
            "GET /v1/orgs/org1/projects": lambda c, n: (200, {"data": [{"id": "p1"}]}, {}),
        })
        r = Relaya("rk_test", base_url=a.url + "/")
        self.assertEqual(r.projects.list()[0]["id"], "p1")
        r.projects.list()
        self.assertEqual(sum(1 for c in a.calls if c["path"] == "/v1/me"), 1)
        self.assertTrue(all(c["auth"] == "Bearer rk_test" for c in a.calls))

    def test_filters_and_iterate(self):
        def events(c, n):
            if c["query"].get("cursor") == ["c2"]:
                return 200, {"data": [{"id": "e3"}], "next_cursor": None}, {}
            return 200, {"data": [{"id": "e1"}, {"id": "e2"}], "next_cursor": "c2"}, {}

        a = self.api({"GET /v1/orgs/o/events": events})
        r = Relaya("rk", org_id="o", base_url=a.url)
        ids = [e["id"] for e in r.events.iterate(contract_status="breaking", since=datetime(2026, 9, 1, tzinfo=timezone.utc), limit=2, type=None)]
        self.assertEqual(ids, ["e1", "e2", "e3"])
        q = a.calls[0]["query"]
        self.assertEqual(q["contract_status"], ["breaking"])
        self.assertEqual(q["since"], ["2026-09-01T00:00:00Z"])
        self.assertNotIn("type", q)

    def test_errors_and_retries(self):
        a = self.api({
            "GET /v1/orgs/o/incidents": lambda c, n: (503, None, {"Retry-After": "0"}) if n < 3 else (200, {"data": []}, {}),
            "POST /v1/orgs/o/incidents/i1/replay": lambda c, n: (503, None, {"Retry-After": "0"}),
        })
        r = Relaya("rk", org_id="o", base_url=a.url)
        self.assertEqual(r.incidents.list("open"), [])
        with self.assertRaises(RelayaError) as cm:
            r.incidents.replay("i1")
        self.assertEqual(cm.exception.status, 503)
        posts = [c for c in a.calls if c["method"] == "POST"]
        self.assertEqual(len(posts), 1)
        self.assertEqual(posts[0]["body"], {"confirm": True})
        with self.assertRaises(RelayaError) as cm:
            r.webhooks.get("nope")
        self.assertEqual((cm.exception.status, cm.exception.code), (404, "not_found"))

    def test_network_error(self):
        r = Relaya("rk", org_id="o", base_url="http://127.0.0.1:1", max_retries=0)
        with self.assertRaises(RelayaError) as cm:
            r.projects.list()
        self.assertEqual((cm.exception.status, cm.exception.code), (0, "network_error"))


if __name__ == "__main__":
    unittest.main()


class ConnectionsTest(unittest.TestCase):
    def api(self, routes):
        a = FakeAPI(routes)
        self.addCleanup(a.close)
        return a

    def test_link_find_token(self):
        a = self.api({
            "POST /v1/orgs/o/connect-sessions": lambda c, n: (201, {"id": "s1", "url": "https://r/connect/cs_x", "expires_at": "z"}, {}),
            "GET /v1/orgs/o/connections": lambda c, n: (200, {"data": [{"id": "c1"}] if c["query"].get("end_user_id") == ["u1"] else []}, {}),
            "GET /v1/orgs/o/connections/c1/token": lambda c, n: (200, {"access_token": "at", "api_base": "https://www.zohoapis.in"}, {}),
        })
        r = Relaya("rk", org_id="o", base_url=a.url)
        self.assertEqual(r.connections.create_link("zoho", "u1")["url"], "https://r/connect/cs_x")
        self.assertEqual(a.calls[0]["body"], {"integration": "zoho", "end_user_id": "u1"})
        self.assertEqual(r.connections.find("zoho", "u1")["id"], "c1")
        self.assertIsNone(r.connections.find("zoho", "nobody"))
        self.assertEqual(r.connections.token("c1")["api_base"], "https://www.zohoapis.in")

    def test_proxy(self):
        a = self.api({
            "GET /v1/orgs/o/connections/c1/proxy/crm/v2/Leads": lambda c, n: (200, {"data": [{"id": "1"}]}, {"Relaya-Proxy-Attempts": "2"}),
            "POST /v1/orgs/o/connections/c1/proxy/crm/v2/Leads": lambda c, n: (201, {"ok": True, "echo": c["body"]}, {"Relaya-Proxy-Attempts": "1"}),
            "GET /v1/orgs/o/connections/c1/proxy/missing": lambda c, n: (404, {"code": "INVALID_URL_PATTERN"}, {"Relaya-Proxy-Attempts": "1"}),
            "GET /v1/orgs/o/connections/c2/proxy/x": lambda c, n: (409, {"error": {"code": "connection_broken", "message": "reconnect"}}, {"Relaya-Proxy-Error": "true"}),
        })
        r = Relaya("rk", org_id="o", base_url=a.url)
        res = r.proxy("c1").get("/crm/v2/Leads", params={"per_page": 10}, headers={"orgId": "42"}, base_url="https://www.zohoapis.in")
        self.assertTrue(res.ok)
        self.assertEqual((res.status, res.attempts, res.data["data"][0]["id"]), (200, 2, "1"))
        call = a.calls[0]
        self.assertEqual(call["query"], {"per_page": ["10"]})
        self.assertEqual(call["headers"]["relaya-proxy-orgid"], "42")
        self.assertEqual(call["headers"]["relaya-proxy-base-url"], "https://www.zohoapis.in")
        self.assertEqual(call["auth"], "Bearer rk")

        created = r.proxy("c1").post("/crm/v2/Leads", {"data": [{"Last_Name": "Rao"}]})
        self.assertEqual((created.status, created.data["echo"]), (201, {"data": [{"Last_Name": "Rao"}]}))

        missing = r.proxy("c1").get("/missing")  # the provider's own error: returned, not raised
        self.assertFalse(missing.ok)
        self.assertEqual(missing.status, 404)

        with self.assertRaises(RelayaError) as e:  # Relaya couldn't make the call: raised
            r.proxy("c2").get("/x")
        self.assertEqual((e.exception.status, e.exception.code), (409, "connection_broken"))

    def test_syncs(self):
        a = self.api({
            "GET /v1/connect/sync-models": lambda c, n: (200, {"data": [{"key": "zoho.crm_records"}]}, {}),
            "POST /v1/orgs/o/syncs": lambda c, n: (201, {"id": "sy1", **c["body"]}, {}),
            "POST /v1/orgs/o/syncs/sy1/run": lambda c, n: (202, {"id": "sy1", "running": True}, {}),
        })
        r = Relaya("rk", org_id="o", base_url=a.url)
        self.assertEqual(r.syncs.models()[0]["key"], "zoho.crm_records")
        s = r.syncs.create("c1", "zoho.crm_records", {"module": "Leads"}, interval_minutes=15)
        self.assertEqual(a.calls[1]["body"], {"connection_id": "c1", "model": "zoho.crm_records", "config": {"module": "Leads"}, "interval_minutes": 15})
        self.assertEqual(s["id"], "sy1")
        self.assertTrue(r.syncs.run("sy1")["running"])
