<?php

declare(strict_types=1);

namespace Relaya\Exception;

/** An error response from the Relaya API. */
final class RelayaException extends \RuntimeException
{
    public function __construct(
        /** HTTP status, or 0 when no response arrived. */
        public readonly int $status,
        /** e.g. "not_found", "bad_request", "network_error". */
        public readonly string $errorCode,
        string $message,
        public readonly ?string $requestId = null,
    ) {
        parent::__construct($message, $status);
    }
}
