#!/usr/bin/env sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 BACKUP_DIRECTORY" >&2
  exit 2
fi
BACKUP_DIR=$1
[ -f "$BACKUP_DIR/postgres.dump" ] || { echo "missing postgres.dump" >&2; exit 1; }
[ -f "$BACKUP_DIR/uploads.tar.gz" ] || { echo "missing uploads.tar.gz" >&2; exit 1; }

docker compose up -d postgres redis
docker compose exec -T postgres sh -c 'dropdb -U "$POSTGRES_USER" --if-exists "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists' < "$BACKUP_DIR/postgres.dump"
docker run --rm -v imagehub_uploads_data:/target -v "$(cd "$(dirname "$BACKUP_DIR")" && pwd):/backup" alpine:3.21 sh -c "tar xzf /backup/$(basename "$BACKUP_DIR")/uploads.tar.gz -C /target"
docker compose up -d app
printf 'Restore completed from %s\n' "$BACKUP_DIR"
