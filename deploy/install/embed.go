// SPDX-License-Identifier: AGPL-3.0-or-later

// Package install serves the one-line install scripts from the hub.
package install

import (
	"embed"
	"net/http"
)

//go:embed install.sh install.ps1
var scripts embed.FS

// Handler returns an HTTP handler for GET /install.sh and GET /install.ps1.
// Both scripts are public.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /install.sh", serve("install.sh", "text/x-shellscript"))
	mux.HandleFunc("GET /install.ps1", serve("install.ps1", "text/plain"))
	return mux
}

func serve(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		b, err := scripts.ReadFile(name)
		if err != nil {
			http.Error(w, "script unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(b)
	}
}
