package io.relaya.model;

import java.util.List;

/** Something that can be synced, e.g. "zoho.crm_records". */
public record SyncModel(String key, String provider, String name, String description, List<SyncModelField> fields, boolean incremental, boolean verified) {
}
