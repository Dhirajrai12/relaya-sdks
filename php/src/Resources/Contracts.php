<?php

declare(strict_types=1);

namespace Relaya\Resources;

final class Contracts extends Resource
{
    public function list(?string $webhookId = null): array
    {
        return $this->client->org('GET', '/contracts', null, ['webhook_id' => $webhookId])['data'];
    }

    public function get(string $id): array
    {
        return $this->client->org('GET', "/contracts/{$id}");
    }

    /**
     * Activates a new version: from what was "observed", or the "active" one with new critical fields.
     * Returns the version number.
     *
     * @param string[]|null $criticalFields
     */
    public function createVersion(string $id, ?array $criticalFields = null, string $source = 'observed'): int
    {
        $body = ['source' => $source];
        if ($criticalFields !== null) {
            $body['critical_fields'] = $criticalFields;
        }
        return $this->client->org('POST', "/contracts/{$id}/versions", $body)['version'];
    }

    /** Throws away what was learned and starts learning again. */
    public function relearn(string $id): void
    {
        $this->client->org('POST', "/contracts/{$id}/relearn");
    }
}
