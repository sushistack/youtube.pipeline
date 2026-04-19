# Web App Test Surface

## Verification Commands

- `npm run lint` validates the keyboard shortcut invariance rule through the normal ESLint path.
- `npm run test:unit` runs the Vitest suite for component, contract, and style checks.
- `npm run test:e2e` runs the single Chromium Playwright smoke spec.
- `npm run test` runs the full Story 6.5 verification sequence in order.

## Playwright Server Path

`npm run serve:e2e` is the production-like local boot path used by Playwright. It:

1. builds the SPA with `npm run build`
2. starts the Go server with `go run ./cmd/pipeline serve`
3. serves the embedded SPA contract on `http://127.0.0.1:4173`

The command sets isolated `DB_PATH`, `OUTPUT_DIR`, and `DATA_DIR` values under `.tmp/playwright/` so the smoke test does not depend on a developer's personal `~/.youtube-pipeline` state.
