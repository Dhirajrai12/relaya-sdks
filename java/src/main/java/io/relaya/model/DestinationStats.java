package io.relaya.model;

import com.fasterxml.jackson.annotation.JsonProperty;
import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record DestinationStats(
        @JsonProperty("succeeded_24h") int succeeded24h,
        @JsonProperty("failed_24h") int failed24h,
        int retrying,
        int pending,
        Instant lastSuccessAt) {
}
