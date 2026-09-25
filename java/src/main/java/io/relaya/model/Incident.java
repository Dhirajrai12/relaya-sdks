package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record Incident(
        String id,
        String webhookId,
        String webhookName,
        String contractId,
        String eventType,
        String kind,
        String path,
        String severity,
        /** open or resolved */ String status,
        String title,
        String expected,
        String actual,
        int eventCount,
        Instant firstSeenAt,
        Instant lastSeenAt,
        String sampleEventId,
        Instant resolvedAt,
        String resolution,
        String resolvedBy,
        /** the latest replay, if any */ Replay replay) {
}
