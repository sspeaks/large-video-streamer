package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/labels"
	"github.com/sspeaks/large-video-streamer/internal/share"
)

func TestImportLegacyStateIdempotentAndPreservesFields(t *testing.T) {
	ctx := context.Background()
	stateDir := testDir(t)
	db, err := Open(ctx, filepath.Join(stateDir, "app.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := ApplyMigrations(ctx, db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	existing := &share.Share{
		TokenHash:   legacyHash("a"),
		Show:        "db-show",
		ChapterName: "db-chapter",
		Start:       1,
		End:         2,
		Segments:    []string{"db-seg.ts"},
		Playlist:    "#EXTM3U\n# DB\n",
		Mode:        share.ModePublic,
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	insertShareRow(t, ctx, db, existing)

	existingLabels := labels.VideoLabels{
		Video:      "newer_video",
		Boundaries: []labels.Boundary{{Name: "db-boundary", Start: 999}},
		Candidates: []labels.Candidate{{Time: 998, Duration: 1, Status: "named"}},
	}
	if err := NewLabelStore(db).Save(existingLabels); err != nil {
		t.Fatalf("Save existing labels: %v", err)
	}

	expiresAt := time.Date(2026, 7, 7, 8, 9, 10, 11, time.UTC)
	claimedAt := time.Date(2026, 7, 7, 9, 9, 10, 12, time.UTC)
	revokedAt := time.Date(2026, 7, 7, 10, 9, 10, 13, time.UTC)
	createdAt := time.Date(2026, 7, 7, 7, 9, 10, 14, time.UTC)
	importedShare := &share.Share{
		TokenHash:   legacyHash("b"),
		Show:        "legacy-show",
		ChapterName: "legacy-chapter",
		Start:       12.25,
		End:         34.5,
		StartOffset: 0.25,
		EndOffset:   22.5,
		Segments:    []string{"seg_0001.ts", "seg_0002.ts"},
		Playlist:    "#EXTM3U\n#EXTINF:2.0,\nseg_0001.ts\n",
		Mode:        share.ModeSingle,
		ExpiresAt:   &expiresAt,
		DeviceHash:  legacyHash("d"),
		ClaimedAt:   &claimedAt,
		RevokedAt:   &revokedAt,
		CreatedAt:   createdAt,
	}
	legacyShares := []*share.Share{
		{
			TokenHash:   existing.TokenHash,
			Show:        "stale-flat-file-show",
			ChapterName: "stale-flat-file-chapter",
			Start:       100,
			End:         200,
			Segments:    []string{"stale.ts"},
			Playlist:    "#EXTM3U\n# STALE\n",
			Mode:        share.ModeSingle,
			CreatedAt:   createdAt,
		},
		importedShare,
	}
	sharesPath := filepath.Join(stateDir, "shares.json")
	sharesBefore := writeJSONFile(t, sharesPath, legacyShares, 0o600)

	legacyLabels := labels.VideoLabels{
		Video: "ignored_video_field",
		Boundaries: []labels.Boundary{
			{Name: "intro", Start: 0},
			{Name: "finale", Start: 123.45},
		},
		Candidates: []labels.Candidate{
			{Time: 11.5, Duration: 2.25, Status: "candidate"},
			{Time: 44, Duration: 1.5, Status: "rejected"},
		},
	}
	staleLabels := labels.VideoLabels{
		Video:      "ignored",
		Boundaries: []labels.Boundary{{Name: "stale-boundary", Start: 1}},
		Candidates: []labels.Candidate{{Time: 2, Duration: 3, Status: "candidate"}},
	}
	labelsDir := filepath.Join(stateDir, "labels")
	labelsPath := filepath.Join(labelsDir, "legacy_video.labels.json")
	labelsBefore := writeJSONFile(t, labelsPath, legacyLabels, 0o644)
	writeJSONFile(t, filepath.Join(labelsDir, "newer_video.labels.json"), staleLabels, 0o644)

	for i := 0; i < 2; i++ {
		if err := ImportLegacyState(ctx, db, stateDir); err != nil {
			t.Fatalf("ImportLegacyState pass %d: %v", i+1, err)
		}
	}

	assertFileUnchanged(t, sharesPath, sharesBefore)
	assertFileUnchanged(t, labelsPath, labelsBefore)
	assertShareCount(t, ctx, db, 2)
	assertRowCount(t, ctx, db, "boundaries", "legacy_video", 2)
	assertRowCount(t, ctx, db, "candidates", "legacy_video", 2)

	gotExisting := loadShareByHash(t, ctx, db, existing.TokenHash)
	if gotExisting.Show != existing.Show || gotExisting.ChapterName != existing.ChapterName || gotExisting.Playlist != existing.Playlist {
		t.Fatalf("existing DB share was overwritten: %#v", gotExisting)
	}

	gotShare := loadShareByHash(t, ctx, db, importedShare.TokenHash)
	assertShareEqual(t, gotShare, importedShare)

	labelStore := NewLabelStore(db)
	gotLabels, err := labelStore.Load("legacy_video")
	if err != nil {
		t.Fatalf("Load imported labels: %v", err)
	}
	wantLabels := legacyLabels
	wantLabels.Video = "legacy_video"
	if !reflect.DeepEqual(gotLabels, wantLabels) {
		t.Fatalf("imported labels = %#v, want %#v", gotLabels, wantLabels)
	}

	gotExistingLabels, err := labelStore.Load("newer_video")
	if err != nil {
		t.Fatalf("Load existing labels: %v", err)
	}
	if !reflect.DeepEqual(gotExistingLabels, existingLabels) {
		t.Fatalf("existing DB labels were overwritten: %#v, want %#v", gotExistingLabels, existingLabels)
	}
}

func writeJSONFile(t *testing.T, path string, value any, perm os.FileMode) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent %s: %v", path, err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return data
}

func assertFileUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s changed during import\n got: %s\nwant: %s", path, got, want)
	}
}

func assertShareCount(t *testing.T, ctx context.Context, db *sql.DB, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shares`).Scan(&got); err != nil {
		t.Fatalf("count shares: %v", err)
	}
	if got != want {
		t.Fatalf("share count = %d, want %d", got, want)
	}
}

func assertRowCount(t *testing.T, ctx context.Context, db *sql.DB, table, video string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE video = ?`, video).Scan(&got); err != nil {
		t.Fatalf("count %s for %s: %v", table, video, err)
	}
	if got != want {
		t.Fatalf("%s count for %s = %d, want %d", table, video, got, want)
	}
}

func loadShareByHash(t *testing.T, ctx context.Context, db *sql.DB, tokenHash string) *share.Share {
	t.Helper()
	row := db.QueryRowContext(ctx, `
SELECT token_hash, show, chapter_name, start_seconds, end_seconds,
       start_offset_seconds, end_offset_seconds, segments_json, playlist,
       mode, expires_at, device_hash, claimed_at, revoked_at, created_at
FROM shares
WHERE token_hash = ?`, tokenHash)
	sh, err := scanSQLiteShare(row)
	if err != nil {
		t.Fatalf("scan share %s: %v", tokenHash, err)
	}
	return sh
}

func assertShareEqual(t *testing.T, got, want *share.Share) {
	t.Helper()
	if got.TokenHash != want.TokenHash ||
		got.Show != want.Show ||
		got.ChapterName != want.ChapterName ||
		got.Start != want.Start ||
		got.End != want.End ||
		got.StartOffset != want.StartOffset ||
		got.EndOffset != want.EndOffset ||
		got.Playlist != want.Playlist ||
		got.Mode != want.Mode ||
		got.DeviceHash != want.DeviceHash ||
		!reflect.DeepEqual(got.Segments, want.Segments) ||
		!got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("share = %#v, want %#v", got, want)
	}
	assertOptionalTimeEqual(t, "ExpiresAt", got.ExpiresAt, want.ExpiresAt)
	assertOptionalTimeEqual(t, "ClaimedAt", got.ClaimedAt, want.ClaimedAt)
	assertOptionalTimeEqual(t, "RevokedAt", got.RevokedAt, want.RevokedAt)
}

func assertOptionalTimeEqual(t *testing.T, field string, got, want *time.Time) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s = %v, want %v", field, got, want)
		}
		return
	}
	if !got.Equal(*want) {
		t.Fatalf("%s = %v, want %v", field, got, want)
	}
}

func legacyHash(seed string) string {
	return strings.Repeat(seed, 64)
}
