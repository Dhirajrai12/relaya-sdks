<?php

declare(strict_types=1);

namespace Relaya\Resources;

final class Webhooks extends Resource
{
    public function list(?string $projectId = null): array
    {
        return $this->client->org('GET', '/webhooks', null, ['project_id' => $projectId])['data'];
    }

    public function get(string $id): array
    {
        return $this->client->org('GET', "/webhooks/{$id}");
    }

    /**
     * Creates an inbound webhook; give its ingest_url to the provider.
     *
     * @param array{project_id: string, name: string, provider?: string, signing_secret?: string, signature_header?: string} $input
     */
    public function create(array $input): array
    {
        return $this->client->org('POST', '/webhooks', $input + ['provider' => 'generic']);
    }

    /** @param array{name?: string, status?: string, signing_secret?: string, signature_header?: string} $input */
    public function update(string $id, array $input): array
    {
        return $this->client->org('PATCH', "/webhooks/{$id}", $input);
    }

    /** Issues a new ingest URL; the old one stops working. */
    public function rotateUrl(string $id): array
    {
        return $this->client->org('POST', "/webhooks/{$id}/rotate-url");
    }

    public function delete(string $id): void
    {
        $this->client->org('DELETE', "/webhooks/{$id}");
    }
}
