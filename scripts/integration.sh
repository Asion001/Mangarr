#!/usr/bin/env bash
# Starts real services in Docker and runs the integration tests against them:
#   - Suwayomi (pinned) + Keiyoushi MangaDex extension (needs internet)
#   - mangarr-upscaler (built locally, lavapipe CPU Vulkan, amd64)
#   - Komga (claimed with a throwaway admin)
#
#   scripts/integration.sh            # all
#   SKIP_UPSCALER=1 scripts/integration.sh
set -euo pipefail
cd "$(dirname "$0")/.."

SUWAYOMI_IMAGE=ghcr.io/suwayomi/suwayomi-server:v2.3.2243
net=mangarr-it
lib=$(mktemp -d)
cleanup() { docker rm -f mangarr-it-suwayomi mangarr-it-upscaler mangarr-it-komga >/dev/null 2>&1 || true; rm -rf "$lib"; }
trap cleanup EXIT
cleanup

docker run -d --name mangarr-it-suwayomi -p 14567:4567 -e WEB_UI_ENABLED=false -e KCEF_ENABLED=false "$SUWAYOMI_IMAGE" >/dev/null
export MANGARR_IT_SUWAYOMI=http://localhost:14567

if [ -z "${SKIP_UPSCALER:-}" ]; then
  docker build --platform linux/amd64 -q -f docker/Dockerfile.upscaler -t mangarr-upscaler:it . >/dev/null
  docker run -d --platform linux/amd64 --name mangarr-it-upscaler -p 18788:8788 -e UPSCALER_TOKEN=it mangarr-upscaler:it >/dev/null
  export MANGARR_IT_UPSCALER=http://localhost:18788 MANGARR_IT_UPSCALER_TOKEN=it
fi

docker run -d --name mangarr-it-komga -p 25601:25600 -v "$lib":/data/manga:ro gotson/komga:latest >/dev/null
echo "waiting for services…"
for i in $(seq 1 90); do
  curl -fsS localhost:14567/api/v1/settings/about >/dev/null 2>&1 && curl -fsS localhost:25601/api/v1/claim >/dev/null 2>&1 && break
  sleep 2
done
curl -fsS -X POST localhost:25601/api/v1/claim -H 'X-Komga-Email: it@mangarr.test' -H 'X-Komga-Password: it-password' >/dev/null
key=$(curl -fsS -u it@mangarr.test:it-password -X POST localhost:25601/api/v2/users/me/api-keys -H 'Content-Type: application/json' -d '{"comment":"it"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["key"])')
curl -fsS -H "X-API-Key: $key" -X POST localhost:25601/api/v1/libraries -H 'Content-Type: application/json' -d '{"name":"Manga","root":"/data/manga"}' >/dev/null
export MANGARR_IT_KOMGA=http://localhost:25601 MANGARR_IT_KOMGA_KEY=$key MANGARR_IT_KOMGA_LOCAL=$lib

go test -tags integration -count=1 -v -timeout 30m ./internal/integration/
