package io.relaya.model;

import java.time.Instant;
import java.util.List;

/** API model; field names match the API in snake_case. */
public record Destination(
        String id,
        String webhookId,
        String name,
        String url,
        boolean enabled,
        int timeoutMs,
        int maxAttempts,
        Instant createdAt,
        Instant updatedAt,
        DestinationStats stats,
        /** only these event types are forwarded; empty means all */ List<String> eventTypes) {
}
