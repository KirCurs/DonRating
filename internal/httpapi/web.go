package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web
var frontendFiles embed.FS

// frontendHandler serves the tablet interface from the application binary.
func frontendHandler() http.Handler {
	files, err := fs.Sub(frontendFiles, "web")
	if err != nil {
		panic(err)
	}
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'; base-uri 'self'; object-src 'none'")
		server.ServeHTTP(w, r)
	})
}
