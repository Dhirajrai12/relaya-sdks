package io.relaya.model;

import java.time.Instant;

/** A one-time link (30 minutes): open it with connect.js ({@code Relaya.connect(url)}) or redirect the user. */
public record ConnectLink(String id, String url, Instant expiresAt) {
}
