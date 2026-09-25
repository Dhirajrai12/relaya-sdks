package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record DeliveryRecord(
        String id,
        String eventId,
        String webhookId,
        String destinationId,
        String destinationName,
        String destinationUrl,
        /** pending, in_flight, retrying, succeeded or failed */ String status,
        int attempts,
        int maxAttempts,
        Instant nextAttemptAt,
        Integer lastStatusCode,
        String lastError,
        Instant lastAttemptAt,
        Instant completedAt,
        Instant createdAt) {
}
