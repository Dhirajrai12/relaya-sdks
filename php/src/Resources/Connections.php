<?php

declare(strict_types=1);

namespace Relaya\Resources;

/** Your users' connected accounts. */
final class Connections extends Resource
{
    /**
     * A one-time link (30 minutes) where your user connects their account: ['id', 'url', 'expires_at'].
     * Open 'url' with connect.js (Relaya.connect(url)) or redirect the user to it.
     */
    public function createLink(string $integration, string $endUserId, ?string $returnUrl = null): array
    {
        $body = ['integration' => $integration, 'end_user_id' => $endUserId];
        if ($returnUrl !== null) {
            $body['return_url'] = $returnUrl;
        }
        return $this->client->org('POST', '/connect-sessions', $body);
    }

    /** @param array{integration?: string, end_user_id?: string, status?: 'active'|'broken'} $filters */
    public function list(array $filters = []): array
    {
        return $this->client->org('GET', '/connections', null, $filters)['data'];
    }

    public function get(string $id): array
    {
        return $this->client->org('GET', "/connections/{$id}");
    }

    /** The connection for one of your users, or null. */
    public function find(string $integration, string $endUserId): ?array
    {
        return $this->list(['integration' => $integration, 'end_user_id' => $endUserId])[0] ?? null;
    }

    /**
     * A working access token, renewed first when about to expire:
     * ['access_token', 'token_type', 'expires_at', 'api_base', ...]. Use it right away.
     */
    public function token(string $id): array
    {
        return $this->client->org('GET', "/connections/{$id}/token");
    }

    /** Renews the token now: ['connection', 'refreshed', 'error'?]. */
    public function refresh(string $id): array
    {
        return $this->client->org('POST', "/connections/{$id}/refresh");
    }

    public function delete(string $id): void
    {
        $this->client->org('DELETE', "/connections/{$id}");
    }
}
