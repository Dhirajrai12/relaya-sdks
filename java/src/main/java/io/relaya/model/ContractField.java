package io.relaya.model;

import com.fasterxml.jackson.annotation.JsonProperty;
import java.util.List;

/** API model; field names match the API in snake_case. */
public record ContractField(
        String path,
        List<String> types,
        boolean required,
        @JsonProperty("enum") List<String> enumValues,
        boolean critical,
        boolean inVersion,
        List<String> observedTypes,
        int observedSeen) {
}
