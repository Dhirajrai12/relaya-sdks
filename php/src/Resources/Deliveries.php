<?php

declare(strict_types=1);

namespace Relaya\Resources;

final class Deliveries extends Resource
{
    /** The latest 100 matching deliveries. Filters: event_id, destination_id, webhook_id, status. */
    public function list(array $filters = []): array
    {
        return $this->client->org('GET', '/deliveries', null, $filters)['data'];
    }

    /** ['delivery' => [...], 'attempts' => [...]] */
    public function get(string $id): array
    {
        return $this->client->org('GET', "/deliveries/{$id}");
    }

    /** Sends a failed or retrying delivery again now. */
    public function retry(string $id): array
    {
        return $this->client->org('POST', "/deliveries/{$id}/retry");
    }
}
