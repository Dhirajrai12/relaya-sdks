package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record ContractVersion(
        int version,
        String fingerprint,
        int criticalCount,
        String createdBy,
        Instant createdAt) {
}
