<?php

declare(strict_types=1);

namespace Relaya\Exception;

/**
 * A request claiming to come from Relaya failed signature verification.
 * $reason is one of: missing_signature, malformed_signature, timestamp_out_of_range, signature_mismatch.
 */
final class WebhookVerificationException extends \RuntimeException
{
    public function __construct(public readonly string $reason, string $message)
    {
        parent::__construct($message);
    }
}
