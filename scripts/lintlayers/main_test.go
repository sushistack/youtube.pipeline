package main

import (
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
	expected := []string{"internal/domain", "internal/db", "internal/llmclient", "internal/clock"}
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
		"internal/llmclient", "internal/pipeline", "internal/service",
		"internal/api", "internal/config", "internal/hitl",
		"internal/testutil", "internal/web",
	}
	for _, pkg := range requiredPackages {
		if _, ok := m[pkg]; !ok {
			t.Errorf("missing package in allowedImports: %s", pkg)
		}
	}
}
