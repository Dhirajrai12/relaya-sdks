<?php

declare(strict_types=1);

namespace Relaya\Resources;

/** One app per customer; refer to it by your own uid (or its id) everywhere. */
final class OutboundApps extends Resource
{
    public function list(): array
    {
        return $this->client->org('GET', '/outbound/apps')['data'];
    }

    /** Returns ['app' => [...], 'endpoints' => [...]]. */
    public function get(string $app): array
    {
        return $this->client->org('GET', '/outbound/apps/' . rawurlencode($app));
    }

    public function create(string $uid, ?string $name = null): array
    {
        return $this->client->org('POST', '/outbound/apps', array_filter(['uid' => $uid, 'name' => $name], fn ($v) => $v !== null));
    }

    /** Also deletes its endpoints and message history. */
    public function delete(string $app): void
    {
        $this->client->org('DELETE', '/outbound/apps/' . rawurlencode($app));
    }

    /** A 24-hour link where the customer manages their endpoints and sees deliveries: ['url', 'expires_at']. */
    public function portalLink(string $app): array
    {
        return $this->client->org('POST', '/outbound/apps/' . rawurlencode($app) . '/portal-link');
    }
}
