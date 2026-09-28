package io.relaya.model;

import java.time.Instant;

/** A working access token. Use it right away rather than storing it. */
public record ConnectionToken(
        String accessToken,
        String tokenType,
        Instant expiresAt,
        /** where to call the provider with it */ String apiBase,
        String provider,
        String endUserId) {
}
