package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"

	"github.com/sspeaks/large-video-streamer/internal/auth"
	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/hls"
	"github.com/sspeaks/large-video-streamer/internal/labels"
	"github.com/sspeaks/large-video-streamer/internal/segment"
	"github.com/sspeaks/large-video-streamer/internal/share"
	dbstore "github.com/sspeaks/large-video-streamer/internal/store"
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
	if cfg.UseFlatFileState {
		log.Printf("WARNING: using legacy flat-file state stores (VIDSTREAMER_FLAT_FILE_STATE); SQLite is disabled")
	}
	shareStore, labelStore, closeState, err := openStateStores(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := closeState(); err != nil {
			log.Printf("close state stores: %v", err)
		}
	}()

	mux := http.NewServeMux()
	a := auth.New(cfg)
	a.RegisterRoutes(mux)
	hlsSrv := hls.New(cfg)
	labelSrv := labels.NewServer(cfg, labelStore)
	shareSrv := share.NewWithStores(cfg, shareStore, labelStore)
	mux.Handle("/static/", web.Handler())
	mux.Handle("/hls/", a.RequireMedia(hlsSrv.Handler()))
	labelSrv.RegisterRoutes(mux, a)
	// Share routes: POST /shares and /admin/shares are owner-gated; the /s/
	// recipient routes are intentionally NOT wrapped by RequireMedia/RequirePage
	// (the device cookie is the credential).
	shareSrv.RegisterRoutes(mux, a)
	// Gated JSON list of available shows for the index page (401 when unauthenticated).
	mux.Handle("GET /api/shows", a.RequireMedia(libraryShowsHandler(hlsSrv, labelStore)))
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

func openStateStores(ctx context.Context, cfg config.Config) (share.ShareStore, labels.LabelStore, func() error, error) {
	if cfg.UseFlatFileState {
		return share.NewStore(filepath.Join(cfg.StateDir, "shares.json")), labels.New(cfg), noopClose, nil
	}

	db, err := dbstore.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, nil, nil, err
	}
	closeDB := func() error { return db.Close() }

	if err := dbstore.ImportLegacyState(ctx, db, cfg.StateDir); err != nil {
		_ = closeDB()
		return nil, nil, nil, err
	}
	shareStore, err := dbstore.NewShareStore(ctx, db)
	if err != nil {
		_ = closeDB()
		return nil, nil, nil, err
	}
	return shareStore, dbstore.NewLabelStore(db), closeDB, nil
}

func noopClose() error { return nil }

type showLister interface {
	ListShows() ([]hls.Show, error)
}

type libraryShow struct {
	Name           string `json:"name"`
	Playlist       string `json:"playlist,omitempty"`
	Status         string `json:"status,omitempty"`
	PendingReviews int    `json:"pendingReviews"`
}

func libraryShowsHandler(shows showLister, labelStore labels.LabelStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items, err := loadLibraryShows(shows, labelStore)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if items == nil {
			items = []libraryShow{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(items)
	})
}

func loadLibraryShows(shows showLister, labelStore labels.LabelStore) ([]libraryShow, error) {
	available, err := shows.ListShows()
	if err != nil {
		return nil, fmt.Errorf("list shows: %w", err)
	}

	items := make([]libraryShow, 0, len(available))
	for _, show := range available {
		labelDoc, err := labelStore.Load(show.Name)
		if err != nil {
			return nil, fmt.Errorf("load labels for %q: %w", show.Name, err)
		}
		items = append(items, libraryShow{
			Name:           show.Name,
			Playlist:       show.Playlist,
			Status:         show.Status,
			PendingReviews: labels.PendingReviewCount(labelDoc.Candidates),
		})
	}
	return items, nil
}

// faviconSVG is a small inline app icon (dark rounded tile + play glyph) served
// at /favicon.ico so browsers stop logging a 404 on every page load.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="7" fill="#0b1020"/><path d="M13 10l9 6-9 6z" fill="#8fb3ff"/></svg>`

func handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.WriteString(w, faviconSVG)
}
