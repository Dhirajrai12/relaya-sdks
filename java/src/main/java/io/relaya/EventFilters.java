package io.relaya;

import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;

/** Filters for {@code events().list} and {@code events().iterate}. Unset filters are ignored. */
public final class EventFilters {
    final Map<String, String> params = new LinkedHashMap<>();

    private EventFilters set(String key, Object value) {
        if (value == null || value.toString().isEmpty()) {
            params.remove(key);
        } else {
            params.put(key, value.toString());
        }
        return this;
    }

    public EventFilters projectId(String v) { return set("project_id", v); }

    public EventFilters webhookId(String v) { return set("webhook_id", v); }

    public EventFilters type(String v) { return set("type", v); }

    /** received or rejected */
    public EventFilters status(String v) { return set("status", v); }

    /** valid, invalid, missing or not_configured */
    public EventFilters signature(String v) { return set("signature", v); }

    /** ok, compatible, suspicious, breaking, … */
    public EventFilters contractStatus(String v) { return set("contract_status", v); }

    public EventFilters dedupKey(String v) { return set("dedup_key", v); }

    /** Only events received at or after this time. */
    public EventFilters since(Instant v) { return set("since", v); }

    /** Only events received before this time. */
    public EventFilters until(Instant v) { return set("until", v); }

    /** Page size, 1–200 (default 50). */
    public EventFilters limit(int v) { return set("limit", v); }

    public EventFilters cursor(String v) { return set("cursor", v); }

    EventFilters copy() {
        EventFilters f = new EventFilters();
        f.params.putAll(params);
        return f;
    }
}
