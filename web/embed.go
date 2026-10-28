// Package web embeds the built frontend (web/dist) and serves it as a single-page app.
package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the embedded build. Before `npm run build`, dist holds only a
// placeholder and every page is a 404.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("opening embedded dist: %w", err)
	}
	return SPA(sub), nil
}

// SPA serves files from fsys and answers every other path with index.html, so
// client-side routes survive a reload.
func SPA(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if info, err := fs.Stat(fsys, name); err == nil && !info.IsDir() && name != "index.html" {
			if strings.HasPrefix(name, "assets/") {
				// Vite fingerprints everything under assets/.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, fsys, "index.html")
	})
}
