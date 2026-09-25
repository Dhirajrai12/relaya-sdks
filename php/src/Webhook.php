<?php

declare(strict_types=1);

namespace Relaya;

use Relaya\Exception\WebhookVerificationException;

/**
 * Verify requests Relaya forwards to your endpoints.
 *
 *     $delivery = Webhook::verifyDelivery(file_get_contents('php://input'), getallheaders(), $secret);
 */
final class Webhook
{
    public const HEADER_SIGNATURE = 'Relaya-Signature';
    public const HEADER_IDEMPOTENCY_KEY = 'Idempotency-Key';
    public const HEADER_EVENT_ID = 'Relaya-Event-Id';
    public const HEADER_DELIVERY_ID = 'Relaya-Delivery-Id';
    public const HEADER_ATTEMPT = 'Relaya-Attempt';
    public const HEADER_REPLAY = 'Relaya-Replay';
    public const HEADER_EVENT_TYPE = 'Relaya-Event-Type';

    /** Reject signatures older (or newer) than this many seconds by default. */
    public const DEFAULT_TOLERANCE = 300;

    /**
     * Checks a Relaya-Signature header (t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<body>")>).
     *
     * @param string|string[] $secret one secret, or several while rotating (any match passes)
     * @param int $tolerance maximum signature age in seconds; 0 disables the check
     * @return int the Unix time the request was signed
     * @throws WebhookVerificationException
     */
    public static function verifySignature(string $body, ?string $header, string|array $secret, int $tolerance = self::DEFAULT_TOLERANCE, ?int $now = null): int
    {
        $secrets = array_values(array_filter(is_array($secret) ? $secret : [$secret], fn ($s) => is_string($s) && $s !== ''));
        if ($secrets === []) {
            throw new \InvalidArgumentException('relaya: a signing secret is required');
        }
        if ($header === null || $header === '') {
            throw new WebhookVerificationException('missing_signature', 'The Relaya-Signature header is missing');
        }

        $timestamp = null;
        $signatures = [];
        foreach (explode(',', $header) as $part) {
            [$k, $v] = array_pad(explode('=', trim($part), 2), 2, '');
            if ($k === 't' && ctype_digit($v)) {
                $timestamp = (int) $v;
            } elseif ($k === 'v1' && $v !== '') {
                $signatures[] = strtolower($v);
            }
        }
        if ($timestamp === null || $signatures === []) {
            throw new WebhookVerificationException('malformed_signature', 'The Relaya-Signature header is malformed');
        }

        $now ??= time();
        if ($tolerance > 0 && abs($now - $timestamp) > $tolerance) {
            throw new WebhookVerificationException('timestamp_out_of_range', "The signature is older than {$tolerance} seconds (or from the future)");
        }

        foreach ($secrets as $s) {
            $expected = hash_hmac('sha256', $timestamp . '.' . $body, $s);
            foreach ($signatures as $sig) {
                if (hash_equals($expected, $sig)) {
                    return $timestamp;
                }
            }
        }
        throw new WebhookVerificationException('signature_mismatch', 'The signature does not match: check the signing secret and that you pass the raw body');
    }

    /** Like verifySignature(), but returns true/false. */
    public static function isValidSignature(string $body, ?string $header, string|array $secret, int $tolerance = self::DEFAULT_TOLERANCE): bool
    {
        try {
            self::verifySignature($body, $header, $secret, $tolerance);
            return true;
        } catch (WebhookVerificationException) {
            return false;
        }
    }

    /**
     * Verifies a request forwarded by Relaya and returns its details.
     *
     * @param array<string, string|string[]> $headers e.g. getallheaders(), $_SERVER, or $request->headers->all()
     * @throws WebhookVerificationException
     */
    public static function verifyDelivery(string $body, array $headers, string|array $secret, int $tolerance = self::DEFAULT_TOLERANCE): Delivery
    {
        $signedAt = self::verifySignature($body, self::header($headers, self::HEADER_SIGNATURE), $secret, $tolerance);
        $deliveryId = self::header($headers, self::HEADER_DELIVERY_ID) ?? '';

        return new Delivery(
            idempotencyKey: self::header($headers, self::HEADER_IDEMPOTENCY_KEY) ?? $deliveryId,
            deliveryId: $deliveryId,
            eventId: self::header($headers, self::HEADER_EVENT_ID) ?? '',
            eventType: self::header($headers, self::HEADER_EVENT_TYPE),
            attempt: max(1, (int) (self::header($headers, self::HEADER_ATTEMPT) ?? 1)),
            replayId: self::header($headers, self::HEADER_REPLAY),
            signedAt: $signedAt,
            body: $body,
        );
    }

    /**
     * Verifies the current request in plain PHP (reads php://input and the request headers).
     *
     * @throws WebhookVerificationException
     */
    public static function fromGlobals(string|array $secret, int $tolerance = self::DEFAULT_TOLERANCE): Delivery
    {
        $headers = function_exists('getallheaders') ? (getallheaders() ?: []) : $_SERVER;
        return self::verifyDelivery((string) file_get_contents('php://input'), $headers, $secret, $tolerance);
    }

    /**
     * Verifies an alert sent to a webhook alert channel and returns it decoded:
     * ['type', 'title', 'body', 'link', 'org_id', 'alert_id', 'sent_at'].
     *
     * @throws WebhookVerificationException
     */
    public static function verifyAlert(string $body, array $headers, string|array $secret, int $tolerance = self::DEFAULT_TOLERANCE): array
    {
        self::verifySignature($body, self::header($headers, self::HEADER_SIGNATURE), $secret, $tolerance);
        return json_decode($body, true, 512, JSON_THROW_ON_ERROR);
    }

    /** Case-insensitive header lookup that also understands $_SERVER's HTTP_* keys. */
    private static function header(array $headers, string $name): ?string
    {
        $lower = strtolower($name);
        $server = 'HTTP_' . strtoupper(str_replace('-', '_', $name));
        foreach ($headers as $k => $v) {
            if (strtolower((string) $k) === $lower || $k === $server) {
                if (is_array($v)) {
                    $v = $v[0] ?? null;
                }
                return $v === null ? null : (string) $v;
            }
        }
        return null;
    }
}
