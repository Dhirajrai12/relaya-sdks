<?php

// A customer endpoint for the integration test, served by `php -S`. Verifies with the SDK and records
// each delivery as a JSON line in $RELAYA_IT_DIR/received. Reads the secret from $RELAYA_IT_DIR/secret and
// answers 500 while $RELAYA_IT_DIR/fail_next holds a positive number.

require __DIR__ . '/../../../vendor/autoload.php';

use Relaya\Exception\WebhookVerificationException;
use Relaya\Webhook;

$dir = getenv('RELAYA_IT_DIR');
try {
    $d = Webhook::fromGlobals(trim((string) file_get_contents("$dir/secret")));
} catch (WebhookVerificationException $e) {
    http_response_code(400);
    echo json_encode(['error' => $e->reason]);
    return;
}
file_put_contents("$dir/received", json_encode([
    'idempotencyKey' => $d->idempotencyKey,
    'deliveryId' => $d->deliveryId,
    'eventId' => $d->eventId,
    'eventType' => $d->eventType,
    'attempt' => $d->attempt,
    'replayId' => $d->replayId,
    'amount' => $d->json()['amount'] ?? null,
]) . "\n", FILE_APPEND | LOCK_EX);

$fail = (int) @file_get_contents("$dir/fail_next");
if ($fail > 0) {
    file_put_contents("$dir/fail_next", (string) ($fail - 1));
    http_response_code(500);
}
