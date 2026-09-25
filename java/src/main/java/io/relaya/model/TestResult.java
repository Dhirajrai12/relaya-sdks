package io.relaya.model;

/** API model; field names match the API in snake_case. */
public record TestResult(
        boolean ok,
        int statusCode,
        int durationMs,
        String responseBody,
        String error) {
}
