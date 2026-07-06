package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/sspeaks/large-video-streamer/internal/auth"
	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/hls"
	"github.com/sspeaks/large-video-streamer/internal/labels"
	"github.com/sspeaks/large-video-streamer/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	a := auth.New(cfg)
	a.RegisterRoutes(mux)
	hlsSrv := hls.New(cfg)
	store := labels.New(cfg)
	mux.Handle("/static/", web.Handler())
	mux.Handle("/hls/", a.RequireMedia(hlsSrv.Handler()))
	store.RegisterRoutes(mux, a)
	// Gated JSON list of available shows for the index page (401 when unauthenticated).
	mux.Handle("GET /api/shows", a.RequireMedia(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shows, err := hlsSrv.List()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if shows == nil {
			shows = []hls.Show{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(shows)
	})))
	mux.Handle("GET /player", a.RequirePage(web.Player()))
	mux.Handle("GET /{$}", a.RequirePage(web.Index()))
	log.Printf("vid-streamer listening on %s (videoDir=%s hlsDir=%s)", cfg.ListenAddr, cfg.VideoDir, cfg.HLSDir)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
