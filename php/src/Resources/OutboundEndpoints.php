<?php

declare(strict_types=1);

namespace Relaya\Resources;

/** Customers manage these themselves in the portal; these let you do it for them. */
final class OutboundEndpoints extends Resource
{
    public function list(string $app): array
    {
        return $this->client->org('GET', '/outbound/apps/' . rawurlencode($app))['endpoints'];
    }

    /**
     * Returns ['endpoint' => [...], 'signing_secret' => 'whsec_...'].
     *
     * @param array{description?: string, event_types?: list<string>} $fields event_types: only these are sent (empty = all)
     */
    public function create(string $app, string $url, array $fields = []): array
    {
        return $this->client->org('POST', '/outbound/apps/' . rawurlencode($app) . '/endpoints', ['url' => $url] + $fields);
    }

    /** @param array{url?: string, description?: string, event_types?: list<string>, enabled?: bool} $fields */
    public function update(string $app, string $id, array $fields): array
    {
        return $this->client->org('PATCH', '/outbound/apps/' . rawurlencode($app) . "/endpoints/{$id}", $fields);
    }

    public function delete(string $app, string $id): void
    {
        $this->client->org('DELETE', '/outbound/apps/' . rawurlencode($app) . "/endpoints/{$id}");
    }

    public function secret(string $app, string $id): string
    {
        return $this->client->org('GET', '/outbound/apps/' . rawurlencode($app) . "/endpoints/{$id}/secret")['signing_secret'];
    }

    /** Sends a signed test event now: ['ok', 'status_code', 'duration_ms', 'response_body', 'error']. */
    public function test(string $app, string $id, ?string $eventType = null): array
    {
        return $this->client->org('POST', '/outbound/apps/' . rawurlencode($app) . "/endpoints/{$id}/test", null, ['event_type' => $eventType]);
    }
}
