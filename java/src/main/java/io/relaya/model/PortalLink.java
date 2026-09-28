package io.relaya.model;

import java.time.Instant;

/** A 24-hour link where a customer manages their endpoints and sees deliveries. */
public record PortalLink(String url, Instant expiresAt) {
}
