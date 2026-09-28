package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record OutboundEventType(String name, String description, Instant createdAt) {
}
