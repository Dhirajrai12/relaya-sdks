package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record DeliveryAttempt(
        int attempt,
        Instant startedAt,
        int durationMs,
        Integer statusCode,
        String error,
        String responseBody,
        /** succeeded, retry or failed */ String outcome) {
}
