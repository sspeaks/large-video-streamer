package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// Assets contains embedded web templates and static files.
//
//go:embed assets
var Assets embed.FS

// Handler serves embedded files under assets/static at /static/.
func Handler() http.Handler {
	staticFS, err := fs.Sub(Assets, "assets/static")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not implemented", http.StatusNotImplemented)
		})
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
}

// Index returns the video index page handler.
func Index() http.Handler {
	return serveEmbeddedFile("assets/index.html")
}

// Player returns the player page handler.
func Player() http.Handler {
	return serveEmbeddedFile("assets/player.html")
}

func serveEmbeddedFile(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contents, err := Assets.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(contents)
	})
}
