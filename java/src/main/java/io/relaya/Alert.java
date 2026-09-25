package io.relaya;

import java.time.Instant;

/**
 * What a webhook alert channel receives.
 *
 * @param type incident_opened, incident_resolved, destination_failing, destination_recovered, signature_failures or test
 */
public record Alert(String type, String title, String body, String link, String orgId, long alertId, Instant sentAt) {
}
