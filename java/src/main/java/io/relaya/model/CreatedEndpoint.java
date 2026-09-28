package io.relaya.model;

/** API model; field names match the API in snake_case. */
public record CreatedEndpoint(
        OutboundEndpoint endpoint,
        /** whsec_…: the customer verifies requests with it */ String signingSecret) {
}
