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
	"internal/pipeline":  {"internal/domain", "internal/db", "internal/llmclient", "internal/clock"},
	"internal/service":   {"internal/domain", "internal/db", "internal/pipeline", "internal/clock"},
	"internal/api":       {"internal/domain", "internal/db", "internal/service", "internal/pipeline", "internal/clock", "internal/web"},
	"internal/config":    {"internal/domain"},
	"internal/hitl":      {"internal/domain"},
	"internal/testutil":  {"internal/domain", "internal/db"},
	"internal/web":       {},
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
// e.g. "internal/llmclient/dashscope/client.go" -> "internal/llmclient"
func resolveTopLevelPackage(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "internal/") {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ""
	}
	return "internal/" + parts[1]
}

// resolveTopLevelFromImport maps an internal import path to its top-level package.
// e.g. "internal/llmclient/dashscope" -> "internal/llmclient"
func resolveTopLevelFromImport(importPath string) string {
	parts := strings.Split(importPath, "/")
	if len(parts) < 2 {
		return importPath
	}
	return parts[0] + "/" + parts[1]
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
