// check-layer-imports.go validates import direction rules for internal/ packages.
// Violations cause CI failure (NFR-M4).
//
// Usage: go run scripts/check-layer-imports.go
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const modulePrefix = "github.com/sushistack/youtube.pipeline/"

// allowedImports defines which internal/ packages each layer may import.
// An empty slice means no internal/ imports are allowed.
var allowedImports = map[string][]string{
	"internal/domain":    {},
	"internal/clock":     {},
	"internal/db":        {"internal/domain"},
	"internal/llmclient": {"internal/domain", "internal/clock"},
	"internal/pipeline":  {"internal/domain", "internal/db", "internal/llmclient", "internal/clock", "internal/pipeline/agents"},
	// Agents are pure functions (architecture.md:1731-1734); LLM calls
	// flow in via domain.TextGenerator closures, not direct llmclient
	// imports. They may depend only on domain types and the clock.
	"internal/pipeline/agents": {"internal/domain", "internal/clock"},
	"internal/service":         {"internal/domain", "internal/db", "internal/pipeline", "internal/clock"},
	"internal/api":             {"internal/domain", "internal/db", "internal/service", "internal/pipeline", "internal/clock", "internal/web"},
	"internal/config":          {"internal/domain"},
	"internal/hitl":            {"internal/domain"},
	"internal/testutil":        {"internal/domain", "internal/db"},
	"internal/web":             {},
	// Golden eval set governance (Story 4.1). Production code may only import
	// domain and clock; db/service/api are explicitly excluded.
	"internal/critic":      {"internal/domain", "internal/clock"},
	"internal/critic/eval": {"internal/domain", "internal/clock"},
	// SCP-Explained quality-uplift packages (next-session-enhance-prompts).
	// All three are additive — no live agent imports them in this cycle.
	"internal/style":            {},
	"internal/contract":         {"internal/domain"},
	"internal/critic/rubricv2":  {"internal/domain", "internal/contract", "internal/style"},
}

// nestedTrackedPackages lists internal/ subpackages that have their
// own layer-import rules distinct from their parent. Checked with
// longest-match semantics BEFORE the generic two-segment collapse.
var nestedTrackedPackages = []string{
	"internal/pipeline/agents",
	"internal/critic/eval",
	"internal/critic/rubricv2",
}

func main() {
	violations := checkImports("internal")
	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Println(v)
		}
		fmt.Printf("\n%d layer-import violation(s) found\n", len(violations))
		os.Exit(1)
	}
	fmt.Println("layer-import lint: OK")
}

func checkImports(root string) []string {
	var violations []string
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendor and testdata directories
			if info.Name() == "vendor" || info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}

		// Determine which top-level internal package this file belongs to
		filePkg := resolveTopLevelPackage(path)
		if filePkg == "" {
			return nil
		}

		allowed, known := allowedImports[filePkg]
		if !known {
			fmt.Fprintf(os.Stderr, "WARN: package %s has no entry in allowedImports — add it to enforce layer rules\n", filePkg)
			return nil
		}

		isTestFile := strings.HasSuffix(path, "_test.go")

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(importPath, modulePrefix+"internal/") {
				continue
			}
			internalPath := strings.TrimPrefix(importPath, modulePrefix)
			internalTop := resolveTopLevelFromImport(internalPath)

			// Skip self-imports (e.g. internal/llmclient importing internal/llmclient/dashscope)
			if internalTop == filePkg {
				continue
			}

			// Test files may additionally import internal/testutil (test infrastructure)
			if isTestFile && internalTop == "internal/testutil" {
				continue
			}

			if !isAllowed(internalTop, allowed) {
				pos := fset.Position(imp.Pos())
				violations = append(violations, fmt.Sprintf(
					"VIOLATION: %s:%d imports %s (not allowed for %s)",
					pos.Filename, pos.Line, internalTop, filePkg,
				))
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error walking directory: %v\n", err)
		os.Exit(2)
	}
	return violations
}

// resolveTopLevelPackage maps a file path to its top-level internal/ package.
// Nested tracked packages (e.g. "internal/pipeline/agents") take
// precedence over the generic two-segment collapse so their stricter
// rules apply.
// e.g. "internal/llmclient/dashscope/client.go" -> "internal/llmclient"
//      "internal/pipeline/agents/agent.go"      -> "internal/pipeline/agents"
//      "internal/pipeline/engine.go"            -> "internal/pipeline"
func resolveTopLevelPackage(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "internal/") {
		return ""
	}
	if nested := matchNestedTracked(path); nested != "" {
		return nested
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ""
	}
	return "internal/" + parts[1]
}

// resolveTopLevelFromImport maps an internal import path to its top-level package.
// Nested tracked packages are checked first with longest-prefix semantics.
// e.g. "internal/llmclient/dashscope"    -> "internal/llmclient"
//      "internal/pipeline/agents"        -> "internal/pipeline/agents"
//      "internal/pipeline/agents/foo"    -> "internal/pipeline/agents"
func resolveTopLevelFromImport(importPath string) string {
	if nested := matchNestedTracked(importPath); nested != "" {
		return nested
	}
	parts := strings.Split(importPath, "/")
	if len(parts) < 2 {
		return importPath
	}
	return parts[0] + "/" + parts[1]
}

// matchNestedTracked returns the longest nestedTrackedPackages entry that
// is a prefix of s, requiring either an exact match or a '/' boundary.
// Returns "" if no entry matches.
func matchNestedTracked(s string) string {
	best := ""
	for _, n := range nestedTrackedPackages {
		if s == n || strings.HasPrefix(s, n+"/") {
			if len(n) > len(best) {
				best = n
			}
		}
	}
	return best
}

func isAllowed(pkg string, allowed []string) bool {
	for _, a := range allowed {
		if pkg == a {
			return true
		}
	}
	return false
}

// ParseImports is exported for testing.
func ParseImports(src string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var imports []string
	for _, imp := range f.Imports {
		imports = append(imports, strings.Trim(imp.Path.Value, `"`))
	}
	return imports, nil
}

// CheckImportsExported exposes checkImports for tests.
func CheckImportsExported(root string) []string {
	return checkImports(root)
}

// IsAllowedExported exposes isAllowed for tests.
func IsAllowedExported(pkg string, allowed []string) bool {
	return isAllowed(pkg, allowed)
}

// ResolveTopLevelPackageExported exposes resolveTopLevelPackage for tests.
func ResolveTopLevelPackageExported(path string) string {
	return resolveTopLevelPackage(path)
}

// ResolveTopLevelFromImportExported exposes resolveTopLevelFromImport for tests.
func ResolveTopLevelFromImportExported(path string) string {
	return resolveTopLevelFromImport(path)
}

// AllowedImportsMap returns the allowedImports map for tests.
func AllowedImportsMap() map[string][]string {
	return allowedImports
}

// Violations returns all import violations for a given file's package and its AST imports.
func Violations(filePkg string, imports []*ast.ImportSpec) []string {
	allowed, known := allowedImports[filePkg]
	if !known {
		return nil
	}
	var result []string
	for _, imp := range imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if !strings.HasPrefix(importPath, modulePrefix+"internal/") {
			continue
		}
		internalPath := strings.TrimPrefix(importPath, modulePrefix)
		internalTop := resolveTopLevelFromImport(internalPath)
		if !isAllowed(internalTop, allowed) {
			result = append(result, fmt.Sprintf("%s imports %s (not allowed for %s)", filePkg, internalTop, filePkg))
		}
	}
	return result
}
