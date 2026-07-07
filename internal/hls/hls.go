package hls

import (
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

// Server serves generated HLS playlists and segments.
type Server struct {
	cfg config.Config
}

// Show describes a video. A "ready" show has a playable HLS playlist; a
// "processing" show is a source video still being segmented (no playlist yet).
type Show struct {
	Name     string `json:"name"`
	Playlist string `json:"playlist,omitempty"`
	Status   string `json:"status,omitempty"`
}

// New returns an HLS server using cfg.HLSDir as its media root.
func New(cfg config.Config) *Server {
	return &Server{cfg: cfg}
}

// Handler returns an HLS HTTP handler that serves whitelisted media files under cfg.HLSDir.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel, ok := cleanHLSPath(r)
		if !ok {
			http.NotFound(w, r)
			return
		}

		contentType, ok := mediaContentType(path.Ext(rel))
		if !ok {
			http.NotFound(w, r)
			return
		}

		name := filepath.Join(s.cfg.HLSDir, filepath.FromSlash(rel))
		file, err := os.Open(name)
		if err != nil {
			// A show with no saved chapters yet has no chapters.vtt on disk.
			// Serve an empty (but valid) cue list so the native <track> never
			// 404s, instead of surfacing a console error to the viewer.
			if path.Base(rel) == "chapters.vtt" {
				setMediaHeaders(w.Header(), contentType)
				_, _ = w.Write([]byte("WEBVTT\n\n"))
				return
			}
			http.NotFound(w, r)
			return
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		setMediaHeaders(w.Header(), contentType)
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	})
}

// List returns generated shows with an immediate playlist.m3u8 under cfg.HLSDir.
func (s *Server) List() ([]Show, error) {
	entries, err := os.ReadDir(s.cfg.HLSDir)
	if err != nil {
		return nil, err
	}

	shows := make([]Show, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		playlist := filepath.Join(s.cfg.HLSDir, name, "playlist.m3u8")
		info, err := os.Stat(playlist)
		if err != nil || info.IsDir() {
			continue
		}
		shows = append(shows, Show{
			Name:     name,
			Playlist: "/hls/" + url.PathEscape(name) + "/playlist.m3u8",
		})
	}

	sort.Slice(shows, func(i, j int) bool {
		return shows[i].Name < shows[j].Name
	})
	return shows, nil
}

// ListShows returns every source video: those already segmented (Status
// "ready", with a Playlist) and those still being segmented (Status
// "processing", no Playlist). This lets the UI show a video that exists on disk
// but whose HLS output isn't ready yet, instead of appearing empty.
func (s *Server) ListShows() ([]Show, error) {
	ready, err := s.List()
	if err != nil {
		return nil, err
	}

	readyNames := make(map[string]bool, len(ready))
	shows := make([]Show, 0, len(ready))
	for _, show := range ready {
		show.Status = "ready"
		readyNames[show.Name] = true
		shows = append(shows, show)
	}

	// Source videos without a ready playlist are still processing.
	if entries, err := os.ReadDir(s.cfg.VideoDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := filepath.Ext(entry.Name())
			if !strings.EqualFold(ext, ".mkv") {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ext)
			if readyNames[name] {
				continue
			}
			shows = append(shows, Show{Name: name, Status: "processing"})
		}
	}

	sort.Slice(shows, func(i, j int) bool {
		return shows[i].Name < shows[j].Name
	})
	return shows, nil
}

func cleanHLSPath(r *http.Request) (string, bool) {
	const prefix = "/hls/"
	if !strings.HasPrefix(r.URL.EscapedPath(), prefix) {
		return "", false
	}
	escapedRel := strings.TrimPrefix(r.URL.EscapedPath(), prefix)
	if escapedRel == "" {
		return "", false
	}
	rel, err := url.PathUnescape(escapedRel)
	if err != nil || hasTraversal(rel) {
		return "", false
	}
	clean := strings.TrimPrefix(path.Clean("/"+rel), "/")
	if clean == "." || clean == "" || hasTraversal(clean) {
		return "", false
	}
	return clean, true
}

func hasTraversal(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func mediaContentType(ext string) (string, bool) {
	switch ext {
	case ".m3u8":
		return "application/vnd.apple.mpegurl", true
	case ".ts":
		return "video/mp2t", true
	case ".m4s":
		return "video/iso.segment", true
	case ".vtt":
		return "text/vtt", true
	case ".mp4":
		return "video/mp4", true
	case ".key":
		return "application/octet-stream", true
	default:
		return "", false
	}
}

func setMediaHeaders(h http.Header, contentType string) {
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	h.Set("Pragma", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Type", contentType)
}
