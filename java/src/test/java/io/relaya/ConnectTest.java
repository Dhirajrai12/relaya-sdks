package io.relaya;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.Headers;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.function.Function;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

/** Connections, proxy and syncs against a fake API that records each request. */
class ConnectTest {
    record Call(String method, String path, String query, String body, Headers headers) {
    }

    record Reply(int status, String json, Map<String, String> headers) {
        static Reply ok(String json) {
            return new Reply(200, json, Map.of());
        }
    }

    private HttpServer server;
    private final List<Call> calls = new CopyOnWriteArrayList<>();

    private String fakeApi(Map<String, Function<Call, Reply>> routes) throws IOException {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/", ex -> {
            Call c = new Call(ex.getRequestMethod(), ex.getRequestURI().getPath(), ex.getRequestURI().getRawQuery(),
                    new String(ex.getRequestBody().readAllBytes(), StandardCharsets.UTF_8), ex.getRequestHeaders());
            calls.add(c);
            Function<Call, Reply> route = routes.get(c.method() + " " + c.path());
            Reply r = route != null ? route.apply(c) : new Reply(404, "{\"error\":{\"code\":\"not_found\",\"message\":\"no route\"}}", Map.of());
            r.headers().forEach((k, v) -> ex.getResponseHeaders().add(k, v));
            byte[] out = r.json().getBytes(StandardCharsets.UTF_8);
            ex.sendResponseHeaders(r.status(), out.length);
            ex.getResponseBody().write(out);
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
    void connectionsLinkFindToken() throws Exception {
        String url = fakeApi(Map.of(
                "POST /v1/orgs/o/connect-sessions", c -> new Reply(201, "{\"id\":\"s1\",\"url\":\"https://relaya.test/connect/cs_x\"}", Map.of()),
                "GET /v1/orgs/o/connections", c -> Reply.ok(c.query().contains("end_user_id=u1") ? "{\"data\":[{\"id\":\"c1\",\"end_user_id\":\"u1\"}]}" : "{\"data\":[]}"),
                "GET /v1/orgs/o/connections/c1/token", c -> Reply.ok("{\"access_token\":\"at\",\"api_base\":\"https://www.zohoapis.in\"}")));
        Relaya r = Relaya.builder().apiKey("rk").orgId("o").baseUrl(url).build();
        assertEquals("https://relaya.test/connect/cs_x", r.connections().createLink("zoho", "u1").url());
        assertEquals("{\"integration\":\"zoho\",\"end_user_id\":\"u1\"}", calls.get(0).body());
        assertEquals("c1", r.connections().find("zoho", "u1").orElseThrow().id());
        assertTrue(r.connections().find("zoho", "nobody").isEmpty());
        assertTrue(calls.get(1).query().contains("integration=zoho"));
        assertEquals("https://www.zohoapis.in", r.connections().token("c1").apiBase());
    }

    @Test
    void proxy() throws Exception {
        String base = "/v1/orgs/o/connections/c1/proxy/";
        String url = fakeApi(Map.of(
                "GET " + base + "crm/v2/Leads", c -> new Reply(200, "{\"data\":[{\"id\":\"1\"}]}", Map.of("Relaya-Proxy-Attempts", "2")),
                "POST " + base + "crm/v2/Leads", c -> new Reply(201, "{}", Map.of("Relaya-Proxy-Attempts", "1")),
                "GET " + base + "missing", c -> new Reply(404, "{\"code\":\"INVALID_URL_PATTERN\"}", Map.of("Relaya-Proxy-Attempts", "1")),
                "GET " + base + "anything", c -> new Reply(409, "{\"error\":{\"code\":\"connection_broken\",\"message\":\"connect again\"}}", Map.of("Relaya-Proxy-Error", "true"))));
        Relaya r = Relaya.builder().apiKey("rk").orgId("o").baseUrl(url).build();
        Relaya.Proxy zoho = r.proxy("c1");

        ProxyResponse res = zoho.get("/crm/v2/Leads", new ProxyOptions().query("per_page", 10).header("orgId", "42"));
        assertTrue(res.ok());
        assertEquals(2, res.attempts());
        assertEquals("1", res.json().get("data").get(0).get("id").asText());
        Call first = calls.get(0);
        assertEquals("per_page=10", first.query());
        assertEquals("42", first.headers().getFirst("Relaya-Proxy-OrgId"));
        assertEquals("Bearer rk", first.headers().getFirst("Authorization"));

        zoho.post("/crm/v2/Leads", Map.of("data", List.of(Map.of("Last_Name", "Rao"))), new ProxyOptions().baseUrl("https://www.zohoapis.in"));
        Call post = calls.get(1);
        assertEquals("{\"data\":[{\"Last_Name\":\"Rao\"}]}", post.body());
        assertEquals("application/json", post.headers().getFirst("Content-Type"));
        assertEquals("https://www.zohoapis.in", post.headers().getFirst("Relaya-Proxy-Base-Url"));

        ProxyResponse missing = zoho.get("/missing");
        assertFalse(missing.ok());
        assertEquals(404, missing.status());

        RelayaException e = assertThrows(RelayaException.class, () -> zoho.get("/anything"));
        assertEquals("connection_broken", e.code());
        assertEquals(409, e.status());
    }

    @Test
    void syncs() throws Exception {
        String url = fakeApi(Map.of(
                "GET /v1/connect/sync-models", c -> Reply.ok("{\"data\":[{\"key\":\"zoho.crm_records\",\"fields\":[{\"key\":\"module\",\"default\":\"Leads\"}]}]}"),
                "POST /v1/orgs/o/syncs", c -> new Reply(201, "{\"id\":\"sy1\",\"config\":{\"module\":\"Leads\"}}", Map.of()),
                "POST /v1/orgs/o/syncs/sy1/run", c -> new Reply(202, "{\"id\":\"sy1\",\"running\":true}", Map.of())));
        Relaya r = Relaya.builder().apiKey("rk").orgId("o").baseUrl(url).build();
        assertEquals("Leads", r.syncs().models().get(0).fields().get(0).defaultValue());
        assertEquals("sy1", r.syncs().create("c1", "zoho.crm_records", Map.of("module", "Leads"), Map.of("interval_minutes", 15)).id());
        assertEquals("{\"connection_id\":\"c1\",\"model\":\"zoho.crm_records\",\"config\":{\"module\":\"Leads\"},\"interval_minutes\":15}", calls.get(1).body());
        assertTrue(r.syncs().run("sy1").running());
    }
}
