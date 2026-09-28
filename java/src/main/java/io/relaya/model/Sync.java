package io.relaya.model;

import java.time.Instant;
import java.util.Map;

/** New and changed records in a connected app, turned into events. API model; field names match the API in snake_case. */
public record Sync(
        String id,
        String connectionId,
        String endUserId,
        String integrationName,
        String provider,
        /** its events are stored here; add destinations to receive them */ String webhookId,
        String webhookName,
        String model,
        String modelName,
        Map<String, String> config,
        int intervalMinutes,
        boolean enabled,
        boolean emitExisting,
        boolean baselineDone,
        boolean running,
        Instant nextRunAt,
        Instant lastRunAt,
        /** never, ok or error */ String lastStatus,
        String lastError,
        int consecutiveFailures,
        int records,
        int events,
        Instant createdAt) {
}
