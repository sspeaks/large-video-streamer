// Package share implements owner-created, device-bound one-time links that let
// a single recipient watch one chapter of a show without logging in. It is the
// application's first server-side mutable state store: shares are held in an
// in-memory map keyed by the SHA-256 of the (never-stored) token and persisted
// to <StateDir>/shares.json with the same atomic write idiom used for the
// cookie secret.
package share

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Mode selects who may watch a share.
type Mode string

const (
	// ModeSingle binds the share to the first device that claims it (default).
	ModeSingle Mode = "single"
	// ModePublic lets any device watch, unlimited times.
	ModePublic Mode = "public"
)

// Share is a persisted, immutable-at-create record describing one shared
// chapter. The raw token is never stored; only its SHA-256 hex is kept as the
// map key. Chapter bounds (Segments/Playlist/offsets) are frozen at creation so
// a one-time link is immune to later label edits or re-segmentation.
type Share struct {
	TokenHash   string     `json:"token_hash"`
	Show        string     `json:"show"`
	ChapterName string     `json:"chapter_name"`
	Start       float64    `json:"start"`
	End         float64    `json:"end"`
	StartOffset float64    `json:"start_offset"`
	EndOffset   float64    `json:"end_offset"`
	Segments    []string   `json:"segments"`
	Playlist    string     `json:"playlist"`
	Mode        Mode       `json:"mode"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	DeviceHash  string     `json:"device_hash,omitempty"`
	ClaimedAt   *time.Time `json:"claimed_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ShareSummary is an admin-safe snapshot of a share. It excludes the frozen
// media payload and device hash; token hashes are enough for management actions.
type ShareSummary struct {
	TokenHash   string     `json:"token_hash"`
	Show        string     `json:"show"`
	ChapterName string     `json:"chapter_name"`
	Start       float64    `json:"start"`
	End         float64    `json:"end"`
	StartOffset float64    `json:"start_offset"`
	EndOffset   float64    `json:"end_offset"`
	Mode        Mode       `json:"mode"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	ClaimedAt   *time.Time `json:"claimed_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// DeviceMatches reports whether deviceSecret (the raw value from the vid_share
// cookie) matches this share's bound device, using a constant-time compare.
func (sh *Share) DeviceMatches(deviceSecret string) bool {
	if sh.DeviceHash == "" || deviceSecret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(sh.DeviceHash), []byte(sha256hex(deviceSecret))) == 1
}

// CreateParams carries the frozen fields for a new share. The store owns token
// generation, hashing, and timestamps.
type CreateParams struct {
	Show        string
	ChapterName string
	Start       float64
	End         float64
	StartOffset float64
	EndOffset   float64
	Segments    []string
	Playlist    string
	Mode        Mode
	ExpiresAt   *time.Time
}

// ShareStore persists shares for routes and future management backends.
type ShareStore interface {
	Create(CreateParams) (string, error)
	Get(token string) (*Share, bool)
	List() []ShareSummary
	Claim(token, deviceSecret string) bool
	RevokeByHash(tokenHash string) (bool, error)
	DeleteByHash(tokenHash string) (bool, error)
}

// Store holds shares in memory and persists them atomically to a JSON file.
type Store struct {
	mu     sync.Mutex
	byHash map[string]*Share
	path   string
}

var _ ShareStore = (*Store)(nil)

// NewStore returns a flat-file share store backed by path.
func NewStore(path string) *Store {
	return newStore(path)
}

// newStore returns a store backed by path, loading any existing shares. A
// missing file starts empty; a corrupt file is logged and ignored (start empty)
// rather than crashing.
func newStore(path string) *Store {
	s := &Store{byHash: make(map[string]*Share), path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var shares []*Share
	if err := json.Unmarshal(data, &shares); err != nil {
		log.Printf("share: ignoring corrupt %s: %v", path, err)
		return s
	}
	for _, sh := range shares {
		if sh != nil && sh.TokenHash != "" {
			s.byHash[sh.TokenHash] = sh
		}
	}
	return s
}

// Create generates a token, stores the record, persists the whole map, and
// returns the raw token (shown once to the owner). The record is removed from
// memory if persistence fails so a returned token always corresponds to a
// durably stored share.
func (s *Store) Create(p CreateParams) (string, error) {
	if p.Mode != ModeSingle && p.Mode != ModePublic {
		return "", fmt.Errorf("invalid share mode %q", p.Mode)
	}
	token, err := generateSecret()
	if err != nil {
		return "", err
	}
	rec := &Share{
		TokenHash:   sha256hex(token),
		Show:        p.Show,
		ChapterName: p.ChapterName,
		Start:       p.Start,
		End:         p.End,
		StartOffset: p.StartOffset,
		EndOffset:   p.EndOffset,
		Segments:    append([]string(nil), p.Segments...),
		Playlist:    p.Playlist,
		Mode:        p.Mode,
		ExpiresAt:   cloneTime(p.ExpiresAt),
		CreatedAt:   time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.byHash[rec.TokenHash] = rec
	if err := s.persistLocked(); err != nil {
		delete(s.byHash, rec.TokenHash)
		return "", err
	}
	return token, nil
}

// Get returns a copy of the share for token, or false if unknown. Callers get a
// snapshot; mutations must go through Claim/Revoke under the store lock.
func (s *Store) Get(token string) (*Share, bool) {
	h := sha256hex(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byHash[h]
	if !ok {
		return nil, false
	}
	return cloneShare(rec), true
}

// List returns admin-safe share summaries, including revoked and expired
// records, sorted deterministically by CreatedAt ascending then TokenHash.
func (s *Store) List() []ShareSummary {
	s.mu.Lock()
	defer s.mu.Unlock()

	shares := make([]*Share, 0, len(s.byHash))
	for _, rec := range s.byHash {
		shares = append(shares, rec)
	}
	sortShares(shares)

	summaries := make([]ShareSummary, 0, len(shares))
	for _, rec := range shares {
		summaries = append(summaries, summarizeShare(rec))
	}
	return summaries
}

// Claim binds a single-mode share to deviceSecret on the first successful call.
// It persists the claim before returning true; exactly one of several racing
// callers wins. Returns false if the token is unknown, unusable (revoked or
// expired), not single-mode, already claimed, or if persistence fails.
func (s *Store) Claim(token, deviceSecret string) bool {
	h := sha256hex(token)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byHash[h]
	if !ok || !usable(rec, now) || rec.Mode != ModeSingle || rec.ClaimedAt != nil {
		return false
	}
	rec.DeviceHash = sha256hex(deviceSecret)
	claimed := now
	rec.ClaimedAt = &claimed
	if err := s.persistLocked(); err != nil {
		rec.DeviceHash = ""
		rec.ClaimedAt = nil
		return false
	}
	return true
}

// Revoke marks a share revoked (deny-by-default thereafter) and persists. It is
// idempotent and a no-op for unknown tokens.
func (s *Store) Revoke(token string) {
	_, _ = s.RevokeByHash(sha256hex(token))
}

// RevokeByHash marks a share revoked by its stored token hash and persists. It
// is idempotent; persistence failures roll back the in-memory change.
func (s *Store) RevokeByHash(tokenHash string) (bool, error) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byHash[tokenHash]
	if !ok {
		return false, nil
	}
	if rec.RevokedAt != nil {
		return true, nil
	}
	prev := rec.RevokedAt
	rec.RevokedAt = &now
	if err := s.persistLocked(); err != nil {
		rec.RevokedAt = prev
		return true, err
	}
	return true, nil
}

// DeleteByHash permanently removes a share by its stored token hash and
// persists. Persistence failures restore the deleted in-memory record.
func (s *Store) DeleteByHash(tokenHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byHash[tokenHash]
	if !ok {
		return false, nil
	}
	delete(s.byHash, tokenHash)
	if err := s.persistLocked(); err != nil {
		s.byHash[tokenHash] = rec
		return true, err
	}
	return true, nil
}

// usable reports whether a share is neither revoked nor expired at now.
func usable(sh *Share, now time.Time) bool {
	if sh.RevokedAt != nil {
		return false
	}
	if sh.ExpiresAt != nil && !now.Before(*sh.ExpiresAt) {
		return false
	}
	return true
}

// persistLocked writes the whole map to disk atomically. Callers must hold mu.
func (s *Store) persistLocked() error {
	if s.path == "" {
		return errors.New("share store has no path")
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir %q for shares: %w", dir, err)
	}

	shares := make([]*Share, 0, len(s.byHash))
	for _, v := range s.byHash {
		shares = append(shares, v)
	}
	sortShares(shares)
	data, err := json.MarshalIndent(shares, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".shares-*")
	if err != nil {
		return fmt.Errorf("persist shares: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("persist shares: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("persist shares: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("persist shares: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("persist shares: %w", err)
	}
	return nil
}

// generateSecret returns a URL-safe base64 string with >=128 bits of entropy,
// used for both share tokens and device secrets.
func generateSecret() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func cloneShare(sh *Share) *Share {
	cp := *sh
	cp.Segments = append([]string(nil), sh.Segments...)
	cp.ExpiresAt = cloneTime(sh.ExpiresAt)
	cp.ClaimedAt = cloneTime(sh.ClaimedAt)
	cp.RevokedAt = cloneTime(sh.RevokedAt)
	return &cp
}

func summarizeShare(sh *Share) ShareSummary {
	return ShareSummary{
		TokenHash:   sh.TokenHash,
		Show:        sh.Show,
		ChapterName: sh.ChapterName,
		Start:       sh.Start,
		End:         sh.End,
		StartOffset: sh.StartOffset,
		EndOffset:   sh.EndOffset,
		Mode:        sh.Mode,
		ExpiresAt:   cloneTime(sh.ExpiresAt),
		ClaimedAt:   cloneTime(sh.ClaimedAt),
		RevokedAt:   cloneTime(sh.RevokedAt),
		CreatedAt:   sh.CreatedAt,
	}
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

func sortShares(shares []*Share) {
	sort.Slice(shares, func(i, j int) bool {
		return lessShare(shares[i], shares[j])
	})
}

func lessShare(a, b *Share) bool {
	if a.CreatedAt.Equal(b.CreatedAt) {
		return a.TokenHash < b.TokenHash
	}
	return a.CreatedAt.Before(b.CreatedAt)
}
