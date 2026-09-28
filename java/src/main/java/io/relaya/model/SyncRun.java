package io.relaya.model;

import java.time.Instant;

/** One run of a sync. */
public record SyncRun(long id, Instant startedAt, Instant finishedAt, /** running, ok or error */ String status, int fetched, int created, int updated, String error) {
}
