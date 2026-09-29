package io.relaya;

import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.JsonNode;
import io.relaya.model.AlertChannel;
import io.relaya.model.AlertLogEntry;
import io.relaya.model.Contract;
import io.relaya.model.ContractDetail;
import io.relaya.model.CreatedDestination;
import io.relaya.model.ConnectLink;
import io.relaya.model.Connection;
import io.relaya.model.ConnectionToken;
import io.relaya.model.CreatedEndpoint;
import io.relaya.model.Integration;
import io.relaya.model.ProxyCall;
import io.relaya.model.RefreshResult;
import io.relaya.model.Sync;
import io.relaya.model.SyncModel;
import io.relaya.model.SyncRun;
import io.relaya.model.OutboundApp;
import io.relaya.model.OutboundAppDetail;
import io.relaya.model.OutboundEndpoint;
import io.relaya.model.OutboundEventType;
import io.relaya.model.PortalLink;
import io.relaya.model.SentMessage;
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
import java.util.Optional;
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
    public static final String VERSION = "0.4.0";

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
    private final Outbound outbound = new Outbound();
    private final Integrations integrations = new Integrations();
    private final Connections connections = new Connections();
    private final ProxyCalls proxyCalls = new ProxyCalls();
    private final Syncs syncs = new Syncs();

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

    /** Send webhooks to your own customers. */
    public Outbound outbound() { return outbound; }

    /** Providers you set up once (Zoho, HubSpot, Google, Shiprocket). */
    public Integrations integrations() { return integrations; }

    /** Your users' accounts at those providers. */
    public Connections connections() { return connections; }

    /** The log of calls made through connections. */
    public ProxyCalls proxyCalls() { return proxyCalls; }

    /** New and changed records in connected apps, turned into events. */
    public Syncs syncs() { return syncs; }

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

        /** Like {@link #create(String, String, String)}, forwarding only these event types (empty means all). */
        public CreatedDestination create(String webhookId, String name, String url, List<String> eventTypes) {
            return org("POST", "/webhooks/" + webhookId + "/destinations", body("name", name, "url", url, "event_types", eventTypes), Map.of(),
                    CreatedDestination.class);
        }

        /** Fields: name, url, enabled, max_attempts, timeout_ms, event_types (a list; empty means all). */
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

    // ---- outbound webhooks: send events to your own customers ----

    /**
     * Sends webhooks to your customers. Relaya signs them (Standard Webhooks), retries failures and logs every
     * attempt; each customer manages their endpoints in a portal.
     *
     * <pre>{@code
     * relaya.outbound().apps().create("customer-123", "Acme");
     * relaya.outbound().send("customer-123", "invoice.paid", Map.of("id", "in_1"), "in_1-paid");
     * }</pre>
     */
    public final class Outbound {
        private final OutboundApps apps = new OutboundApps();
        private final OutboundEndpoints endpoints = new OutboundEndpoints();
        private final OutboundEventTypes eventTypes = new OutboundEventTypes();

        public OutboundApps apps() { return apps; }

        public OutboundEndpoints endpoints() { return endpoints; }

        public OutboundEventTypes eventTypes() { return eventTypes; }

        /**
         * Sends an event to every endpoint of that customer that takes its type. The payload must serialize to a
         * JSON object. With an idempotency key (may be null), sending the same message again returns the first one
         * ({@code duplicate() == true}) instead of a copy.
         */
        public SentMessage send(String app, String eventType, Object payload, String idempotencyKey) {
            return org("POST", "/outbound/messages", body("app", app, "event_type", eventType, "payload", payload, "idempotency_key", idempotencyKey),
                    Map.of(), SentMessage.class);
        }

        public SentMessage send(String app, String eventType, Object payload) { return send(app, eventType, payload, null); }
    }

    private static String appPath(String app) { return "/outbound/apps/" + enc(app); }

    /** One app per customer; refer to it by your own uid (or its id) everywhere. */
    public final class OutboundApps {
        public List<OutboundApp> list() { return orgList("/outbound/apps", Map.of(), OutboundApp.class); }

        public OutboundAppDetail get(String app) { return org("GET", appPath(app), null, Map.of(), OutboundAppDetail.class); }

        /** name may be null (defaults to uid). */
        public OutboundApp create(String uid, String name) { return org("POST", "/outbound/apps", body("uid", uid, "name", name), Map.of(), OutboundApp.class); }

        /** Also deletes its endpoints and message history. */
        public void delete(String app) { org("DELETE", appPath(app), null, Map.of(), Void.class); }

        /** A 24-hour link where the customer manages their endpoints and sees deliveries. */
        public PortalLink portalLink(String app) { return org("POST", appPath(app) + "/portal-link", null, Map.of(), PortalLink.class); }
    }

    /** Customers manage these themselves in the portal; these let you do it for them. */
    public final class OutboundEndpoints {
        public List<OutboundEndpoint> list(String app) { return outbound.apps().get(app).endpoints(); }

        /** eventTypes may be null or empty (all types). The result carries the whsec_… signing secret. */
        public CreatedEndpoint create(String app, String url, List<String> eventTypes) {
            return org("POST", appPath(app) + "/endpoints", body("url", url, "event_types", eventTypes), Map.of(), CreatedEndpoint.class);
        }

        /** Fields: url, description, event_types (a list; empty means all), enabled. */
        public OutboundEndpoint update(String app, String id, Map<String, Object> fields) {
            return org("PATCH", appPath(app) + "/endpoints/" + id, fields, Map.of(), OutboundEndpoint.class);
        }

        public void delete(String app, String id) { org("DELETE", appPath(app) + "/endpoints/" + id, null, Map.of(), Void.class); }

        public String secret(String app, String id) {
            return org("GET", appPath(app) + "/endpoints/" + id + "/secret", null, Map.of(), JsonNode.class).path("signing_secret").asText();
        }

        /** Sends a signed test event now (eventType may be null) and reports what the endpoint answered. */
        public TestResult test(String app, String id, String eventType) {
            return org("POST", appPath(app) + "/endpoints/" + id + "/test", null, params("event_type", eventType), TestResult.class);
        }
    }

    /** The catalog customers pick from in the portal. Types you send are added automatically. */
    public final class OutboundEventTypes {
        public List<OutboundEventType> list() { return orgList("/outbound/event-types", Map.of(), OutboundEventType.class); }

        /** Adds it, or updates its description. */
        public OutboundEventType save(String name, String description) {
            return org("POST", "/outbound/event-types", body("name", name, "description", description), Map.of(), OutboundEventType.class);
        }

        public void delete(String name) { org("DELETE", "/outbound/event-types/" + enc(name), null, Map.of(), Void.class); }
    }

    // ---- connections: your users' accounts at other apps ----

    public final class Integrations {
        public List<Integration> list() { return orgList("/integrations", Map.of(), Integration.class); }

        /**
         * Sets a provider up. Zoho, HubSpot and Google need your OAuth app's client ID and secret; Shiprocket needs
         * neither (pass nulls).
         */
        public Integration create(String provider, String clientId, String clientSecret) {
            return org("POST", "/integrations", body("provider", provider, "client_id", clientId, "client_secret", clientSecret), Map.of(), Integration.class);
        }

        /** Fields: provider, key, name, client_id, client_secret, scopes. */
        public Integration create(Map<String, Object> fields) { return org("POST", "/integrations", fields, Map.of(), Integration.class); }

        /** Fields: name, client_id, client_secret, scopes. */
        public Integration update(String id, Map<String, Object> fields) { return org("PATCH", "/integrations/" + id, fields, Map.of(), Integration.class); }

        /** Deletes its connections too. */
        public void delete(String id) { org("DELETE", "/integrations/" + id, null, Map.of(), Void.class); }
    }

    public final class Connections {
        /**
         * A one-time link (30 minutes) where your user (endUserId: their ID in your system) connects their account.
         * Open it with connect.js or redirect them. returnUrl may be null.
         */
        public ConnectLink createLink(String integration, String endUserId, String returnUrl) {
            return org("POST", "/connect-sessions", body("integration", integration, "end_user_id", endUserId, "return_url", returnUrl), Map.of(),
                    ConnectLink.class);
        }

        public ConnectLink createLink(String integration, String endUserId) { return createLink(integration, endUserId, null); }

        /** Nulls are ignored; status: active or broken. */
        public List<Connection> list(String integration, String endUserId, String status) {
            return orgList("/connections", params("integration", integration, "end_user_id", endUserId, "status", status), Connection.class);
        }

        public Connection get(String id) { return org("GET", "/connections/" + id, null, Map.of(), Connection.class); }

        /** The connection for one of your users, if they have connected. */
        public Optional<Connection> find(String integration, String endUserId) {
            return list(integration, endUserId, null).stream().findFirst();
        }

        /** A working access token, renewed first when about to expire. Use it right away rather than storing it. */
        public ConnectionToken token(String id) { return org("GET", "/connections/" + id + "/token", null, Map.of(), ConnectionToken.class); }

        /** Renews the token now, e.g. to check the connection works. */
        public RefreshResult refresh(String id) { return org("POST", "/connections/" + id + "/refresh", null, Map.of(), RefreshResult.class); }

        public void delete(String id) { org("DELETE", "/connections/" + id, null, Map.of(), Void.class); }
    }

    /**
     * Calls the provider's API as the connected user; Relaya adds and renews the token and retries what is safe
     * to retry.
     *
     * <pre>{@code
     * ProxyResponse res = relaya.proxy(connectionId).get("/crm/v2/Leads", new ProxyOptions().query("per_page", 10));
     * if (res.ok()) System.out.println(res.json());
     * }</pre>
     *
     * The provider's own errors come back in the response. A {@link RelayaException} is thrown only when Relaya
     * couldn't make the call, e.g. code "connection_broken": send the user a new link.
     */
    public Proxy proxy(String connectionId) { return new Proxy(connectionId); }

    public final class Proxy {
        private final String connectionId;

        private Proxy(String connectionId) { this.connectionId = connectionId; }

        public ProxyResponse get(String path, ProxyOptions options) { return request("GET", path, null, options); }

        public ProxyResponse get(String path) { return get(path, null); }

        public ProxyResponse delete(String path, ProxyOptions options) { return request("DELETE", path, null, options); }

        /** body: JSON-encoded unless it is a String or byte[]. */
        public ProxyResponse post(String path, Object body, ProxyOptions options) { return request("POST", path, body, options); }

        public ProxyResponse put(String path, Object body, ProxyOptions options) { return request("PUT", path, body, options); }

        public ProxyResponse patch(String path, Object body, ProxyOptions options) { return request("PATCH", path, body, options); }

        /** One attempt: Relaya already retries what is safe to retry. */
        public ProxyResponse request(String method, String path, Object body, ProxyOptions options) {
            ProxyOptions o = options != null ? options : new ProxyOptions();
            StringBuilder url = new StringBuilder(baseUrl).append("/v1/orgs/").append(orgId()).append("/connections/").append(connectionId)
                    .append("/proxy/").append(path.replaceFirst("^/+", ""));
            if (!o.query.isEmpty()) {
                url.append('?').append(o.query.entrySet().stream().map(e -> enc(e.getKey()) + "=" + enc(e.getValue())).collect(Collectors.joining("&")));
            }
            HttpRequest.Builder rb = HttpRequest.newBuilder(URI.create(url.toString()))
                    .timeout(o.timeout)
                    .header("Authorization", "Bearer " + apiKey)
                    .header("Accept", "application/json")
                    .header("User-Agent", "relaya-java/" + VERSION);
            o.headers.forEach((k, v) -> rb.header("Relaya-Proxy-" + k, v));
            if (o.baseUrl != null) {
                rb.header("Relaya-Proxy-Base-Url", o.baseUrl);
            }
            if (body != null) {
                byte[] data = body instanceof byte[] b ? b : body instanceof String s ? s.getBytes(StandardCharsets.UTF_8) : Json.write(body);
                rb.header("Content-Type", o.contentType != null ? o.contentType : "application/json").method(method, HttpRequest.BodyPublishers.ofByteArray(data));
            } else {
                rb.method(method, HttpRequest.BodyPublishers.noBody());
            }
            HttpResponse<byte[]> res;
            try {
                res = http.send(rb.build(), HttpResponse.BodyHandlers.ofByteArray());
            } catch (IOException e) {
                boolean timedOut = e instanceof HttpTimeoutException;
                throw new RelayaException(0, timedOut ? "timeout" : "network_error", timedOut ? "Proxy call timed out" : "Could not reach Relaya: " + e, null);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new RelayaException(0, "interrupted", "Request interrupted", null);
            }
            int status = res.statusCode();
            Optional<String> attempts = res.headers().firstValue("Relaya-Proxy-Attempts");
            // Relaya's own errors are marked; a 401 without Relaya-Proxy-Attempts is Relaya refusing the API key.
            if (res.headers().firstValue("Relaya-Proxy-Error").orElse("").equals("true") || (status == 401 && attempts.isEmpty())) {
                String code = "proxy_error";
                String message = "HTTP " + status;
                try {
                    JsonNode err = Json.tree(res.body()).path("error");
                    code = err.path("code").asText(code);
                    message = err.path("message").asText(message);
                } catch (RuntimeException ignored) {
                    // not JSON
                }
                throw new RelayaException(status, code, message, res.headers().firstValue("X-Request-Id").orElse(null));
            }
            return new ProxyResponse(status, res.headers(), res.body(), attempts.map(Integer::parseInt).orElse(1));
        }
    }

    public final class ProxyCalls {
        /** The last 100 calls, for one connection when connectionId is not null. */
        public List<ProxyCall> list(String connectionId) { return orgList("/proxy-calls", params("connection", connectionId), ProxyCall.class); }
    }

    // ---- syncs: new and changed records in connected apps become events ----

    public final class Syncs {
        /** What can be synced, per provider, and the settings each needs. */
        public List<SyncModel> models() {
            JavaType listOf = Json.MAPPER.getTypeFactory().constructParametricType(DataList.class, SyncModel.class);
            DataList<SyncModel> l = request("GET", "/v1/connect/sync-models", null, Map.of(), listOf);
            return l.data();
        }

        public List<Sync> list() { return orgList("/syncs", Map.of(), Sync.class); }

        /**
         * Starts syncing, e.g. {@code create(connId, "zoho.crm_records", Map.of("module", "Leads"), Map.of("interval_minutes", 15))}.
         * Events land on a new webhook unless you pass webhook_id; add a destination there to receive them.
         * Extra fields (may be empty): interval_minutes, webhook_id, emit_existing.
         */
        public Sync create(String connectionId, String model, Map<String, String> config, Map<String, Object> fields) {
            Map<String, Object> b = body("connection_id", connectionId, "model", model, "config", config != null ? config : Map.of());
            if (fields != null) {
                b.putAll(fields);
            }
            return org("POST", "/syncs", b, Map.of(), Sync.class);
        }

        /** Fields: enabled, interval_minutes, config (a new config starts the sync over). */
        public Sync update(String id, Map<String, Object> fields) { return org("PATCH", "/syncs/" + id, fields, Map.of(), Sync.class); }

        public void delete(String id) { org("DELETE", "/syncs/" + id, null, Map.of(), Void.class); }

        /** Runs it within seconds instead of waiting for the schedule. */
        public Sync run(String id) { return org("POST", "/syncs/" + id + "/run", null, Map.of(), Sync.class); }

        /** The last 50 runs. */
        public List<SyncRun> runs(String id) { return orgList("/syncs/" + id + "/runs", Map.of(), SyncRun.class); }
    }
}
