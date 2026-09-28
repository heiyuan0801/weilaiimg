#!/usr/bin/env bash
set -euo pipefail

proxy_url="${DOCKER_HTTP_PROXY:-http://127.0.0.1:7897}"
proxy_host="${proxy_url#*://}"
proxy_host="${proxy_host%%:*}"
proxy_port="${proxy_url##*:}"

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is unavailable. Start Docker Desktop and retry." >&2
  exit 1
fi
if ! (echo >/dev/tcp/"$proxy_host"/"$proxy_port") >/dev/null 2>&1; then
  echo "Proxy $proxy_url is not reachable. Start the local proxy or set DOCKER_HTTP_PROXY." >&2
  exit 1
fi

export DOCKER_HTTP_PROXY="${DOCKER_HTTP_PROXY:-http://host.docker.internal:7897}"
export DOCKER_HTTPS_PROXY="${DOCKER_HTTPS_PROXY:-$DOCKER_HTTP_PROXY}"
export APP_PORT="${APP_PORT:-18080}"
export PUBLIC_URL="${PUBLIC_URL:-http://localhost:${APP_PORT}}"
docker compose -f docker-compose.yml -f compose.proxy.yaml up -d --build
