<?php

declare(strict_types=1);

namespace Relaya\Tests;

use PHPUnit\Framework\TestCase;
use Relaya\Client;
use Relaya\Exception\RelayaException;

final class ClientTest extends TestCase
{
    private static $server;
    private static string $url;
    private static string $log;

    public static function setUpBeforeClass(): void
    {
        $port = 20000 + random_int(0, 9999);
        self::$log = sys_get_temp_dir() . "/relaya-fake-{$port}.log";
        self::$url = "http://127.0.0.1:{$port}";
        $env = array_merge(getenv(), ['RELAYA_FAKE_LOG' => self::$log]);
        self::$server = proc_open([PHP_BINARY, '-S', "127.0.0.1:{$port}", __DIR__ . '/fixtures/fake_api.php'], [1 => ['file', 'nul', 'w'], 2 => ['file', 'nul', 'w']], $pipes, null, $env);
        for ($i = 0; $i < 50 && !@fsockopen('127.0.0.1', $port); $i++) {
            usleep(100_000);
        }
    }

    public static function tearDownAfterClass(): void
    {
        proc_terminate(self::$server);
        @unlink(self::$log);
    }

    protected function setUp(): void
    {
        @unlink(self::$log);
    }

    private function calls(): array
    {
        return array_map(fn ($l) => json_decode($l, true), array_filter(explode("\n", (string) @file_get_contents(self::$log))));
    }

    public function testOrgLookupOnceAndAuth(): void
    {
        $c = new Client('rk_test', ['base_url' => self::$url . '/']);
        $this->assertSame('p1', $c->projects->list()[0]['id']);
        $c->projects->list();
        $calls = $this->calls();
        $this->assertCount(1, array_filter($calls, fn ($x) => $x['path'] === '/v1/me'));
        foreach ($calls as $x) {
            $this->assertSame('Bearer rk_test', $x['auth']);
        }
    }

    public function testFiltersAndIterate(): void
    {
        $c = new Client('rk', ['base_url' => self::$url, 'org_id' => 'o']);
        $ids = [];
        foreach ($c->events->iterate(['contract_status' => 'breaking', 'since' => new \DateTimeImmutable('2026-09-01T05:30:00+05:30'), 'type' => null, 'limit' => 2]) as $e) {
            $ids[] = $e['id'];
        }
        $this->assertSame(['e1', 'e2', 'e3'], $ids);
        $q = $this->calls()[0]['query'];
        $this->assertSame('breaking', $q['contract_status']);
        $this->assertSame('2026-09-01T00:00:00Z', $q['since']);
        $this->assertArrayNotHasKey('type', $q);
    }

    public function testErrorsAndRetries(): void
    {
        $c = new Client('rk', ['base_url' => self::$url, 'org_id' => 'o']);
        $this->assertSame([], $c->incidents->list('open'));
        try {
            $c->incidents->replay('i1');
            $this->fail('expected an exception');
        } catch (RelayaException $e) {
            $this->assertSame(503, $e->status);
        }
        $posts = array_values(array_filter($this->calls(), fn ($x) => $x['method'] === 'POST'));
        $this->assertCount(1, $posts);
        $this->assertSame(['confirm' => true], $posts[0]['body']);

        try {
            $c->webhooks->get('nope');
            $this->fail('expected an exception');
        } catch (RelayaException $e) {
            $this->assertSame([404, 'not_found'], [$e->status, $e->errorCode]);
        }
    }

    public function testNetworkError(): void
    {
        $c = new Client('rk', ['base_url' => 'http://127.0.0.1:1', 'org_id' => 'o', 'max_retries' => 0]);
        try {
            $c->projects->list();
            $this->fail('expected an exception');
        } catch (RelayaException $e) {
            $this->assertSame([0, 'network_error'], [$e->status, $e->errorCode]);
        }
    }
}
