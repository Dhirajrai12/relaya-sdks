package io.relaya;

import com.fasterxml.jackson.databind.JsonNode;
import java.net.http.HttpHeaders;
import java.nio.charset.StandardCharsets;

/** The provider's answer to a proxy call, errors included: check {@link #ok()} or {@link #status()}. */
public final class ProxyResponse {
    private final int status;
    private final HttpHeaders headers;
    private final byte[] body;
    private final int attempts;

    ProxyResponse(int status, HttpHeaders headers, byte[] body, int attempts) {
        this.status = status;
        this.headers = headers;
        this.body = body;
        this.attempts = attempts;
    }

    /** The provider's status code. */
    public int status() { return status; }

    public boolean ok() { return status >= 200 && status < 300; }

    public HttpHeaders headers() { return headers; }

    public byte[] body() { return body.clone(); }

    public String text() { return new String(body, StandardCharsets.UTF_8); }

    /** The body parsed as JSON. */
    public JsonNode json() { return Json.tree(body); }

    /** The body mapped onto your own type (snake_case fields become camelCase). */
    public <T> T json(Class<T> type) { return Json.read(body, type); }

    /** How many times Relaya called the provider (retries, token renewal). */
    public int attempts() { return attempts; }
}
