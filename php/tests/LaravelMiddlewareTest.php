<?php

declare(strict_types=1);

namespace Relaya\Tests {

    use PHPUnit\Framework\TestCase;
    use Relaya\Delivery;
    use Relaya\Laravel\VerifyRelayaSignature;

    /** Exercises the middleware with stand-ins for Laravel's Request and helpers (defined below). */
    final class LaravelMiddlewareTest extends TestCase
    {
        private function request(string $body, array $headers): object
        {
            return new class ($body, $headers) {
                public object $headers;
                public object $attributes;

                public function __construct(private string $body, array $headers)
                {
                    $this->headers = new class ($headers) {
                        public function __construct(private array $h)
                        {
                        }

                        public function all(): array
                        {
                            return array_map(fn ($v) => [$v], array_change_key_case($this->h));
                        }
                    };
                    $this->attributes = new class () {
                        public array $items = [];

                        public function set(string $k, $v): void
                        {
                            $this->items[$k] = $v;
                        }
                    };
                }

                public function getContent(): string
                {
                    return $this->body;
                }
            };
        }

        public function testPassesVerifiedRequestsAndRejectsBadOnes(): void
        {
            $GLOBALS['relaya_test_config'] = ['services.relaya.signing_secret' => WebhookTest::SECRET];
            $mw = new VerifyRelayaSignature();

            $req = $this->request(WebhookTest::BODY, ['Relaya-Signature' => WebhookTest::sign(WebhookTest::BODY), 'Relaya-Event-Id' => 'evt_9']);
            $out = $mw->handle($req, fn ($r) => 'next called');
            $this->assertSame('next called', $out);
            $this->assertInstanceOf(Delivery::class, $req->attributes->items['relaya']);
            $this->assertSame('evt_9', $req->attributes->items['relaya']->eventId);

            $bad = $this->request(WebhookTest::BODY, ['Relaya-Signature' => WebhookTest::sign(WebhookTest::BODY, 'other')]);
            $out = $mw->handle($bad, fn () => $this->fail('must not reach the route'));
            $this->assertSame([['error' => 'signature_mismatch'], 400], $out);

            // A named config key for a second destination
            $GLOBALS['relaya_test_config']['services.relaya.orders'] = 'orders_secret';
            $req = $this->request(WebhookTest::BODY, ['Relaya-Signature' => WebhookTest::sign(WebhookTest::BODY, 'orders_secret')]);
            $this->assertSame('ok', $mw->handle($req, fn () => 'ok', 'services.relaya.orders'));
        }
    }
}

namespace {
    // Minimal stand-ins for Laravel's global helpers.
    if (!function_exists('config')) {
        function config(string $key)
        {
            return $GLOBALS['relaya_test_config'][$key] ?? null;
        }
    }
    if (!function_exists('response')) {
        function response()
        {
            return new class () {
                public function json(array $data, int $status): array
                {
                    return [$data, $status];
                }
            };
        }
    }
}
