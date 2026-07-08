package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/share"
)

func TestSQLiteShareStoreCreateGetReload(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(testDir(t), "app.db")
	db, s := newTestShareStore(t, ctx, dbPath)
	exp := time.Date(2026, 1, 2, 3, 4, 5, 600, time.UTC)
	s.now = func() time.Time { return time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC) }
	token, err := s.Create(share.CreateParams{
		Show:        "demo",
		ChapterName: "chap",
		Start:       12,
		End:         30,
		StartOffset: 0,
		EndOffset:   18,
		Segments:    []string{"seg_0002.ts"},
		Playlist:    "#EXTM3U\n#EXT-X-ENDLIST\n",
		Mode:        share.ModePublic,
		ExpiresAt:   &exp,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	db, err = Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer db.Close()
	reloaded, err := NewShareStore(ctx, db)
	if err != nil {
		t.Fatalf("NewShareStore reload: %v", err)
	}
	got, ok := reloaded.Get(token)
	if !ok {
		t.Fatal("reloaded store is missing the created share")
	}
	if got.Show != "demo" || got.ChapterName != "chap" || got.Mode != share.ModePublic {
		t.Fatalf("reloaded share = %#v", got)
	}
	if got.End != 30 || got.EndOffset != 18 {
		t.Fatalf("reloaded bounds End=%v EndOffset=%v", got.End, got.EndOffset)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, exp)
	}
	if !reflect.DeepEqual(got.Segments, []string{"seg_0002.ts"}) {
		t.Fatalf("segments = %v", got.Segments)
	}
}

func TestSQLiteShareStoreRawTokenNotStored(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()

	token, err := s.Create(share.CreateParams{
		Show:        "demo",
		ChapterName: "chap",
		Segments:    []string{"seg_0000.ts"},
		Playlist:    "#EXTM3U\n",
		Mode:        share.ModeSingle,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var tokenHash string
	if err := db.QueryRowContext(ctx, `SELECT token_hash FROM shares`).Scan(&tokenHash); err != nil {
		t.Fatalf("read token_hash: %v", err)
	}
	if tokenHash == token {
		t.Fatal("token_hash stored the raw token")
	}
	if tokenHash != sha256hex(token) {
		t.Fatalf("token_hash = %q, want SHA-256 of raw token", tokenHash)
	}
	assertSQLiteTextDoesNotContain(t, ctx, db, token)
}

func TestSQLiteShareStoreCreateGetAndListReturnSnapshots(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()
	exp := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	origExp := exp
	segments := []string{"seg_0002.ts"}
	token, err := s.Create(share.CreateParams{
		Show:        "demo",
		ChapterName: "chap",
		Segments:    segments,
		Playlist:    "#EXTM3U\n",
		Mode:        share.ModePublic,
		ExpiresAt:   &exp,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	segments[0] = "mutated.ts"
	exp = exp.Add(time.Hour)
	got, ok := s.Get(token)
	if !ok {
		t.Fatal("created share missing")
	}
	if got.Segments[0] != "seg_0002.ts" {
		t.Fatalf("Create kept caller-owned segments slice: %v", got.Segments)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(origExp) {
		t.Fatalf("Create kept caller-owned expiry pointer: %v, want %v", got.ExpiresAt, origExp)
	}

	got.Show = "mutated"
	got.Segments[0] = "changed.ts"
	*got.ExpiresAt = got.ExpiresAt.Add(time.Hour)
	again, _ := s.Get(token)
	if again.Show != "demo" || again.Segments[0] != "seg_0002.ts" || !again.ExpiresAt.Equal(origExp) {
		t.Fatalf("Get did not return a deep snapshot: %#v", again)
	}

	listed := s.List()
	if len(listed) != 1 {
		t.Fatalf("List returned %d summaries, want 1", len(listed))
	}
	*listed[0].ExpiresAt = listed[0].ExpiresAt.Add(time.Hour)
	listedAgain := s.List()
	if listedAgain[0].ExpiresAt == nil || !listedAgain[0].ExpiresAt.Equal(origExp) {
		t.Fatalf("List did not return a snapshot expiry: %v", listedAgain[0].ExpiresAt)
	}
}

func TestSQLiteShareStoreClaimRejectsPublicShare(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()
	token, err := s.Create(share.CreateParams{Show: "demo", ChapterName: "c", Mode: share.ModePublic})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.Claim(token, "secret") {
		t.Fatal("public shares must not be claimable")
	}
	got, _ := s.Get(token)
	if got.DeviceHash != "" || got.ClaimedAt != nil {
		t.Fatalf("public claim mutated share: %#v", got)
	}
}

func TestSQLiteShareStoreClaimSingleFirstWins(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()
	token, err := s.Create(share.CreateParams{Show: "demo", ChapterName: "c", Mode: share.ModeSingle, Segments: []string{"a"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !s.Claim(token, "dev-secret-1") {
		t.Fatal("first claim should succeed")
	}
	if s.Claim(token, "dev-secret-2") {
		t.Fatal("second claim should fail (already claimed)")
	}
	got, _ := s.Get(token)
	if !got.DeviceMatches("dev-secret-1") {
		t.Fatal("device 1 should match the bound device")
	}
	if got.DeviceMatches("dev-secret-2") {
		t.Fatal("device 2 must not match the bound device")
	}
}

func TestSQLiteShareStoreClaimConcurrentSingleWinner(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()
	token, err := s.Create(share.CreateParams{Show: "demo", ChapterName: "c", Mode: share.ModeSingle})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	const n = 24
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if s.Claim(token, fmt.Sprintf("secret-%d", i)) {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent claims won = %d, want exactly 1", wins)
	}
	got, _ := s.Get(token)
	if got.DeviceHash == "" || got.ClaimedAt == nil {
		t.Fatal("winning claim did not persist a device binding")
	}
}

func TestSQLiteShareStoreRevokeAndExpiryDenyClaim(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	past := now.Add(-time.Minute)
	s.now = func() time.Time { return now }

	revokedToken, err := s.Create(share.CreateParams{Show: "demo", ChapterName: "revoke", Mode: share.ModeSingle})
	if err != nil {
		t.Fatalf("Create revoked: %v", err)
	}
	expiredToken, err := s.Create(share.CreateParams{Show: "demo", ChapterName: "expired", Mode: share.ModeSingle, ExpiresAt: &past})
	if err != nil {
		t.Fatalf("Create expired: %v", err)
	}

	s.Revoke(revokedToken)
	revoked, ok := s.Get(revokedToken)
	if !ok || revoked.RevokedAt == nil {
		t.Fatalf("revoked share = %#v, ok=%v", revoked, ok)
	}
	if s.Claim(revokedToken, "secret") {
		t.Fatal("revoked share must not be claimable")
	}
	if s.Claim(expiredToken, "secret") {
		t.Fatal("expired share must not be claimable")
	}
}

func TestSQLiteShareStoreListIncludesAllSharesSortedByCreatedAtThenHash(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	expired := base.Add(-time.Hour)
	claimed := base.Add(-30 * time.Minute)
	revoked := base.Add(-15 * time.Minute)

	insertShareRow(t, ctx, db, &share.Share{TokenHash: "b", Show: "revoked", Mode: share.ModePublic, RevokedAt: &revoked, CreatedAt: base})
	insertShareRow(t, ctx, db, &share.Share{TokenHash: "a", Show: "expired", Mode: share.ModeSingle, ExpiresAt: &expired, CreatedAt: base})
	insertShareRow(t, ctx, db, &share.Share{TokenHash: "c", Show: "claimed", Mode: share.ModeSingle, ClaimedAt: &claimed, CreatedAt: base.Add(-time.Minute)})

	got := s.List()
	if len(got) != 3 {
		t.Fatalf("List returned %d summaries, want 3", len(got))
	}
	wantHashes := []string{"c", "a", "b"}
	for i, want := range wantHashes {
		if got[i].TokenHash != want {
			t.Fatalf("List order hashes = %v, want %v", []string{got[0].TokenHash, got[1].TokenHash, got[2].TokenHash}, wantHashes)
		}
	}
	if got[1].ExpiresAt == nil || got[2].RevokedAt == nil {
		t.Fatalf("List omitted expired/revoked records: %#v", got)
	}

	got[0].Show = "mutated"
	*got[0].ClaimedAt = got[0].ClaimedAt.Add(time.Hour)
	*got[1].ExpiresAt = got[1].ExpiresAt.Add(time.Hour)
	*got[2].RevokedAt = got[2].RevokedAt.Add(time.Hour)
	again := s.List()
	if again[0].Show != "claimed" || !again[0].ClaimedAt.Equal(claimed) || !again[1].ExpiresAt.Equal(expired) || !again[2].RevokedAt.Equal(revoked) {
		t.Fatalf("List summaries were not independent snapshots: %#v", again)
	}
}

func TestSQLiteShareStoreDeleteByHash(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()
	deleteToken, err := s.Create(share.CreateParams{Show: "demo", ChapterName: "delete", Mode: share.ModePublic})
	if err != nil {
		t.Fatalf("Create delete share: %v", err)
	}
	keepToken, err := s.Create(share.CreateParams{Show: "demo", ChapterName: "keep", Mode: share.ModePublic})
	if err != nil {
		t.Fatalf("Create keep share: %v", err)
	}

	ok, err := s.DeleteByHash(sha256hex(deleteToken))
	if err != nil || !ok {
		t.Fatalf("DeleteByHash ok=%v err=%v, want ok with no error", ok, err)
	}
	if _, ok := s.Get(deleteToken); ok {
		t.Fatal("deleted share is still present")
	}
	if _, ok := s.Get(keepToken); !ok {
		t.Fatal("deleting one share removed another share")
	}
	ok, err = s.DeleteByHash("missing")
	if err != nil || ok {
		t.Fatalf("missing DeleteByHash ok=%v err=%v, want false with no error", ok, err)
	}
}

func TestSQLiteShareStoreRevokeByHash(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()
	token, err := s.Create(share.CreateParams{Show: "demo", ChapterName: "revoke", Mode: share.ModePublic})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ok, err := s.RevokeByHash(sha256hex(token))
	if err != nil || !ok {
		t.Fatalf("RevokeByHash ok=%v err=%v, want ok with no error", ok, err)
	}
	ok, err = s.RevokeByHash(sha256hex(token))
	if err != nil || !ok {
		t.Fatalf("idempotent RevokeByHash ok=%v err=%v, want ok with no error", ok, err)
	}
	got, ok := s.Get(token)
	if !ok || got.RevokedAt == nil {
		t.Fatalf("revoked share = %#v, ok=%v", got, ok)
	}
	ok, err = s.RevokeByHash("missing")
	if err != nil || ok {
		t.Fatalf("missing RevokeByHash ok=%v err=%v, want false with no error", ok, err)
	}
}

func newTestShareStore(t *testing.T, ctx context.Context, dbPath string) (*sql.DB, *SQLiteShareStore) {
	t.Helper()
	db, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s, err := NewShareStore(ctx, db)
	if err != nil {
		db.Close()
		t.Fatalf("NewShareStore: %v", err)
	}
	return db, s
}

func assertSQLiteTextDoesNotContain(t *testing.T, ctx context.Context, db *sql.DB, needle string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT token_hash, show, chapter_name, segments_json, playlist, mode,
       COALESCE(expires_at, ''), COALESCE(device_hash, ''),
       COALESCE(claimed_at, ''), COALESCE(revoked_at, ''), created_at
FROM shares`)
	if err != nil {
		t.Fatalf("query shares text columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		values := make([]string, 11)
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("scan shares text columns: %v", err)
		}
		for _, value := range values {
			if strings.Contains(value, needle) {
				t.Fatalf("raw token found in persisted shares table value %q", value)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("shares text rows error: %v", err)
	}
}

func insertShareRow(t *testing.T, ctx context.Context, db *sql.DB, sh *share.Share) {
	t.Helper()
	segments, err := jsonMarshalStrings(sh.Segments)
	if err != nil {
		t.Fatalf("marshal segments: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO shares (
	token_hash, show, chapter_name, start_seconds, end_seconds,
	start_offset_seconds, end_offset_seconds, segments_json, playlist,
	mode, expires_at, device_hash, claimed_at, revoked_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sh.TokenHash,
		sh.Show,
		sh.ChapterName,
		sh.Start,
		sh.End,
		sh.StartOffset,
		sh.EndOffset,
		segments,
		sh.Playlist,
		string(sh.Mode),
		nullableTimeString(sh.ExpiresAt),
		nullableString(sh.DeviceHash),
		nullableTimeString(sh.ClaimedAt),
		nullableTimeString(sh.RevokedAt),
		formatShareTime(sh.CreatedAt),
	); err != nil {
		t.Fatalf("insert share row: %v", err)
	}
}

func jsonMarshalStrings(values []string) (string, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func TestSQLiteShareStoreListOrdersCreatedAtThenHashForCreatedRecords(t *testing.T) {
	ctx := context.Background()
	db, s := newTestShareStore(t, ctx, filepath.Join(testDir(t), "app.db"))
	defer db.Close()
	fixed := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	s.now = func() time.Time { return fixed }

	tokens := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		token, err := s.Create(share.CreateParams{Show: "demo", ChapterName: fmt.Sprintf("c%d", i), Mode: share.ModePublic})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		tokens = append(tokens, sha256hex(token))
	}
	sort.Strings(tokens)
	got := s.List()
	gotHashes := make([]string, 0, len(got))
	for _, summary := range got {
		gotHashes = append(gotHashes, summary.TokenHash)
	}
	if !reflect.DeepEqual(gotHashes, tokens) {
		t.Fatalf("hash order = %v, want %v", gotHashes, tokens)
	}
}
