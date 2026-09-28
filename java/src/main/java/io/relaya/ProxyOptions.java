package io.relaya;

import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Map;

/** Options for a proxy call; all optional. */
public final class ProxyOptions {
    final Map<String, String> query = new LinkedHashMap<>();
    final Map<String, String> headers = new LinkedHashMap<>();
    String baseUrl;
    String contentType;
    Duration timeout = Duration.ofSeconds(120);

    public ProxyOptions query(String name, Object value) {
        if (value != null) {
            query.put(name, String.valueOf(value));
        }
        return this;
    }

    /** Sent to the provider (as Relaya-Proxy-&lt;name&gt;); your Relaya API key never is. */
    public ProxyOptions header(String name, String value) {
        headers.put(name, value);
        return this;
    }

    /** Another API host of the same provider, e.g. https://sheets.googleapis.com. */
    public ProxyOptions baseUrl(String v) { baseUrl = v; return this; }

    /** Default application/json. */
    public ProxyOptions contentType(String v) { contentType = v; return this; }

    /** Default 120 s. */
    public ProxyOptions timeout(Duration v) { timeout = v; return this; }
}
