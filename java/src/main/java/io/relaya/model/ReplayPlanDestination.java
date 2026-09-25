package io.relaya.model;

/** API model; field names match the API in snake_case. */
public record ReplayPlanDestination(
        String destinationId,
        String destinationName,
        String destinationUrl,
        boolean enabled,
        int deliveries,
        int alreadySucceeded,
        int inFlight) {
}
