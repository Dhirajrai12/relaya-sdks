from __future__ import annotations

from typing import Optional


class RelayaError(Exception):
    """An error response from the Relaya API."""

    def __init__(self, status: int, code: str, message: str, request_id: Optional[str] = None) -> None:
        super().__init__(message)
        #: HTTP status, or 0 when no response arrived.
        self.status = status
        #: e.g. "not_found", "bad_request", "network_error", "timeout".
        self.code = code
        self.message = message
        self.request_id = request_id

    def __str__(self) -> str:
        return self.message if self.status == 0 else f"{self.status} {self.code}: {self.message}"


class WebhookVerificationError(Exception):
    """A request claiming to come from Relaya failed signature verification.

    ``reason`` is one of: missing_signature, malformed_signature, timestamp_out_of_range, signature_mismatch.
    """

    def __init__(self, reason: str, message: str) -> None:
        super().__init__(message)
        self.reason = reason
