package io.relaya.model;

import java.time.Instant;

/** API model; field names match the API in snake_case. */
public record AlertLogEntry(
        long id,
        String channelId,
        String channelName,
        String channelType,
        String kind,
        String title,
        /** pending, sent or failed */ String status,
        int attempts,
        String lastError,
        Instant createdAt,
        Instant sentAt) {
}
