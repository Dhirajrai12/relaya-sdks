package io.relaya;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import io.relaya.model.Integration;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;

/** Connections, proxy and syncs against a running Relaya, short of a real provider account. */
@EnabledIfEnvironmentVariable(named = "RELAYA_IT_API_URL", matches = ".+")
class ConnectIntegrationTest {
    @Test
    void connectAgainstLiveRelaya() throws Exception {
        String api = System.getenv("RELAYA_IT_API_URL");
        JsonNode session = IntegrationTest.post(api + "/v1/auth/signup",
                "{\"email\":\"java-conn-" + System.nanoTime() + "@example.com\",\"password\":\"sdk-test-password-1\",\"org_name\":\"Java connect\"}", null, null);
        String token = session.get("token").asText();
        String orgId = Relaya.builder().apiKey(token).baseUrl(api).build().request("GET", "/v1/me", null, Map.of(), JsonNode.class)
                .get("orgs").get(0).get("id").asText();
        String key = IntegrationTest.post(api + "/v1/orgs/" + orgId + "/api-keys", "{\"name\":\"sdk\",\"role\":\"admin\"}", token, null).get("key").asText();
        Relaya relaya = Relaya.builder().apiKey(key).baseUrl(api).build();

        Integration in = relaya.integrations().create("shiprocket", null, null);
        assertEquals("shiprocket", in.key());
        assertEquals(1, relaya.integrations().list().size());
        assertFalse(relaya.connections().createLink("shiprocket", "user-1").url().isEmpty());
        assertTrue(relaya.connections().find("shiprocket", "user-1").isEmpty());
        assertFalse(relaya.syncs().models().isEmpty());
        assertTrue(relaya.syncs().list().isEmpty());
        assertTrue(relaya.proxyCalls().list(null).isEmpty());
        RelayaException e = assertThrows(RelayaException.class, () -> relaya.proxy("00000000-0000-0000-0000-000000000000").get("/v1/external/orders"));
        assertEquals(404, e.status());
        relaya.integrations().delete(in.id());
    }
}
