<?php

// Router for `php -S`: a tiny fake Relaya API. Every call is appended to $LOG as a JSON line.

$log = getenv('RELAYA_FAKE_LOG');
$method = $_SERVER['REQUEST_METHOD'];
$path = parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);
parse_str((string) parse_url($_SERVER['REQUEST_URI'], PHP_URL_QUERY), $query);
$body = file_get_contents('php://input');

$calls = is_file($log) ? array_map(fn ($l) => json_decode($l, true), array_filter(explode("\n", file_get_contents($log)))) : [];
$n = 1 + count(array_filter($calls, fn ($c) => $c['method'] === $method && $c['path'] === $path));
file_put_contents($log, json_encode([
    'method' => $method,
    'path' => $path,
    'query' => $query,
    'body' => $body === '' ? null : json_decode($body, true),
    'auth' => $_SERVER['HTTP_AUTHORIZATION'] ?? null,
]) . "\n", FILE_APPEND);

$json = function (int $status, $data = null, array $headers = []) {
    http_response_code($status);
    foreach ($headers as $k => $v) {
        header("$k: $v");
    }
    header('Content-Type: application/json');
    if ($data !== null) {
        echo json_encode($data);
    }
};

switch ("$method $path") {
    case 'GET /v1/me':
        return $json(200, ['api_key' => ['org_id' => 'org1']]);
    case 'GET /v1/orgs/org1/projects':
        return $json(200, ['data' => [['id' => 'p1']]]);
    case 'GET /v1/orgs/o/events':
        return ($query['cursor'] ?? '') === 'c2'
            ? $json(200, ['data' => [['id' => 'e3']], 'next_cursor' => null])
            : $json(200, ['data' => [['id' => 'e1'], ['id' => 'e2']], 'next_cursor' => 'c2']);
    case 'GET /v1/orgs/o/incidents':
        return $n < 3 ? $json(503, null, ['Retry-After' => '0']) : $json(200, ['data' => []]);
    case 'POST /v1/orgs/o/incidents/i1/replay':
        return $json(503, null, ['Retry-After' => '0']);
    default:
        return $json(404, ['error' => ['code' => 'not_found', 'message' => 'no route']]);
}
