package io.relaya.model;

import com.fasterxml.jackson.annotation.JsonProperty;
import java.util.List;

/** A setting a sync model needs. */
public record SyncModelField(
        String key,
        String label,
        String help,
        String placeholder,
        boolean required,
        List<String> options,
        @JsonProperty("default") String defaultValue) {
}
