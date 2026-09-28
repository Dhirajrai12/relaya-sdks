package io.relaya.model;

import java.time.Instant;

/** One logged call made through a connection (no query strings or bodies). */
public record ProxyCall(
        long id,
        String connectionId,
        String endUserId,
        String integrationName,
        String method,
        String host,
        String path,
        int status,
        int attempts,
        int durationMs,
        String error,
        Instant createdAt) {
}
