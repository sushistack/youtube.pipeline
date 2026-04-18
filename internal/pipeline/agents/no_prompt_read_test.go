package agents

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestAgents_NoPromptFileReferences(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := filepath.Join(testutil.ProjectRoot(t), "internal", "pipeline", "agents")
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, root, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value := strings.Trim(lit.Value, `"`)
				if value == "docs/prompts/scenario/01_research.md" || value == "docs/prompts/scenario/02_structure.md" {
					t.Fatalf("unexpected prompt reference %q", value)
				}
				return true
			})
		}
	}
}
