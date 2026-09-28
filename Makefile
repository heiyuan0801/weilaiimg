.PHONY: frontend-install frontend-build backend-test backend-vet compose-config up down backup restore

frontend-install:
	pnpm install --frozen-lockfile

frontend-build:
	pnpm build

backend-test:
	cd backend && GOTOOLCHAIN=local go test ./...

backend-vet:
	cd backend && GOTOOLCHAIN=local go vet ./...

compose-config:
	docker compose config --quiet

up:
	docker compose up -d --build

down:
	docker compose down

backup:
	./scripts/backup.sh

restore:
	@test -n "$(BACKUP_DIR)" || (echo 'set BACKUP_DIR=/path/to/backup' >&2; exit 2)
	./scripts/restore.sh "$(BACKUP_DIR)"
