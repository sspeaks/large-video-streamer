package share

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	return newStore(filepath.Join(t.TempDir(), "shares.json"))
}

func TestGenerateSecretUniqueAndURLSafe(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		s, err := generateSecret()
		if err != nil {
			t.Fatalf("generateSecret: %v", err)
		}
		if len(s) < 20 {
			t.Fatalf("secret too short (%d): %q", len(s), s)
		}
		if strings.ContainsAny(s, "+/=") {
			t.Fatalf("secret not URL-safe: %q", s)
		}
		if seen[s] {
			t.Fatalf("duplicate secret at draw %d: %q", i, s)
		}
		seen[s] = true
	}
}

func TestCreateStoresHashNotRawToken(t *testing.T) {
	s := tempStore(t)
	token, err := s.Create(CreateParams{Show: "demo", ChapterName: "c", Mode: ModeSingle, Segments: []string{"seg_0000.ts"}, Playlist: "#EXTM3U\n"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatalf("read persisted shares: %v", err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("raw token found in persisted shares.json; only the hash should be stored")
	}
	if _, ok := s.byHash[token]; ok {
		t.Fatal("map keyed by raw token, want SHA-256 hex")
	}
	if _, ok := s.byHash[sha256hex(token)]; !ok {
		t.Fatal("map not keyed by token hash")
	}
}

func TestStoreRoundTripReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shares.json")
	s1 := newStore(path)
	exp := time.Now().Add(time.Hour).UTC().Round(time.Second)
	token, err := s1.Create(CreateParams{
		Show: "demo", ChapterName: "chap", Start: 12, End: 30, StartOffset: 0, EndOffset: 18,
		Segments: []string{"seg_0002.ts"}, Playlist: "#EXTM3U\n#EXT-X-ENDLIST\n", Mode: ModePublic, ExpiresAt: &exp,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	s2 := newStore(path)
	got, ok := s2.Get(token)
	if !ok {
		t.Fatal("reloaded store is missing the created share")
	}
	if got.Show != "demo" || got.ChapterName != "chap" || got.Mode != ModePublic {
		t.Fatalf("reloaded share = %#v", got)
	}
	if got.End != 30 || got.EndOffset != 18 {
		t.Fatalf("reloaded bounds End=%v EndOffset=%v", got.End, got.EndOffset)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, exp)
	}
	if len(got.Segments) != 1 || got.Segments[0] != "seg_0002.ts" {
		t.Fatalf("segments = %v", got.Segments)
	}
}

func TestCreateGetAndListReturnSnapshots(t *testing.T) {
	s := tempStore(t)
	exp := time.Now().Add(time.Hour).UTC().Round(time.Second)
	origExp := exp
	segments := []string{"seg_0002.ts"}
	token, err := s.Create(CreateParams{
		Show:        "demo",
		ChapterName: "chap",
		Segments:    segments,
		Playlist:    "#EXTM3U\n",
		Mode:        ModePublic,
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

func TestListIncludesAllSharesSortedByCreatedAtThenHash(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	expired := base.Add(-time.Hour)
	claimed := base.Add(-30 * time.Minute)
	revoked := base.Add(-15 * time.Minute)
	s := &Store{
		byHash: map[string]*Share{
			"b": {TokenHash: "b", Show: "revoked", Mode: ModePublic, RevokedAt: &revoked, CreatedAt: base},
			"a": {TokenHash: "a", Show: "expired", Mode: ModeSingle, ExpiresAt: &expired, CreatedAt: base},
			"c": {TokenHash: "c", Show: "claimed", Mode: ModeSingle, ClaimedAt: &claimed, CreatedAt: base.Add(-time.Minute)},
		},
	}

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

func TestCorruptStoreStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shares.json")
	if err := os.WriteFile(path, []byte("{ this is not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newStore(path)
	if len(s.byHash) != 0 {
		t.Fatalf("want empty store after corrupt load, got %d entries", len(s.byHash))
	}
	if _, err := s.Create(CreateParams{Show: "x", ChapterName: "c", Mode: ModeSingle}); err != nil {
		t.Fatalf("Create after corrupt load: %v", err)
	}
}

func TestPersistLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s := newStore(filepath.Join(dir, "shares.json"))
	for i := 0; i < 5; i++ {
		if _, err := s.Create(CreateParams{Show: "demo", ChapterName: "c", Mode: ModeSingle}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".shares-") {
			t.Fatalf("leftover temp file after atomic write: %s", e.Name())
		}
	}
}

func TestClaimSingleFirstWins(t *testing.T) {
	s := tempStore(t)
	token, _ := s.Create(CreateParams{Show: "demo", ChapterName: "c", Mode: ModeSingle, Segments: []string{"a"}})
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

func TestClaimRejectsPublicShare(t *testing.T) {
	s := tempStore(t)
	token, _ := s.Create(CreateParams{Show: "demo", ChapterName: "c", Mode: ModePublic})
	if s.Claim(token, "secret") {
		t.Fatal("public shares must not be claimable")
	}
}

func TestClaimConcurrentSingleWinner(t *testing.T) {
	s := tempStore(t)
	token, _ := s.Create(CreateParams{Show: "demo", ChapterName: "c", Mode: ModeSingle})
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

func TestUsableRevokedAndExpired(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	if !usable(&Share{}, now) {
		t.Fatal("a fresh share should be usable")
	}
	if usable(&Share{RevokedAt: &past}, now) {
		t.Fatal("revoked share should be unusable")
	}
	if usable(&Share{ExpiresAt: &past}, now) {
		t.Fatal("expired share should be unusable")
	}
	if usable(&Share{ExpiresAt: &now}, now) {
		t.Fatal("share at exactly its expiry should be unusable")
	}
	if !usable(&Share{ExpiresAt: &future}, now) {
		t.Fatal("share with future expiry should be usable")
	}
}

func TestRevokeIsIdempotentAndDenies(t *testing.T) {
	s := tempStore(t)
	token, _ := s.Create(CreateParams{Show: "demo", ChapterName: "c", Mode: ModePublic})
	s.Revoke(token)
	s.Revoke(token) // idempotent
	got, _ := s.Get(token)
	if got.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set")
	}
	if usable(got, time.Now().UTC()) {
		t.Fatal("revoked share should be unusable")
	}
}

func TestRevokeByHashAndDeleteByHashPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shares.json")
	s := newStore(path)
	revokeToken, err := s.Create(CreateParams{Show: "demo", ChapterName: "revoke", Mode: ModePublic})
	if err != nil {
		t.Fatalf("Create revoke share: %v", err)
	}
	deleteToken, err := s.Create(CreateParams{Show: "demo", ChapterName: "delete", Mode: ModePublic})
	if err != nil {
		t.Fatalf("Create delete share: %v", err)
	}

	ok, err := s.RevokeByHash(sha256hex(revokeToken))
	if err != nil || !ok {
		t.Fatalf("RevokeByHash ok=%v err=%v, want ok with no error", ok, err)
	}
	ok, err = s.RevokeByHash(sha256hex(revokeToken))
	if err != nil || !ok {
		t.Fatalf("idempotent RevokeByHash ok=%v err=%v, want ok with no error", ok, err)
	}
	reloaded := newStore(path)
	revoked, ok := reloaded.Get(revokeToken)
	if !ok || revoked.RevokedAt == nil {
		t.Fatalf("reloaded revoked share = %#v, ok=%v", revoked, ok)
	}

	ok, err = s.DeleteByHash(sha256hex(deleteToken))
	if err != nil || !ok {
		t.Fatalf("DeleteByHash ok=%v err=%v, want ok with no error", ok, err)
	}
	if _, ok := s.Get(deleteToken); ok {
		t.Fatal("deleted share is still present in memory")
	}
	reloaded = newStore(path)
	if _, ok := reloaded.Get(deleteToken); ok {
		t.Fatal("deleted share was still present after reload")
	}
	if _, ok := reloaded.Get(revokeToken); !ok {
		t.Fatal("deleting one share removed another share")
	}

	ok, err = s.RevokeByHash("missing")
	if err != nil || ok {
		t.Fatalf("missing RevokeByHash ok=%v err=%v, want false with no error", ok, err)
	}
	ok, err = s.DeleteByHash("missing")
	if err != nil || ok {
		t.Fatalf("missing DeleteByHash ok=%v err=%v, want false with no error", ok, err)
	}
}

func TestRevokeRollsBackOnPersistFailure(t *testing.T) {
	s := tempStore(t)
	token, err := s.Create(CreateParams{Show: "demo", ChapterName: "c", Mode: ModePublic})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	s.path = directoryPersistenceTarget(t, s.path)

	s.Revoke(token)
	got, ok := s.Get(token)
	if !ok {
		t.Fatal("share missing after failed revoke")
	}
	if got.RevokedAt != nil {
		t.Fatalf("failed Revoke left in-memory RevokedAt set: %v", got.RevokedAt)
	}
}

func TestRevokeByHashRollsBackOnPersistFailure(t *testing.T) {
	s := tempStore(t)
	token, err := s.Create(CreateParams{Show: "demo", ChapterName: "c", Mode: ModePublic})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	s.path = directoryPersistenceTarget(t, s.path)

	ok, err := s.RevokeByHash(sha256hex(token))
	if err == nil || !ok {
		t.Fatalf("RevokeByHash ok=%v err=%v, want ok with persistence error", ok, err)
	}
	got, _ := s.Get(token)
	if got.RevokedAt != nil {
		t.Fatalf("failed RevokeByHash left in-memory RevokedAt set: %v", got.RevokedAt)
	}
}

func TestDeleteByHashRollsBackOnPersistFailure(t *testing.T) {
	s := tempStore(t)
	token, err := s.Create(CreateParams{Show: "demo", ChapterName: "c", Mode: ModePublic})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	s.path = directoryPersistenceTarget(t, s.path)

	ok, err := s.DeleteByHash(sha256hex(token))
	if err == nil || !ok {
		t.Fatalf("DeleteByHash ok=%v err=%v, want ok with persistence error", ok, err)
	}
	if _, ok := s.Get(token); !ok {
		t.Fatal("failed DeleteByHash did not restore in-memory record")
	}
}

func TestDeviceMatchesEmptyInputs(t *testing.T) {
	if (&Share{}).DeviceMatches("anything") {
		t.Fatal("unclaimed share (no DeviceHash) must not match any device")
	}
	sh := &Share{DeviceHash: sha256hex("secret")}
	if sh.DeviceMatches("") {
		t.Fatal("empty device secret must not match")
	}
	if !sh.DeviceMatches("secret") {
		t.Fatal("correct device secret should match")
	}
}

func directoryPersistenceTarget(t *testing.T, currentPath string) string {
	t.Helper()
	target := filepath.Join(filepath.Dir(currentPath), "persist-target-directory")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create persistence failure target: %v", err)
	}
	return target
}
