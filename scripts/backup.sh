#!/usr/bin/env sh
set -eu

BACKUP_DIR=${BACKUP_DIR:-./backups/$(date -u +%Y%m%dT%H%M%SZ)}
mkdir -p "$BACKUP_DIR"

# Compose reads the credentials from .env. The dump is created inside the
# PostgreSQL container so the host does not need a local pg_dump binary.
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' > "$BACKUP_DIR/postgres.dump"
docker run --rm -v imagehub_uploads_data:/source -v "$(cd "$(dirname "$BACKUP_DIR")" && pwd):/backup" alpine:3.21 sh -c "tar czf /backup/$(basename "$BACKUP_DIR")/uploads.tar.gz -C /source ."
cp .env.example "$BACKUP_DIR/env.example"
printf 'Backup written to %s\n' "$BACKUP_DIR"
