// SPDX-License-Identifier: SUL-1.0

// Package web embeds the built UI (web/dist).
package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the UI: files from dist, and index.html for any other GET
// path that is not under /api/, so client-side routes work on reload. When
// the UI was not built (only .keep exists) it answers 503 with a short text
// telling the developer to run `pnpm -C web build`.
func Handler() http.Handler {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(fmt.Errorf("opening embedded web/dist: %w", err))
	}
	// Read once: index.html is tiny and re-reads on every request would be waste.
	index, indexErr := fs.ReadFile(files, "index.html")
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if indexErr != nil {
			http.Error(w, "web UI is not built; run `pnpm -C web build`", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if st, statErr := fs.Stat(files, path); statErr == nil && !st.IsDir() {
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write(index); err != nil {
			// The client is gone; the response is already committed.
			return
		}
	})
}
