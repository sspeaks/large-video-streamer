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

const (
	legacyLabelSuffix                           = ".labels.json"
	legacyImportKindShares                      = "shares"
	legacyImportKindLabels                      = "labels"
	legacyImportKindMigration                   = "migration"
	legacyImportSourcePreMarkerBackfillRequired = "pre_marker_backfill_required"
)

// ImportLegacyState copies legacy flat-file state into SQLite without removing
// the source files. Durable import markers keep SQLite authoritative after the
// first migration from each legacy source.
func ImportLegacyState(ctx context.Context, db *sql.DB, stateDir string) error {
	if db == nil {
		return errors.New("sqlite db is nil")
	}
	if strings.TrimSpace(stateDir) == "" {
		return errors.New("state dir is required")
	}
	backfillMarkers, err := legacyImportMarkersNeedBackfill(ctx, db)
	if err != nil {
		return err
	}
	backfilledDuringMigration := false
	if err := applyMigrations(ctx, db, func(ctx context.Context, tx *sql.Tx, m migration) error {
		if !backfillMarkers || m.version != legacyImportMarkersMigrationVersion {
			return nil
		}
		backfilledDuringMigration = true
		return recordLegacySourceImports(ctx, tx, stateDir)
	}); err != nil {
		return err
	}
	if backfillMarkers && !backfilledDuringMigration {
		if err := backfillLegacyImportMarkers(ctx, db, stateDir); err != nil {
			return err
		}
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
	sourceID := legacyImportSourceID(path)
	imported, err := legacyImportCompleted(ctx, db, legacyImportKindShares, sourceID)
	if err != nil {
		return err
	}
	if imported {
		return nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy shares %q: %w", path, err)
	}

	var shares []*share.Share
	if strings.TrimSpace(string(data)) != "" {
		if err := json.Unmarshal(data, &shares); err != nil {
			return fmt.Errorf("decode legacy shares %q: %w", path, err)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy shares import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	imported, err = legacyImportCompleted(ctx, tx, legacyImportKindShares, sourceID)
	if err != nil {
		return err
	}
	if imported {
		return nil
	}

	for i, sh := range shares {
		if sh == nil || sh.TokenHash == "" {
			continue
		}
		if err := insertLegacyShare(ctx, tx, sh); err != nil {
			return fmt.Errorf("import legacy share %d (%s): %w", i, sh.TokenHash, err)
		}
	}
	if err := recordLegacyImport(ctx, tx, legacyImportKindShares, sourceID); err != nil {
		return err
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
	sourceID := legacyImportSourceID(dir)
	imported, err := legacyImportCompleted(ctx, db, legacyImportKindLabels, sourceID)
	if err != nil {
		return err
	}
	if imported {
		return nil
	}

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

	imported, err = legacyImportCompleted(ctx, tx, legacyImportKindLabels, sourceID)
	if err != nil {
		return err
	}
	if imported {
		return nil
	}

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
	if err := recordLegacyImport(ctx, tx, legacyImportKindLabels, sourceID); err != nil {
		return err
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
		sourcesJSON, err := marshalCandidateSources(candidate.Sources)
		if err != nil {
			return fmt.Errorf("encode legacy candidate %d sources for %q: %w", sortPos, doc.Video, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO candidates (video, sort_pos, time_seconds, duration_seconds, status, sources_json, confidence, suggested_name, conflict)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			doc.Video,
			sortPos,
			candidate.Time,
			candidate.Duration,
			candidate.Status,
			sourcesJSON,
			candidate.Confidence,
			candidate.SuggestedName,
			boolInt(candidate.Conflict),
		); err != nil {
			return fmt.Errorf("insert legacy candidate %d for %q: %w", sortPos, doc.Video, err)
		}
	}
	for sortPos, name := range doc.Lineup {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO lineup (video, sort_pos, name)
VALUES (?, ?, ?)`, doc.Video, sortPos, name); err != nil {
			return fmt.Errorf("insert legacy lineup entry %d for %q: %w", sortPos, doc.Video, err)
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

type legacyImportMarkerReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type legacyImportMarkerWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func legacyImportMarkersNeedBackfill(ctx context.Context, db *sql.DB) (bool, error) {
	required, err := legacyImportBackfillRequired(ctx, db)
	if err != nil {
		return false, err
	}
	if required {
		return true, nil
	}

	var tableName string
	err = db.QueryRowContext(ctx, `
SELECT name
FROM sqlite_master
WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&tableName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check schema_migrations table for legacy import markers: %w", err)
	}

	var markerApplied int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM schema_migrations
WHERE version = ?`, legacyImportMarkersMigrationVersion).Scan(&markerApplied); err != nil {
		return false, fmt.Errorf("check legacy import marker migration: %w", err)
	}
	if markerApplied > 0 {
		return false, nil
	}

	var previousMigrations int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM schema_migrations
WHERE version < ?`, legacyImportMarkersMigrationVersion).Scan(&previousMigrations); err != nil {
		return false, fmt.Errorf("check previous migrations for legacy import marker backfill: %w", err)
	}
	return previousMigrations > 0, nil
}

func legacyImportBackfillRequired(ctx context.Context, db *sql.DB) (bool, error) {
	var tableName string
	err := db.QueryRowContext(ctx, `
SELECT name
FROM sqlite_master
WHERE type = 'table' AND name = 'legacy_imports'`).Scan(&tableName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check legacy_imports table for backfill sentinel: %w", err)
	}
	return legacyImportCompleted(ctx, db, legacyImportKindMigration, legacyImportSourcePreMarkerBackfillRequired)
}

func backfillLegacyImportMarkers(ctx context.Context, db *sql.DB, stateDir string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy import marker backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := recordLegacySourceImports(ctx, tx, stateDir); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy import marker backfill: %w", err)
	}
	return nil
}

func recordLegacySourceImports(ctx context.Context, writer legacyImportMarkerWriter, stateDir string) error {
	if err := recordLegacyImport(ctx, writer, legacyImportKindShares, legacyImportSourceID(filepath.Join(stateDir, "shares.json"))); err != nil {
		return err
	}
	if err := recordLegacyImport(ctx, writer, legacyImportKindLabels, legacyImportSourceID(filepath.Join(stateDir, "labels"))); err != nil {
		return err
	}
	if err := clearLegacyImport(ctx, writer, legacyImportKindMigration, legacyImportSourcePreMarkerBackfillRequired); err != nil {
		return err
	}
	return nil
}

func legacyImportSourceID(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func legacyImportCompleted(ctx context.Context, reader legacyImportMarkerReader, kind, sourceID string) (bool, error) {
	var exists int
	err := reader.QueryRowContext(ctx, `
SELECT 1
FROM legacy_imports
WHERE source_kind = ? AND source_id = ?`, kind, sourceID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check legacy import marker %s %q: %w", kind, sourceID, err)
	}
	return true, nil
}

func recordLegacyImport(ctx context.Context, writer legacyImportMarkerWriter, kind, sourceID string) error {
	if _, err := writer.ExecContext(ctx, `
INSERT OR IGNORE INTO legacy_imports (source_kind, source_id)
VALUES (?, ?)`, kind, sourceID); err != nil {
		return fmt.Errorf("record legacy import marker %s %q: %w", kind, sourceID, err)
	}
	return nil
}

func clearLegacyImport(ctx context.Context, writer legacyImportMarkerWriter, kind, sourceID string) error {
	if _, err := writer.ExecContext(ctx, `
DELETE FROM legacy_imports
WHERE source_kind = ? AND source_id = ?`, kind, sourceID); err != nil {
		return fmt.Errorf("clear legacy import marker %s %q: %w", kind, sourceID, err)
	}
	return nil
}

func nullableLegacyString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
