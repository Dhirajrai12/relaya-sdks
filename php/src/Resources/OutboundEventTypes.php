<?php

declare(strict_types=1);

namespace Relaya\Resources;

/** The catalog customers pick from in the portal. Types you send are added automatically. */
final class OutboundEventTypes extends Resource
{
    public function list(): array
    {
        return $this->client->org('GET', '/outbound/event-types')['data'];
    }

    /** Adds it, or updates its description. */
    public function save(string $name, string $description = ''): array
    {
        return $this->client->org('POST', '/outbound/event-types', ['name' => $name, 'description' => $description]);
    }

    public function delete(string $name): void
    {
        $this->client->org('DELETE', '/outbound/event-types/' . rawurlencode($name));
    }
}
