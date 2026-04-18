package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTopLevelPackage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"internal/domain/types.go", "internal/domain"},
		{"internal/llmclient/dashscope/client.go", "internal/llmclient"},
		{"internal/db/sqlite.go", "internal/db"},
		{"internal/pipeline/e2e_test.go", "internal/pipeline"},
		{"cmd/pipeline/main.go", ""},
		{"main.go", ""},
	}
	for _, tt := range tests {
		got := ResolveTopLevelPackageExported(tt.path)
		if got != tt.want {
			t.Errorf("resolveTopLevelPackage(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestResolveTopLevelFromImport(t *testing.T) {
	tests := []struct {
		importPath string
		want       string
	}{
		{"internal/domain", "internal/domain"},
		{"internal/llmclient/dashscope", "internal/llmclient"},
		{"internal/db", "internal/db"},
		{"internal/clock", "internal/clock"},
	}
	for _, tt := range tests {
		got := ResolveTopLevelFromImportExported(tt.importPath)
		if got != tt.want {
			t.Errorf("resolveTopLevelFromImport(%q) = %q, want %q", tt.importPath, got, tt.want)
		}
	}
}

func TestIsAllowed(t *testing.T) {
	allowed := []string{"internal/domain", "internal/db"}
	if !IsAllowedExported("internal/domain", allowed) {
		t.Error("expected internal/domain to be allowed")
	}
	if !IsAllowedExported("internal/db", allowed) {
		t.Error("expected internal/db to be allowed")
	}
	if IsAllowedExported("internal/service", allowed) {
		t.Error("expected internal/service to NOT be allowed")
	}
}

func TestAllowedImportsMap_DomainImportsNothing(t *testing.T) {
	m := AllowedImportsMap()
	if len(m["internal/domain"]) != 0 {
		t.Errorf("domain should import nothing from internal/, got %v", m["internal/domain"])
	}
	if len(m["internal/clock"]) != 0 {
		t.Errorf("clock should import nothing from internal/, got %v", m["internal/clock"])
	}
	if len(m["internal/web"]) != 0 {
		t.Errorf("web should import nothing from internal/, got %v", m["internal/web"])
	}
}

func TestAllowedImportsMap_PipelineAllowed(t *testing.T) {
	m := AllowedImportsMap()
	pipelineAllowed := m["internal/pipeline"]
	expected := []string{
		"internal/domain", "internal/db", "internal/llmclient",
		"internal/clock", "internal/pipeline/agents",
	}
	if len(pipelineAllowed) != len(expected) {
		t.Fatalf("pipeline allowed = %v, want %v", pipelineAllowed, expected)
	}
	for _, e := range expected {
		if !IsAllowedExported(e, pipelineAllowed) {
			t.Errorf("expected %s in pipeline allowed list", e)
		}
	}
}

func TestCheckImports_NoViolationsOnCleanCodebase(t *testing.T) {
	violations := CheckImportsExported("../../internal")
	if len(violations) > 0 {
		for _, v := range violations {
			t.Log(v)
		}
		t.Errorf("expected 0 violations on clean codebase, got %d", len(violations))
	}
}

func TestAllowedImportsMap_AllPackagesCovered(t *testing.T) {
	m := AllowedImportsMap()
	requiredPackages := []string{
		"internal/domain", "internal/clock", "internal/db",
		"internal/llmclient", "internal/pipeline", "internal/pipeline/agents",
		"internal/service", "internal/api", "internal/config", "internal/hitl",
		"internal/testutil", "internal/web",
	}
	for _, pkg := range requiredPackages {
		if _, ok := m[pkg]; !ok {
			t.Errorf("missing package in allowedImports: %s", pkg)
		}
	}
}

func TestResolveTopLevelPackage_NestedAgents(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"internal/pipeline/agents/agent.go", "internal/pipeline/agents"},
		{"internal/pipeline/agents/noop.go", "internal/pipeline/agents"},
		{"internal/pipeline/engine.go", "internal/pipeline"},
		{"internal/pipeline/phase_a.go", "internal/pipeline"},
		{"internal/llmclient/dashscope/client.go", "internal/llmclient"},
	}
	for _, tt := range tests {
		got := ResolveTopLevelPackageExported(tt.path)
		if got != tt.want {
			t.Errorf("resolveTopLevelPackage(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestResolveTopLevelFromImport_NestedAgents(t *testing.T) {
	tests := []struct {
		importPath string
		want       string
	}{
		{"internal/pipeline/agents", "internal/pipeline/agents"},
		{"internal/pipeline/agents/sub", "internal/pipeline/agents"},
		{"internal/pipeline", "internal/pipeline"},
		{"internal/llmclient/dashscope", "internal/llmclient"},
	}
	for _, tt := range tests {
		got := ResolveTopLevelFromImportExported(tt.importPath)
		if got != tt.want {
			t.Errorf("resolveTopLevelFromImport(%q) = %q, want %q", tt.importPath, got, tt.want)
		}
	}
}

func TestAllowedImports_Agents(t *testing.T) {
	m := AllowedImportsMap()
	got := m["internal/pipeline/agents"]
	expected := []string{"internal/domain", "internal/clock"}
	if len(got) != len(expected) {
		t.Fatalf("agents allowed = %v, want %v", got, expected)
	}
	for _, e := range expected {
		if !IsAllowedExported(e, got) {
			t.Errorf("expected %s in agents allowed list", e)
		}
	}
	// Explicitly forbidden: db, llmclient (must be injected via domain).
	for _, forbidden := range []string{"internal/db", "internal/llmclient", "internal/service"} {
		if IsAllowedExported(forbidden, got) {
			t.Errorf("expected %s to be FORBIDDEN for internal/pipeline/agents, but it was allowed", forbidden)
		}
	}
}

// TestAgents_ForbiddenImport_Negative proves the rule bites in practice:
// drop a fake Go file under internal/pipeline/agents/ that imports
// internal/llmclient, run checkImports, expect one violation.
func TestAgents_ForbiddenImport_Negative(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "internal", "pipeline", "agents")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	src := `package agents

import _ "github.com/sushistack/youtube.pipeline/internal/llmclient"
`
	if err := os.WriteFile(filepath.Join(pkgDir, "bad.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write bad.go: %v", err)
	}

	// Capture cwd up-front and fail the test if we cannot — a silent
	// empty-string fallback would corrupt subsequent tests if the
	// Cleanup ran Chdir("").
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	violations := CheckImportsExported("internal")
	// The temp root contains exactly one Go file (bad.go). Any additional
	// violation would indicate the fixture is leaking paths from the real
	// module — fail explicitly in that case.
	if len(violations) != 1 {
		t.Fatalf("expected exactly 1 violation in isolated tempdir, got %d: %v", len(violations), violations)
	}
	v := violations[0]
	if !strings.Contains(v, "internal/llmclient") {
		t.Errorf("violation should mention internal/llmclient: %s", v)
	}
	if !strings.Contains(v, "internal/pipeline/agents") {
		t.Errorf("violation should mention internal/pipeline/agents: %s", v)
	}
}
