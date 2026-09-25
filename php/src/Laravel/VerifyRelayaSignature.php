<?php

declare(strict_types=1);

namespace Relaya\Laravel;

use Relaya\Exception\WebhookVerificationException;
use Relaya\Webhook;

/**
 * Laravel middleware: verifies requests forwarded by Relaya and puts the result on the request.
 *
 *     // config/services.php
 *     'relaya' => ['signing_secret' => env('RELAYA_SIGNING_SECRET')],
 *
 *     // routes/api.php (exclude the route from CSRF if you use routes/web.php)
 *     Route::post('/webhooks/relaya', function (Request $request) {
 *         $delivery = $request->attributes->get('relaya'); // Relaya\Delivery
 *         return response()->noContent();
 *     })->middleware(\Relaya\Laravel\VerifyRelayaSignature::class);
 *
 * Requests with a bad signature get 400 {"error": "<reason>"} and never reach your route.
 * Several destinations? Name the config key: ->middleware(VerifyRelayaSignature::class.':services.relaya.orders_secret')
 */
final class VerifyRelayaSignature
{
    public function handle(object $request, \Closure $next, string $configKey = 'services.relaya.signing_secret'): mixed
    {
        $secret = function_exists('config') ? config($configKey) : null;
        $secret = $secret ?: (getenv('RELAYA_SIGNING_SECRET') ?: null);
        if (!$secret) {
            throw new \RuntimeException("relaya: set {$configKey} (or RELAYA_SIGNING_SECRET) to your destination's signing secret");
        }

        try {
            $delivery = Webhook::verifyDelivery($request->getContent(), $request->headers->all(), $secret);
        } catch (WebhookVerificationException $e) {
            return response()->json(['error' => $e->reason], 400);
        }

        $request->attributes->set('relaya', $delivery);
        return $next($request);
    }
}
