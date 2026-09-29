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
DEFAULT_BASE_URL = "https://api.relaya.sbs"

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
        self.integrations = Integrations(self)
        self.connections = Connections(self)
        self.proxy_calls = ProxyCalls(self)
        self.syncs = Syncs(self)
        self.outbound = Outbound(self)

    def proxy(self, connection_id: str) -> "Proxy":
        """Call the provider's API as the connected user; Relaya adds and renews the token.

        >>> res = relaya.proxy(connection_id).get("/crm/v2/Leads", params={"per_page": 10})
        >>> if res.ok: print(res.data)

        The provider's answer comes back as is, errors included (check ``ok``/``status``).
        ``RelayaError`` is raised only when Relaya couldn't make the call
        (e.g. ``code == "connection_broken"``).
        """
        return Proxy(self, connection_id)

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
        Optional: max_attempts, timeout_ms, enabled, event_types (only these are forwarded; empty = all)."""
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


# ---- connections: your users' accounts at other apps ------------------------------


class Integrations(_Resource):
    def list(self) -> List[JSON]:
        return self._c._org("GET", "/integrations")["data"]

    def create(self, provider: str, **fields: Any) -> JSON:
        """Zoho, HubSpot and Google need your OAuth app's client_id and client_secret;
        Shiprocket needs neither. Optional: key, name, scopes."""
        return self._c._org("POST", "/integrations", {"provider": provider, **fields})

    def update(self, id: str, **fields: Any) -> JSON:
        """Fields: name, client_id, client_secret, scopes."""
        return self._c._org("PATCH", f"/integrations/{id}", fields)

    def delete(self, id: str) -> None:
        """Deletes its connections too."""
        self._c._org("DELETE", f"/integrations/{id}")


class Connections(_Resource):
    def create_link(self, integration: str, end_user_id: str, return_url: Optional[str] = None) -> JSON:
        """A one-time link (30 minutes) where your user connects their account:
        ``{"id", "url", "expires_at"}``. Open ``url`` with connect.js or redirect the user to it."""
        body: JSON = {"integration": integration, "end_user_id": end_user_id}
        if return_url:
            body["return_url"] = return_url
        return self._c._org("POST", "/connect-sessions", body)

    def list(self, integration: Optional[str] = None, end_user_id: Optional[str] = None, status: Optional[str] = None) -> List[JSON]:
        return self._c._org("GET", "/connections", params={"integration": integration, "end_user_id": end_user_id, "status": status})["data"]

    def get(self, id: str) -> JSON:
        return self._c._org("GET", f"/connections/{id}")

    def find(self, integration: str, end_user_id: str) -> Optional[JSON]:
        """The connection for one of your users, or None."""
        found = self.list(integration=integration, end_user_id=end_user_id)
        return found[0] if found else None

    def token(self, id: str) -> JSON:
        """A working access token (renewed first when about to expire):
        ``{"access_token", "token_type", "expires_at", "api_base", ...}``. Use it right away."""
        return self._c._org("GET", f"/connections/{id}/token")

    def refresh(self, id: str) -> JSON:
        """Renew the token now: ``{"connection", "refreshed", "error"?}``."""
        return self._c._org("POST", f"/connections/{id}/refresh")

    def delete(self, id: str) -> None:
        self._c._org("DELETE", f"/connections/{id}")


class ProxyResponse:
    """The provider's answer to a proxied call."""

    def __init__(self, status: int, headers: Dict[str, str], data: Any, attempts: int) -> None:
        self.status = status
        self.ok = 200 <= status < 300
        self.headers = headers
        #: Parsed JSON, or the text when the answer isn't JSON.
        self.data = data
        #: How many times Relaya called the provider (retries, token renewal).
        self.attempts = attempts

    def __repr__(self) -> str:
        return f"<ProxyResponse {self.status} attempts={self.attempts}>"


class Proxy:
    def __init__(self, client: Relaya, connection_id: str) -> None:
        self._c = client
        self._id = connection_id

    def request(
        self,
        method: str,
        path: str,
        *,
        params: Optional[Dict[str, Any]] = None,
        json_body: Any = None,
        headers: Optional[Dict[str, str]] = None,
        base_url: Optional[str] = None,
        timeout: float = 120.0,
    ) -> ProxyResponse:
        """``headers`` go to the provider (as Relaya-Proxy-<name>); your API key never does.
        ``base_url``: another API host of the same provider, e.g. https://sheets.googleapis.com."""
        c = self._c
        url = f"{c.base_url}/v1/orgs/{c.org_id}/connections/{self._id}/proxy/{path.lstrip('/')}"
        query = _clean(params or {})
        if query:
            url += "?" + urllib.parse.urlencode(query)
        h = {"Authorization": f"Bearer {c.api_key}", "Accept": "application/json", "User-Agent": f"relaya-python/{__version__}"}
        for k, v in (headers or {}).items():
            h[f"Relaya-Proxy-{k}"] = v
        if base_url:
            h["Relaya-Proxy-Base-Url"] = base_url
        data = None
        if json_body is not None:
            data = json_body.encode() if isinstance(json_body, str) else json.dumps(json_body).encode()
            h["Content-Type"] = "application/json"
        req = urllib.request.Request(url, data=data, headers=h, method=method)
        # One attempt: Relaya already retries what is safe to retry.
        try:
            with urllib.request.urlopen(req, timeout=timeout) as res:
                return self._response(res.status, res.headers, res.read())
        except urllib.error.HTTPError as e:
            raw = e.read()
            relaya_error = e.headers.get("Relaya-Proxy-Error") == "true" or (e.code == 401 and "Relaya-Proxy-Attempts" not in e.headers)
            if relaya_error:
                code, message = "proxy_error", f"HTTP {e.code}"
                try:
                    err = json.loads(raw).get("error") or {}
                    code, message = err.get("code", code), err.get("message", message)
                except (ValueError, AttributeError):
                    pass
                raise RelayaError(e.code, code, message, e.headers.get("X-Request-Id")) from None
            return self._response(e.code, e.headers, raw)
        except (urllib.error.URLError, socket.timeout, TimeoutError, ConnectionError) as e:
            raise RelayaError(0, "network_error", f"Could not reach Relaya: {getattr(e, 'reason', e)}") from None

    @staticmethod
    def _response(status: int, headers: Any, raw: bytes) -> ProxyResponse:
        text = raw.decode("utf-8", "replace")
        try:
            data: Any = json.loads(text) if text else None
        except ValueError:
            data = text
        return ProxyResponse(status, dict(headers.items()), data, int(headers.get("Relaya-Proxy-Attempts") or 1))

    def get(self, path: str, **kw: Any) -> ProxyResponse:
        return self.request("GET", path, **kw)

    def post(self, path: str, json_body: Any = None, **kw: Any) -> ProxyResponse:
        return self.request("POST", path, json_body=json_body, **kw)

    def put(self, path: str, json_body: Any = None, **kw: Any) -> ProxyResponse:
        return self.request("PUT", path, json_body=json_body, **kw)

    def patch(self, path: str, json_body: Any = None, **kw: Any) -> ProxyResponse:
        return self.request("PATCH", path, json_body=json_body, **kw)

    def delete(self, path: str, **kw: Any) -> ProxyResponse:
        return self.request("DELETE", path, **kw)


class ProxyCalls(_Resource):
    def list(self, connection: Optional[str] = None) -> List[JSON]:
        """The last 100 calls made through connections (no query strings or bodies)."""
        return self._c._org("GET", "/proxy-calls", params={"connection": connection})["data"]


# ---- syncs: changes in connected apps become events ---------------------------------


class Syncs(_Resource):
    def models(self) -> List[JSON]:
        """What can be synced, per provider, and the settings each needs."""
        return self._c.request("GET", "/v1/connect/sync-models")["data"]

    def list(self) -> List[JSON]:
        return self._c._org("GET", "/syncs")["data"]

    def create(self, connection_id: str, model: str, config: Optional[Dict[str, str]] = None, **fields: Any) -> JSON:
        """Start syncing, e.g. ``create(conn_id, "zoho.crm_records", {"module": "Leads"})``.
        Optional: interval_minutes (default 15), webhook_id, emit_existing.
        Events land on a new webhook unless you pass webhook_id; add a destination there to receive them."""
        return self._c._org("POST", "/syncs", {"connection_id": connection_id, "model": model, "config": config or {}, **fields})

    def update(self, id: str, **fields: Any) -> JSON:
        """Fields: enabled, interval_minutes, config (a new config starts the sync over)."""
        return self._c._org("PATCH", f"/syncs/{id}", fields)

    def delete(self, id: str) -> None:
        self._c._org("DELETE", f"/syncs/{id}")

    def run(self, id: str) -> JSON:
        """Run it within seconds instead of waiting for the schedule."""
        return self._c._org("POST", f"/syncs/{id}/run")

    def runs(self, id: str) -> List[JSON]:
        return self._c._org("GET", f"/syncs/{id}/runs")["data"]


# ---- outbound webhooks: send events to your own customers ---------------------------


def _app(app: str) -> str:
    return urllib.parse.quote(app, safe="")


class OutboundApps(_Resource):
    """One app per customer; refer to it by your own uid (or its id) everywhere."""

    def list(self) -> List[JSON]:
        return self._c._org("GET", "/outbound/apps")["data"]

    def get(self, app: str) -> JSON:
        """``{"app": ..., "endpoints": [...]}``."""
        return self._c._org("GET", f"/outbound/apps/{_app(app)}")

    def create(self, uid: str, name: Optional[str] = None) -> JSON:
        body: JSON = {"uid": uid}
        if name:
            body["name"] = name
        return self._c._org("POST", "/outbound/apps", body)

    def delete(self, app: str) -> None:
        """Also deletes its endpoints and message history."""
        self._c._org("DELETE", f"/outbound/apps/{_app(app)}")

    def portal_link(self, app: str) -> JSON:
        """A 24-hour link where the customer manages their endpoints and sees deliveries:
        ``{"url", "expires_at"}``."""
        return self._c._org("POST", f"/outbound/apps/{_app(app)}/portal-link")


class OutboundEndpoints(_Resource):
    """Customers manage these themselves in the portal; these let you do it for them."""

    def list(self, app: str) -> List[JSON]:
        return self._c.outbound.apps.get(app)["endpoints"]

    def create(self, app: str, url: str, **fields: Any) -> JSON:
        """Returns ``{"endpoint": ..., "signing_secret": "whsec_..."}``.
        Optional: description, event_types (only these are sent; empty = all)."""
        return self._c._org("POST", f"/outbound/apps/{_app(app)}/endpoints", {"url": url, **fields})

    def update(self, app: str, id: str, **fields: Any) -> JSON:
        """Fields: url, description, event_types, enabled."""
        return self._c._org("PATCH", f"/outbound/apps/{_app(app)}/endpoints/{id}", fields)

    def delete(self, app: str, id: str) -> None:
        self._c._org("DELETE", f"/outbound/apps/{_app(app)}/endpoints/{id}")

    def secret(self, app: str, id: str) -> str:
        return self._c._org("GET", f"/outbound/apps/{_app(app)}/endpoints/{id}/secret")["signing_secret"]

    def test(self, app: str, id: str, event_type: Optional[str] = None) -> JSON:
        """Send a signed test event now; returns ``{ok, status_code, duration_ms, response_body, error}``."""
        return self._c._org("POST", f"/outbound/apps/{_app(app)}/endpoints/{id}/test", params={"event_type": event_type})


class OutboundEventTypes(_Resource):
    """The catalog customers pick from in the portal. Types you send are added automatically."""

    def list(self) -> List[JSON]:
        return self._c._org("GET", "/outbound/event-types")["data"]

    def save(self, name: str, description: str = "") -> JSON:
        """Add it, or update its description."""
        return self._c._org("POST", "/outbound/event-types", {"name": name, "description": description})

    def delete(self, name: str) -> None:
        self._c._org("DELETE", f"/outbound/event-types/{_app(name)}")


class Outbound(_Resource):
    def __init__(self, client: Relaya) -> None:
        super().__init__(client)
        self.apps = OutboundApps(client)
        self.endpoints = OutboundEndpoints(client)
        self.event_types = OutboundEventTypes(client)

    def send(self, app: str, event_type: str, payload: JSON, idempotency_key: Optional[str] = None) -> JSON:
        """Send an event to every endpoint of that customer that takes its type. Relaya signs it
        (Standard Webhooks), retries failures and logs every attempt. Returns
        ``{"id", "app", "event_type", "endpoints", "duplicate"}``; with an idempotency_key, sending
        the same message again returns the first one (duplicate True) instead of a copy."""
        body: JSON = {"app": app, "event_type": event_type, "payload": payload}
        if idempotency_key:
            body["idempotency_key"] = idempotency_key
        return self._c._org("POST", "/outbound/messages", body)
