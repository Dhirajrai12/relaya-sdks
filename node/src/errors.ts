/** An error response from the Relaya API. */
export class RelayaError extends Error {
  /** HTTP status, or 0 when the request never got a response. */
  readonly status: number
  /** Machine-readable code from the API, e.g. "not_found", "bad_request", or "network_error". */
  readonly code: string
  readonly requestId: string | null

  constructor(status: number, code: string, message: string, requestId: string | null = null) {
    super(message)
    this.name = 'RelayaError'
    this.status = status
    this.code = code
    this.requestId = requestId
  }
}

export type VerificationFailure = 'missing_signature' | 'malformed_signature' | 'timestamp_out_of_range' | 'signature_mismatch'

/** A request that claims to come from Relaya but fails signature verification. */
export class WebhookVerificationError extends Error {
  readonly reason: VerificationFailure

  constructor(reason: VerificationFailure, message: string) {
    super(message)
    this.name = 'WebhookVerificationError'
    this.reason = reason
  }
}
