<?php

declare(strict_types=1);

namespace Relaya\Resources;

final class Destinations extends Resource
{
    public function list(string $webhookId): array
    {
        return $this->client->org('GET', "/webhooks/{$webhookId}/destinations")['data'];
    }

    /**
     * Returns ['destination' => [...], 'signing_secret' => '...']. The secret is shown once.
     *
     * @param array{name: string, url: string, max_attempts?: int, timeout_ms?: int, enabled?: bool} $input
     */
    public function create(string $webhookId, array $input): array
    {
        return $this->client->org('POST', "/webhooks/{$webhookId}/destinations", $input);
    }

    public function update(string $id, array $input): array
    {
        return $this->client->org('PATCH', "/destinations/{$id}", $input);
    }

    public function delete(string $id): void
    {
        $this->client->org('DELETE', "/destinations/{$id}");
    }

    public function rotateSecret(string $id): string
    {
        return $this->client->org('POST', "/destinations/{$id}/rotate-secret")['signing_secret'];
    }

    /** Sends a signed test request now: ['ok', 'status_code', 'duration_ms', 'response_body', 'error']. */
    public function test(string $id): array
    {
        return $this->client->org('POST', "/destinations/{$id}/test");
    }
}
