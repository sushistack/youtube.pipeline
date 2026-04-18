// Package eval implements Critic eval infrastructure.
//
// Two distinct modes live here:
//
//   - Golden (Story 4.1): file-backed baseline governance. Fixtures are
//     version-controlled under testdata/golden/eval/; the manifest is the
//     durable record of pairs, last successful prompt hash, and last report.
//     Golden writes to disk on success.
//
//   - Shadow (Story 4.2): recent-run replay. Selects the most recent passed
//     runs from the live runs table, replays each persisted Phase A artifact
//     through the injected Evaluator, and reports verdict/score drift.
//     Shadow is intentionally ephemeral — it writes NO manifest, NO generated
//     files under testdata/, and ships NO CLI command in this story. CI
//     enforcement of either Golden or Shadow is owned by Story 10.4.
//
// The package consumes a narrow Evaluator interface; it must not import
// internal/db, internal/service, or internal/api. The SQLite-backed
// ShadowSource adapter lives here (internal/critic/eval/shadow_source.go)
// rather than under internal/db because putting it there would create an
// internal/db → internal/critic/eval import edge that cycles through
// internal/testutil → internal/db during testing.
package eval
