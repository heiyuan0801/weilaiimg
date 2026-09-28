# ImageHub backend

Go API for the image hosting system. The server serves the compiled admin UI from `WEB_DIR` and exposes API routes under `/api/v1`.

## Local development

```bash
cp ../.env.example ../.env
export DATABASE_URL='postgres://imagehub:imagehub@localhost:5432/imagehub?sslmode=disable'
export REDIS_URL='redis://:imagehub@localhost:6379/0'
export BOOTSTRAP_ADMIN_EMAIL='admin@example.com'
export BOOTSTRAP_ADMIN_PASSWORD='change-this-admin-password'
go run ./cmd/server
```

The first process run applies the embedded PostgreSQL migration. A bootstrap admin is created only when `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD` are set and the users table is empty.

## Docker

From the repository root:

```bash
cp .env.example .env
# Set real passwords and, if needed, Telegram Bot values.
docker compose up -d --build
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

`uploads_data`, `postgres_data` and `redis_data` are persistent volumes. Redis is used for sessions, rate limits and short-lived state; PostgreSQL and the storage backend remain the source of truth.

## Storage

Local storage is selected by default. To enable Telegram storage, provide `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` and select Telegram in the administrator system settings. The adapter stores the Telegram message/file reference in PostgreSQL and streams the file through the Bot API when it is requested.

The administrator system settings API supports registration, email verification and password recovery switches, upload policy, SMTP fields, storage/CDN, and OIDC settings. SMTP delivery, PKCE/JWKS-validated OIDC, team invitations, team quotas, Stripe Checkout/webhook reconciliation, Cloudflare purge hooks, and verified domain/TLS authorization are wired into the API. Caddy can be enabled with `docker compose --profile edge up -d`.

Useful media management endpoints include `PATCH /api/v1/images/:id` for private/public visibility, `GET /media/:id/thumbnail` for generated video thumbnails, and `POST /api/v1/domains/:id/verify` for the DNS TXT check.

Video uploads are stored immediately. The production Docker image includes `ffmpeg` (and `ffprobe`), so the server records codec, dimensions and duration and stores a JPEG first-frame thumbnail. Failed or tool-unavailable inspections are placed in the PostgreSQL `media_jobs` queue, leased by a worker, recovered after crashes and retried up to five times. Minimal non-Docker deployments can omit those binaries; the original video remains usable and its processing state stays visible.

## Useful commands

```bash
make backend-test
make backend-vet
make compose-config
make up
```

## Backup and restore

Use the included scripts from the repository root. They preserve the PostgreSQL custom-format dump and the named uploads volume:

```bash
./scripts/backup.sh
./scripts/restore.sh ./backups/20260926T120000Z
```

Before a restore, stop writes to the application and verify the backup directory. Redis is intentionally not included because sessions and cache entries are disposable.
