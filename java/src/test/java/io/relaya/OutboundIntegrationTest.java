package io.relaya;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.sun.net.httpserver.HttpServer;
import io.relaya.model.CreatedEndpoint;
import io.relaya.model.SentMessage;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Base64;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;

/** Outbound webhooks end-to-end against a running Relaya; runs only when RELAYA_IT_API_URL is set. */
@EnabledIfEnvironmentVariable(named = "RELAYA_IT_API_URL", matches = ".+")
class OutboundIntegrationTest {
    record Received(String id, String timestamp, String signature, byte[] body) {
    }

    @Test
    void outboundAgainstLiveRelaya() throws Exception {
        String api = System.getenv("RELAYA_IT_API_URL");
        JsonNode session = IntegrationTest.post(api + "/v1/auth/signup", "{\"email\":\"java-out-" + System.nanoTime() + "@example.com\",\"password\":\"sdk-test-password-1\",\"org_name\":\"Java outbound\"}", null, null);
        String token = session.get("token").asText();
        String orgId = Relaya.builder().apiKey(token).baseUrl(api).build().request("GET", "/v1/me", null, Map.of(), JsonNode.class).get("orgs").get(0).get("id").asText();
        String key = IntegrationTest.post(api + "/v1/orgs/" + orgId + "/api-keys", "{\"name\":\"sdk\",\"role\":\"admin\"}", token, null).get("key").asText();
        Relaya relaya = Relaya.builder().apiKey(key).baseUrl(api).build();

        // The customer's server, checking the Standard Webhooks signature by hand.
        List<Received> got = new CopyOnWriteArrayList<>();
        HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/", ex -> {
            got.add(new Received(ex.getRequestHeaders().getFirst("webhook-id"), ex.getRequestHeaders().getFirst("webhook-timestamp"),
                    ex.getRequestHeaders().getFirst("webhook-signature"), ex.getRequestBody().readAllBytes()));
            ex.sendResponseHeaders(200, -1);
            ex.close();
        });
        server.start();
        try {
            relaya.outbound().apps().create("customer-1", "Customer One");
            CreatedEndpoint ep = relaya.outbound().endpoints().create("customer-1", "http://127.0.0.1:" + server.getAddress().getPort() + "/hooks", List.of("invoice.paid"));
            assertTrue(ep.signingSecret().startsWith("whsec_"));
            SentMessage m = relaya.outbound().send("customer-1", "invoice.paid", Map.of("invoice", "in_1"), "in_1");
            assertEquals(1, m.endpoints());
            SentMessage again = relaya.outbound().send("customer-1", "invoice.paid", Map.of("invoice", "in_1"), "in_1");
            assertTrue(again.duplicate());
            assertEquals(m.id(), again.id());

            Received r = IntegrationTest.waitFor("the message", () -> got.isEmpty() ? null : got.get(0));
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(Base64.getDecoder().decode(ep.signingSecret().substring(6)), "HmacSHA256"));
            mac.update((r.id() + "." + r.timestamp() + ".").getBytes(StandardCharsets.UTF_8));
            String want = "v1," + Base64.getEncoder().encodeToString(mac.doFinal(r.body()));
            assertEquals(m.id(), r.id());
            assertTrue(Arrays.asList(r.signature().split(" ")).contains(want), r.signature());
            assertEquals("in_1", Json.tree(r.body()).get("data").get("invoice").asText());

            assertTrue(relaya.outbound().endpoints().test("customer-1", ep.endpoint().id(), null).ok());
            assertTrue(relaya.outbound().apps().portalLink("customer-1").url().contains("/portal#ps_"));
            assertTrue(relaya.outbound().eventTypes().list().stream().anyMatch(t -> t.name().equals("invoice.paid")));
            relaya.outbound().apps().delete("customer-1");
            RelayaException e = assertThrows(RelayaException.class, () -> relaya.outbound().apps().get("customer-1"));
            assertEquals(404, e.status());
        } finally {
            server.stop(0);
        }
    }
}
