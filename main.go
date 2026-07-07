package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/sspeaks/large-video-streamer/internal/auth"
	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/hls"
	"github.com/sspeaks/large-video-streamer/internal/labels"
	"github.com/sspeaks/large-video-streamer/internal/segment"
	"github.com/sspeaks/large-video-streamer/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if cfg.NoAuth {
		log.Printf("WARNING: authentication DISABLED (VIDSTREAMER_DEV_NOAUTH) — do NOT expose this server to the internet")
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
		shows, err := hlsSrv.ListShows()
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
	// Public favicon so every page (including /login) gets a 200 instead of a
	// noisy 404, without embedding a <link> in each page's <head>.
	mux.HandleFunc("GET /favicon.ico", handleFavicon)
	if cfg.SegmentOnStart {
		go func() {
			if err := segment.SegmentAll(cfg); err != nil {
				log.Printf("segment-on-start: %v", err)
			}
		}()
	}
	log.Printf("vid-streamer listening on %s (videoDir=%s hlsDir=%s)", cfg.ListenAddr, cfg.VideoDir, cfg.HLSDir)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}

// faviconSVG is a small inline app icon (dark rounded tile + play glyph) served
// at /favicon.ico so browsers stop logging a 404 on every page load.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="7" fill="#0b1020"/><path d="M13 10l9 6-9 6z" fill="#8fb3ff"/></svg>`

func handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.WriteString(w, faviconSVG)
}
