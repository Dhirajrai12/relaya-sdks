<?php

declare(strict_types=1);

namespace Relaya\Resources;

final class ProxyCalls extends Resource
{
    /** The last 100 calls made through connections (no query strings or bodies). */
    public function list(?string $connectionId = null): array
    {
        return $this->client->org('GET', '/proxy-calls', null, ['connection' => $connectionId])['data'];
    }
}
