package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/labels"
)

func TestLabelStoreLoadMissingReturnsFreshLabels(t *testing.T) {
	_, db, store := newTestLabelStore(t)
	defer db.Close()

	got, err := store.Load("missing_video")
	if err != nil {
		t.Fatalf("Load missing labels returned error: %v", err)
	}
	if got.Video != "missing_video" || len(got.Boundaries) != 0 || len(got.Candidates) != 0 {
		t.Fatalf("Load missing labels = %#v, want fresh labels for missing_video", got)
	}
}

func TestLabelStoreSaveLoadRoundTripPreservesOrderAndCandidates(t *testing.T) {
	_, db, store := newTestLabelStore(t)
	defer db.Close()

	want := labels.VideoLabels{
		Video: "demo",
		Boundaries: []labels.Boundary{
			{Name: "third in file", Start: 30},
			{Name: "first by time", Start: 10},
			{Name: "second by time", Start: 20},
		},
		Candidates: []labels.Candidate{
			{
				Time:          12.5,
				Duration:      1.25,
				Status:        "candidate",
				Sources:       []string{"silence", "lineup", "ocr"},
				Confidence:    0.91,
				SuggestedName: "Quartet A",
				Conflict:      true,
			},
			{Time: 90, Duration: 0.5, Status: "rejected"},
			{Time: 45.75, Duration: 2, Status: "named"},
			{Time: 60, Duration: 3.5, Status: ""},
			{Time: 75, Duration: 4.25, Status: "custom-status"},
		},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	got, err := store.Load("demo")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %#v, want %#v", got, want)
	}
}

func TestLabelStoreSaveReplacesWholeDocument(t *testing.T) {
	_, db, store := newTestLabelStore(t)
	defer db.Close()

	first := labels.VideoLabels{
		Video: "demo",
		Boundaries: []labels.Boundary{
			{Name: "stale-a", Start: 1},
			{Name: "stale-b", Start: 2},
		},
		Candidates: []labels.Candidate{
			{Time: 10, Duration: 1, Status: "candidate"},
			{Time: 20, Duration: 2, Status: "rejected"},
		},
	}
	if err := store.Save(first); err != nil {
		t.Fatalf("Save first returned error: %v", err)
	}

	want := labels.VideoLabels{
		Video:      "demo",
		Boundaries: []labels.Boundary{{Name: "fresh", Start: 3}},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save replacement returned error: %v", err)
	}

	got, err := store.Load("demo")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load after replacement = %#v, want %#v", got, want)
	}
}

func TestLabelStoreSaveRequiresVideo(t *testing.T) {
	_, db, store := newTestLabelStore(t)
	defer db.Close()

	if err := store.Save(labels.VideoLabels{}); err == nil {
		t.Fatal("Save without video returned nil error, want error")
	}
}

func TestLabelStoreSaveRollsBackOnInsertError(t *testing.T) {
	ctx, db, store := newTestLabelStore(t)
	defer db.Close()

	want := labels.VideoLabels{
		Video:      "demo",
		Boundaries: []labels.Boundary{{Name: "keep", Start: 1}},
		Candidates: []labels.Candidate{{Time: 10, Duration: 1, Status: "candidate"}},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save fixture returned error: %v", err)
	}

	_, err := db.ExecContext(ctx, `
CREATE TRIGGER fail_bad_boundary
BEFORE INSERT ON boundaries
WHEN NEW.name = 'boom'
BEGIN
	SELECT RAISE(ABORT, 'boom boundary');
END`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	bad := labels.VideoLabels{
		Video:      "demo",
		Boundaries: []labels.Boundary{{Name: "boom", Start: 2}},
		Candidates: []labels.Candidate{{Time: 20, Duration: 2, Status: "rejected"}},
	}
	if err := store.Save(bad); err == nil {
		t.Fatal("Save with failing trigger returned nil error, want error")
	}

	got, err := store.Load("demo")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load after failed save = %#v, want %#v", got, want)
	}
}

func TestLabelStoreConcurrentAccess(t *testing.T) {
	_, db, store := newTestLabelStore(t)
	defer db.Close()

	const workers = 8
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := labels.VideoLabels{
				Video:      fmt.Sprintf("video-%02d", i),
				Boundaries: []labels.Boundary{{Name: fmt.Sprintf("chapter-%02d", i), Start: float64(i)}},
				Candidates: []labels.Candidate{{Time: float64(i) * 10, Duration: 1.5, Status: "candidate"}},
			}
			if err := store.Save(want); err != nil {
				errs <- fmt.Errorf("save %s: %w", want.Video, err)
				return
			}
			got, err := store.Load(want.Video)
			if err != nil {
				errs <- fmt.Errorf("load %s: %w", want.Video, err)
				return
			}
			if !reflect.DeepEqual(got, want) {
				errs <- fmt.Errorf("load %s = %#v, want %#v", want.Video, got, want)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func newTestLabelStore(t *testing.T) (context.Context, *sql.DB, *SQLiteLabelStore) {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(testDir(t), "labels.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := ApplyMigrations(ctx, db); err != nil {
		_ = db.Close()
		t.Fatalf("ApplyMigrations() error = %v", err)
	}
	return ctx, db, NewLabelStore(db)
}
