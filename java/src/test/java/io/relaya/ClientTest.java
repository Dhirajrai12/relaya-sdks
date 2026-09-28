package io.relaya;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import io.relaya.model.EventSummary;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.BiFunction;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

class ClientTest {
    record Call(String method, String path, String query, String body, String auth) {
    }

    record Reply(int status, String json, Map<String, String> headers) {
        static Reply ok(String json) {
            return new Reply(200, json, Map.of());
        }
    }

    private HttpServer server;
    private final List<Call> calls = new CopyOnWriteArrayList<>();

    private String fakeApi(Map<String, BiFunction<Call, Integer, Reply>> routes) throws IOException {
        Map<String, AtomicInteger> counts = new ConcurrentHashMap<>();
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/", (HttpExchange ex) -> {
            Call c = new Call(ex.getRequestMethod(), ex.getRequestURI().getPath(), ex.getRequestURI().getRawQuery(),
                    new String(ex.getRequestBody().readAllBytes(), StandardCharsets.UTF_8), ex.getRequestHeaders().getFirst("Authorization"));
            calls.add(c);
            String key = c.method() + " " + c.path();
            int n = counts.computeIfAbsent(key, k -> new AtomicInteger()).incrementAndGet();
            BiFunction<Call, Integer, Reply> route = routes.get(key);
            Reply r = route != null ? route.apply(c, n) : new Reply(404, "{\"error\":{\"code\":\"not_found\",\"message\":\"no route\"}}", Map.of());
            r.headers().forEach((k, v) -> ex.getResponseHeaders().add(k, v));
            byte[] out = r.json() == null ? new byte[0] : r.json().getBytes(StandardCharsets.UTF_8);
            ex.sendResponseHeaders(r.status(), out.length == 0 ? -1 : out.length);
            if (out.length > 0) {
                ex.getResponseBody().write(out);
            }
            ex.close();
        });
        server.start();
        return "http://127.0.0.1:" + server.getAddress().getPort();
    }

    @AfterEach
    void stop() {
        if (server != null) {
            server.stop(0);
        }
    }

    @Test
    void looksUpOrgOnceAndSendsKey() throws Exception {
        String url = fakeApi(Map.of(
                "GET /v1/me", (c, n) -> Reply.ok("{\"api_key\":{\"org_id\":\"org1\"}}"),
                "GET /v1/orgs/org1/projects", (c, n) -> Reply.ok("{\"data\":[{\"id\":\"p1\",\"created_at\":\"2026-09-26T00:00:00Z\"}]}")));
        Relaya r = Relaya.builder().apiKey("rk_test").baseUrl(url + "/").build();
        assertEquals("p1", r.projects().list().get(0).id());
        r.projects().list();
        assertEquals(1, calls.stream().filter(c -> c.path().equals("/v1/me")).count());
        assertTrue(calls.stream().allMatch(c -> "Bearer rk_test".equals(c.auth())));
    }

    @Test
    void filtersAndPaging() throws Exception {
        String url = fakeApi(Map.of("GET /v1/orgs/o/events", (c, n) -> c.query() != null && c.query().contains("cursor=c2")
                ? Reply.ok("{\"data\":[{\"id\":\"e3\"}],\"next_cursor\":null}")
                : Reply.ok("{\"data\":[{\"id\":\"e1\"},{\"id\":\"e2\"}],\"next_cursor\":\"c2\"}")));
        Relaya r = Relaya.builder().apiKey("rk").orgId("o").baseUrl(url).build();
        List<String> ids = new ArrayList<>();
        for (EventSummary e : r.events().iterate(new EventFilters().contractStatus("breaking").since(Instant.parse("2026-09-01T00:00:00Z")).limit(2))) {
            ids.add(e.id());
        }
        assertEquals(List.of("e1", "e2", "e3"), ids);
        String q = calls.get(0).query();
        assertTrue(q.contains("contract_status=breaking"), q);
        assertTrue(q.contains("since=2026-09-01T00%3A00%3A00Z"), q);
        assertFalse(q.contains("cursor"), q);
        assertEquals(3, r.events().stream(new EventFilters()).count());
    }

    @Test
    void errorsAndRetries() throws Exception {
        String url = fakeApi(Map.of(
                "GET /v1/orgs/o/incidents", (c, n) -> n < 3 ? new Reply(503, null, Map.of("Retry-After", "0")) : Reply.ok("{\"data\":[]}"),
                "POST /v1/orgs/o/incidents/i1/replay", (c, n) -> new Reply(503, null, Map.of("Retry-After", "0"))));
        Relaya r = Relaya.builder().apiKey("rk").orgId("o").baseUrl(url).build();
        assertEquals(List.of(), r.incidents().list("open"));
        RelayaException e = assertThrows(RelayaException.class, () -> r.incidents().replay("i1"));
        assertEquals(503, e.status());
        List<Call> posts = calls.stream().filter(c -> c.method().equals("POST")).toList();
        assertEquals(1, posts.size());
        assertEquals("{\"confirm\":true}", posts.get(0).body());
        RelayaException nf = assertThrows(RelayaException.class, () -> r.webhooks().get("nope"));
        assertEquals(404, nf.status());
        assertEquals("not_found", nf.code());
    }

    @Test
    void networkError() {
        Relaya r = Relaya.builder().apiKey("rk").orgId("o").baseUrl("http://127.0.0.1:1").maxRetries(0).build();
        RelayaException e = assertThrows(RelayaException.class, () -> r.projects().list());
        assertEquals(0, e.status());
        assertEquals("network_error", e.code());
    }

    @Test
    void outbound() throws Exception {
        String url = fakeApi(Map.of(
                "POST /v1/orgs/o/outbound/apps", (c, n) -> new Reply(201, "{\"id\":\"a1\",\"uid\":\"cust:42\",\"messages_24h\":3}", Map.of()),
                "GET /v1/orgs/o/outbound/apps/cust:42", (c, n) -> Reply.ok("{\"app\":{\"uid\":\"cust:42\"},\"endpoints\":[{\"id\":\"ep1\",\"event_types\":[\"invoice.paid\"]}]}"),
                "POST /v1/orgs/o/outbound/apps/cust:42/endpoints", (c, n) -> new Reply(201, "{\"endpoint\":{\"id\":\"ep1\"},\"signing_secret\":\"whsec_x\"}", Map.of()),
                "POST /v1/orgs/o/outbound/apps/cust:42/endpoints/ep1/test", (c, n) -> Reply.ok("{\"ok\":true}"),
                "POST /v1/orgs/o/outbound/messages", (c, n) -> new Reply(202, "{\"id\":\"m1\",\"endpoints\":1,\"duplicate\":false}", Map.of()),
                "POST /v1/orgs/o/outbound/apps/cust:42/portal-link", (c, n) -> new Reply(201, "{\"url\":\"https://relaya.test/portal#ps_1\",\"expires_at\":\"2026-09-29T00:00:00Z\"}", Map.of())));
        Relaya r = Relaya.builder().apiKey("rk").orgId("o").baseUrl(url).build();
        assertEquals(3, r.outbound().apps().create("cust:42", "Acme").messages24h());
        assertEquals("{\"uid\":\"cust:42\",\"name\":\"Acme\"}", calls.get(0).body());
        assertEquals("whsec_x", r.outbound().endpoints().create("cust:42", "https://acme.test/hooks", List.of("invoice.paid")).signingSecret());
        assertEquals(List.of("invoice.paid"), r.outbound().endpoints().list("cust:42").get(0).eventTypes());
        assertTrue(r.outbound().endpoints().test("cust:42", "ep1", "invoice.paid").ok());
        assertEquals("event_type=invoice.paid", calls.get(3).query());
        assertEquals("m1", r.outbound().send("cust:42", "invoice.paid", Map.of("id", "in_1"), "in_1").id());
        assertEquals("{\"app\":\"cust:42\",\"event_type\":\"invoice.paid\",\"payload\":{\"id\":\"in_1\"},\"idempotency_key\":\"in_1\"}", calls.get(4).body());
        assertTrue(r.outbound().apps().portalLink("cust:42").url().contains("#ps_"));
    }
}
