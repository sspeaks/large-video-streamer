package detect

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Silence is a detected quiet interval. Time is the moment audio resumes (the
// silence end), which aligns with a performance start; Duration is the length
// of the silence in seconds.
type Silence struct {
	Time     float64
	Duration float64
}

const (
	DefaultNoiseDB = -35.0
	DefaultMinDur  = 2.0
)

var (
	silenceStartRE = regexp.MustCompile(`silence_start:\s*([-+]?(?:\d+(?:\.\d*)?|\.\d+))`)
	silenceEndRE   = regexp.MustCompile(`silence_end:\s*([-+]?(?:\d+(?:\.\d*)?|\.\d+))\s*\|\s*silence_duration:\s*([-+]?(?:\d+(?:\.\d*)?|\.\d+))`)
)

// DetectSilence returns detected silence intervals from ffmpeg silencedetect.
func DetectSilence(path string, noiseDB float64, minDur float64) ([]Silence, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	var stderr bytes.Buffer
	cmd := exec.Command(
		ffmpeg,
		"-hide_banner",
		"-nostats",
		"-i", path,
		"-vn",
		"-af", fmt.Sprintf("silencedetect=noise=%sdB:d=%s", formatFloat(noiseDB), formatFloat(minDur)),
		"-f", "null",
		"-",
	)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg silencedetect failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return parseSilenceDetect(stderr.String()), nil
}

func parseSilenceDetect(output string) []Silence {
	var silences []Silence
	pendingStart := false

	for _, line := range strings.Split(output, "\n") {
		if silenceStartRE.FindStringSubmatch(line) != nil {
			pendingStart = true
			continue
		}

		matches := silenceEndRE.FindStringSubmatch(line)
		if matches == nil || !pendingStart {
			continue
		}

		end, endErr := strconv.ParseFloat(matches[1], 64)
		duration, durationErr := strconv.ParseFloat(matches[2], 64)
		if endErr != nil || durationErr != nil {
			pendingStart = false
			continue
		}

		silences = append(silences, Silence{Time: end, Duration: duration})
		pendingStart = false
	}

	sort.Slice(silences, func(i, j int) bool {
		return silences[i].Time < silences[j].Time
	})

	return silences
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
