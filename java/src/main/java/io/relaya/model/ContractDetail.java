package io.relaya.model;

import com.fasterxml.jackson.databind.JsonNode;
import java.util.List;

/** API model; field names match the API in snake_case. */
public record ContractDetail(
        Contract contract,
        List<ContractField> fields,
        int observedSamples,
        JsonNode newFields,
        List<ContractVersion> versions,
        List<Violation> violations) {
}
