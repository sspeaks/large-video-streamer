package labels

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	bareWordRE  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	timestampRE = regexp.MustCompile(`^(\d+):(\d{2}):(\d{2})$`)
)

func importTimestamps(r io.Reader) (VideoLabels, error) {
	var labels VideoLabels
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			filename := strings.TrimSpace(strings.TrimPrefix(line, ">"))
			if filename != "" {
				base := filepath.Base(filename)
				labels.Video = strings.TrimSuffix(base, filepath.Ext(base))
			}
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 1 && bareWordRE.MatchString(fields[0]) {
			continue
		}
		if len(fields) < 2 || len(fields) > 3 || !bareWordRE.MatchString(fields[0]) {
			return VideoLabels{}, fmt.Errorf("invalid timestamp line %d: %q", lineNo, scanner.Text())
		}
		start, err := parseClockSeconds(fields[1])
		if err != nil {
			return VideoLabels{}, fmt.Errorf("invalid start timestamp on line %d: %w", lineNo, err)
		}
		if len(fields) == 3 {
			if _, err := parseClockSeconds(fields[2]); err != nil {
				return VideoLabels{}, fmt.Errorf("invalid stop timestamp on line %d: %w", lineNo, err)
			}
		}
		labels.Boundaries = append(labels.Boundaries, Boundary{Name: fields[0], Start: start})
	}
	if err := scanner.Err(); err != nil {
		return VideoLabels{}, err
	}
	return labels, nil
}

func exportTimestamps(labels VideoLabels) string {
	var b strings.Builder
	if labels.Video != "" {
		fmt.Fprintf(&b, "> %s.mkv\n", labels.Video)
	}

	boundaries := sortedBoundaries(labels.Boundaries)
	width := 0
	for _, boundary := range boundaries {
		if len(boundary.Name) > width {
			width = len(boundary.Name)
		}
	}
	for _, boundary := range boundaries {
		if width > 0 {
			fmt.Fprintf(&b, "%-*s %s\n", width, boundary.Name, secondsToClock(boundary.Start))
		}
	}
	return b.String()
}

func parseClockSeconds(value string) (float64, error) {
	matches := timestampRE.FindStringSubmatch(value)
	if matches == nil {
		return 0, fmt.Errorf("%q is not HH:MM:SS", value)
	}
	hours, _ := strconv.Atoi(matches[1])
	minutes, _ := strconv.Atoi(matches[2])
	seconds, _ := strconv.Atoi(matches[3])
	return float64(hours*3600 + minutes*60 + seconds), nil
}

func secondsToClock(seconds float64) string {
	total := int(seconds + 0.5)
	if total < 0 {
		total = 0
	}
	hours := total / 3600
	minutes := (total % 3600) / 60
	secs := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, secs)
}

func sortedBoundaries(boundaries []Boundary) []Boundary {
	sorted := append([]Boundary(nil), boundaries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Start < sorted[j].Start
	})
	return sorted
}
