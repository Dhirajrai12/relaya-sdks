package io.relaya.model;

import com.fasterxml.jackson.annotation.JsonProperty;
import java.time.Instant;
import java.util.List;

/** API model; field names match the API in snake_case. */
public record AlertChannel(
        String id,
        /** slack, email or webhook */ String type,
        String name,
        String target,
        List<String> events,
        boolean enabled,
        Instant createdAt,
        @JsonProperty("sent_7d") int sent7d,
        @JsonProperty("failed_7d") int failed7d) {
}
