# Relaya CLI

Forward the webhooks Relaya receives to your own computer, and send test events, while you build. Like the Stripe CLI, but for every provider Relaya receives: Razorpay, Stripe, Shopify, GitHub, Jira, Cashfree, PayU, PhonePe, Standard Webhooks and your own.

```sh
relaya login
relaya listen --forward-to http://localhost:3000/webhooks
relaya trigger payment.captured          # in another terminal
```

```
Forwarding events from Payments (razorpay)
  to http://localhost:3000/webhooks
Relaya signs each forwarded request with this secret; verify with it locally
(e.g. RELAYA_SIGNING_SECRET=rsec_Ta6e…)
Press Ctrl+C to stop.
11:15:19  Ready. Waiting for events…
11:15:22  payment.captured (simulated)  6b20d442  → ✓ 200 OK (27 ms)
11:15:40  payment.failed  03867e95  → ✗ 500 Internal Server Error (4 ms)
      {"error":"amount must be a number"}
```

## Install

Download the file for your computer from the [latest release](https://github.com/Dhirajrai12/relaya-sdks/releases/latest) (`relaya_…_windows_amd64.zip`, `…_darwin_arm64.tar.gz` for Apple silicon Macs, `…_linux_amd64.tar.gz`…), unpack it and put `relaya` (or `relaya.exe`) somewhere on your PATH.

With Go 1.23 or newer:

```sh
go install github.com/Dhirajrai12/relaya-sdks/cli/cmd/relaya@latest
```

## Commands

| Command | What it does |
|---|---|
| `relaya login [--api-key rk_…] [--base-url URL]` | Saves an API key (Relaya → Settings → API keys, **admin** role) in your user config folder, readable only by you. Asks for it when `--api-key` is left out. |
| `relaya webhooks` | Lists your webhooks. |
| `relaya listen --forward-to URL` | Forwards each new event to URL until Ctrl+C. `3000` and `3000/webhooks` mean `http://localhost:3000…`. |
| `relaya trigger EVENT_TYPE [--webhook ID\|name] [--payload file.json]` | Sends a sample event with the [event simulator](https://github.com/Dhirajrai12/relaya-sdks#readme): signed with the webhook's own secret, the way the provider signs it. |
| `relaya logout`, `relaya version` | |

`listen` options: `--webhook` (ID or name; repeat or comma-separate; default all), `--events a,b` (only these types), `-H "Name: value"` (extra header, repeatable), `--print-body`, `--timeout 30s`.

`RELAYA_API_KEY` and `RELAYA_BASE_URL` override the saved login, e.g. in CI.

## What your local server receives

The provider's original body and headers, so your existing provider signature check (Razorpay's `X-Razorpay-Signature`, Stripe's `Stripe-Signature`…) keeps working, plus the headers a Relaya destination gets: `Relaya-Event-Id`, `Relaya-Event-Type`, `Relaya-Delivery-Id`, `Idempotency-Key`, `Relaya-Attempt` and `Relaya-Signature`, signed with the listen secret the CLI prints. It stays the same across runs, so put it in your local `.env` and verify with any Relaya SDK, exactly as in production. Simulated events also carry `Relaya-Simulated: true`.

## How it works

`listen` keeps a WebSocket open to Relaya (outbound only: nothing needs to reach your computer, and it works behind any firewall or NAT). For each new accepted event of the chosen webhooks it fetches the event exactly as it arrived and posts it to your URL, one at a time, in order. If the connection drops it reconnects and catches up on anything that arrived meanwhile.

Your real destinations keep receiving events as usual; `listen` is an extra copy for your machine, and your server's answer isn't recorded in Relaya.

## Development

```sh
go test ./...
RELAYA_IT_API_URL=http://127.0.0.1:18080 go test ./...   # also against a running Relaya
```
