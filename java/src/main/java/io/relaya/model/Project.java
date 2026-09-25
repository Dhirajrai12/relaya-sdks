package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record Project(
        String id,
        String orgId,
        String name,
        String slug,
        Instant createdAt) {
}
