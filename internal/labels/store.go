package labels

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) sidecarPath(video string) string {
	return filepath.Join(s.cfg.StateDir, "labels", video+".labels.json")
}

func (s *Store) load(video string) (VideoLabels, error) {
	path := s.sidecarPath(video)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return VideoLabels{Video: video}, nil
		}
		return VideoLabels{}, err
	}

	var labels VideoLabels
	if err := json.Unmarshal(data, &labels); err != nil {
		return VideoLabels{}, err
	}
	labels.Video = video
	return labels, nil
}

func (s *Store) save(labels VideoLabels) error {
	if labels.Video == "" {
		return errors.New("labels video is required")
	}

	data, err := json.MarshalIndent(labels, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Join(s.cfg.StateDir, "labels")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	target := s.sidecarPath(labels.Video)
	tmp, err := os.CreateTemp(dir, "."+labels.Video+".*.labels.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("rename labels sidecar: %w", err)
	}
	return nil
}
