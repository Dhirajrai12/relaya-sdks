<?php

declare(strict_types=1);

namespace Relaya\Resources;

final class Events extends Resource
{
    /**
     * One page, newest first: ['data' => [...], 'next_cursor' => ?string].
     *
     * Filters: project_id, webhook_id, type, status, signature, contract_status, dedup_key,
     * since, until (DateTimeInterface or RFC 3339), limit (1-200), cursor.
     */
    public function list(array $filters = []): array
    {
        return $this->client->org('GET', '/events', null, $filters);
    }

    /**
     * Every matching event, newest first, fetching pages as you go.
     *
     * @return \Generator<int, array>
     */
    public function iterate(array $filters = []): \Generator
    {
        unset($filters['cursor']);
        $cursor = null;
        do {
            $page = $this->list($filters + ['cursor' => $cursor]);
            yield from $page['data'];
            $cursor = $page['next_cursor'] ?? null;
        } while ($cursor);
    }

    /** Full event: payload (sensitive fields masked), headers, deliveries and contract findings. */
    public function get(string $id): array
    {
        return $this->client->org('GET', "/events/{$id}");
    }
}
