package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record Violation(
        String eventId,
        /** suspicious or breaking */ String severity,
        String kind,
        String path,
        String expected,
        String actual,
        Instant createdAt) {
}
