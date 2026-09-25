package io.relaya;

import com.fasterxml.jackson.databind.JsonNode;
import java.nio.charset.StandardCharsets;
import java.time.Instant;

/**
 * A verified request forwarded by Relaya.
 *
 * @param idempotencyKey the same across retries and replays of one delivery: dedupe on this
 * @param eventType      e.g. "payment.captured", when Relaya could tell (may be null)
 * @param attempt        1 for the first try, then 2, 3… on retries
 * @param replayId       set when the request is part of an incident replay (may be null)
 * @param body           the provider's original body, byte for byte
 */
public record Delivery(
        String idempotencyKey,
        String deliveryId,
        String eventId,
        String eventType,
        int attempt,
        String replayId,
        Instant signedAt,
        byte[] body) {

    public String bodyAsString() {
        return new String(body, StandardCharsets.UTF_8);
    }

    /** The body as a JSON tree. */
    public JsonNode json() {
        return Json.tree(body);
    }

    /** The body decoded into {@code type}. */
    public <T> T json(Class<T> type) {
        return Json.read(body, type);
    }
}
