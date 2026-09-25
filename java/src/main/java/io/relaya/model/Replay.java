package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record Replay(
        String id,
        /** running or completed */ String status,
        int total,
        int succeeded,
        int failed,
        String createdBy,
        Instant createdAt,
        Instant completedAt) {
}
