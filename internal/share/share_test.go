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
