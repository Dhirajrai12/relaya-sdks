<?php

declare(strict_types=1);

namespace Relaya\Tests;

use PHPUnit\Framework\TestCase;
use Relaya\Client;
use Relaya\Exception\RelayaException;

/**
 * End-to-end against a running Relaya (API + ingest + worker). Skipped unless RELAYA_IT_API_URL is set:
 *
 *     RELAYA_IT_API_URL=http://127.0.0.1:18080 vendor/bin/phpunit --filter Integration
 *
 * The server must allow http://127.0.0.1 destinations (APP_ENV=dev) and use CONTRACT_MIN_SAMPLES=3.
 */
final class IntegrationTest extends TestCase
{
    private static function waitFor(string $what, callable $fn, int $seconds = 15): mixed
    {
        $until = microtime(true) + $seconds;
        while (true) {
            $v = $fn();
            if ($v) {
                return $v;
            }
            if (microtime(true) > $until) {
                throw new \RuntimeException("timed out waiting for {$what}");
            }
            usleep(150_000);
        }
    }

    private static function post(string $url, array $body, ?string $token = null, array $extra = []): array
    {
        $ch = curl_init($url);
        curl_setopt_array($ch, [
            CURLOPT_POST => true,
            CURLOPT_POSTFIELDS => json_encode($body),
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_HTTPHEADER => array_merge(['Content-Type: application/json'], $token ? ["Authorization: Bearer {$token}"] : [], $extra),
        ]);
        $raw = curl_exec($ch);
        $status = curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
        curl_close($ch);
        if ($status >= 300) {
            throw new \RuntimeException("{$url}: {$status} {$raw}");
        }
        return json_decode((string) $raw, true) ?? [];
    }

    public function testSdkAgainstLiveRelaya(): void
    {
        $api = getenv('RELAYA_IT_API_URL');
        if (!$api) {
            $this->markTestSkipped('set RELAYA_IT_API_URL to run');
        }

        $session = self::post("{$api}/v1/auth/signup", ['email' => 'php-sdk-' . bin2hex(random_bytes(6)) . '@example.com', 'password' => 'sdk-test-password-1', 'org_name' => 'PHP SDK']);
        $orgId = (new Client($session['token'], ['base_url' => $api]))->request('GET', '/v1/me')['orgs'][0]['id'];
        $key = self::post("{$api}/v1/orgs/{$orgId}/api-keys", ['name' => 'sdk', 'role' => 'admin'], $session['token']);

        $relaya = new Client($key['key'], ['base_url' => $api]);
        $this->assertSame($orgId, $relaya->orgId());

        // A customer endpoint (php -S) verifying with the SDK.
        $dir = sys_get_temp_dir() . '/relaya-it-' . bin2hex(random_bytes(4));
        mkdir($dir);
        file_put_contents("{$dir}/secret", '');
        $port = 30000 + random_int(0, 9999);
        $server = proc_open([PHP_BINARY, '-S', "127.0.0.1:{$port}", __DIR__ . '/fixtures/endpoint.php'], [1 => ['file', 'nul', 'w'], 2 => ['file', 'nul', 'w']], $pipes, null, array_merge(getenv(), ['RELAYA_IT_DIR' => $dir]));
        self::waitFor('the endpoint', fn () => @fsockopen('127.0.0.1', $port));
        $received = fn (): array => array_map(fn ($l) => json_decode($l, true), array_filter(explode("\n", (string) @file_get_contents("{$dir}/received"))));

        try {
            $project = $relaya->projects->create('SDK');
            $wh = $relaya->webhooks->create(['project_id' => $project['id'], 'name' => 'Payments']);
            $dest = $relaya->destinations->create($wh['id'], ['name' => 'My app', 'url' => "http://127.0.0.1:{$port}/hooks"]);
            file_put_contents("{$dir}/secret", $dest['signing_secret']);

            $send = fn (string $id, array $body) => self::post($wh['ingest_url'], $body, null, ["X-Event-Id: {$id}"]);
            for ($i = 1; $i <= 3; $i++) {
                $send("e{$i}", ['type' => 'payment.captured', 'amount' => 100 * $i]);
            }

            self::waitFor('3 deliveries', fn () => count($received()) >= 3);
            $first = $received()[0];
            $this->assertSame($first['deliveryId'], $first['idempotencyKey']);
            $this->assertSame(1, $first['attempt']);
            $this->assertSame('payment.captured', $first['eventType']);
            $this->assertIsInt($first['amount']);

            $events = iterator_to_array($relaya->events->iterate(['webhook_id' => $wh['id'], 'limit' => 2]), false);
            $this->assertCount(3, $events);
            $this->assertSame('payment.captured', $relaya->events->get($first['eventId'])['payload_json']['type']);

            $ok = self::waitFor('succeeded deliveries', function () use ($relaya, $wh) {
                $l = $relaya->deliveries->list(['webhook_id' => $wh['id'], 'status' => 'succeeded']);
                return count($l) === 3 ? $l : null;
            });
            $this->assertSame('succeeded', $relaya->deliveries->get($ok[0]['id'])['attempts'][0]['outcome']);

            // Failing endpoint, then a manual retry.
            file_put_contents("{$dir}/fail_next", '1');
            $send('e4', ['type' => 'payment.captured', 'amount' => 400]);
            $retrying = self::waitFor('a retrying delivery', fn () => $relaya->deliveries->list(['webhook_id' => $wh['id'], 'status' => 'retrying']))[0];
            $relaya->deliveries->retry($retrying['id']);
            self::waitFor('the retry to succeed', fn () => $relaya->deliveries->get($retrying['id'])['delivery']['status'] === 'succeeded');
            $last = array_slice($received(), -1)[0];
            $this->assertSame(2, $last['attempt']);
            $this->assertSame($retrying['id'], $last['idempotencyKey']);

            // Contract -> incident -> replay.
            $contract = self::waitFor('a proposed contract', function () use ($relaya, $wh) {
                foreach ($relaya->contracts->list($wh['id']) as $c) {
                    if ($c['status'] !== 'learning') {
                        return $c;
                    }
                }
                return null;
            });
            $this->assertGreaterThanOrEqual(1, $relaya->contracts->createVersion($contract['id'], ['amount'], 'observed'));
            $send('e5', ['type' => 'payment.captured', 'amount' => '500']);
            $incident = self::waitFor('an open incident', fn () => $relaya->incidents->list('open'))[0];
            $this->assertStringContainsString('amount changed type', $incident['title']);
            $this->assertSame(1, $relaya->incidents->previewReplay($incident['id'])['events']);
            $before = count($received());
            $replay = $relaya->incidents->replay($incident['id']);
            $this->assertSame(1, $replay['total']);
            self::waitFor('the replayed delivery', fn () => count($received()) > $before);
            $this->assertSame($replay['id'], array_slice($received(), -1)[0]['replayId']);
            self::waitFor('the incident to resolve', fn () => $relaya->incidents->list('open') === []);

            // Destination test and API errors.
            $this->assertTrue($relaya->destinations->test($dest['destination']['id'])['ok']);
            try {
                $relaya->webhooks->get('00000000-0000-0000-0000-000000000000');
                $this->fail('expected 404');
            } catch (RelayaException $e) {
                $this->assertSame(404, $e->status);
            }
            try {
                $relaya->deliveries->retry($ok[0]['id']);
                $this->fail('expected 409');
            } catch (RelayaException $e) {
                $this->assertSame(409, $e->status);
            }
        } finally {
            proc_terminate($server);
            array_map('unlink', glob("{$dir}/*"));
            @rmdir($dir);
        }
    }
}
