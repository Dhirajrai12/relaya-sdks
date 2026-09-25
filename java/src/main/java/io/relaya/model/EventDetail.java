package io.relaya.model;

import com.fasterxml.jackson.databind.JsonNode;
import java.time.Instant;
import java.util.List;
import java.util.Map;

/** API model; field names match the API in snake_case. */
public record EventDetail(
        String id,
        String projectId,
        String webhookId,
        String dedupKey,
        String type,
        String status,
        String signature,
        String contentType,
        int payloadSize,
        Instant receivedAt,
        String delivery,
        String contractStatus,
        List<DeliveryRecord> deliveries,
        List<Violation> violations,
        String contractId,
        Map<String, String> headers,
        String sourceIp,
        /** the payload with sensitive fields masked, when it is JSON */ JsonNode payloadJson,
        String payloadText,
        String payloadBase64) {
}
