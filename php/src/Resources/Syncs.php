<?php

declare(strict_types=1);

namespace Relaya\Resources;

/** Syncs: new and changed records in connected apps become Relaya events. */
final class Syncs extends Resource
{
    /** What can be synced, per provider, and the settings each needs. */
    public function models(): array
    {
        return $this->client->request('GET', '/v1/connect/sync-models')['data'];
    }

    public function list(): array
    {
        return $this->client->org('GET', '/syncs')['data'];
    }

    /**
     * Starts syncing, e.g. create($connId, 'zoho.crm_records', ['module' => 'Leads']).
     * Events land on a new webhook unless you pass webhook_id; add a destination there to receive them.
     *
     * @param array<string, string> $config
     * @param array{interval_minutes?: int, webhook_id?: string, emit_existing?: bool} $fields
     */
    public function create(string $connectionId, string $model, array $config = [], array $fields = []): array
    {
        return $this->client->org('POST', '/syncs', ['connection_id' => $connectionId, 'model' => $model, 'config' => (object) $config] + $fields);
    }

    /**
     * A new config starts the sync over (fresh first run).
     *
     * @param array{enabled?: bool, interval_minutes?: int, config?: array<string, string>} $fields
     */
    public function update(string $id, array $fields): array
    {
        return $this->client->org('PATCH', "/syncs/{$id}", $fields);
    }

    public function delete(string $id): void
    {
        $this->client->org('DELETE', "/syncs/{$id}");
    }

    /** Runs it within seconds instead of waiting for the schedule. */
    public function run(string $id): array
    {
        return $this->client->org('POST', "/syncs/{$id}/run");
    }

    public function runs(string $id): array
    {
        return $this->client->org('GET', "/syncs/{$id}/runs")['data'];
    }
}
