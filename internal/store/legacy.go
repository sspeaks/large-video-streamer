package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sspeaks/large-video-streamer/internal/labels"
	"github.com/sspeaks/large-video-streamer/internal/share"
)

const legacyLabelSuffix = ".labels.json"

// ImportLegacyState copies legacy flat-file state into SQLite without removing
// the source files, so operators can roll back to the flat-file stores.
func ImportLegacyState(ctx context.Context, db *sql.DB, stateDir string) error {
	if db == nil {
		return errors.New("sqlite db is nil")
	}
	if strings.TrimSpace(stateDir) == "" {
		return errors.New("state dir is required")
	}
	if err := ApplyMigrations(ctx, db); err != nil {
		return err
	}
	if err := importLegacyShares(ctx, db, filepath.Join(stateDir, "shares.json")); err != nil {
		return err
	}
	if err := importLegacyLabels(ctx, db, filepath.Join(stateDir, "labels")); err != nil {
		return err
	}
	return nil
}

func importLegacyShares(ctx context.Context, db *sql.DB, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy shares %q: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}

	var shares []*share.Share
	if err := json.Unmarshal(data, &shares); err != nil {
		return fmt.Errorf("decode legacy shares %q: %w", path, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy shares import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i, sh := range shares {
		if sh == nil || sh.TokenHash == "" {
			continue
		}
		if err := insertLegacyShare(ctx, tx, sh); err != nil {
			return fmt.Errorf("import legacy share %d (%s): %w", i, sh.TokenHash, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy shares import: %w", err)
	}
	return nil
}

func insertLegacyShare(ctx context.Context, tx *sql.Tx, sh *share.Share) error {
	mode, err := normalizedShareMode(sh.Mode)
	if err != nil {
		return err
	}
	segments, err := json.Marshal(append([]string(nil), sh.Segments...))
	if err != nil {
		return fmt.Errorf("marshal share segments: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
INSERT OR IGNORE INTO shares (
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
		string(segments),
		sh.Playlist,
		string(mode),
		nullableTimeString(sh.ExpiresAt),
		nullableLegacyString(sh.DeviceHash),
		nullableTimeString(sh.ClaimedAt),
		nullableTimeString(sh.RevokedAt),
		formatShareTime(sh.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert legacy share: %w", err)
	}
	return nil
}

func normalizedShareMode(mode share.Mode) (share.Mode, error) {
	if mode == "" {
		return share.ModeSingle, nil
	}
	if mode != share.ModeSingle && mode != share.ModePublic {
		return "", fmt.Errorf("invalid legacy share mode %q", mode)
	}
	return mode, nil
}

func importLegacyLabels(ctx context.Context, db *sql.DB, dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy labels dir %q: %w", dir, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy labels import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), legacyLabelSuffix) {
			continue
		}
		video := strings.TrimSuffix(entry.Name(), legacyLabelSuffix)
		if video == "" {
			continue
		}
		doc, err := readLegacyLabels(filepath.Join(dir, entry.Name()), video)
		if err != nil {
			return err
		}
		if err := insertLegacyLabels(ctx, tx, doc); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy labels import: %w", err)
	}
	return nil
}

func readLegacyLabels(path, video string) (labels.VideoLabels, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return labels.VideoLabels{}, fmt.Errorf("read legacy labels %q: %w", path, err)
	}
	var doc labels.VideoLabels
	if err := json.Unmarshal(data, &doc); err != nil {
		return labels.VideoLabels{}, fmt.Errorf("decode legacy labels %q: %w", path, err)
	}
	doc.Video = video
	return doc, nil
}

func insertLegacyLabels(ctx context.Context, tx *sql.Tx, doc labels.VideoLabels) error {
	exists, err := legacyLabelsExist(ctx, tx, doc.Video)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	for sortPos, boundary := range doc.Boundaries {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO boundaries (video, sort_pos, name, start_seconds)
VALUES (?, ?, ?, ?)`, doc.Video, sortPos, boundary.Name, boundary.Start); err != nil {
			return fmt.Errorf("insert legacy boundary %d for %q: %w", sortPos, doc.Video, err)
		}
	}
	for sortPos, candidate := range doc.Candidates {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO candidates (video, sort_pos, time_seconds, duration_seconds, status)
VALUES (?, ?, ?, ?, ?)`, doc.Video, sortPos, candidate.Time, candidate.Duration, candidate.Status); err != nil {
			return fmt.Errorf("insert legacy candidate %d for %q: %w", sortPos, doc.Video, err)
		}
	}
	return nil
}

func legacyLabelsExist(ctx context.Context, tx *sql.Tx, video string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
SELECT CASE WHEN
	EXISTS (SELECT 1 FROM boundaries WHERE video = ?)
	OR EXISTS (SELECT 1 FROM candidates WHERE video = ?)
THEN 1 ELSE 0 END`, video, video).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check legacy labels for %q: %w", video, err)
	}
	return exists == 1, nil
}

func nullableLegacyString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
