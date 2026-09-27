<?php

declare(strict_types=1);

namespace Relaya;

/** The provider's answer to a proxied call. */
final class ProxyResponse
{
    public readonly bool $ok;

    /**
     * @param array<string, string> $headers lower-cased names
     * @param mixed $data parsed JSON, or the text when the answer isn't JSON
     * @param int $attempts how many times Relaya called the provider (retries, token renewal)
     */
    public function __construct(
        public readonly int $status,
        public readonly array $headers,
        public readonly mixed $data,
        public readonly int $attempts,
    ) {
        $this->ok = $status >= 200 && $status < 300;
    }
}
