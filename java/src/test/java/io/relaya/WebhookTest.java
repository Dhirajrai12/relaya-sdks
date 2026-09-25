package io.relaya;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.util.HexFormat;
import java.util.List;
import java.util.Map;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import org.junit.jupiter.api.Test;

class WebhookTest {
    static final String SECRET = "whsec_test_secret";
    static final byte[] BODY = "{\"type\":\"payment.captured\",\"amount\":100,\"note\":\"héllo\"}".getBytes(StandardCharsets.UTF_8);

    /** Same algorithm as the Relaya worker: hex HMAC-SHA256(secret, "<t>.<body>"). */
    static String sign(byte[] body, String secret, long t) throws Exception {
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
        mac.update((t + ".").getBytes(StandardCharsets.UTF_8));
        return "t=" + t + ",v1=" + HexFormat.of().formatHex(mac.doFinal(body));
    }

    static String sign(byte[] body) throws Exception {
        return sign(body, SECRET, Instant.now().getEpochSecond());
    }

    private static String reason(Runnable r) {
        return assertThrows(WebhookVerificationException.class, r::run).reason();
    }

    @Test
    void acceptsValidSignatures() throws Exception {
        Webhook.verifySignature(BODY, sign(BODY), Webhook.secret(SECRET));
        assertTrue(Webhook.isValidSignature(BODY, sign(BODY), Webhook.secret(SECRET)));
    }

    @Test
    void rejectsBadOnes() throws Exception {
        String wrong = sign(BODY, "other", Instant.now().getEpochSecond());
        byte[] tampered = (new String(BODY, StandardCharsets.UTF_8) + " ").getBytes(StandardCharsets.UTF_8);
        String good = sign(BODY);
        assertEquals("signature_mismatch", reason(() -> Webhook.verifySignature(BODY, wrong, Webhook.secret(SECRET))));
        assertEquals("signature_mismatch", reason(() -> Webhook.verifySignature(tampered, good, Webhook.secret(SECRET))));
        assertEquals("missing_signature", reason(() -> Webhook.verifySignature(BODY, null, Webhook.secret(SECRET))));
        assertEquals("malformed_signature", reason(() -> Webhook.verifySignature(BODY, "v1=abc", Webhook.secret(SECRET))));
        assertEquals("malformed_signature", reason(() -> Webhook.verifySignature(BODY, "t=x,v1=abc", Webhook.secret(SECRET))));
        assertFalse(Webhook.isValidSignature(BODY, wrong, Webhook.secret(SECRET)));
    }

    @Test
    void tolerance() throws Exception {
        long old = Instant.now().getEpochSecond() - 301;
        String h = sign(BODY, SECRET, old);
        assertEquals("timestamp_out_of_range", reason(() -> Webhook.verifySignature(BODY, h, Webhook.secret(SECRET))));
        Webhook.verifySignature(BODY, h, Webhook.secret(SECRET).tolerance(Duration.ofMinutes(10)));
        Webhook.verifySignature(BODY, h, Webhook.secret(SECRET).tolerance(Duration.ZERO));
        Instant at = Webhook.verifySignature(BODY, sign(BODY, SECRET, 1_700_000_000L), Webhook.secret(SECRET).now(Instant.ofEpochSecond(1_700_000_005L)));
        assertEquals(1_700_000_000L, at.getEpochSecond());
    }

    @Test
    void rotation() throws Exception {
        Webhook.verifySignature(BODY, sign(BODY, "new", Instant.now().getEpochSecond()), new Webhook.Options().secret("old").secret("new"));
        assertThrows(IllegalArgumentException.class, () -> Webhook.verifySignature(BODY, "t=1,v1=a", new Webhook.Options()));
    }

    @Test
    void deliveryFromSpringStyleHeaders() throws Exception {
        Map<String, List<String>> headers = Map.of(
                "relaya-signature", List.of(sign(BODY)),
                "Idempotency-Key", List.of("dlv_1"),
                "RELAYA-DELIVERY-ID", List.of("dlv_1"),
                "Relaya-Event-Id", List.of("evt_1"),
                "Relaya-Attempt", List.of("3"),
                "Relaya-Event-Type", List.of("payment.captured"));
        Delivery d = Webhook.verifyDelivery(BODY, headers, SECRET);
        assertEquals("dlv_1", d.idempotencyKey());
        assertEquals("evt_1", d.eventId());
        assertEquals(3, d.attempt());
        assertEquals("payment.captured", d.eventType());
        assertNull(d.replayId());
        assertEquals("héllo", d.json().get("note").asText());
    }

    @Test
    void alert() throws Exception {
        byte[] body = "{\"type\":\"test\",\"title\":\"Test alert\",\"alert_id\":7,\"sent_at\":\"2026-09-26T00:00:00Z\"}".getBytes(StandardCharsets.UTF_8);
        Alert a = Webhook.verifyAlert(body, Map.of("Relaya-Signature", sign(body)), Webhook.secret(SECRET));
        assertEquals("Test alert", a.title());
        assertEquals(7, a.alertId());
    }
}
