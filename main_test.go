package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/hls"
	"github.com/sspeaks/large-video-streamer/internal/labels"
	"github.com/sspeaks/large-video-streamer/internal/share"
	dbstore "github.com/sspeaks/large-video-streamer/internal/store"
)

func TestOpenStateStoresUsesSQLiteAndMigratesLegacyFiles(t *testing.T) {
	stateDir := mainTestDir(t)
	legacyShare := share.Share{
		TokenHash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Show:        "demo",
		ChapterName: "intro",
		Start:       1,
		End:         9,
		Segments:    []string{"seg_0001.ts"},
		Playlist:    "#EXTM3U\n",
		Mode:        share.ModePublic,
		CreatedAt:   time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
	}
	writeMainJSON(t, filepath.Join(stateDir, "shares.json"), []share.Share{legacyShare})
	writeMainJSON(t, filepath.Join(stateDir, "labels", "demo.labels.json"), labels.VideoLabels{
		Video:      "ignored",
		Boundaries: []labels.Boundary{{Name: "intro", Start: 1}},
		Candidates: []labels.Candidate{{Time: 5, Duration: 2, Status: "candidate"}},
	})

	shareStore, labelStore, closeState, err := openStateStores(context.Background(), config.Config{
		StateDir: stateDir,
		DBPath:   filepath.Join(stateDir, "app.db"),
	})
	if err != nil {
		t.Fatalf("openStateStores: %v", err)
	}
	defer func() {
		if err := closeState(); err != nil {
			t.Fatalf("closeState: %v", err)
		}
	}()

	if _, ok := shareStore.(*dbstore.SQLiteShareStore); !ok {
		t.Fatalf("share store type = %T, want SQLite", shareStore)
	}
	if _, ok := labelStore.(*dbstore.SQLiteLabelStore); !ok {
		t.Fatalf("label store type = %T, want SQLite", labelStore)
	}
	summaries := shareStore.List()
	if len(summaries) != 1 || summaries[0].TokenHash != legacyShare.TokenHash || summaries[0].Show != "demo" {
		t.Fatalf("migrated shares = %#v", summaries)
	}
	gotLabels, err := labelStore.Load("demo")
	if err != nil {
		t.Fatalf("Load migrated labels: %v", err)
	}
	if len(gotLabels.Boundaries) != 1 || gotLabels.Boundaries[0].Name != "intro" || len(gotLabels.Candidates) != 1 {
		t.Fatalf("migrated labels = %#v", gotLabels)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "shares.json")); err != nil {
		t.Fatalf("legacy shares file missing after migration: %v", err)
	}
}

func TestOpenStateStoresFlatFileRollbackSkipsSQLite(t *testing.T) {
	stateDir := mainTestDir(t)
	writeMainJSON(t, filepath.Join(stateDir, "shares.json"), []share.Share{{
		TokenHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Show:      "flat",
		Mode:      share.ModePublic,
		CreatedAt: time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
	}})
	writeMainJSON(t, filepath.Join(stateDir, "labels", "flat.labels.json"), labels.VideoLabels{
		Video:      "flat",
		Boundaries: []labels.Boundary{{Name: "flat-boundary", Start: 3}},
	})
	dbPath := filepath.Join(stateDir, "app.db")

	shareStore, labelStore, closeState, err := openStateStores(context.Background(), config.Config{
		StateDir:         stateDir,
		DBPath:           dbPath,
		UseFlatFileState: true,
	})
	if err != nil {
		t.Fatalf("openStateStores flat-file: %v", err)
	}
	defer func() {
		if err := closeState(); err != nil {
			t.Fatalf("closeState: %v", err)
		}
	}()

	if _, ok := shareStore.(*share.Store); !ok {
		t.Fatalf("share store type = %T, want flat-file", shareStore)
	}
	if _, ok := labelStore.(*labels.Store); !ok {
		t.Fatalf("label store type = %T, want flat-file", labelStore)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("SQLite DB exists in flat-file rollback mode or stat failed: %v", err)
	}
	if got := shareStore.List(); len(got) != 1 || got[0].Show != "flat" {
		t.Fatalf("flat-file shares = %#v", got)
	}
	gotLabels, err := labelStore.Load("flat")
	if err != nil {
		t.Fatalf("Load flat-file labels: %v", err)
	}
	if len(gotLabels.Boundaries) != 1 || gotLabels.Boundaries[0].Name != "flat-boundary" {
		t.Fatalf("flat-file labels = %#v", gotLabels)
	}
}

func TestLibraryShowsHandlerIncludesOnlyPersistedPendingReviewCounts(t *testing.T) {
	handler := libraryShowsHandler(
		staticShowLister{shows: []hls.Show{
			{Name: "group-01", Playlist: "/hls/group-01/playlist.m3u8", Status: "ready"},
			{Name: "group-02", Status: "processing"},
		}},
		&stubLabelStore{docs: map[string]labels.VideoLabels{
			"group-01": {
				Video: "group-01",
				Candidates: []labels.Candidate{
					{Status: "candidate", SuggestedName: "private suggestion"},
					{Status: ""},
					{Status: "named"},
					{Status: "rejected"},
				},
			},
			"group-02": {Video: "group-02"},
		}},
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/shows", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if body := rec.Body.String(); strings.Contains(body, "private suggestion") || strings.Contains(body, "candidates") {
		t.Fatalf("response exposes candidate details: %s", body)
	}

	var got []libraryShow
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(shows) = %d, want 2: %#v", len(got), got)
	}
	if got[0].Name != "group-01" || got[0].PendingReviews != 2 {
		t.Fatalf("group-01 = %#v, want 2 pending reviews", got[0])
	}
	if got[1].Name != "group-02" || got[1].PendingReviews != 0 {
		t.Fatalf("group-02 = %#v, want no pending reviews", got[1])
	}
}

func TestLibraryShowsHandlerFailsClosedWhenPersistedLabelsCannotLoad(t *testing.T) {
	handler := libraryShowsHandler(
		staticShowLister{shows: []hls.Show{{Name: "group-01", Status: "ready"}}},
		&stubLabelStore{loadErr: errors.New("database unavailable")},
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/shows", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); body != "internal error\n" {
		t.Fatalf("body = %q, want generic internal error", body)
	}
}

type staticShowLister struct {
	shows []hls.Show
	err   error
}

func (s staticShowLister) ListShows() ([]hls.Show, error) {
	return s.shows, s.err
}

type stubLabelStore struct {
	docs    map[string]labels.VideoLabels
	loadErr error
}

func (s *stubLabelStore) Load(video string) (labels.VideoLabels, error) {
	if s.loadErr != nil {
		return labels.VideoLabels{}, s.loadErr
	}
	return s.docs[video], nil
}

func (s *stubLabelStore) Save(labels.VideoLabels) error {
	return nil
}

func mainTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".main-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	return abs
}

func writeMainJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
