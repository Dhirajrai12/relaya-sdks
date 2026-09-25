<?php

declare(strict_types=1);

namespace Relaya\Resources;

final class Incidents extends Resource
{
    /** @param string|null $status "open" (default) or "resolved" */
    public function list(?string $status = null): array
    {
        return $this->client->org('GET', '/incidents', null, ['status' => $status])['data'];
    }

    public function resolve(string $id, string $resolution): void
    {
        $this->client->org('POST', "/incidents/{$id}/resolve", ['resolution' => $resolution]);
    }

    /** Dry run: which events and deliveries a replay would resend. Changes nothing. */
    public function previewReplay(string $id): array
    {
        return $this->client->org('GET', "/incidents/{$id}/replay");
    }

    /** Resends the incident's deliveries. The incident resolves itself if all of them succeed. */
    public function replay(string $id): array
    {
        return $this->client->org('POST', "/incidents/{$id}/replay", ['confirm' => true]);
    }
}
