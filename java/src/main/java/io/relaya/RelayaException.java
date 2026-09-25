package io.relaya;

/** An error response from the Relaya API. */
public final class RelayaException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    private final int status;
    private final String code;
    private final String requestId;

    public RelayaException(int status, String code, String message, String requestId) {
        super(status == 0 ? message : status + " " + code + ": " + message);
        this.status = status;
        this.code = code;
        this.requestId = requestId;
    }

    /** HTTP status, or 0 when no response arrived. */
    public int status() {
        return status;
    }

    /** e.g. "not_found", "bad_request", "network_error", "timeout". */
    public String code() {
        return code;
    }

    public String requestId() {
        return requestId;
    }
}
