package web

import (
	"io/fs"
	"testing"
)

func TestEmbeddedFS_ContainsIndexHTML(t *testing.T) {
	info, err := fs.Stat(FS, "dist/index.html")
	if err != nil {
		t.Fatalf("stat dist/index.html: %v", err)
	}
	if info.IsDir() {
		t.Fatal("dist/index.html should be a file")
	}
}
