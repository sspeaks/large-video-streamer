package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/sspeaks/large-video-streamer/internal/labels"
)

// SQLiteLabelStore persists labels in SQLite.
type SQLiteLabelStore struct {
	db *sql.DB
}

var _ labels.LabelStore = (*SQLiteLabelStore)(nil)

// NewLabelStore returns a SQLite-backed label store using db.
func NewLabelStore(db *sql.DB) *SQLiteLabelStore {
	return &SQLiteLabelStore{db: db}
}

// Load reads labels for video. Missing rows are treated like a missing sidecar.
func (s *SQLiteLabelStore) Load(video string) (labels.VideoLabels, error) {
	if s == nil || s.db == nil {
		return labels.VideoLabels{}, errors.New("sqlite db is nil")
	}

	ctx := context.Background()
	result := labels.VideoLabels{Video: video}

	boundaries, err := s.loadBoundaries(ctx, video)
	if err != nil {
		return labels.VideoLabels{}, err
	}
	result.Boundaries = boundaries

	candidates, err := s.loadCandidates(ctx, video)
	if err != nil {
		return labels.VideoLabels{}, err
	}
	result.Candidates = candidates

	return result, nil
}

// Save replaces all labels for labels.Video in one transaction.
func (s *SQLiteLabelStore) Save(labelDoc labels.VideoLabels) error {
	if labelDoc.Video == "" {
		return errors.New("labels video is required")
	}
	if s == nil || s.db == nil {
		return errors.New("sqlite db is nil")
	}

	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin labels transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM boundaries WHERE video = ?`, labelDoc.Video); err != nil {
		return fmt.Errorf("replace boundaries for %q: %w", labelDoc.Video, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM candidates WHERE video = ?`, labelDoc.Video); err != nil {
		return fmt.Errorf("replace candidates for %q: %w", labelDoc.Video, err)
	}

	for sortPos, boundary := range labelDoc.Boundaries {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO boundaries (video, sort_pos, name, start_seconds)
VALUES (?, ?, ?, ?)`, labelDoc.Video, sortPos, boundary.Name, boundary.Start); err != nil {
			return fmt.Errorf("insert boundary %d for %q: %w", sortPos, labelDoc.Video, err)
		}
	}

	for sortPos, candidate := range labelDoc.Candidates {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO candidates (video, sort_pos, time_seconds, duration_seconds, status)
VALUES (?, ?, ?, ?, ?)`, labelDoc.Video, sortPos, candidate.Time, candidate.Duration, candidate.Status); err != nil {
			return fmt.Errorf("insert candidate %d for %q: %w", sortPos, labelDoc.Video, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit labels transaction: %w", err)
	}
	return nil
}

func (s *SQLiteLabelStore) loadBoundaries(ctx context.Context, video string) ([]labels.Boundary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, start_seconds
FROM boundaries
WHERE video = ?
ORDER BY sort_pos`, video)
	if err != nil {
		return nil, fmt.Errorf("load boundaries for %q: %w", video, err)
	}
	defer rows.Close()

	var boundaries []labels.Boundary
	for rows.Next() {
		var boundary labels.Boundary
		if err := rows.Scan(&boundary.Name, &boundary.Start); err != nil {
			return nil, fmt.Errorf("scan boundary for %q: %w", video, err)
		}
		boundaries = append(boundaries, boundary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load boundaries for %q: %w", video, err)
	}
	return boundaries, nil
}

func (s *SQLiteLabelStore) loadCandidates(ctx context.Context, video string) ([]labels.Candidate, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT time_seconds, duration_seconds, status
FROM candidates
WHERE video = ?
ORDER BY sort_pos`, video)
	if err != nil {
		return nil, fmt.Errorf("load candidates for %q: %w", video, err)
	}
	defer rows.Close()

	var candidates []labels.Candidate
	for rows.Next() {
		var candidate labels.Candidate
		if err := rows.Scan(&candidate.Time, &candidate.Duration, &candidate.Status); err != nil {
			return nil, fmt.Errorf("scan candidate for %q: %w", video, err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load candidates for %q: %w", video, err)
	}
	return candidates, nil
}
