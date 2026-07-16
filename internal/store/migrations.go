package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type migration struct {
	version    int
	name       string
	statements []string
}

type migrationHook func(context.Context, *sql.Tx, migration) error

const legacyImportMarkersMigrationVersion = 6

const createSchemaMigrations = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
)`

var migrations = []migration{
	{
		version: 1,
		name:    "schema_migrations",
		statements: []string{
			createSchemaMigrations,
		},
	},
	{
		version: 2,
		name:    "shares",
		statements: []string{
			`
CREATE TABLE IF NOT EXISTS shares (
	token_hash TEXT PRIMARY KEY,
	show TEXT NOT NULL,
	chapter_name TEXT NOT NULL,
	start_seconds REAL NOT NULL,
	end_seconds REAL NOT NULL,
	start_offset_seconds REAL NOT NULL DEFAULT 0,
	end_offset_seconds REAL NOT NULL DEFAULT 0,
	segments_json TEXT NOT NULL DEFAULT '[]',
	playlist TEXT NOT NULL,
	mode TEXT NOT NULL DEFAULT 'single' CHECK (mode IN ('single', 'public')),
	expires_at TEXT,
	device_hash TEXT,
	claimed_at TEXT,
	revoked_at TEXT,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	CHECK (end_seconds >= start_seconds)
)`,
			`CREATE INDEX IF NOT EXISTS shares_show_idx ON shares(show)`,
			`CREATE INDEX IF NOT EXISTS shares_expires_at_idx ON shares(expires_at)`,
		},
	},
	{
		version: 3,
		name:    "boundaries",
		statements: []string{
			`
CREATE TABLE IF NOT EXISTS boundaries (
	video TEXT NOT NULL,
	sort_pos INTEGER NOT NULL CHECK (sort_pos >= 0),
	name TEXT NOT NULL,
	start_seconds REAL NOT NULL,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	PRIMARY KEY (video, sort_pos)
)`,
			`CREATE INDEX IF NOT EXISTS boundaries_video_start_idx ON boundaries(video, start_seconds)`,
		},
	},
	{
		version: 4,
		name:    "candidates",
		statements: []string{
			`
CREATE TABLE IF NOT EXISTS candidates (
	video TEXT NOT NULL,
	sort_pos INTEGER NOT NULL CHECK (sort_pos >= 0),
	time_seconds REAL NOT NULL,
	duration_seconds REAL NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	PRIMARY KEY (video, sort_pos)
)`,
			`CREATE INDEX IF NOT EXISTS candidates_video_time_idx ON candidates(video, time_seconds)`,
		},
	},
	{
		version: 5,
		name:    "candidate_metadata",
		statements: []string{
			`ALTER TABLE candidates ADD COLUMN sources_json TEXT NOT NULL DEFAULT '[]'`,
			`ALTER TABLE candidates ADD COLUMN confidence REAL NOT NULL DEFAULT 0`,
			`ALTER TABLE candidates ADD COLUMN suggested_name TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE candidates ADD COLUMN conflict INTEGER NOT NULL DEFAULT 0`,
		},
	},
	{
		version: legacyImportMarkersMigrationVersion,
		name:    "legacy_import_markers",
		statements: []string{
			`
CREATE TABLE IF NOT EXISTS legacy_imports (
	source_kind TEXT NOT NULL,
	source_id TEXT NOT NULL,
	imported_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	PRIMARY KEY (source_kind, source_id)
)`,
		},
	},
	{
		version: 7,
		name:    "lineup",
		statements: []string{
			`
CREATE TABLE IF NOT EXISTS lineup (
	video TEXT NOT NULL,
	sort_pos INTEGER NOT NULL CHECK (sort_pos >= 0),
	name TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	PRIMARY KEY (video, sort_pos)
)`,
		},
	},
}

// ApplyMigrations applies all known schema migrations exactly once.
func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	return applyMigrations(ctx, db, nil)
}

func applyMigrations(ctx context.Context, db *sql.DB, hook migrationHook) error {
	if db == nil {
		return errors.New("sqlite db is nil")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	preExistingMigrations, err := schemaMigrationCount(ctx, tx)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, createSchemaMigrations); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, m := range migrations {
		applied, err := migrationApplied(ctx, tx, m)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		for _, stmt := range m.statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("apply migration %d %s: %w", m.version, m.name, err)
			}
		}
		if hook != nil {
			if err := hook(ctx, tx, m); err != nil {
				return err
			}
		} else if m.version == legacyImportMarkersMigrationVersion && preExistingMigrations > 0 {
			if err := recordLegacyImport(ctx, tx, legacyImportKindMigration, legacyImportSourcePreMarkerBackfillRequired); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, m.version, m.name); err != nil {
			return fmt.Errorf("record migration %d %s: %w", m.version, m.name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite migrations: %w", err)
	}
	return nil
}

func schemaMigrationCount(ctx context.Context, tx *sql.Tx) (int, error) {
	var tableName string
	err := tx.QueryRowContext(ctx, `
SELECT name
FROM sqlite_master
WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&tableName)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("check schema_migrations table: %w", err)
	}

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count schema_migrations: %w", err)
	}
	return count, nil
}

func migrationApplied(ctx context.Context, tx *sql.Tx, m migration) (bool, error) {
	var name string
	err := tx.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE version = ?`, m.version).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read migration %d: %w", m.version, err)
	}
	if name != m.name {
		return false, fmt.Errorf("migration %d is %q, want %q", m.version, name, m.name)
	}
	return true, nil
}
