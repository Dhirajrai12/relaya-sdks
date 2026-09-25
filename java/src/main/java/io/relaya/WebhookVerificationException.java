package io.relaya;

/** A request claiming to come from Relaya failed signature verification. */
public final class WebhookVerificationException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    public static final String MISSING_SIGNATURE = "missing_signature";
    public static final String MALFORMED_SIGNATURE = "malformed_signature";
    public static final String TIMESTAMP_OUT_OF_RANGE = "timestamp_out_of_range";
    public static final String SIGNATURE_MISMATCH = "signature_mismatch";

    private final String reason;

    public WebhookVerificationException(String reason, String message) {
        super(message);
        this.reason = reason;
    }

    /** One of the constants above. */
    public String reason() {
        return reason;
    }
}
