package io.relaya.model;

/** API model; field names match the API in snake_case. */
public record CreatedDestination(
        Destination destination,
        /** shown once: store it where your endpoint can read it */ String signingSecret) {
}
