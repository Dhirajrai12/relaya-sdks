import hashlib
import hmac
import json
import time
import unittest

from relaya import WebhookVerificationError, is_valid_signature, verify_alert, verify_delivery, verify_signature

SECRET = "whsec_test_secret"
BODY = '{"type":"payment.captured","amount":100,"note":"héllo"}'.encode()


def sign(body: bytes, secret: str = SECRET, t: int = None) -> str:
    """Same algorithm as the Relaya worker: hex HMAC-SHA256(secret, "<t>.<body>")."""
    t = int(time.time()) if t is None else t
    return f"t={t},v1=" + hmac.new(secret.encode(), f"{t}.".encode() + body, hashlib.sha256).hexdigest()


class VerifySignatureTest(unittest.TestCase):
    def reason(self, *args, **kwargs):
        with self.assertRaises(WebhookVerificationError) as cm:
            verify_signature(*args, **kwargs)
        return cm.exception.reason

    def test_valid_bytes_and_str(self):
        h = sign(BODY)
        verify_signature(BODY, h, SECRET)
        verify_signature(BODY.decode(), h, SECRET)
        verify_signature(bytearray(BODY), h, SECRET)
        self.assertTrue(is_valid_signature(BODY, h, SECRET))

    def test_rejections(self):
        self.assertEqual(self.reason(BODY, sign(BODY, "other"), SECRET), "signature_mismatch")
        self.assertEqual(self.reason(BODY + b" ", sign(BODY), SECRET), "signature_mismatch")
        self.assertEqual(self.reason(BODY, None, SECRET), "missing_signature")
        self.assertEqual(self.reason(BODY, "v1=abc", SECRET), "malformed_signature")
        self.assertEqual(self.reason(BODY, "t=x,v1=abc", SECRET), "malformed_signature")
        self.assertFalse(is_valid_signature(BODY, sign(BODY, "other"), SECRET))

    def test_tolerance(self):
        old = int(time.time()) - 301
        self.assertEqual(self.reason(BODY, sign(BODY, t=old), SECRET), "timestamp_out_of_range")
        verify_signature(BODY, sign(BODY, t=old), SECRET, tolerance=600)
        verify_signature(BODY, sign(BODY, t=old), SECRET, tolerance=0)
        at = verify_signature(BODY, sign(BODY, t=1_700_000_000), SECRET, now=1_700_000_005)
        self.assertEqual(at.timestamp(), 1_700_000_000)

    def test_rotation_and_bad_input(self):
        verify_signature(BODY, sign(BODY, "new"), ["old", "new"])
        with self.assertRaises(ValueError):
            verify_signature(BODY, sign(BODY), [])
        with self.assertRaisesRegex(TypeError, "raw request body"):
            verify_signature(json.loads(BODY), sign(BODY), SECRET)


class VerifyDeliveryTest(unittest.TestCase):
    def test_headers_any_case_and_django_meta(self):
        plain = {
            "relaya-signature": sign(BODY),
            "Idempotency-Key": "dlv_1",
            "RELAYA-DELIVERY-ID": "dlv_1",
            "Relaya-Event-Id": "evt_1",
            "Relaya-Attempt": "3",
            "Relaya-Event-Type": "payment.captured",
        }
        meta = {"HTTP_" + k.upper().replace("-", "_"): v for k, v in plain.items()}  # Django request.META
        for headers in (plain, meta):
            d = verify_delivery(BODY, headers, SECRET)
            self.assertEqual(d.idempotency_key, "dlv_1")
            self.assertEqual(d.event_id, "evt_1")
            self.assertEqual(d.attempt, 3)
            self.assertEqual(d.event_type, "payment.captured")
            self.assertIsNone(d.replay_id)
            self.assertEqual(d.json()["note"], "héllo")

    def test_alert(self):
        body = json.dumps({"type": "test", "title": "Test alert", "alert_id": 1}).encode()
        self.assertEqual(verify_alert(body, {"Relaya-Signature": sign(body)}, SECRET)["title"], "Test alert")


if __name__ == "__main__":
    unittest.main()
