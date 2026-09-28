package io.relaya.model;

import com.fasterxml.jackson.annotation.JsonProperty;
import java.time.Instant;
import java.util.List;

/** A URL where one of your customers receives their events. API model; field names match the API in snake_case. */
public record OutboundEndpoint(
        String id,
        String url,
        String description,
        /** only these are sent; empty means all */ List<String> eventTypes,
        boolean enabled,
        Instant createdAt,
        @JsonProperty("succeeded_24h") int succeeded24h,
        @JsonProperty("failed_24h") int failed24h,
        int retrying,
        Instant lastSuccessAt) {
}
