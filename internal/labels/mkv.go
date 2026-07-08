package labels

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type ffprobeChapters struct {
	Chapters []struct {
		StartTime string            `json:"start_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

// ImportMKVChapters reads MKV chapter starts and titles using ffprobe.
func (s *Store) ImportMKVChapters(mkvPath string) ([]Boundary, error) {
	return importMKVChapters(mkvPath)
}

func importMKVChapters(mkvPath string) ([]Boundary, error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe is required to import MKV chapters: %w", err)
	}
	cmd := exec.Command(ffprobe, "-v", "quiet", "-print_format", "json", "-show_chapters", mkvPath)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe chapters for %q: %w", mkvPath, err)
	}

	var probed ffprobeChapters
	if err := json.Unmarshal(out, &probed); err != nil {
		return nil, fmt.Errorf("parse ffprobe chapters JSON: %w", err)
	}
	boundaries := make([]Boundary, 0, len(probed.Chapters))
	for i, chapter := range probed.Chapters {
		start, err := strconv.ParseFloat(chapter.StartTime, 64)
		if err != nil {
			return nil, fmt.Errorf("parse chapter %d start_time %q: %w", i+1, chapter.StartTime, err)
		}
		boundaries = append(boundaries, Boundary{Name: chapter.Tags["title"], Start: start})
	}
	sortBoundariesInPlace(boundaries)
	return boundaries, nil
}

// ExportMKVChapters embeds boundaries into an MKV using mkvpropedit.
func (s *Store) ExportMKVChapters(mkvPath string, boundaries []Boundary) error {
	return exportMKVChapters(mkvPath, boundaries)
}

func exportMKVChapters(mkvPath string, boundaries []Boundary) error {
	mkvpropedit, err := exec.LookPath("mkvpropedit")
	if err != nil {
		return fmt.Errorf("mkvpropedit is required to export MKV chapters: %w", err)
	}
	chapterFile, err := os.CreateTemp(filepath.Dir(mkvPath), ".chapters-*.txt")
	if err != nil {
		return fmt.Errorf("create chapter file: %w", err)
	}
	chapterPath := chapterFile.Name()
	defer func() { _ = os.Remove(chapterPath) }()

	chapterText, err := formatOGMChapters(boundaries)
	if err != nil {
		_ = chapterFile.Close()
		return err
	}
	if _, err := chapterFile.WriteString(chapterText); err != nil {
		_ = chapterFile.Close()
		return fmt.Errorf("write chapter file: %w", err)
	}
	if err := chapterFile.Close(); err != nil {
		return fmt.Errorf("close chapter file: %w", err)
	}

	cmd := exec.Command(mkvpropedit, mkvPath, "--chapters", chapterPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkvpropedit chapters for %q: %w: %s", mkvPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func formatOGMChapters(boundaries []Boundary) (string, error) {
	if err := validateBoundaryNames(boundaries); err != nil {
		return "", err
	}
	boundaries = sortedBoundaries(boundaries)
	var b strings.Builder
	for i, boundary := range boundaries {
		fmt.Fprintf(&b, "CHAPTER%02d=%s\n", i+1, secondsToOGMTime(boundary.Start))
		fmt.Fprintf(&b, "CHAPTER%02dNAME=%s\n", i+1, boundary.Name)
	}
	return b.String(), nil
}

func secondsToOGMTime(seconds float64) string {
	milliseconds := int(seconds*1000 + 0.5)
	if milliseconds < 0 {
		milliseconds = 0
	}
	hours := milliseconds / 3_600_000
	milliseconds %= 3_600_000
	minutes := milliseconds / 60_000
	milliseconds %= 60_000
	secs := milliseconds / 1_000
	milliseconds %= 1_000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, milliseconds)
}
