package io.relaya;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.sun.net.httpserver.HttpServer;
import io.relaya.model.Contract;
import io.relaya.model.CreatedDestination;
import io.relaya.model.DeliveryRecord;
import io.relaya.model.EventSummary;
import io.relaya.model.InboundWebhook;
import io.relaya.model.Incident;
import io.relaya.model.Replay;
import java.net.InetSocketAddress;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Supplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;

/**
 * End-to-end against a running Relaya (API + ingest + worker). Runs only when RELAYA_IT_API_URL is set:
 * {@code RELAYA_IT_API_URL=http://127.0.0.1:18080 mvn test}. The server must allow http://127.0.0.1
 * destinations (APP_ENV=dev) and use CONTRACT_MIN_SAMPLES=3.
 */
@EnabledIfEnvironmentVariable(named = "RELAYA_IT_API_URL", matches = ".+")
class IntegrationTest {
    private static final HttpClient HTTP = HttpClient.newHttpClient();

    static <T> T waitFor(String what, Supplier<T> fn) throws InterruptedException {
        long until = System.currentTimeMillis() + 15_000;
        while (true) {
            T v = fn.get();
            if (v != null && !Boolean.FALSE.equals(v) && !(v instanceof List<?> l && l.isEmpty())) {
                return v;
            }
            if (System.currentTimeMillis() > until) {
                throw new AssertionError("timed out waiting for " + what);
            }
            Thread.sleep(150);
        }
    }

    static JsonNode post(String url, String json, String token, String eventId) throws Exception {
        HttpRequest.Builder b = HttpRequest.newBuilder(URI.create(url)).header("Content-Type", "application/json").POST(HttpRequest.BodyPublishers.ofString(json));
        if (token != null) {
            b.header("Authorization", "Bearer " + token);
        }
        if (eventId != null) {
            b.header("X-Event-Id", eventId);
        }
        HttpResponse<byte[]> res = HTTP.send(b.build(), HttpResponse.BodyHandlers.ofByteArray());
        assertTrue(res.statusCode() < 300, url + ": " + res.statusCode());
        return res.body().length == 0 ? null : Json.tree(res.body());
    }

    @Test
    void sdkAgainstLiveRelaya() throws Exception {
        String api = System.getenv("RELAYA_IT_API_URL");
        JsonNode session = post(api + "/v1/auth/signup", "{\"email\":\"java-sdk-" + System.nanoTime() + "@example.com\",\"password\":\"sdk-test-password-1\",\"org_name\":\"Java SDK\"}", null, null);
        String token = session.get("token").asText();
        String orgId = Relaya.builder().apiKey(token).baseUrl(api).build().request("GET", "/v1/me", null, Map.of(), JsonNode.class).get("orgs").get(0).get("id").asText();
        String key = post(api + "/v1/orgs/" + orgId + "/api-keys", "{\"name\":\"sdk\",\"role\":\"admin\"}", token, null).get("key").asText();

        Relaya relaya = Relaya.builder().apiKey(key).baseUrl(api).build();
        assertEquals(orgId, relaya.orgId());

        // A customer endpoint verifying with the SDK.
        AtomicReference<String> secret = new AtomicReference<>("");
        AtomicInteger failNext = new AtomicInteger();
        List<Delivery> received = new CopyOnWriteArrayList<>();
        HttpServer endpoint = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        endpoint.createContext("/hooks", ex -> {
            byte[] body = ex.getRequestBody().readAllBytes();
            int status;
            try {
                received.add(Webhook.verifyDelivery(body, ex.getRequestHeaders(), secret.get()));
                status = failNext.getAndDecrement() > 0 ? 500 : 200;
            } catch (WebhookVerificationException e) {
                status = 400;
            }
            ex.sendResponseHeaders(status, -1);
            ex.close();
        });
        endpoint.start();
        Delivery[] last = new Delivery[1];
        Supplier<Delivery> latest = () -> received.get(received.size() - 1);

        try {
            var project = relaya.projects().create("SDK");
            InboundWebhook wh = relaya.webhooks().create(project.id(), "Payments", "generic");
            CreatedDestination dest = relaya.destinations().create(wh.id(), "My app", "http://127.0.0.1:" + endpoint.getAddress().getPort() + "/hooks");
            secret.set(dest.signingSecret());

            for (int i = 1; i <= 3; i++) {
                post(wh.ingestUrl(), "{\"type\":\"payment.captured\",\"amount\":" + (100 * i) + "}", null, "e" + i);
            }
            waitFor("3 deliveries", () -> received.size() >= 3);
            Delivery first = received.get(0);
            assertEquals(first.deliveryId(), first.idempotencyKey());
            assertEquals(1, first.attempt());
            assertEquals("payment.captured", first.eventType());
            assertTrue(first.json().get("amount").isInt());

            List<EventSummary> all = new ArrayList<>();
            relaya.events().iterate(new EventFilters().webhookId(wh.id()).limit(2)).forEach(all::add);
            assertEquals(3, all.size());
            assertEquals("payment.captured", relaya.events().get(first.eventId()).payloadJson().get("type").asText());

            List<DeliveryRecord> ok = waitFor("succeeded deliveries", () -> {
                var l = relaya.deliveries().list(wh.id(), "succeeded");
                return l.size() == 3 ? l : null;
            });
            assertEquals("succeeded", relaya.deliveries().get(ok.get(0).id()).attempts().get(0).outcome());

            // Failing endpoint, then a manual retry.
            failNext.set(1);
            post(wh.ingestUrl(), "{\"type\":\"payment.captured\",\"amount\":400}", null, "e4");
            DeliveryRecord retrying = waitFor("a retrying delivery", () -> relaya.deliveries().list(wh.id(), "retrying")).get(0);
            relaya.deliveries().retry(retrying.id());
            waitFor("the retry to succeed", () -> "succeeded".equals(relaya.deliveries().get(retrying.id()).delivery().status()));
            last[0] = latest.get();
            assertEquals(2, last[0].attempt());
            assertEquals(retrying.id(), last[0].idempotencyKey());

            // Contract -> incident -> replay.
            Contract contract = waitFor("a proposed contract",
                    () -> relaya.contracts().list(wh.id()).stream().filter(c -> !c.status().equals("learning")).findFirst().orElse(null));
            assertTrue(relaya.contracts().createVersion(contract.id(), List.of("amount"), "observed") >= 1);
            post(wh.ingestUrl(), "{\"type\":\"payment.captured\",\"amount\":\"500\"}", null, "e5");
            Incident incident = waitFor("an open incident", () -> relaya.incidents().list("open")).get(0);
            assertTrue(incident.title().contains("amount changed type"), incident.title());
            assertEquals(1, relaya.incidents().previewReplay(incident.id()).events());
            int before = received.size();
            Replay replay = relaya.incidents().replay(incident.id());
            assertEquals(1, replay.total());
            waitFor("the replayed delivery", () -> received.size() > before);
            assertEquals(replay.id(), latest.get().replayId());
            waitFor("the incident to resolve", () -> relaya.incidents().list("open").isEmpty());

            // Destination test and API errors.
            assertTrue(relaya.destinations().test(dest.destination().id()).ok());
            assertEquals(404, assertThrows(RelayaException.class, () -> relaya.webhooks().get("00000000-0000-0000-0000-000000000000")).status());
            assertEquals(409, assertThrows(RelayaException.class, () -> relaya.deliveries().retry(ok.get(0).id())).status());
        } finally {
            endpoint.stop(0);
        }
    }
}
