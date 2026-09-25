<?php

declare(strict_types=1);

namespace Relaya\Resources;

final class Projects extends Resource
{
    public function list(): array
    {
        return $this->client->org('GET', '/projects')['data'];
    }

    public function get(string $id): array
    {
        return $this->client->org('GET', "/projects/{$id}");
    }

    public function create(string $name): array
    {
        return $this->client->org('POST', '/projects', ['name' => $name]);
    }
}
