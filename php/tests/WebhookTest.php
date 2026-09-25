<?php

declare(strict_types=1);

namespace Relaya\Tests;

use PHPUnit\Framework\TestCase;
use Relaya\Exception\WebhookVerificationException;
use Relaya\Webhook;

final class WebhookTest extends TestCase
{
    public const SECRET = 'whsec_test_secret';
    public const BODY = '{"type":"payment.captured","amount":100,"note":"héllo"}';

    /** Same algorithm as the Relaya worker: hex HMAC-SHA256(secret, "<t>.<body>"). */
    public static function sign(string $body, string $secret = self::SECRET, ?int $t = null): string
    {
        $t ??= time();
        return "t={$t},v1=" . hash_hmac('sha256', "{$t}.{$body}", $secret);
    }

    private function reason(callable $fn): string
    {
        try {
            $fn();
        } catch (WebhookVerificationException $e) {
            return $e->reason;
        }
        $this->fail('expected verification to fail');
    }

    public function testValidSignature(): void
    {
        $this->assertGreaterThan(0, Webhook::verifySignature(self::BODY, self::sign(self::BODY), self::SECRET));
        $this->assertTrue(Webhook::isValidSignature(self::BODY, self::sign(self::BODY), self::SECRET));
    }

    public function testRejections(): void
    {
        $this->assertSame('signature_mismatch', $this->reason(fn () => Webhook::verifySignature(self::BODY, self::sign(self::BODY, 'other'), self::SECRET)));
        $this->assertSame('signature_mismatch', $this->reason(fn () => Webhook::verifySignature(self::BODY . ' ', self::sign(self::BODY), self::SECRET)));
        $this->assertSame('missing_signature', $this->reason(fn () => Webhook::verifySignature(self::BODY, null, self::SECRET)));
        $this->assertSame('malformed_signature', $this->reason(fn () => Webhook::verifySignature(self::BODY, 'v1=abc', self::SECRET)));
        $this->assertSame('malformed_signature', $this->reason(fn () => Webhook::verifySignature(self::BODY, 't=x,v1=abc', self::SECRET)));
        $this->assertFalse(Webhook::isValidSignature(self::BODY, self::sign(self::BODY, 'other'), self::SECRET));
    }

    public function testTolerance(): void
    {
        $old = time() - 301;
        $this->assertSame('timestamp_out_of_range', $this->reason(fn () => Webhook::verifySignature(self::BODY, self::sign(self::BODY, self::SECRET, $old), self::SECRET)));
        Webhook::verifySignature(self::BODY, self::sign(self::BODY, self::SECRET, $old), self::SECRET, 600);
        Webhook::verifySignature(self::BODY, self::sign(self::BODY, self::SECRET, $old), self::SECRET, 0);
        $this->assertSame(1_700_000_000, Webhook::verifySignature(self::BODY, self::sign(self::BODY, self::SECRET, 1_700_000_000), self::SECRET, 300, 1_700_000_005));
    }

    public function testRotation(): void
    {
        Webhook::verifySignature(self::BODY, self::sign(self::BODY, 'new'), ['old', 'new']);
        $this->expectException(\InvalidArgumentException::class);
        Webhook::verifySignature(self::BODY, self::sign(self::BODY), []);
    }

    public function testDeliveryFromHeadersAndServerVars(): void
    {
        $plain = [
            'relaya-signature' => self::sign(self::BODY),
            'Idempotency-Key' => 'dlv_1',
            'RELAYA-DELIVERY-ID' => 'dlv_1',
            'Relaya-Event-Id' => ['evt_1'], // Symfony's headers->all() gives arrays
            'Relaya-Attempt' => '3',
            'Relaya-Event-Type' => 'payment.captured',
        ];
        $server = [];
        foreach ($plain as $k => $v) {
            $server['HTTP_' . strtoupper(str_replace('-', '_', $k))] = $v;
        }
        foreach ([$plain, $server] as $headers) {
            $d = Webhook::verifyDelivery(self::BODY, $headers, self::SECRET);
            $this->assertSame('dlv_1', $d->idempotencyKey);
            $this->assertSame('evt_1', $d->eventId);
            $this->assertSame(3, $d->attempt);
            $this->assertSame('payment.captured', $d->eventType);
            $this->assertNull($d->replayId);
            $this->assertSame('héllo', $d->json()['note']);
        }
    }

    public function testAlert(): void
    {
        $body = json_encode(['type' => 'test', 'title' => 'Test alert', 'alert_id' => 1]);
        $this->assertSame('Test alert', Webhook::verifyAlert($body, ['Relaya-Signature' => self::sign($body)], self::SECRET)['title']);
    }
}
