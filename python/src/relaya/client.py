"""Relaya API client. Responses are plain dicts with the API's field names."""

from __future__ import annotations

import json
import os
import random
import socket
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from typing import Any, Dict, Iterator, List, Optional

from . import __version__
from .errors import RelayaError

#: Where the API lives until the product has its own domain.
DEFAULT_BASE_URL = "https://server.aegonassett.com/api"

JSON = Dict[str, Any]


def _clean(params: Dict[str, Any]) -> Dict[str, str]:
    out = {}
    for k, v in params.items():
        if v is None or v == "":
            continue
        if isinstance(v, datetime):
            v = (v if v.tzinfo else v.replace(tzinfo=timezone.utc)).astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        out[k] = str(v)
    return out


def _backoff(attempt: int, retry_after: Optional[str]) -> float:
    if retry_after is not None:
        try:
            return min(max(float(retry_after), 0.0), 30.0)
        except ValueError:
            pass
    return min(0.5 * 2**attempt, 8.0) * (0.8 + random.random() * 0.4)


class Relaya:
    """Relaya API client.

    >>> relaya = Relaya(api_key=os.environ["RELAYA_API_KEY"])
    >>> for event in relaya.events.iterate(contract_status="breaking"):
    ...     print(event["type"])
    """

    def __init__(
        self,
        api_key: Optional[str] = None,
        *,
        org_id: Optional[str] = None,
        base_url: Optional[str] = None,
        timeout: float = 30.0,
        max_retries: int = 2,
    ) -> None:
        self.api_key = api_key or os.environ.get("RELAYA_API_KEY")
        if not self.api_key:
            raise ValueError("relaya: pass api_key or set RELAYA_API_KEY")
        self.base_url = (base_url or os.environ.get("RELAYA_BASE_URL") or DEFAULT_BASE_URL).rstrip("/")
        self.timeout = timeout
        self.max_retries = max_retries
        self._org_id = org_id
        self._org_lock = threading.Lock()

        self.projects = Projects(self)
        self.webhooks = Webhooks(self)
        self.events = Events(self)
        self.destinations = Destinations(self)
        self.deliveries = Deliveries(self)
        self.contracts = Contracts(self)
        self.incidents = Incidents(self)
        self.alerts = Alerts(self)

    @property
    def org_id(self) -> str:
        """The org this client acts on, looked up from the API key on first use."""
        with self._org_lock:
            if not self._org_id:
                me = self.request("GET", "/v1/me")
                if not me.get("api_key"):
                    raise ValueError("relaya: pass org_id when using a session token instead of an API key")
                self._org_id = me["api_key"]["org_id"]
            return self._org_id

    def request(self, method: str, path: str, body: Any = None, params: Optional[Dict[str, Any]] = None) -> Any:
        """Low-level request; paths start with /v1. Returns the decoded JSON (None for 204)."""
        url = self.base_url + path
        query = _clean(params or {})
        if query:
            url += "?" + urllib.parse.urlencode(query)
        data = None if body is None else json.dumps(body).encode()
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Accept": "application/json",
            "User-Agent": f"relaya-python/{__version__}",
        }
        if data is not None:
            headers["Content-Type"] = "application/json"
        retries = self.max_retries if method == "GET" else 0

        attempt = 0
        while True:
            req = urllib.request.Request(url, data=data, headers=headers, method=method)
            try:
                with urllib.request.urlopen(req, timeout=self.timeout) as res:
                    raw = res.read()
                    return json.loads(raw) if raw else None
            except urllib.error.HTTPError as e:
                raw = e.read()
                if (e.code == 429 or e.code >= 500) and attempt < retries:
                    time.sleep(_backoff(attempt, e.headers.get("Retry-After")))
                    attempt += 1
                    continue
                code, message = "http_error", f"HTTP {e.code}"
                try:
                    err = json.loads(raw).get("error") or {}
                    code, message = err.get("code", code), err.get("message", message)
                except (ValueError, AttributeError):
                    pass
                raise RelayaError(e.code, code, message, e.headers.get("X-Request-Id")) from None
            except (urllib.error.URLError, socket.timeout, TimeoutError, ConnectionError) as e:
                if attempt < retries:
                    time.sleep(_backoff(attempt, None))
                    attempt += 1
                    continue
                reason = getattr(e, "reason", e)
                timed_out = isinstance(reason, (socket.timeout, TimeoutError))
                raise RelayaError(
                    0,
                    "timeout" if timed_out else "network_error",
                    f"Request timed out after {self.timeout}s" if timed_out else f"Could not reach Relaya: {reason}",
                ) from None

    def _org(self, method: str, path: str, body: Any = None, params: Optional[Dict[str, Any]] = None) -> Any:
        return self.request(method, f"/v1/orgs/{self.org_id}{path}", body, params)


class _Resource:
    def __init__(self, client: Relaya) -> None:
        self._c = client


class Projects(_Resource):
    def list(self) -> List[JSON]:
        return self._c._org("GET", "/projects")["data"]

    def get(self, id: str) -> JSON:
        return self._c._org("GET", f"/projects/{id}")

    def create(self, name: str) -> JSON:
        return self._c._org("POST", "/projects", {"name": name})


class Webhooks(_Resource):
    def list(self, project_id: Optional[str] = None) -> List[JSON]:
        return self._c._org("GET", "/webhooks", params={"project_id": project_id})["data"]

    def get(self, id: str) -> JSON:
        return self._c._org("GET", f"/webhooks/{id}")

    def create(self, project_id: str, name: str, provider: str = "generic", **fields: Any) -> JSON:
        """Create an inbound webhook; give its ``ingest_url`` to the provider.
        Optional: signing_secret, signature_header."""
        return self._c._org("POST", "/webhooks", {"project_id": project_id, "name": name, "provider": provider, **fields})

    def update(self, id: str, **fields: Any) -> JSON:
        """Fields: name, status ("active"/"paused"), signing_secret, signature_header."""
        return self._c._org("PATCH", f"/webhooks/{id}", fields)

    def rotate_url(self, id: str) -> JSON:
        """Issue a new ingest URL; the old one stops working."""
        return self._c._org("POST", f"/webhooks/{id}/rotate-url")

    def delete(self, id: str) -> None:
        self._c._org("DELETE", f"/webhooks/{id}")


class Events(_Resource):
    def list(self, **filters: Any) -> JSON:
        """One page, newest first: ``{"data": [...], "next_cursor": ...}``.

        Filters: project_id, webhook_id, type, status, signature, contract_status, dedup_key,
        since, until (datetime or RFC 3339), limit (1-200), cursor.
        """
        return self._c._org("GET", "/events", params=filters)

    def iterate(self, **filters: Any) -> Iterator[JSON]:
        """Every matching event, newest first, fetching pages as you go."""
        filters.pop("cursor", None)
        cursor = None
        while True:
            page = self.list(**filters, cursor=cursor)
            yield from page["data"]
            cursor = page.get("next_cursor")
            if not cursor:
                return

    def get(self, id: str) -> JSON:
        """Full event: payload (sensitive fields masked), headers, deliveries and contract findings."""
        return self._c._org("GET", f"/events/{id}")


class Destinations(_Resource):
    def list(self, webhook_id: str) -> List[JSON]:
        return self._c._org("GET", f"/webhooks/{webhook_id}/destinations")["data"]

    def create(self, webhook_id: str, name: str, url: str, **fields: Any) -> JSON:
        """Returns ``{"destination": ..., "signing_secret": ...}``. The secret is shown once.
        Optional: max_attempts, timeout_ms, enabled."""
        return self._c._org("POST", f"/webhooks/{webhook_id}/destinations", {"name": name, "url": url, **fields})

    def update(self, id: str, **fields: Any) -> JSON:
        return self._c._org("PATCH", f"/destinations/{id}", fields)

    def delete(self, id: str) -> None:
        self._c._org("DELETE", f"/destinations/{id}")

    def rotate_secret(self, id: str) -> str:
        return self._c._org("POST", f"/destinations/{id}/rotate-secret")["signing_secret"]

    def test(self, id: str) -> JSON:
        """Send a signed test request now; returns ``{ok, status_code, duration_ms, response_body, error}``."""
        return self._c._org("POST", f"/destinations/{id}/test")


class Deliveries(_Resource):
    def list(self, **filters: Any) -> List[JSON]:
        """The latest 100 matching deliveries. Filters: event_id, destination_id, webhook_id, status."""
        return self._c._org("GET", "/deliveries", params=filters)["data"]

    def get(self, id: str) -> JSON:
        """``{"delivery": ..., "attempts": [...]}``"""
        return self._c._org("GET", f"/deliveries/{id}")

    def retry(self, id: str) -> JSON:
        """Send a failed or retrying delivery again now."""
        return self._c._org("POST", f"/deliveries/{id}/retry")


class Contracts(_Resource):
    def list(self, webhook_id: Optional[str] = None) -> List[JSON]:
        return self._c._org("GET", "/contracts", params={"webhook_id": webhook_id})["data"]

    def get(self, id: str) -> JSON:
        return self._c._org("GET", f"/contracts/{id}")

    def create_version(self, id: str, critical_fields: Optional[List[str]] = None, source: str = "observed") -> int:
        """Activate a new version: from what was ``observed``, or the ``active`` one with new critical fields.
        Returns the version number."""
        body: JSON = {"source": source}
        if critical_fields is not None:
            body["critical_fields"] = critical_fields
        return self._c._org("POST", f"/contracts/{id}/versions", body)["version"]

    def relearn(self, id: str) -> None:
        """Throw away what was learned and start learning again."""
        self._c._org("POST", f"/contracts/{id}/relearn")


class Incidents(_Resource):
    def list(self, status: Optional[str] = None) -> List[JSON]:
        """status: "open" (default) or "resolved"."""
        return self._c._org("GET", "/incidents", params={"status": status})["data"]

    def resolve(self, id: str, resolution: str) -> None:
        self._c._org("POST", f"/incidents/{id}/resolve", {"resolution": resolution})

    def preview_replay(self, id: str) -> JSON:
        """Dry run: which events and deliveries a replay would resend. Changes nothing."""
        return self._c._org("GET", f"/incidents/{id}/replay")

    def replay(self, id: str) -> JSON:
        """Resend the incident's deliveries. The incident resolves itself if all of them succeed."""
        return self._c._org("POST", f"/incidents/{id}/replay", {"confirm": True})


class Alerts(_Resource):
    def channels(self) -> List[JSON]:
        return self._c._org("GET", "/alert-channels")["data"]

    def log(self) -> List[JSON]:
        return self._c._org("GET", "/alerts")["data"]
