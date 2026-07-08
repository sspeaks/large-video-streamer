package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/share"
)

const sqliteShareTimeFormat = "2006-01-02T15:04:05.000000000Z"

// SQLiteShareStore persists share records in the shares SQLite table.
type SQLiteShareStore struct {
	db  *sql.DB
	now func() time.Time
}

var _ share.ShareStore = (*SQLiteShareStore)(nil)

// NewShareStore returns a SQLite-backed share store, applying the shared schema
// migrations needed by the shares table.
func NewShareStore(ctx context.Context, db *sql.DB) (*SQLiteShareStore, error) {
	if db == nil {
		return nil, errors.New("sqlite share store db is nil")
	}
	if err := ApplyMigrations(ctx, db); err != nil {
		return nil, err
	}
	return &SQLiteShareStore{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

// Create generates a raw token, stores only its SHA-256 hash, and returns the
// raw token once to the caller.
func (s *SQLiteShareStore) Create(p share.CreateParams) (string, error) {
	if p.Mode != share.ModeSingle && p.Mode != share.ModePublic {
		return "", fmt.Errorf("invalid share mode %q", p.Mode)
	}
	token, err := generateShareSecret()
	if err != nil {
		return "", err
	}
	segments, err := json.Marshal(append([]string(nil), p.Segments...))
	if err != nil {
		return "", err
	}
	createdAt := s.nowUTC()
	_, err = s.db.ExecContext(context.Background(), `
INSERT INTO shares (
	token_hash, show, chapter_name, start_seconds, end_seconds,
	start_offset_seconds, end_offset_seconds, segments_json, playlist,
	mode, expires_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sha256hex(token),
		p.Show,
		p.ChapterName,
		p.Start,
		p.End,
		p.StartOffset,
		p.EndOffset,
		string(segments),
		p.Playlist,
		string(p.Mode),
		nullableTimeString(p.ExpiresAt),
		formatShareTime(createdAt),
	)
	if err != nil {
		return "", fmt.Errorf("insert share: %w", err)
	}
	return token, nil
}

// Get returns a snapshot of the share addressed by the raw token.
func (s *SQLiteShareStore) Get(token string) (*share.Share, bool) {
	sh, err := s.getByHash(context.Background(), sha256hex(token))
	if err != nil {
		return nil, false
	}
	return sh, true
}

// List returns all shares, including revoked and expired records, in
// deterministic CreatedAt/TokenHash order.
func (s *SQLiteShareStore) List() []share.ShareSummary {
	rows, err := s.db.QueryContext(context.Background(), `
SELECT token_hash, show, chapter_name, start_seconds, end_seconds,
       start_offset_seconds, end_offset_seconds, segments_json, playlist,
       mode, expires_at, device_hash, claimed_at, revoked_at, created_at
FROM shares
ORDER BY created_at ASC, token_hash ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var summaries []share.ShareSummary
	for rows.Next() {
		sh, err := scanSQLiteShare(rows)
		if err != nil {
			continue
		}
		summaries = append(summaries, summarizeSQLiteShare(sh))
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return summaries
}

// Claim atomically binds a single share to the first claiming device. Public,
// expired, revoked, missing, and already claimed shares are rejected.
func (s *SQLiteShareStore) Claim(token, deviceSecret string) bool {
	now := s.nowUTC()
	res, err := s.db.ExecContext(context.Background(), `
UPDATE shares
SET device_hash = ?, claimed_at = ?
WHERE token_hash = ?
  AND mode = ?
  AND claimed_at IS NULL
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > ?)`,
		sha256hex(deviceSecret),
		formatShareTime(now),
		sha256hex(token),
		string(share.ModeSingle),
		formatShareTime(now),
	)
	if err != nil {
		return false
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1
}

// Revoke marks the raw-token share revoked. Unknown tokens are ignored.
func (s *SQLiteShareStore) Revoke(token string) {
	_, _ = s.RevokeByHash(sha256hex(token))
}

// RevokeByHash marks a share revoked by token hash. It is idempotent.
func (s *SQLiteShareStore) RevokeByHash(tokenHash string) (bool, error) {
	res, err := s.db.ExecContext(context.Background(), `
UPDATE shares
SET revoked_at = COALESCE(revoked_at, ?)
WHERE token_hash = ?`,
		formatShareTime(s.nowUTC()),
		tokenHash,
	)
	if err != nil {
		return false, fmt.Errorf("revoke share: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("revoke share rows affected: %w", err)
	}
	if n > 0 {
		return true, nil
	}
	return s.shareHashExists(context.Background(), tokenHash)
}

// DeleteByHash hard-deletes a share by token hash.
func (s *SQLiteShareStore) DeleteByHash(tokenHash string) (bool, error) {
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM shares WHERE token_hash = ?`, tokenHash)
	if err != nil {
		return false, fmt.Errorf("delete share: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete share rows affected: %w", err)
	}
	return n > 0, nil
}

func (s *SQLiteShareStore) getByHash(ctx context.Context, tokenHash string) (*share.Share, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT token_hash, show, chapter_name, start_seconds, end_seconds,
       start_offset_seconds, end_offset_seconds, segments_json, playlist,
       mode, expires_at, device_hash, claimed_at, revoked_at, created_at
FROM shares
WHERE token_hash = ?`, tokenHash)
	return scanSQLiteShare(row)
}

func (s *SQLiteShareStore) shareHashExists(ctx context.Context, tokenHash string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM shares WHERE token_hash = ?`, tokenHash).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteShareStore) nowUTC() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

type shareScanner interface {
	Scan(dest ...any) error
}

func scanSQLiteShare(scanner shareScanner) (*share.Share, error) {
	var (
		sh                   share.Share
		segmentsJSON, mode   string
		expiresAt            sql.NullString
		deviceHash           sql.NullString
		claimedAt, revokedAt sql.NullString
		createdAt            string
	)
	err := scanner.Scan(
		&sh.TokenHash,
		&sh.Show,
		&sh.ChapterName,
		&sh.Start,
		&sh.End,
		&sh.StartOffset,
		&sh.EndOffset,
		&segmentsJSON,
		&sh.Playlist,
		&mode,
		&expiresAt,
		&deviceHash,
		&claimedAt,
		&revokedAt,
		&createdAt,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(segmentsJSON), &sh.Segments); err != nil {
		return nil, err
	}
	created, err := parseShareTime(createdAt)
	if err != nil {
		return nil, err
	}
	sh.CreatedAt = created
	sh.Mode = share.Mode(mode)
	if deviceHash.Valid {
		sh.DeviceHash = deviceHash.String
	}
	if sh.ExpiresAt, err = parseNullableShareTime(expiresAt); err != nil {
		return nil, err
	}
	if sh.ClaimedAt, err = parseNullableShareTime(claimedAt); err != nil {
		return nil, err
	}
	if sh.RevokedAt, err = parseNullableShareTime(revokedAt); err != nil {
		return nil, err
	}
	sh.Segments = append([]string(nil), sh.Segments...)
	return &sh, nil
}

func summarizeSQLiteShare(sh *share.Share) share.ShareSummary {
	return share.ShareSummary{
		TokenHash:   sh.TokenHash,
		Show:        sh.Show,
		ChapterName: sh.ChapterName,
		Start:       sh.Start,
		End:         sh.End,
		StartOffset: sh.StartOffset,
		EndOffset:   sh.EndOffset,
		Mode:        sh.Mode,
		ExpiresAt:   cloneShareTime(sh.ExpiresAt),
		ClaimedAt:   cloneShareTime(sh.ClaimedAt),
		RevokedAt:   cloneShareTime(sh.RevokedAt),
		CreatedAt:   sh.CreatedAt,
	}
}

func nullableTimeString(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatShareTime(*t)
}

func formatShareTime(t time.Time) string {
	return t.UTC().Format(sqliteShareTimeFormat)
}

func parseNullableShareTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	t, err := parseShareTime(value.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func parseShareTime(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func cloneShareTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

func generateShareSecret() (string, error) {
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
