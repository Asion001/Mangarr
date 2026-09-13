VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X github.com/Asion001/mangarr/internal/version.Version=$(VERSION) -X github.com/Asion001/mangarr/internal/version.Commit=$(COMMIT)
NODE_IMAGE ?= node:24-alpine

.PHONY: build build-upscaler run test test-pg test-integration vet web web-types lint docker docker-upscaler clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/mangarr ./cmd/mangarr

build-upscaler:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/mangarr-upscaler ./cmd/mangarr-upscaler

run: build
	MANGARR_DATA_DIR=./config ./bin/mangarr

test:
	go test ./...

# Runs the database-backed tests against a throwaway Postgres as well.
test-pg:
	MANGARR_TEST_POSTGRES=$${MANGARR_TEST_POSTGRES:-postgres://postgres:pg@localhost:55432/postgres?sslmode=disable} go test ./...

test-integration:
	go test -tags integration -timeout 20m ./...

vet:
	go vet ./...

# Build the web UI. Uses local npm when available, otherwise a Node container.
web:
	@if command -v npm >/dev/null 2>&1; then cd web && npm ci && npm run build; \
	else docker run --rm -v "$(CURDIR)/web:/app" -w /app $(NODE_IMAGE) sh -c "npm ci && npm run build"; fi

# Regenerate web/src/api/schema.d.ts from the server's OpenAPI document.
web-types: build
	./bin/mangarr openapi > web/openapi.json
	@if command -v npx >/dev/null 2>&1; then cd web && npx openapi-typescript openapi.json -o src/api/schema.d.ts; \
	else docker run --rm -v "$(CURDIR)/web:/app" -w /app $(NODE_IMAGE) npx openapi-typescript openapi.json -o src/api/schema.d.ts; fi

docker:
	docker build -f docker/Dockerfile -t ghcr.io/asion001/mangarr:dev --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) .

docker-upscaler:
	docker build -f docker/Dockerfile.upscaler -t ghcr.io/asion001/mangarr-upscaler:dev .

clean:
	rm -rf bin web/dist/assets web/dist/index.html
