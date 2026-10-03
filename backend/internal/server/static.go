package server

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Go's built-in list of file types doesn't know web app manifests, and would
// send site.webmanifest as plain text.
func init() {
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// staticHandler serves the built React app from dir.
//
// React Router handles URLs like /workloads/pods in the browser, but if the
// user refreshes that page the request reaches us. There is no file called
// "workloads/pods", so we send index.html and let React take over.
func staticHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// filepath.Clean on a path starting with "/" removes any "../" tricks.
		path := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))

		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			// Files in assets/ have a content hash in their name, so they never
			// change and the browser can cache them for a long time.
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}

		// index.html must not be cached, so a new deploy is picked up right away.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}
