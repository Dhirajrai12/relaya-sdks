package io.relaya.model;

import com.fasterxml.jackson.annotation.JsonProperty;
import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record Contract(
        String id,
        String webhookId,
        String webhookName,
        String eventType,
        /** learning, proposed or active */ String status,
        int samples,
        int minSamples,
        Integer activeVersion,
        String fingerprint,
        int fieldCount,
        int criticalCount,
        int newFields,
        @JsonProperty("suspicious_24h") int suspicious24h,
        @JsonProperty("breaking_24h") int breaking24h,
        int openIncidents,
        Instant firstSeenAt,
        Instant lastSeenAt) {
}
