package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var frontendFS embed.FS

// FrontendHandler serves the embedded Vue3 SPA.
// It serves static assets under /web/ and falls back to index.html for SPA routing.
func FrontendHandler() http.Handler {
	// Get the dist subdirectory
	distFS, err := fs.Sub(frontendFS, "dist")
	if err != nil {
		panic("failed to get dist subdirectory: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip /web/ prefix
		path := strings.TrimPrefix(r.URL.Path, "/web")
		if path == "" {
			path = "/"
		}

		// Try to serve the file directly
		// We need to check if the file exists
		f, err := distFS.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			f.Close()
			// File exists, serve it
			r2 := *r
			r2.URL.Path = path
			fileServer.ServeHTTP(w, &r2)
			return
		}

		// File doesn't exist, serve index.html for SPA routing
		r2 := *r
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, &r2)
	})
}
