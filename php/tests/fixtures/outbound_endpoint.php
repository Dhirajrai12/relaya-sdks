<?php

// A customer's server for the outbound integration test, served by `php -S`. Records each request's
// Standard Webhooks headers and raw body as a JSON line in $RELAYA_IT_DIR/received.

file_put_contents(getenv('RELAYA_IT_DIR') . '/received', json_encode([
    'id' => $_SERVER['HTTP_WEBHOOK_ID'] ?? '',
    'timestamp' => $_SERVER['HTTP_WEBHOOK_TIMESTAMP'] ?? '',
    'signature' => $_SERVER['HTTP_WEBHOOK_SIGNATURE'] ?? '',
    'body' => file_get_contents('php://input'),
]) . "\n", FILE_APPEND | LOCK_EX);
echo 'ok';
