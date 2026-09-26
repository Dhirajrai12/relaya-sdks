# Relaya SDKs

Official SDKs for Relaya. Each one does the same two jobs, with the same names where the language allows:

1. **Receive:** verify the `Relaya-Signature` on requests Relaya forwards to your endpoint, and read the delivery's details (idempotency key, event ID, attempt, replay ID, original body).
2. **Call the API:** events (with paging), deliveries and retry, incidents and replay, contracts, destinations, webhooks, projects, alerts.

| Language | Folder | Install | Receive helpers |
|---|---|---|---|
| Node.js / TypeScript | [node](node) | `npm install relaya-node` | Express middleware, Fetch handler (Next.js, Hono, Bun, Deno, Workers) |
| Python | [python](python) | `pip install relaya` | Django, Flask, FastAPI |
| PHP | [php](php) | `composer require dhirajrai12/relaya-php` | Laravel middleware, plain PHP |
| Go | [go](go) | `go get github.com/Dhirajrai12/relaya-sdks/go` | `net/http` middleware |
| Java / Kotlin | [java](java) | Maven `io.github.dhirajrai12:relaya-java` | Spring Boot, Servlet |

## The signature, for any other language

```
Relaya-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>
```

Recompute the HMAC over the exact bytes received, compare in constant time, and reject timestamps more than 5 minutes from now. Dedupe on `Idempotency-Key`, which stays the same across retries and replays.

## Development

| SDK | Unit tests |
|---|---|
| Node | `cd node && npm ci && npm test` |
| Python | `cd python && PYTHONPATH=src python -m unittest discover -s tests` |
| PHP | `composer install && vendor/bin/phpunit` (from the repo root) |
| Go | `cd go && go test ./...` |
| Java | `cd java && mvn test` |

Every SDK also has an integration test that runs against a real Relaya (API, ingest and worker) started with `APP_ENV=dev` and `CONTRACT_MIN_SAMPLES=3`. Set `RELAYA_IT_API_URL` (e.g. `http://127.0.0.1:18080`) and run the same commands.

[Test](.github/workflows/test.yml) runs on every push, on the oldest and newest supported runtime of each language.

## Releasing

Publish a GitHub release with a tag like `v0.2.0`. [Release](.github/workflows/release.yml) stamps that version into every SDK, runs the tests again and publishes:

| Registry | One-time setup | Then |
|---|---|---|
| npm | Create an npm account and the `@relaya` org. Add a granular access token (publish rights) as the repo secret `NPM_TOKEN`. | Automatic |
| PyPI | Create a PyPI account. Under **Publishing**, add a pending trusted publisher: this repo, workflow `release.yml`, environment `pypi`. Set the repo variable `PUBLISH_PYPI` to `true`. | Automatic, no token stored |
| Packagist | Create a Packagist account, **Submit** this repo's URL, and connect GitHub so it updates on each tag. | Automatic |
| Go | Nothing. The workflow pushes the `go/vX.Y.Z` tag Go needs. | Automatic |
| Maven Central | Verify the `io.github.dhirajrai12` namespace at central.sonatype.com (sign in with GitHub). Add `MAVEN_CENTRAL_USERNAME` / `MAVEN_CENTRAL_PASSWORD` (a user token) and `MAVEN_GPG_PRIVATE_KEY` / `MAVEN_GPG_PASSPHRASE` (a GPG key published to keyserver.ubuntu.com). | Automatic |

A registry that isn't set up yet is skipped with a note in the run, so they can be switched on one at a time. Secrets go in GitHub → Settings → Secrets and variables → Actions; never in code or chat.

Published names: `relaya-node` (npm), `dhirajrai12/relaya-php` (Packagist), `relaya` (PyPI), `github.com/Dhirajrai12/relaya-sdks/go` (Go) and `io.github.dhirajrai12:relaya-java` (Maven Central). A published name can't be taken back.

## License

[MIT](LICENSE)
