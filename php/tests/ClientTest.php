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

    public function testConnectionsLinkFindToken(): void
    {
        $c = new Client('rk', ['base_url' => self::$url, 'org_id' => 'o']);
        $this->assertSame('https://r/connect/cs_x', $c->connections->createLink('zoho', 'u1')['url']);
        $this->assertSame(['integration' => 'zoho', 'end_user_id' => 'u1'], $this->calls()[0]['body']);
        $this->assertSame('c1', $c->connections->find('zoho', 'u1')['id']);
        $this->assertNull($c->connections->find('zoho', 'nobody'));
        $this->assertSame('https://www.zohoapis.in', $c->connections->token('c1')['api_base']);
    }

    public function testProxy(): void
    {
        $c = new Client('rk', ['base_url' => self::$url, 'org_id' => 'o']);
        $res = $c->proxy('c1')->get('/crm/v2/Leads', ['query' => ['per_page' => 10], 'headers' => ['orgId' => '42'], 'base_url' => 'https://www.zohoapis.in']);
        $this->assertTrue($res->ok);
        $this->assertSame([200, 2, '1'], [$res->status, $res->attempts, $res->data['data'][0]['id']]);
        $call = $this->calls()[0];
        $this->assertSame(['per_page' => '10'], $call['query']);
        $this->assertSame('42', $call['proxy_headers']['HTTP_RELAYA_PROXY_ORGID']);
        $this->assertSame('https://www.zohoapis.in', $call['proxy_headers']['HTTP_RELAYA_PROXY_BASE_URL']);
        $this->assertSame('Bearer rk', $call['auth']);

        $created = $c->proxy('c1')->post('/crm/v2/Leads', ['data' => [['Last_Name' => 'Rao']]]);
        $this->assertSame([201, ['data' => [['Last_Name' => 'Rao']]]], [$created->status, $created->data['echo']]);

        $missing = $c->proxy('c1')->get('/missing'); // the provider's own error: returned
        $this->assertFalse($missing->ok);
        $this->assertSame(404, $missing->status);

        try { // Relaya couldn't make the call: thrown
            $c->proxy('c2')->get('/x');
            $this->fail('expected an exception');
        } catch (RelayaException $e) {
            $this->assertSame([409, 'connection_broken'], [$e->status, $e->errorCode]);
        }
    }

    public function testSyncs(): void
    {
        $c = new Client('rk', ['base_url' => self::$url, 'org_id' => 'o']);
        $this->assertSame('zoho.crm_records', $c->syncs->models()[0]['key']);
        $s = $c->syncs->create('c1', 'zoho.crm_records', ['module' => 'Leads'], ['interval_minutes' => 15]);
        $this->assertSame('sy1', $s['id']);
        $this->assertSame(['connection_id' => 'c1', 'model' => 'zoho.crm_records', 'config' => ['module' => 'Leads'], 'interval_minutes' => 15], $this->calls()[1]['body']);
        $this->assertTrue($c->syncs->run('sy1')['running']);
    }

    public function testOutbound(): void
    {
        $c = new Client('rk', ['base_url' => self::$url, 'org_id' => 'o']);
        $this->assertSame('a1', $c->outbound->apps->create('cust:42', 'Acme')['id']);
        $this->assertSame(['uid' => 'cust:42', 'name' => 'Acme'], $this->calls()[0]['body']);
        $this->assertSame('whsec_x', $c->outbound->endpoints->create('cust:42', 'https://acme.test/hooks', ['event_types' => ['invoice.paid']])['signing_secret']);
        $this->assertSame('ep1', $c->outbound->endpoints->list('cust:42')[0]['id']);
        $this->assertTrue($c->outbound->endpoints->test('cust:42', 'ep1', 'invoice.paid')['ok']);
        $this->assertSame('invoice.paid', $this->calls()[3]['query']['event_type']);
        $this->assertSame('m1', $c->outbound->send('cust:42', 'invoice.paid', ['id' => 'in_1'], 'in_1')['id']);
        $this->assertSame(['app' => 'cust:42', 'event_type' => 'invoice.paid', 'payload' => ['id' => 'in_1'], 'idempotency_key' => 'in_1'], $this->calls()[4]['body']);
        $this->assertStringContainsString('#ps_', $c->outbound->apps->portalLink('cust:42')['url']);
    }
}
