package io.relaya.model;

import java.time.Instant;
import java.util.List;

/** A provider set up once: your OAuth app for Zoho, HubSpot or Google; nothing for Shiprocket. */
public record Integration(
        String id,
        /** the name your code uses, e.g. "zoho" */ String key,
        String provider,
        String providerName,
        /** oauth2 or login */ String auth,
        String name,
        String clientId,
        boolean hasClientSecret,
        List<String> scopes,
        int connections,
        int broken,
        Instant createdAt,
        Instant updatedAt) {
}
