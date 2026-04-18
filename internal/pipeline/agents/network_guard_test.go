package agents

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestAgents_PackageImports_NoNetPkgs(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := filepath.Join(testutil.ProjectRoot(t), "internal", "pipeline", "agents")
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, root, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				switch path {
				case "net", "net/http", "net/url":
					t.Fatalf("%s imports forbidden network package %q", filename, path)
				}
			}
		}
	}
}
