package io.relaya.model;

import java.util.List;

/** An app with its endpoints. API model; field names match the API in snake_case. */
public record OutboundAppDetail(OutboundApp app, List<OutboundEndpoint> endpoints) {
}
