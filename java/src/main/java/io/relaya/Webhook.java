package io.relaya;

import java.nio.charset.StandardCharsets;
import java.security.InvalidKeyException;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Collection;
import java.util.HexFormat;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.function.Function;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

/**
 * Verify requests Relaya forwards to your endpoints.
 *
 * <pre>{@code
 * // Spring
 * @PostMapping("/webhooks/relaya")
 * ResponseEntity<Void> relaya(@RequestBody byte[] body, @RequestHeader HttpHeaders headers) {
 *     Delivery d = Webhook.verifyDelivery(body, headers, secret); // throws WebhookVerificationException
 *     ...
 * }
 * }</pre>
 */
public final class Webhook {
    public static final String HEADER_SIGNATURE = "Relaya-Signature";
    public static final String HEADER_IDEMPOTENCY_KEY = "Idempotency-Key";
    public static final String HEADER_EVENT_ID = "Relaya-Event-Id";
    public static final String HEADER_DELIVERY_ID = "Relaya-Delivery-Id";
    public static final String HEADER_ATTEMPT = "Relaya-Attempt";
    public static final String HEADER_REPLAY = "Relaya-Replay";
    public static final String HEADER_EVENT_TYPE = "Relaya-Event-Type";

    /** Reject signatures older (or newer) than this by default. */
    public static final Duration DEFAULT_TOLERANCE = Duration.ofMinutes(5);

    private Webhook() {
    }

    /** Options for signature checks. */
    public static final class Options {
        private final List<String> secrets = new ArrayList<>();
        private Duration tolerance = DEFAULT_TOLERANCE;
        private Instant now;

        /** One secret, or call again while rotating: any match passes. */
        public Options secret(String secret) {
            if (secret != null && !secret.isEmpty()) {
                secrets.add(secret);
            }
            return this;
        }

        /** Maximum signature age; {@link Duration#ZERO} disables the check. */
        public Options tolerance(Duration tolerance) {
            this.tolerance = tolerance;
            return this;
        }

        /** Override the clock (tests). */
        public Options now(Instant now) {
            this.now = now;
            return this;
        }
    }

    public static Options secret(String secret) {
        return new Options().secret(secret);
    }

    /**
     * Checks a Relaya-Signature header ({@code t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<body>")>})
     * and returns when the request was signed.
     *
     * @throws WebhookVerificationException if the header is missing, malformed, too old, or does not match
     */
    public static Instant verifySignature(byte[] body, String header, Options opts) {
        if (opts.secrets.isEmpty()) {
            throw new IllegalArgumentException("relaya: a signing secret is required");
        }
        if (header == null || header.isEmpty()) {
            throw new WebhookVerificationException(WebhookVerificationException.MISSING_SIGNATURE, "The Relaya-Signature header is missing");
        }
        Long timestamp = null;
        List<String> signatures = new ArrayList<>();
        for (String part : header.split(",")) {
            String[] kv = part.trim().split("=", 2);
            if (kv.length != 2) {
                continue;
            }
            if (kv[0].equals("t")) {
                try {
                    timestamp = Long.parseLong(kv[1]);
                } catch (NumberFormatException e) {
                    timestamp = null;
                }
            } else if (kv[0].equals("v1") && !kv[1].isEmpty()) {
                signatures.add(kv[1].toLowerCase(Locale.ROOT));
            }
        }
        if (timestamp == null || signatures.isEmpty()) {
            throw new WebhookVerificationException(WebhookVerificationException.MALFORMED_SIGNATURE, "The Relaya-Signature header is malformed");
        }

        Instant signedAt = Instant.ofEpochSecond(timestamp);
        Instant now = opts.now != null ? opts.now : Instant.now();
        if (!opts.tolerance.isZero() && Duration.between(signedAt, now).abs().compareTo(opts.tolerance) > 0) {
            throw new WebhookVerificationException(WebhookVerificationException.TIMESTAMP_OUT_OF_RANGE,
                    "The signature is older than " + opts.tolerance.toSeconds() + " seconds (or from the future)");
        }

        byte[] prefix = (timestamp + ".").getBytes(StandardCharsets.UTF_8);
        for (String secret : opts.secrets) {
            byte[] expected = HexFormat.of().formatHex(hmac(secret, prefix, body)).getBytes(StandardCharsets.US_ASCII);
            for (String sig : signatures) {
                if (MessageDigest.isEqual(expected, sig.getBytes(StandardCharsets.US_ASCII))) {
                    return signedAt;
                }
            }
        }
        throw new WebhookVerificationException(WebhookVerificationException.SIGNATURE_MISMATCH,
                "The signature does not match: check the signing secret and that you pass the raw body");
    }

    /** Like {@link #verifySignature} but returns true/false. */
    public static boolean isValidSignature(byte[] body, String header, Options opts) {
        try {
            verifySignature(body, header, opts);
            return true;
        } catch (WebhookVerificationException e) {
            return false;
        }
    }

    /**
     * Verifies a request forwarded by Relaya. {@code header} looks a header up by name, e.g.
     * {@code request::getHeader} (Servlet) or {@code headers::getFirst} (Spring).
     */
    public static Delivery verifyDelivery(byte[] body, Function<String, String> header, Options opts) {
        Instant signedAt = verifySignature(body, header.apply(HEADER_SIGNATURE), opts);
        String deliveryId = orEmpty(header.apply(HEADER_DELIVERY_ID));
        String idem = header.apply(HEADER_IDEMPOTENCY_KEY);
        int attempt = 1;
        try {
            String a = header.apply(HEADER_ATTEMPT);
            attempt = a == null ? 1 : Math.max(1, Integer.parseInt(a));
        } catch (NumberFormatException ignored) {
            // keep 1
        }
        return new Delivery(idem == null || idem.isEmpty() ? deliveryId : idem, deliveryId, orEmpty(header.apply(HEADER_EVENT_ID)),
                header.apply(HEADER_EVENT_TYPE), attempt, header.apply(HEADER_REPLAY), signedAt, body);
    }

    /** Same, with headers as a map (String or list values, any case), e.g. Spring's {@code HttpHeaders}. */
    public static Delivery verifyDelivery(byte[] body, Map<String, ?> headers, Options opts) {
        return verifyDelivery(body, name -> lookup(headers, name), opts);
    }

    public static Delivery verifyDelivery(byte[] body, Map<String, ?> headers, String secret) {
        return verifyDelivery(body, headers, secret(secret));
    }

    /** Verifies an alert sent to a webhook alert channel and decodes it. */
    public static Alert verifyAlert(byte[] body, Map<String, ?> headers, Options opts) {
        verifySignature(body, lookup(headers, HEADER_SIGNATURE), opts);
        return Json.read(body, Alert.class);
    }

    private static String lookup(Map<String, ?> headers, String name) {
        for (Map.Entry<String, ?> e : headers.entrySet()) {
            if (e.getKey() != null && e.getKey().equalsIgnoreCase(name)) {
                Object v = e.getValue();
                if (v instanceof Collection<?> c) {
                    v = c.isEmpty() ? null : c.iterator().next();
                }
                return v == null ? null : v.toString();
            }
        }
        return null;
    }

    private static String orEmpty(String s) {
        return s == null ? "" : s;
    }

    private static byte[] hmac(String secret, byte[] prefix, byte[] body) {
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
            mac.update(prefix);
            return mac.doFinal(body);
        } catch (NoSuchAlgorithmException | InvalidKeyException e) {
            throw new IllegalStateException(e);
        }
    }
}
