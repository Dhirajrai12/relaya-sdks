<?php

declare(strict_types=1);

namespace Relaya;

/**
 * Calls a provider's API as a connected user; Relaya adds and renews the token.
 *
 *     $res = $relaya->proxy($connectionId)->get('/crm/v2/Leads', ['query' => ['per_page' => 10]]);
 *     if ($res->ok) { ... $res->data ... }
 *
 * The provider's answer comes back as is, errors included (check ok/status).
 * RelayaException is thrown only when Relaya couldn't make the call
 * (e.g. getCode() === 'connection_broken').
 *
 * Options: query (array), headers (array, sent to the provider), base_url
 * (another API host of the same provider), timeout (seconds, default 120).
 */
final class Proxy
{
    public function __construct(private readonly Client $client, private readonly string $connectionId)
    {
    }

    public function request(string $method, string $path, mixed $body = null, array $options = []): ProxyResponse
    {
        return $this->client->proxyRequest($this->connectionId, $method, $path, $body, $options);
    }

    public function get(string $path, array $options = []): ProxyResponse
    {
        return $this->request('GET', $path, null, $options);
    }

    public function post(string $path, mixed $body = null, array $options = []): ProxyResponse
    {
        return $this->request('POST', $path, $body, $options);
    }

    public function put(string $path, mixed $body = null, array $options = []): ProxyResponse
    {
        return $this->request('PUT', $path, $body, $options);
    }

    public function patch(string $path, mixed $body = null, array $options = []): ProxyResponse
    {
        return $this->request('PATCH', $path, $body, $options);
    }

    public function delete(string $path, array $options = []): ProxyResponse
    {
        return $this->request('DELETE', $path, null, $options);
    }
}
