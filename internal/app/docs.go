package app

import (
	"embed"
	"net/http"
)

//go:embed apidocs/*
var documentation embed.FS

func documentationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusPermanentRedirect)
	})
	assets := map[string]struct{ file, contentType string }{
		"/docs/":                     {"index.html", "text/html; charset=utf-8"},
		"/docs/init.js":              {"init.js", "application/javascript"},
		"/docs/swagger-ui-bundle.js": {"swagger-ui-bundle.js", "application/javascript"},
		"/docs/swagger-ui.css":       {"swagger-ui.css", "text/css; charset=utf-8"},
		"/openapi.json":              {"openapi.json", "application/json"},
	}
	for path, asset := range assets {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != path {
				http.NotFound(w, r)
				return
			}
			body, err := documentation.ReadFile("apidocs/" + asset.file)
			if err != nil {
				http.Error(w, "Documentation unavailable", 503)
				return
			}
			w.Header().Set("Content-Type", asset.contentType)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
			_, _ = w.Write(body)
		})
	}
}
