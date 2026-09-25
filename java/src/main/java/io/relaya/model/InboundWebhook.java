package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record InboundWebhook(
        String id,
        String orgId,
        String projectId,
        String name,
        String provider,
        String ingestUrl,
        boolean hasSigningSecret,
        String signatureHeader,
        /** active or paused */ String status,
        Instant createdAt,
        Instant updatedAt) {
}
