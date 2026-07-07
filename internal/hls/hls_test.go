package hls

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

func TestHandlerServesWhitelistedMediaWithHeaders(t *testing.T) {
	root := buildTestHLSDir(t)
	srv := New(config.Config{HLSDir: root})

	tests := []struct {
		name        string
		path        string
		wantStatus  int
		wantType    string
		wantNoCache bool
	}{
		{
			name:        "playlist",
			path:        "/hls/show1/playlist.m3u8",
			wantStatus:  http.StatusOK,
			wantType:    "application/vnd.apple.mpegurl",
			wantNoCache: true,
		},
		{
			name:        "segment",
			path:        "/hls/show1/seg_0001.ts",
			wantStatus:  http.StatusOK,
			wantType:    "video/mp2t",
			wantNoCache: true,
		},
		{
			name:       "forbidden extension",
			path:       "/hls/show1/notes.txt",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "directory listing disabled",
			path:       "/hls/show1/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "traversal stays contained",
			path:       "/hls/../go.mod",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantType != "" && rec.Header().Get("Content-Type") != tt.wantType {
				t.Fatalf("Content-Type = %q, want %q", rec.Header().Get("Content-Type"), tt.wantType)
			}
			if tt.wantNoCache && rec.Header().Get("Cache-Control") != "no-store, no-cache, must-revalidate, private" {
				t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
			}
			if tt.wantNoCache && rec.Header().Get("Pragma") != "no-cache" {
				t.Fatalf("Pragma = %q", rec.Header().Get("Pragma"))
			}
			if tt.wantNoCache && rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q", rec.Header().Get("X-Content-Type-Options"))
			}
		})
	}
}

func TestListReturnsShowsWithPlaylistsSortedByName(t *testing.T) {
	root := buildTestHLSDir(t)
	if err := os.MkdirAll(filepath.Join(root, "aaa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "aaa", "playlist.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := New(config.Config{HLSDir: root})
	shows, err := srv.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	want := []Show{
		{Name: "aaa", Playlist: "/hls/aaa/playlist.m3u8"},
		{Name: "show1", Playlist: "/hls/show1/playlist.m3u8"},
	}
	if len(shows) != len(want) {
		t.Fatalf("len(shows) = %d, want %d: %#v", len(shows), len(want), shows)
	}
	for i := range want {
		if shows[i] != want[i] {
			t.Fatalf("shows[%d] = %#v, want %#v", i, shows[i], want[i])
		}
	}
}

func TestListShowsMarksReadyAndProcessing(t *testing.T) {
	hlsRoot := t.TempDir()
	videoDir := t.TempDir()

	// A ready show: HLS playlist already generated.
	if err := os.MkdirAll(filepath.Join(hlsRoot, "ready1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hlsRoot, "ready1", "playlist.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Source videos: ready1.mkv (already segmented) and pending1.mkv (not yet).
	for _, name := range []string{"ready1.mkv", "pending1.mkv"} {
		if err := os.WriteFile(filepath.Join(videoDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	srv := New(config.Config{HLSDir: hlsRoot, VideoDir: videoDir})
	shows, err := srv.ListShows()
	if err != nil {
		t.Fatalf("ListShows returned error: %v", err)
	}
	if len(shows) != 2 {
		t.Fatalf("len(shows) = %d, want 2: %#v", len(shows), shows)
	}

	byName := map[string]Show{}
	for _, show := range shows {
		byName[show.Name] = show
	}
	if r := byName["ready1"]; r.Status != "ready" || r.Playlist == "" {
		t.Fatalf("ready1 = %#v, want ready with playlist", r)
	}
	if p := byName["pending1"]; p.Status != "processing" || p.Playlist != "" {
		t.Fatalf("pending1 = %#v, want processing without playlist", p)
	}
}

func TestHandlerServesEmptyChaptersWhenMissing(t *testing.T) {
	root := buildTestHLSDir(t)
	srv := New(config.Config{HLSDir: root})

	// show1 has no chapters.vtt on disk (a show with no saved chapters yet).
	// It must return 200 with a valid empty cue list, not a 404, so the
	// player's native <track> never surfaces a console error.
	req := httptest.NewRequest(http.MethodGet, "/hls/show1/chapters.vtt", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("missing chapters.vtt status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/vtt" {
		t.Fatalf("Content-Type = %q, want text/vtt", ct)
	}
	if body := rec.Body.String(); body != "WEBVTT\n\n" {
		t.Fatalf("body = %q, want %q", body, "WEBVTT\n\n")
	}
}

func TestHandlerServesExistingChaptersFile(t *testing.T) {
	root := buildTestHLSDir(t)
	want := "WEBVTT\n\n1\n00:00:05.000 --> 00:00:10.000\ngroup-a\n\n"
	if err := os.WriteFile(filepath.Join(root, "show1", "chapters.vtt"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(config.Config{HLSDir: root})

	req := httptest.NewRequest(http.MethodGet, "/hls/show1/chapters.vtt", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("existing chapters.vtt status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func buildTestHLSDir(t *testing.T) string {
	t.Helper()
	root := "hls-test-root"
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("cleanup HLS dir: %v", err)
		}
	})
	if err := os.MkdirAll(filepath.Join(root, "show1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "show2"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(root, "show1", "playlist.m3u8"): "#EXTM3U\n",
		filepath.Join(root, "show1", "seg_0001.ts"):   "segment",
		filepath.Join(root, "show1", "notes.txt"):     "notes",
	}
	for name, contents := range files {
		if err := os.WriteFile(name, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBuildChapterPlaylistMidVideoChapter(t *testing.T) {
	root := buildChapterTestHLSDir(t, "show1")
	srv := New(config.Config{HLSDir: root})

	playlist, segments, startOffset, endOffset, total, err := srv.BuildChapterPlaylist("show1", 6, 18)
	if err != nil {
		t.Fatalf("BuildChapterPlaylist returned error: %v", err)
	}
	assertSegments(t, segments, []string{"seg_0001.ts", "seg_0002.ts"})
	if !closeFloat(startOffset, 0) {
		t.Fatalf("startOffset = %v, want 0", startOffset)
	}
	if !closeFloat(endOffset, 12) {
		t.Fatalf("endOffset = %v, want 12", endOffset)
	}
	if !closeFloat(total, 30) {
		t.Fatalf("total = %v, want 30", total)
	}

	body := string(playlist)
	for _, want := range []string{
		"#EXT-X-ENDLIST",
		"#EXT-X-MEDIA-SEQUENCE:0",
		"#EXT-X-START:TIME-OFFSET:0.000",
		"seg_0001.ts",
		"seg_0002.ts",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("playlist missing %q:\n%s", want, body)
		}
	}
	for _, notWant := range []string{"seg_0000.ts", "seg_0003.ts"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("playlist contains %q:\n%s", notWant, body)
		}
	}
}

func TestBuildChapterPlaylistLastChapterEndZero(t *testing.T) {
	root := buildChapterTestHLSDir(t, "show1")
	srv := New(config.Config{HLSDir: root})

	_, segments, startOffset, endOffset, total, err := srv.BuildChapterPlaylist("show1", 24, 0)
	if err != nil {
		t.Fatalf("BuildChapterPlaylist returned error: %v", err)
	}
	assertSegments(t, segments, []string{"seg_0004.ts"})
	if !closeFloat(startOffset, 0) {
		t.Fatalf("startOffset = %v, want 0", startOffset)
	}
	if !closeFloat(endOffset, 6) {
		t.Fatalf("endOffset = %v, want 6", endOffset)
	}
	if !closeFloat(total, 30) {
		t.Fatalf("total = %v, want 30", total)
	}
}

func TestBuildChapterPlaylistRejectsBadShowNames(t *testing.T) {
	root := buildChapterTestHLSDir(t, "show1")
	srv := New(config.Config{HLSDir: root})

	for _, show := range []string{"..", "a/b", ""} {
		t.Run(show, func(t *testing.T) {
			if _, _, _, _, _, err := srv.BuildChapterPlaylist(show, 0, 6); err == nil {
				t.Fatal("BuildChapterPlaylist error = nil, want non-nil")
			}
		})
	}
}

func TestBuildChapterPlaylistMidSegmentStart(t *testing.T) {
	root := buildChapterTestHLSDir(t, "show1")
	srv := New(config.Config{HLSDir: root})

	playlist, segments, startOffset, endOffset, _, err := srv.BuildChapterPlaylist("show1", 9, 15)
	if err != nil {
		t.Fatalf("BuildChapterPlaylist returned error: %v", err)
	}
	assertSegments(t, segments, []string{"seg_0001.ts", "seg_0002.ts"})
	if !closeFloat(startOffset, 3) {
		t.Fatalf("startOffset = %v, want 3", startOffset)
	}
	if !closeFloat(endOffset, 9) {
		t.Fatalf("endOffset = %v, want 9", endOffset)
	}
	if body := string(playlist); !strings.Contains(body, "#EXT-X-START:TIME-OFFSET:3.000") {
		t.Fatalf("playlist missing start offset:\n%s", body)
	}
}

func TestServeScopedSegmentServesWhitelistedSegment(t *testing.T) {
	root := buildChapterTestHLSDir(t, "show1")
	srv := New(config.Config{HLSDir: root})

	req := httptest.NewRequest(http.MethodGet, "/scoped/show1/seg_0000.ts", nil)
	rec := httptest.NewRecorder()
	srv.ServeScopedSegment(rec, req, "show1", "seg_0000.ts")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Fatalf("Content-Type = %q, want video/mp2t", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store, no-cache, must-revalidate, private" {
		t.Fatalf("Cache-Control = %q", cc)
	}
}

func TestServeScopedSegmentRejectsInvalidRequests(t *testing.T) {
	root := buildChapterTestHLSDir(t, "show1")
	if err := os.WriteFile(filepath.Join(root, "show1", "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(config.Config{HLSDir: root})

	tests := []struct {
		name string
		show string
		file string
	}{
		{name: "non-whitelisted extension", show: "show1", file: "notes.txt"},
		{name: "traversal file dotdot", show: "show1", file: ".."},
		{name: "traversal file path", show: "show1", file: "../seg_0000.ts"},
		{name: "bad show", show: "../show1", file: "seg_0000.ts"},
		{name: "missing file", show: "show1", file: "seg_9999.ts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/scoped/"+tt.show+"/"+tt.file, nil)
			rec := httptest.NewRecorder()
			srv.ServeScopedSegment(rec, req, tt.show, tt.file)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
		})
	}
}

func buildChapterTestHLSDir(t *testing.T, show string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, show)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	playlist := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-TARGETDURATION:6",
		"#EXT-X-MEDIA-SEQUENCE:0",
		"#EXT-X-PLAYLIST-TYPE:VOD",
		"#EXTINF:6.000,",
		"seg_0000.ts",
		"#EXTINF:6.000,",
		"seg_0001.ts",
		"#EXTINF:6.000,",
		"seg_0002.ts",
		"#EXTINF:6.000,",
		"seg_0003.ts",
		"#EXTINF:6.000,",
		"seg_0004.ts",
		"#EXT-X-ENDLIST",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "playlist.m3u8"), []byte(playlist), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"seg_0000.ts", "seg_0001.ts", "seg_0002.ts", "seg_0003.ts", "seg_0004.ts"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func assertSegments(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("segments = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("segments = %#v, want %#v", got, want)
		}
	}
}

func closeFloat(got, want float64) bool {
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-6
}
