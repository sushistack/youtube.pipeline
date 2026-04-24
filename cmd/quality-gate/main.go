// cmd/quality-gate is the CI entry point for Golden and Shadow eval quality
// gates. It delegates all quality math to internal/critic/eval and owns only
// CI orchestration: threshold enforcement, exit codes, and $GITHUB_STEP_SUMMARY
// rendering. Story 10.4.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	projectRoot := flag.String("project-root", ".", "path to the project root directory")
	gate := flag.String("gate", "both", "which gate to run: golden, shadow, or both")
	flag.Parse()

	if *gate != "golden" && *gate != "shadow" && *gate != "both" {
		fmt.Fprintf(os.Stderr, "error: unknown --gate value %q (must be golden, shadow, or both)\n", *gate)
		os.Exit(1)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	ev := fixtureExpectationEvaluator{}

	var hardFail bool

	if *gate == "golden" || *gate == "both" {
		result := runGoldenGate(ctx, *projectRoot, ev, now)
		appendStepSummary(result.summary())
		if result.Err != nil || !result.Pass {
			hardFail = true
		}
	}

	if *gate == "shadow" || *gate == "both" {
		src := nullShadowSource{}
		result := runShadowGate(ctx, *projectRoot, src, ev, now)
		appendStepSummary(result.summary())
		if result.Err != nil || result.HardFail {
			hardFail = true
		}
	}

	if hardFail {
		os.Exit(1)
	}
}

// appendStepSummary writes text to $GITHUB_STEP_SUMMARY when set, otherwise
// to stdout. Failures to open the summary file are non-fatal and fall back to
// stdout so CI output is never silently swallowed.
func appendStepSummary(text string) {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		fmt.Print(text)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("warning: open GITHUB_STEP_SUMMARY %s: %v", path, err)
		fmt.Print(text)
		return
	}
	defer f.Close()
	if _, err := fmt.Fprint(f, text); err != nil {
		log.Printf("warning: write GITHUB_STEP_SUMMARY: %v", err)
		fmt.Print(text)
	}
}
