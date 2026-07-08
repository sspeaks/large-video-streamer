package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestOpenConfiguresSQLite(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(testDir(t), "app.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
	var foreignKeys int
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys error = %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	var busyTimeout int
	if err := db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("PRAGMA busy_timeout error = %v", err)
	}
	if busyTimeout != busyTimeoutMillis {
		t.Fatalf("busy_timeout = %d, want %d", busyTimeout, busyTimeoutMillis)
	}
	var journalMode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode error = %v", err)
	}
	if strings.ToLower(journalMode) != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
}

func TestApplyMigrationsCreatesSchemaIdempotently(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(testDir(t), "app.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	for i := 0; i < 2; i++ {
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatalf("ApplyMigrations() pass %d error = %v", i+1, err)
		}
	}

	wantTables := []string{"boundaries", "candidates", "schema_migrations", "shares"}
	if got := tableNames(t, ctx, db); strings.Join(got, ",") != strings.Join(wantTables, ",") {
		t.Fatalf("tables = %v, want %v", got, wantTables)
	}

	var migrationCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count schema_migrations error = %v", err)
	}
	if migrationCount != len(migrations) {
		t.Fatalf("migration count = %d, want %d", migrationCount, len(migrations))
	}

	assertColumns(t, ctx, db, "shares", "token_hash", "show", "chapter_name", "segments_json", "mode", "created_at")
	assertColumns(t, ctx, db, "boundaries", "video", "sort_pos", "name", "start_seconds")
	assertColumns(t, ctx, db, "candidates", "video", "sort_pos", "time_seconds", "duration_seconds", "status")
}

func testDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".store-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	return abs
}

func tableNames(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT name
FROM sqlite_master
WHERE type = 'table'
  AND name IN ('schema_migrations', 'shares', 'boundaries', 'candidates')
ORDER BY name`)
	if err != nil {
		t.Fatalf("query sqlite_master error = %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name error = %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table rows error = %v", err)
	}
	return names
}

func assertColumns(t *testing.T, ctx context.Context, db *sql.DB, table string, want ...string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s) error = %v", table, err)
	}
	defer rows.Close()

	got := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info(%s) error = %v", table, err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s) rows error = %v", table, err)
	}

	missing := make([]string, 0)
	for _, column := range want {
		if !got[column] {
			missing = append(missing, column)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("%s missing columns %v", table, missing)
	}
}
