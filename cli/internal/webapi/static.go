package webapi

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The production build of ../web is copied here by Vite. A committed
// placeholder keeps "go build" usable even before the frontend is rebuilt; the
// serve command reports a clear error if the real bundle is missing.
//
//go:embed all:dist
var distFS embed.FS

func newStaticHandler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		if _, err := fs.Stat(sub, name); err != nil {
			// Unknown extensionless paths are SPA routes, not missing files.
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			files.ServeHTTP(w, clone)
			return
		}
		files.ServeHTTP(w, r)
	}), nil
}
