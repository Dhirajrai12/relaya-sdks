<?php

declare(strict_types=1);

namespace Relaya;

use Relaya\Exception\RelayaException;
use Relaya\Resources\Alerts;
use Relaya\Resources\Contracts;
use Relaya\Resources\Deliveries;
use Relaya\Resources\Destinations;
use Relaya\Resources\Events;
use Relaya\Resources\Incidents;
use Relaya\Resources\Projects;
use Relaya\Resources\Webhooks;

/**
 * Relaya API client. Responses are associative arrays with the API's field names.
 *
 *     $relaya = new Relaya\Client(getenv('RELAYA_API_KEY'));
 *     foreach ($relaya->events->iterate(['contract_status' => 'breaking']) as $event) { ... }
 */
final class Client
{
    public const VERSION = '0.1.0';

    /** Where the API lives until the product has its own domain. */
    public const DEFAULT_BASE_URL = 'https://server.aegonassett.com/api';

    public readonly string $baseUrl;
    public readonly Projects $projects;
    public readonly Webhooks $webhooks;
    public readonly Events $events;
    public readonly Destinations $destinations;
    public readonly Deliveries $deliveries;
    public readonly Contracts $contracts;
    public readonly Incidents $incidents;
    public readonly Alerts $alerts;

    private string $apiKey;
    private ?string $orgId;
    private int $timeout;
    private int $maxRetries;

    /**
     * @param array{org_id?: string, base_url?: string, timeout?: int, max_retries?: int} $options
     *   org_id is only needed with a session token; an API key belongs to one org.
     */
    public function __construct(?string $apiKey = null, array $options = [])
    {
        $apiKey = $apiKey ?: (getenv('RELAYA_API_KEY') ?: null);
        if ($apiKey === null) {
            throw new \InvalidArgumentException('relaya: pass an API key or set RELAYA_API_KEY');
        }
        $this->apiKey = $apiKey;
        $this->baseUrl = rtrim($options['base_url'] ?? (getenv('RELAYA_BASE_URL') ?: self::DEFAULT_BASE_URL), '/');
        $this->orgId = $options['org_id'] ?? null;
        $this->timeout = $options['timeout'] ?? 30;
        $this->maxRetries = $options['max_retries'] ?? 2;

        $this->projects = new Projects($this);
        $this->webhooks = new Webhooks($this);
        $this->events = new Events($this);
        $this->destinations = new Destinations($this);
        $this->deliveries = new Deliveries($this);
        $this->contracts = new Contracts($this);
        $this->incidents = new Incidents($this);
        $this->alerts = new Alerts($this);
    }

    /** The org this client acts on, looked up from the API key on first use. */
    public function orgId(): string
    {
        if ($this->orgId === null) {
            $me = $this->request('GET', '/v1/me');
            if (!isset($me['api_key']['org_id'])) {
                throw new \LogicException('relaya: pass org_id when using a session token instead of an API key');
            }
            $this->orgId = $me['api_key']['org_id'];
        }
        return $this->orgId;
    }

    /**
     * Low-level request; paths start with /v1. Returns the decoded JSON (null for 204).
     *
     * @param array<string, scalar|\DateTimeInterface|null> $query
     * @throws RelayaException
     */
    public function request(string $method, string $path, ?array $body = null, array $query = []): mixed
    {
        $url = $this->baseUrl . $path;
        $params = [];
        foreach ($query as $k => $v) {
            if ($v === null || $v === '') {
                continue;
            }
            if ($v instanceof \DateTimeInterface) {
                $v = (new \DateTimeImmutable('@' . $v->getTimestamp()))->format('Y-m-d\TH:i:s\Z');
            }
            $params[$k] = is_bool($v) ? ($v ? 'true' : 'false') : (string) $v;
        }
        if ($params !== []) {
            $url .= '?' . http_build_query($params);
        }
        $headers = [
            'Authorization: Bearer ' . $this->apiKey,
            'Accept: application/json',
            'User-Agent: relaya-php/' . self::VERSION,
        ];
        $payload = null;
        if ($body !== null) {
            $payload = json_encode($body === [] ? new \stdClass() : $body, JSON_THROW_ON_ERROR);
            $headers[] = 'Content-Type: application/json';
        }
        $retries = $method === 'GET' ? $this->maxRetries : 0;

        for ($attempt = 0; ; $attempt++) {
            $responseHeaders = [];
            $ch = curl_init($url);
            curl_setopt_array($ch, [
                CURLOPT_CUSTOMREQUEST => $method,
                CURLOPT_HTTPHEADER => $headers,
                CURLOPT_RETURNTRANSFER => true,
                CURLOPT_TIMEOUT => $this->timeout,
                CURLOPT_CONNECTTIMEOUT => min(10, $this->timeout),
                CURLOPT_HEADERFUNCTION => function ($ch, string $line) use (&$responseHeaders): int {
                    if (str_contains($line, ':')) {
                        [$k, $v] = explode(':', $line, 2);
                        $responseHeaders[strtolower(trim($k))] = trim($v);
                    }
                    return strlen($line);
                },
            ]);
            if ($payload !== null) {
                curl_setopt($ch, CURLOPT_POSTFIELDS, $payload);
            }
            $raw = curl_exec($ch);
            $status = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
            $errno = curl_errno($ch);
            $error = curl_error($ch);
            curl_close($ch);

            if ($raw === false || $errno !== 0) {
                if ($attempt < $retries) {
                    usleep(self::backoff($attempt, null));
                    continue;
                }
                $timedOut = $errno === CURLE_OPERATION_TIMEDOUT;
                throw new RelayaException(0, $timedOut ? 'timeout' : 'network_error', $timedOut ? "Request timed out after {$this->timeout}s" : "Could not reach Relaya: {$error}");
            }
            if (($status === 429 || $status >= 500) && $attempt < $retries) {
                usleep(self::backoff($attempt, $responseHeaders['retry-after'] ?? null));
                continue;
            }
            $data = $raw === '' ? null : json_decode($raw, true);
            if ($status >= 300) {
                $err = is_array($data) ? ($data['error'] ?? []) : [];
                throw new RelayaException($status, $err['code'] ?? 'http_error', $err['message'] ?? "HTTP {$status}", $responseHeaders['x-request-id'] ?? null);
            }
            return $data;
        }
    }

    /** @internal used by the resources */
    public function org(string $method, string $path, ?array $body = null, array $query = []): mixed
    {
        return $this->request($method, '/v1/orgs/' . $this->orgId() . $path, $body, $query);
    }

    /** Microseconds to wait: Retry-After (capped at 30 s) or exponential backoff with jitter. */
    private static function backoff(int $attempt, ?string $retryAfter): int
    {
        if ($retryAfter !== null && is_numeric($retryAfter)) {
            return (int) (min(max((float) $retryAfter, 0), 30) * 1_000_000);
        }
        return (int) (min(0.5 * 2 ** $attempt, 8) * (0.8 + mt_rand() / mt_getrandmax() * 0.4) * 1_000_000);
    }
}
