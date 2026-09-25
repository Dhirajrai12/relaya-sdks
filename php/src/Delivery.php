<?php

declare(strict_types=1);

namespace Relaya;

/** A verified request forwarded by Relaya. */
final class Delivery
{
    public function __construct(
        /** The same across retries and replays of one delivery: dedupe on this. */
        public readonly string $idempotencyKey,
        public readonly string $deliveryId,
        public readonly string $eventId,
        /** e.g. "payment.captured", when Relaya could tell. */
        public readonly ?string $eventType,
        /** 1 for the first try, then 2, 3… on retries. */
        public readonly int $attempt,
        /** Set when the request is part of an incident replay. */
        public readonly ?string $replayId,
        /** Unix time Relaya signed the request. */
        public readonly int $signedAt,
        /** The provider's original body, byte for byte. */
        public readonly string $body,
    ) {
    }

    /** The body decoded as JSON (associative arrays by default). */
    public function json(bool $associative = true): mixed
    {
        return json_decode($this->body, $associative, 512, JSON_THROW_ON_ERROR);
    }
}
