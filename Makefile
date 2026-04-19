.PHONY: build web-build go-build test test-go test-web test-e2e dev clean lint-layers check-fr-coverage ci

build: go-build

web-build:
	cd web && npm ci && npm run build
	@touch internal/web/dist/.gitkeep

go-build: web-build
	CGO_ENABLED=0 go build -o bin/pipeline ./cmd/pipeline

test: test-go test-web

test-go:
	CGO_ENABLED=0 go test ./cmd/... ./internal/... ./migrations/... -count=1 -timeout=120s

test-web:
	cd web && npx vitest run

test-e2e:
	cd e2e && npx playwright test

dev:
	@cd web && npm run dev & VITE_PID=$$!; \
	trap "kill $$VITE_PID 2>/dev/null" EXIT; \
	air -- serve --dev

lint-layers:
	CGO_ENABLED=0 go run ./scripts/lintlayers/

check-fr-coverage:
	CGO_ENABLED=0 go run ./scripts/frcoverage/

ci: test lint-layers check-fr-coverage build

clean:
	rm -rf bin/
	find internal/web/dist/ -not -name '.gitkeep' -not -path internal/web/dist/ -delete 2>/dev/null || true
