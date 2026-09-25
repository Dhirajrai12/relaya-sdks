package io.relaya.model;

import java.util.List;

/** API model; field names match the API in snake_case. */
public record DeliveryWithAttempts(
        DeliveryRecord delivery,
        List<DeliveryAttempt> attempts) {
}
