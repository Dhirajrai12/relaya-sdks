package io.relaya.model;

/** API model; field names match the API in snake_case. */
public record SentMessage(
        /** also the webhook-id header the customer receives, the same on every retry */ String id,
        String app,
        String eventType,
        /** how many endpoints it was queued for */ int endpoints,
        /** the idempotency key was seen before; nothing new was sent */ boolean duplicate) {
}
