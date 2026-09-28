package io.relaya.model;

import java.time.Instant;
import java.util.Map;

/** One of your users' accounts at a provider. API model; field names match the API in snake_case. */
public record Connection(
        String id,
        String integrationId,
        String integrationKey,
        String integrationName,
        String provider,
        /** your ID for the user or account that connected */ String endUserId,
        /** active, or broken: the user must connect again */ String status,
        Instant expiresAt,
        Instant lastRefreshedAt,
        int refreshFailures,
        String lastError,
        Instant brokenAt,
        Map<String, Object> metadata,
        Instant createdAt,
        Instant updatedAt) {
}
