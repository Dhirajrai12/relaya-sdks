"""Verify requests Relaya forwards to your endpoints."""

from __future__ import annotations

import hashlib
import hmac
import json
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Mapping, Optional, Sequence, Union

from .errors import WebhookVerificationError

HEADER_SIGNATURE = "Relaya-Signature"
HEADER_IDEMPOTENCY_KEY = "Idempotency-Key"
HEADER_EVENT_ID = "Relaya-Event-Id"
HEADER_DELIVERY_ID = "Relaya-Delivery-Id"
HEADER_ATTEMPT = "Relaya-Attempt"
HEADER_REPLAY = "Relaya-Replay"
HEADER_EVENT_TYPE = "Relaya-Event-Type"

#: Reject signatures older (or newer) than this many seconds by default.
DEFAULT_TOLERANCE = 300

Body = Union[bytes, bytearray, memoryview, str]
Secrets = Union[str, Sequence[str]]


def _bytes(body: Body) -> bytes:
    if isinstance(body, str):
        return body.encode("utf-8")
    if isinstance(body, (bytes, bytearray, memoryview)):
        return bytes(body)
    raise TypeError(
        "relaya: pass the raw request body (bytes or str), not parsed JSON. "
        "Django: request.body, Flask: request.get_data(), FastAPI: await request.body()"
    )


def _header(headers: Mapping[str, Any], name: str) -> Optional[str]:
    value = headers.get(name)
    if value is None:
        lower = name.lower()
        for k, v in headers.items():
            if k.lower() == lower or k.upper() == "HTTP_" + lower.upper().replace("-", "_"):
                value = v
                break
    if isinstance(value, (list, tuple)):
        value = value[0] if value else None
    return None if value is None else str(value)


def verify_signature(
    body: Body,
    signature_header: Optional[str],
    secret: Secrets,
    tolerance: int = DEFAULT_TOLERANCE,
    now: Optional[float] = None,
) -> datetime:
    """Check a ``Relaya-Signature`` header (``t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<body>")>``).

    ``secret`` may be a list while rotating (any match passes). ``tolerance=0`` disables the age check.
    Returns when the request was signed; raises :class:`WebhookVerificationError`.
    """
    secrets = [secret] if isinstance(secret, str) else [s for s in secret]
    secrets = [s for s in secrets if s]
    if not secrets:
        raise ValueError("relaya: a signing secret is required")
    if not signature_header:
        raise WebhookVerificationError("missing_signature", "The Relaya-Signature header is missing")

    timestamp: Optional[int] = None
    signatures = []
    for part in signature_header.split(","):
        key, _, value = part.strip().partition("=")
        if key == "t":
            try:
                timestamp = int(value)
            except ValueError:
                timestamp = None
        elif key == "v1" and value:
            signatures.append(value.lower())
    if timestamp is None or not signatures:
        raise WebhookVerificationError("malformed_signature", "The Relaya-Signature header is malformed")

    current = time.time() if now is None else now
    if tolerance > 0 and abs(current - timestamp) > tolerance:
        raise WebhookVerificationError(
            "timestamp_out_of_range", f"The signature is older than {tolerance} seconds (or from the future)"
        )

    signed = f"{timestamp}.".encode() + _bytes(body)
    for s in secrets:
        expected = hmac.new(s.encode("utf-8"), signed, hashlib.sha256).hexdigest()
        if any(hmac.compare_digest(expected, sig) for sig in signatures):
            return datetime.fromtimestamp(timestamp, tz=timezone.utc)
    raise WebhookVerificationError(
        "signature_mismatch", "The signature does not match: check the signing secret and that you pass the raw body"
    )


def is_valid_signature(body: Body, signature_header: Optional[str], secret: Secrets, **kwargs: Any) -> bool:
    """Like :func:`verify_signature` but returns True/False."""
    try:
        verify_signature(body, signature_header, secret, **kwargs)
        return True
    except WebhookVerificationError:
        return False


@dataclass(frozen=True)
class Delivery:
    """A verified request forwarded by Relaya."""

    #: The same across retries and replays of one delivery: dedupe on this.
    idempotency_key: str
    delivery_id: str
    event_id: str
    #: e.g. "payment.captured", when Relaya could tell.
    event_type: Optional[str]
    #: 1 for the first try, then 2, 3… on retries.
    attempt: int
    #: Set when the request is part of an incident replay.
    replay_id: Optional[str]
    signed_at: datetime
    #: The provider's original body, byte for byte.
    body: bytes = field(repr=False)

    def json(self) -> Any:
        return json.loads(self.body)


def verify_delivery(body: Body, headers: Mapping[str, Any], secret: Secrets, **kwargs: Any) -> Delivery:
    """Verify a request forwarded by Relaya and return its details.

    ``headers`` can be Django's ``request.headers`` (or ``request.META``), Flask's or FastAPI's ``request.headers``,
    or a plain dict.
    """
    raw = _bytes(body)
    signed_at = verify_signature(raw, _header(headers, HEADER_SIGNATURE), secret, **kwargs)
    delivery_id = _header(headers, HEADER_DELIVERY_ID) or ""
    try:
        attempt = max(1, int(_header(headers, HEADER_ATTEMPT) or 1))
    except ValueError:
        attempt = 1
    return Delivery(
        idempotency_key=_header(headers, HEADER_IDEMPOTENCY_KEY) or delivery_id,
        delivery_id=delivery_id,
        event_id=_header(headers, HEADER_EVENT_ID) or "",
        event_type=_header(headers, HEADER_EVENT_TYPE),
        attempt=attempt,
        replay_id=_header(headers, HEADER_REPLAY),
        signed_at=signed_at,
        body=raw,
    )


def verify_alert(body: Body, headers: Mapping[str, Any], secret: Secrets, **kwargs: Any) -> dict:
    """Verify an alert sent to a webhook alert channel and return it:
    ``{type, title, body, link, org_id, alert_id, sent_at}``."""
    raw = _bytes(body)
    verify_signature(raw, _header(headers, HEADER_SIGNATURE), secret, **kwargs)
    return json.loads(raw)
