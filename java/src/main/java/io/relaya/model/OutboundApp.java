package io.relaya.model;

import com.fasterxml.jackson.annotation.JsonProperty;
import java.time.Instant;

/** One of your customers, who receives webhooks from you. API model; field names match the API in snake_case. */
public record OutboundApp(
        String id,
        /** your ID for this customer */ String uid,
        String name,
        String webhookId,
        int endpoints,
        @JsonProperty("messages_24h") int messages24h,
        @JsonProperty("failed_24h") int failed24h,
        Instant createdAt) {
}
