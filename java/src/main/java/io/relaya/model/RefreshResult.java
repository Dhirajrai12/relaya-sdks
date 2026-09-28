package io.relaya.model;

/** A token renewal: whether it worked, and the connection after it. */
public record RefreshResult(Connection connection, boolean refreshed, String error) {
}
