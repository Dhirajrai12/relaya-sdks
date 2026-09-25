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
                call = {"method": self.command, "path": u.path, "query": parse_qs(u.query), "body": body, "auth": self.headers.get("Authorization")}
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

            do_GET = do_POST = do_PATCH = do_DELETE = handle_any

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
