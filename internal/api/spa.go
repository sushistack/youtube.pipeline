package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler returns an http.Handler that serves a single-page application
// from fsys (typically an embed.FS rooted at "dist"). Requests for files that
// exist are served as-is; everything else falls through to index.html for
// client-side routing.
//
// Path handling uses io/fs.Sub and path.Clean to defend against directory
// traversal regardless of the underlying fs.FS implementation.
func spaHandler(fsys fs.FS) http.Handler {
	sub, err := fs.Sub(fsys, "dist")
	if err != nil {
		// fsys doesn't contain dist/ — fall back to the raw fs.
		sub = fsys
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := r.URL.Path
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
		}
		cleaned := path.Clean(upath)
		// fs.FS expects paths without a leading slash.
		lookup := strings.TrimPrefix(cleaned, "/")
		if lookup == "" || lookup == "." {
			lookup = "index.html"
		}

		if f, err := sub.Open(lookup); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for client-side routing.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
