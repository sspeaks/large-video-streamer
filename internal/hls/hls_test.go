package hls

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
