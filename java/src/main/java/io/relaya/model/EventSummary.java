package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record EventSummary(
        String id,
        String projectId,
        String webhookId,
        String dedupKey,
        String type,
        /** received or rejected */ String status,
        /** valid, invalid, missing or not_configured */ String signature,
        String contentType,
        int payloadSize,
        Instant receivedAt,
        /** none, pending, delivered or failed */ String delivery,
        /** none, pending, learning, ok, compatible, suspicious or breaking */ String contractStatus) {
}
