package io.relaya;

import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.JsonNode;
import io.relaya.model.AlertChannel;
import io.relaya.model.AlertLogEntry;
import io.relaya.model.Contract;
import io.relaya.model.ContractDetail;
import io.relaya.model.CreatedDestination;
import io.relaya.model.DeliveryRecord;
import io.relaya.model.DeliveryWithAttempts;
import io.relaya.model.Destination;
import io.relaya.model.EventDetail;
import io.relaya.model.EventPage;
import io.relaya.model.EventSummary;
import io.relaya.model.Incident;
import io.relaya.model.Project;
import io.relaya.model.Replay;
import io.relaya.model.ReplayPlan;
import io.relaya.model.TestResult;
import io.relaya.model.InboundWebhook;
import java.io.IOException;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.NoSuchElementException;
import java.util.Spliterator;
import java.util.Spliterators;
import java.util.concurrent.ThreadLocalRandom;
import java.util.stream.Collectors;
import java.util.stream.Stream;
import java.util.stream.StreamSupport;

/**
 * Relaya API client. Thread-safe; create one and reuse it.
 *
 * <pre>{@code
 * Relaya relaya = Relaya.builder().apiKey(System.getenv("RELAYA_API_KEY")).build();
 * relaya.events().stream(new EventFilters().contractStatus("breaking")).forEach(e -> System.out.println(e.type()));
 * }</pre>
 */
public final class Relaya {
    public static final String VERSION = "0.2.1";

    /** Where the API lives until the product has its own domain. */
    public static final String DEFAULT_BASE_URL = "https://server.aegonassett.com/api";

    private final String apiKey;
    private final String baseUrl;
    private final Duration timeout;
    private final int maxRetries;
    private final HttpClient http;
    private volatile String orgId;

    private final Projects projects = new Projects();
    private final Webhooks webhooks = new Webhooks();
    private final Events events = new Events();
    private final Destinations destinations = new Destinations();
    private final Deliveries deliveries = new Deliveries();
    private final Contracts contracts = new Contracts();
    private final Incidents incidents = new Incidents();
    private final Alerts alerts = new Alerts();

    private Relaya(Builder b) {
        String key = b.apiKey != null ? b.apiKey : System.getenv("RELAYA_API_KEY");
        if (key == null || key.isEmpty()) {
            throw new IllegalArgumentException("relaya: set apiKey or RELAYA_API_KEY");
        }
        String base = b.baseUrl != null ? b.baseUrl : System.getenv("RELAYA_BASE_URL");
        this.apiKey = key;
        this.baseUrl = (base == null || base.isEmpty() ? DEFAULT_BASE_URL : base).replaceAll("/+$", "");
        this.orgId = b.orgId;
        this.timeout = b.timeout;
        this.maxRetries = b.maxRetries;
        this.http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(10)).followRedirects(HttpClient.Redirect.NEVER).build();
    }

    public static Builder builder() {
        return new Builder();
    }

    public static final class Builder {
        private String apiKey;
        private String baseUrl;
        private String orgId;
        private Duration timeout = Duration.ofSeconds(30);
        private int maxRetries = 2;

        /** An API key (rk_…) from Settings → API keys. Defaults to RELAYA_API_KEY. */
        public Builder apiKey(String v) { apiKey = v; return this; }

        /** Defaults to RELAYA_BASE_URL, then the hosted API. */
        public Builder baseUrl(String v) { baseUrl = v; return this; }

        /** Only needed with a session token; an API key belongs to one org. */
        public Builder orgId(String v) { orgId = v; return this; }

        public Builder timeout(Duration v) { timeout = v; return this; }

        /** Retries for GET requests on network errors, 429 and 5xx (default 2). */
        public Builder maxRetries(int v) { maxRetries = v; return this; }

        public Relaya build() { return new Relaya(this); }
    }

    public Projects projects() { return projects; }

    public Webhooks webhooks() { return webhooks; }

    public Events events() { return events; }

    public Destinations destinations() { return destinations; }

    public Deliveries deliveries() { return deliveries; }

    public Contracts contracts() { return contracts; }

    public Incidents incidents() { return incidents; }

    public Alerts alerts() { return alerts; }

    /** The org this client acts on, looked up from the API key on first use. */
    public String orgId() {
        String id = orgId;
        if (id == null) {
            synchronized (this) {
                if (orgId == null) {
                    JsonNode me = request("GET", "/v1/me", null, Map.of(), JsonNode.class);
                    JsonNode org = me.path("api_key").path("org_id");
                    if (org.isMissingNode()) {
                        throw new IllegalStateException("relaya: set orgId when using a session token instead of an API key");
                    }
                    orgId = org.asText();
                }
                id = orgId;
            }
        }
        return id;
    }

    /** Low-level request; paths start with /v1. Returns null for 204 responses. */
    public <T> T request(String method, String path, Object body, Map<String, String> query, Class<T> type) {
        byte[] raw = send(method, path, body, query);
        return raw == null ? null : Json.read(raw, type);
    }

    private <T> T request(String method, String path, Object body, Map<String, String> query, JavaType type) {
        byte[] raw = send(method, path, body, query);
        return raw == null ? null : Json.read(raw, type);
    }

    private byte[] send(String method, String path, Object body, Map<String, String> query) {
        StringBuilder url = new StringBuilder(baseUrl).append(path);
        if (query != null && !query.isEmpty()) {
            url.append('?').append(query.entrySet().stream()
                    .map(e -> enc(e.getKey()) + "=" + enc(e.getValue()))
                    .collect(Collectors.joining("&")));
        }
        HttpRequest.Builder rb = HttpRequest.newBuilder(URI.create(url.toString()))
                .timeout(timeout)
                .header("Authorization", "Bearer " + apiKey)
                .header("Accept", "application/json")
                .header("User-Agent", "relaya-java/" + VERSION);
        if (body != null) {
            rb.header("Content-Type", "application/json").method(method, HttpRequest.BodyPublishers.ofByteArray(Json.write(body)));
        } else {
            rb.method(method, HttpRequest.BodyPublishers.noBody());
        }
        HttpRequest req = rb.build();
        int retries = method.equals("GET") ? maxRetries : 0;

        for (int attempt = 0; ; attempt++) {
            HttpResponse<byte[]> res;
            try {
                res = http.send(req, HttpResponse.BodyHandlers.ofByteArray());
            } catch (IOException e) {
                if (attempt < retries) {
                    sleep(backoff(attempt, null));
                    continue;
                }
                boolean timedOut = e instanceof HttpTimeoutException;
                throw new RelayaException(0, timedOut ? "timeout" : "network_error",
                        timedOut ? "Request timed out after " + timeout.toSeconds() + "s" : "Could not reach Relaya: " + e, null);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new RelayaException(0, "interrupted", "Request interrupted", null);
            }
            int status = res.statusCode();
            if ((status == 429 || status >= 500) && attempt < retries) {
                sleep(backoff(attempt, res.headers().firstValue("Retry-After").orElse(null)));
                continue;
            }
            byte[] data = res.body();
            if (status >= 300) {
                String code = "http_error";
                String message = "HTTP " + status;
                try {
                    JsonNode err = Json.tree(data).path("error");
                    code = err.path("code").asText(code);
                    message = err.path("message").asText(message);
                } catch (RuntimeException ignored) {
                    // not JSON
                }
                throw new RelayaException(status, code, message, res.headers().firstValue("X-Request-Id").orElse(null));
            }
            return status == 204 || data.length == 0 ? null : data;
        }
    }

    private static String enc(String s) {
        return URLEncoder.encode(s, StandardCharsets.UTF_8);
    }

    private static void sleep(Duration d) {
        try {
            Thread.sleep(d.toMillis());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }

    private static Duration backoff(int attempt, String retryAfter) {
        if (retryAfter != null) {
            try {
                return Duration.ofMillis((long) (Math.min(Math.max(Double.parseDouble(retryAfter), 0), 30) * 1000));
            } catch (NumberFormatException ignored) {
                // fall through
            }
        }
        double s = Math.min(0.5 * Math.pow(2, attempt), 8) * (0.8 + ThreadLocalRandom.current().nextDouble() * 0.4);
        return Duration.ofMillis((long) (s * 1000));
    }

    // ---- helpers for resources ----

    private <T> T org(String method, String path, Object body, Map<String, String> query, Class<T> type) {
        return request(method, "/v1/orgs/" + orgId() + path, body, query, type);
    }

    private <T> List<T> orgList(String path, Map<String, String> query, Class<T> item) {
        JavaType listOf = Json.MAPPER.getTypeFactory().constructParametricType(DataList.class, item);
        DataList<T> l = request("GET", "/v1/orgs/" + orgId() + path, null, query, listOf);
        return l.data();
    }

    record DataList<T>(List<T> data) {
    }

    private static Map<String, String> params(String... kv) {
        Map<String, String> m = new LinkedHashMap<>();
        for (int i = 0; i + 1 < kv.length; i += 2) {
            if (kv[i + 1] != null && !kv[i + 1].isEmpty()) {
                m.put(kv[i], kv[i + 1]);
            }
        }
        return m;
    }

    private static Map<String, Object> body(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i + 1 < kv.length; i += 2) {
            if (kv[i + 1] != null) {
                m.put((String) kv[i], kv[i + 1]);
            }
        }
        return m;
    }

    // ---- resources ----

    public final class Projects {
        public List<Project> list() { return orgList("/projects", Map.of(), Project.class); }

        public Project get(String id) { return org("GET", "/projects/" + id, null, Map.of(), Project.class); }

        public Project create(String name) { return org("POST", "/projects", body("name", name), Map.of(), Project.class); }
    }

    public final class Webhooks {
        public List<InboundWebhook> list() { return list(null); }

        public List<InboundWebhook> list(String projectId) { return orgList("/webhooks", params("project_id", projectId), InboundWebhook.class); }

        public InboundWebhook get(String id) { return org("GET", "/webhooks/" + id, null, Map.of(), InboundWebhook.class); }

        /** Creates an inbound webhook; give its ingestUrl to the provider. provider: generic, razorpay, stripe, shopify, github. */
        public InboundWebhook create(String projectId, String name, String provider) {
            return create(projectId, name, provider, null);
        }

        public InboundWebhook create(String projectId, String name, String provider, String signingSecret) {
            return org("POST", "/webhooks", body("project_id", projectId, "name", name, "provider", provider, "signing_secret", signingSecret), Map.of(), InboundWebhook.class);
        }

        /** Fields: name, status (active/paused), signing_secret, signature_header. */
        public InboundWebhook update(String id, Map<String, Object> fields) { return org("PATCH", "/webhooks/" + id, fields, Map.of(), InboundWebhook.class); }

        /** Issues a new ingest URL; the old one stops working. */
        public InboundWebhook rotateUrl(String id) { return org("POST", "/webhooks/" + id + "/rotate-url", null, Map.of(), InboundWebhook.class); }

        public void delete(String id) { org("DELETE", "/webhooks/" + id, null, Map.of(), Void.class); }
    }

    public final class Events {
        /** One page, newest first. Pass nextCursor back as cursor for the next page. */
        public EventPage list(EventFilters filters) { return org("GET", "/events", null, filters.params, EventPage.class); }

        /** Every matching event, newest first, fetching pages as you go. */
        public Iterable<EventSummary> iterate(EventFilters filters) {
            return () -> new Iterator<>() {
                private final EventFilters f = filters.copy().cursor(null);
                private Iterator<EventSummary> page = List.<EventSummary>of().iterator();
                private boolean more = true;

                @Override
                public boolean hasNext() {
                    while (!page.hasNext() && more) {
                        EventPage p = list(f);
                        page = p.data().iterator();
                        more = p.nextCursor() != null && !p.nextCursor().isEmpty();
                        f.cursor(p.nextCursor());
                    }
                    return page.hasNext();
                }

                @Override
                public EventSummary next() {
                    if (!hasNext()) {
                        throw new NoSuchElementException();
                    }
                    return page.next();
                }
            };
        }

        /** Same as iterate, as a Stream. */
        public Stream<EventSummary> stream(EventFilters filters) {
            return StreamSupport.stream(Spliterators.spliteratorUnknownSize(iterate(filters).iterator(), Spliterator.ORDERED), false);
        }

        /** Full event: payload (sensitive fields masked), headers, deliveries and contract findings. */
        public EventDetail get(String id) { return org("GET", "/events/" + id, null, Map.of(), EventDetail.class); }
    }

    public final class Destinations {
        public List<Destination> list(String webhookId) { return orgList("/webhooks/" + webhookId + "/destinations", Map.of(), Destination.class); }

        /** The signing secret is returned once; store it where your endpoint can read it. */
        public CreatedDestination create(String webhookId, String name, String url) {
            return org("POST", "/webhooks/" + webhookId + "/destinations", body("name", name, "url", url), Map.of(), CreatedDestination.class);
        }

        /** Fields: name, url, enabled, max_attempts, timeout_ms. */
        public Destination update(String id, Map<String, Object> fields) { return org("PATCH", "/destinations/" + id, fields, Map.of(), Destination.class); }

        public void delete(String id) { org("DELETE", "/destinations/" + id, null, Map.of(), Void.class); }

        public String rotateSecret(String id) {
            return org("POST", "/destinations/" + id + "/rotate-secret", null, Map.of(), JsonNode.class).path("signing_secret").asText();
        }

        /** Sends a signed test request now and reports what the endpoint answered. */
        public TestResult test(String id) { return org("POST", "/destinations/" + id + "/test", null, Map.of(), TestResult.class); }
    }

    public final class Deliveries {
        /** The latest 100 matching deliveries; status: pending, in_flight, retrying, succeeded, failed. Nulls are ignored. */
        public List<DeliveryRecord> list(String webhookId, String status) {
            return orgList("/deliveries", params("webhook_id", webhookId, "status", status), DeliveryRecord.class);
        }

        public List<DeliveryRecord> listForEvent(String eventId) { return orgList("/deliveries", params("event_id", eventId), DeliveryRecord.class); }

        public DeliveryWithAttempts get(String id) { return org("GET", "/deliveries/" + id, null, Map.of(), DeliveryWithAttempts.class); }

        /** Sends a failed or retrying delivery again now. */
        public DeliveryRecord retry(String id) { return org("POST", "/deliveries/" + id + "/retry", null, Map.of(), DeliveryRecord.class); }
    }

    public final class Contracts {
        public List<Contract> list(String webhookId) { return orgList("/contracts", params("webhook_id", webhookId), Contract.class); }

        public ContractDetail get(String id) { return org("GET", "/contracts/" + id, null, Map.of(), ContractDetail.class); }

        /**
         * Activates a new version and returns its number. source: "observed" (what was learned) or "active"
         * (the current version with new critical fields).
         */
        public int createVersion(String id, List<String> criticalFields, String source) {
            return org("POST", "/contracts/" + id + "/versions", body("critical_fields", criticalFields, "source", source), Map.of(), JsonNode.class)
                    .path("version").asInt();
        }

        /** Throws away what was learned and starts learning again. */
        public void relearn(String id) { org("POST", "/contracts/" + id + "/relearn", null, Map.of(), Void.class); }
    }

    public final class Incidents {
        /** status: "open" or "resolved"; null for open. */
        public List<Incident> list(String status) { return orgList("/incidents", params("status", status), Incident.class); }

        public void resolve(String id, String resolution) {
            org("POST", "/incidents/" + id + "/resolve", body("resolution", resolution), Map.of(), Void.class);
        }

        /** Dry run: which events and deliveries a replay would resend. Changes nothing. */
        public ReplayPlan previewReplay(String id) { return org("GET", "/incidents/" + id + "/replay", null, Map.of(), ReplayPlan.class); }

        /** Resends the incident's deliveries. The incident resolves itself if all of them succeed. */
        public Replay replay(String id) { return org("POST", "/incidents/" + id + "/replay", body("confirm", true), Map.of(), Replay.class); }
    }

    public final class Alerts {
        public List<AlertChannel> channels() { return orgList("/alert-channels", Map.of(), AlertChannel.class); }

        public List<AlertLogEntry> log() { return orgList("/alerts", Map.of(), AlertLogEntry.class); }
    }
}
