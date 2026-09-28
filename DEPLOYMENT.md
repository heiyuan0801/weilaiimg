# ImageHub deployment

ImageHub ships as one Go process that serves the built React application and the `/api/v1` API. PostgreSQL stores users, teams, image metadata, settings, jobs, domains and billing events. Redis stores sessions, rate-limit counters and short-lived OIDC state. Local storage is the default; Telegram storage can be selected from System settings when `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` are set.

## Compose

1. Copy `.env.example` to `.env` and replace every password, `APP_SECRET`, and bootstrap credential.
2. Run `docker compose up -d --build`.
3. Open `PUBLIC_URL` and sign in with the bootstrap account. The bootstrap account is created only when the database has no users.
4. Configure SMTP before enabling email verification or password recovery. SMTP passwords are encrypted with `APP_SECRET` before being written to PostgreSQL.

If Docker Desktop needs the host proxy on port 7897 for image and dependency downloads, start that proxy first and run:

```sh
docker compose -f docker-compose.yml -f compose.proxy.yaml build
docker compose -f docker-compose.yml -f compose.proxy.yaml up -d
```

The build uses `host.docker.internal:7897` from inside Docker. Override `DOCKER_HTTP_PROXY`, `DOCKER_HTTPS_PROXY`, or `DOCKER_ALL_PROXY` in `.env` when the proxy uses another address or protocol.

The optional `edge` profile adds Caddy and on-demand TLS:

```sh
docker compose --profile edge up -d
```

Add a domain in the admin UI, publish the returned TXT record, click Verify DNS, and point the domain's A/AAAA record at the Caddy host. Caddy asks `/api/v1/domains/tls-authorize` before obtaining a certificate, so an unverified domain cannot trigger certificate issuance.

## Billing and CDN

Free subscriptions are created locally. Paid plans require a Stripe price ID configured by an administrator and `STRIPE_SECRET_KEY` in the environment. The checkout endpoint creates a subscription-mode Checkout Session; `/api/v1/billing/webhook` verifies the `Stripe-Signature`, deduplicates event IDs in `billing_events`, and only then updates the team plan. `STRIPE_WEBHOOK_SECRET` is required.

Set `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ZONE_ID` to purge public media after visibility changes and deletes. Without those variables, the configured CDN base URL is still returned but purge calls are skipped.

## OIDC

Add an enabled provider from System settings. The authorization-code flow uses a browser-bound state cookie, S256 PKCE, a nonce, discovery issuer matching, JWKS signature validation (RS256/ES256), audience/issuer/lifetime checks, and verified userinfo email. Disabled local accounts are never reactivated by an OIDC login.

## Backups

Use `scripts/backup.sh` for a PostgreSQL dump and upload archive, and `scripts/restore.sh` to restore both. Keep the database dump and object volume from the same point in time when restoring image metadata.

A convenience wrapper is available at `scripts/start-with-proxy.sh`; it checks both the Docker daemon and the local proxy before starting the stack, and defaults to `http://localhost:18080` for local inspection.
