<?php

declare(strict_types=1);

namespace Relaya\Resources;

/** Apps your users can connect (Zoho, HubSpot, Google, Shiprocket…), with your OAuth app for each. */
final class Integrations extends Resource
{
    public function list(): array
    {
        return $this->client->org('GET', '/integrations')['data'];
    }

    /**
     * Zoho, HubSpot and Google need your OAuth app's client_id and client_secret; Shiprocket needs neither.
     *
     * @param array{key?: string, name?: string, client_id?: string, client_secret?: string, scopes?: list<string>} $fields
     */
    public function create(string $provider, array $fields = []): array
    {
        return $this->client->org('POST', '/integrations', ['provider' => $provider] + $fields);
    }

    /** @param array{name?: string, client_id?: string, client_secret?: string, scopes?: list<string>} $fields */
    public function update(string $id, array $fields): array
    {
        return $this->client->org('PATCH', "/integrations/{$id}", $fields);
    }

    /** Deletes its connections too. */
    public function delete(string $id): void
    {
        $this->client->org('DELETE', "/integrations/{$id}");
    }
}
