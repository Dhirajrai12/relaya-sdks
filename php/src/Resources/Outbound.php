<?php

declare(strict_types=1);

namespace Relaya\Resources;

use Relaya\Client;

/**
 * Outbound webhooks: send events to your own customers. Relaya signs them (Standard Webhooks),
 * retries failures and logs every attempt; each customer manages their endpoints in a portal.
 *
 *     $relaya->outbound->apps->create('customer-123', 'Acme');
 *     $relaya->outbound->send('customer-123', 'invoice.paid', ['id' => 'in_1'], 'in_1-paid');
 */
final class Outbound extends Resource
{
    public readonly OutboundApps $apps;
    public readonly OutboundEndpoints $endpoints;
    public readonly OutboundEventTypes $eventTypes;

    public function __construct(Client $client)
    {
        parent::__construct($client);
        $this->apps = new OutboundApps($client);
        $this->endpoints = new OutboundEndpoints($client);
        $this->eventTypes = new OutboundEventTypes($client);
    }

    /**
     * Sends an event to every endpoint of that customer that takes its type. Returns
     * ['id', 'app', 'event_type', 'endpoints', 'duplicate']; with an idempotency key, sending the
     * same message again returns the first one ('duplicate' => true) instead of a copy.
     *
     * @param array<string, mixed> $payload
     */
    public function send(string $app, string $eventType, array $payload, ?string $idempotencyKey = null): array
    {
        $body = ['app' => $app, 'event_type' => $eventType, 'payload' => (object) $payload];
        if ($idempotencyKey !== null) {
            $body['idempotency_key'] = $idempotencyKey;
        }
        return $this->client->org('POST', '/outbound/messages', $body);
    }
}
