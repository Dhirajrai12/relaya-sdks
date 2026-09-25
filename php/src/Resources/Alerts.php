<?php

declare(strict_types=1);

namespace Relaya\Resources;

final class Alerts extends Resource
{
    public function channels(): array
    {
        return $this->client->org('GET', '/alert-channels')['data'];
    }

    public function log(): array
    {
        return $this->client->org('GET', '/alerts')['data'];
    }
}
