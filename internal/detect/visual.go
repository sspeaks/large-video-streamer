package detect

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SceneChange is a video scene change reported by ffmpeg's scdet filter.
type SceneChange struct {
	Time  float64
	Score float64
}

// ColorSample is a single signalstats sample of frame YUV mean values.
type ColorSample struct {
	Time  float64
	YMean float64
	UMean float64
	VMean float64
}

// ColorShift is a sustained change in sampled frame color.
type ColorShift struct {
	Time  float64
	Delta float64
}

const visualFloatPattern = `[-+]?(?:\d+(?:\.\d*)?|\.\d+)`

var (
	frameTimeRE  = regexp.MustCompile(`(?:^|\s)pts_time:(` + visualFloatPattern + `)(?:\s|$)`)
	leadingFloat = regexp.MustCompile(`^` + visualFloatPattern)
)

// DetectSceneChanges returns scene changes from ffmpeg's scdet filter.
func DetectSceneChanges(path string, threshold float64) ([]SceneChange, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	filter := fmt.Sprintf(
		"scdet=threshold=%s,metadata=mode=print:file=-",
		formatFloat(threshold),
	)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(
		ffmpeg,
		"-hide_banner",
		"-nostats",
		"-i", path,
		"-an",
		"-vf", filter,
		"-f", "null",
		"-",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg scdet failed: %w: %s", err, ffmpegErrorText(stdout.String(), stderr.String()))
	}

	return parseSceneChanges(stdout.String() + "\n" + stderr.String()), nil
}

// SampleFrameColors returns ffmpeg signalstats YUV mean samples.
func SampleFrameColors(path string, sampleRate float64, crop string) ([]ColorSample, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(
		ffmpeg,
		"-hide_banner",
		"-nostats",
		"-i", path,
		"-an",
		"-vf", buildColorFilterChain(sampleRate, crop),
		"-f", "null",
		"-",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg signalstats failed: %w: %s", err, ffmpegErrorText(stdout.String(), stderr.String()))
	}

	return parseColorSamples(stdout.String() + "\n" + stderr.String()), nil
}

// DetectColorShifts returns sustained YUV mean changes across adjacent windows.
func DetectColorShifts(samples []ColorSample, threshold float64, windowSeconds float64) []ColorShift {
	if len(samples) < 2 {
		return nil
	}

	sortedSamples := append([]ColorSample(nil), samples...)
	sort.Slice(sortedSamples, func(i, j int) bool {
		return sortedSamples[i].Time < sortedSamples[j].Time
	})

	if windowSeconds <= 0 {
		return detectAdjacentColorShifts(sortedSamples, threshold)
	}

	var candidates []ColorShift
	for _, pivot := range sortedSamples {
		before, beforeOK := averageColorWindow(sortedSamples, pivot.Time-windowSeconds, pivot.Time)
		after, afterOK := averageColorWindow(sortedSamples, pivot.Time, pivot.Time+windowSeconds)
		if !beforeOK || !afterOK {
			continue
		}

		delta := colorDistance(before, after)
		if delta >= threshold {
			candidates = append(candidates, ColorShift{Time: pivot.Time, Delta: delta})
		}
	}

	return collapseColorShifts(candidates, windowSeconds)
}

type sceneFrame struct {
	time     float64
	score    float64
	hasTime  bool
	hasScore bool
}

func parseSceneChanges(output string) []SceneChange {
	var changes []SceneChange
	var current sceneFrame

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if timeValue, ok := parseFrameTime(line); ok {
			changes = appendSceneFrame(changes, current)
			current = sceneFrame{time: timeValue, hasTime: true}
			continue
		}

		if score, scoreOK := parseMetadataNumber(line, "lavfi.scd.score"); scoreOK {
			if timeValue, timeOK := parseMetadataNumber(line, "lavfi.scd.time"); timeOK {
				changes = appendUniqueSceneChange(changes, SceneChange{Time: timeValue, Score: score})
				continue
			}
			current.score = score
			current.hasScore = true
			continue
		}

		if timeValue, ok := parseMetadataNumber(line, "lavfi.scd.time"); ok {
			current.time = timeValue
			current.hasTime = true
		}
	}

	changes = appendSceneFrame(changes, current)
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Time < changes[j].Time
	})

	return changes
}

func appendSceneFrame(changes []SceneChange, frame sceneFrame) []SceneChange {
	if !frame.hasScore || !frame.hasTime || frame.score <= 0 {
		return changes
	}
	return appendUniqueSceneChange(changes, SceneChange{Time: frame.time, Score: frame.score})
}

func appendUniqueSceneChange(changes []SceneChange, change SceneChange) []SceneChange {
	for _, existing := range changes {
		if math.Abs(existing.Time-change.Time) < 0.000001 && math.Abs(existing.Score-change.Score) < 0.000001 {
			return changes
		}
	}
	return append(changes, change)
}

type colorFrame struct {
	sample  ColorSample
	hasTime bool
	hasY    bool
	hasU    bool
	hasV    bool
}

func parseColorSamples(output string) []ColorSample {
	var samples []ColorSample
	var current colorFrame

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if timeValue, ok := parseFrameTime(line); ok {
			samples = appendColorFrame(samples, current)
			current = colorFrame{sample: ColorSample{Time: timeValue}, hasTime: true}
			continue
		}

		if value, ok := parseMetadataNumber(line, "lavfi.signalstats.YAVG"); ok {
			current.sample.YMean = value
			current.hasY = true
			continue
		}
		if value, ok := parseMetadataNumber(line, "lavfi.signalstats.UAVG"); ok {
			current.sample.UMean = value
			current.hasU = true
			continue
		}
		if value, ok := parseMetadataNumber(line, "lavfi.signalstats.VAVG"); ok {
			current.sample.VMean = value
			current.hasV = true
			continue
		}
	}

	samples = appendColorFrame(samples, current)
	sort.Slice(samples, func(i, j int) bool {
		return samples[i].Time < samples[j].Time
	})

	return samples
}

func appendColorFrame(samples []ColorSample, frame colorFrame) []ColorSample {
	if !frame.hasTime || !frame.hasY || !frame.hasU || !frame.hasV {
		return samples
	}
	return append(samples, frame.sample)
}

func buildColorFilterChain(sampleRate float64, crop string) string {
	var filters []string
	if crop = strings.TrimSpace(crop); crop != "" {
		filters = append(filters, normalizeCropFilter(crop))
	}
	if sampleRate > 0 {
		filters = append(filters, "fps="+formatFloat(sampleRate))
	}
	filters = append(filters, "signalstats", "metadata=mode=print:file=-")
	return strings.Join(filters, ",")
}

func normalizeCropFilter(crop string) string {
	if strings.HasPrefix(crop, "crop") {
		return crop
	}
	return "crop=" + crop
}

func parseFrameTime(line string) (float64, bool) {
	if !strings.HasPrefix(line, "frame:") {
		return 0, false
	}

	matches := frameTimeRE.FindStringSubmatch(line)
	if matches == nil {
		return 0, false
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, false
	}

	return value, true
}

func parseMetadataNumber(line string, key string) (float64, bool) {
	index := strings.Index(line, key)
	if index < 0 {
		return 0, false
	}

	rest := strings.TrimSpace(line[index+len(key):])
	if rest == "" || (rest[0] != '=' && rest[0] != ':') {
		return 0, false
	}

	matches := leadingFloat.FindStringSubmatch(strings.TrimSpace(rest[1:]))
	if matches == nil {
		return 0, false
	}

	value, err := strconv.ParseFloat(matches[0], 64)
	if err != nil {
		return 0, false
	}

	return value, true
}

type colorMean struct {
	y float64
	u float64
	v float64
}

func averageColorWindow(samples []ColorSample, start float64, end float64) (colorMean, bool) {
	var total colorMean
	count := 0

	for _, sample := range samples {
		if sample.Time < start || sample.Time >= end {
			continue
		}
		total.y += sample.YMean
		total.u += sample.UMean
		total.v += sample.VMean
		count++
	}

	if count == 0 {
		return colorMean{}, false
	}

	divisor := float64(count)
	return colorMean{
		y: total.y / divisor,
		u: total.u / divisor,
		v: total.v / divisor,
	}, true
}

func colorDistance(a colorMean, b colorMean) float64 {
	yDelta := a.y - b.y
	uDelta := a.u - b.u
	vDelta := a.v - b.v
	return math.Sqrt(yDelta*yDelta + uDelta*uDelta + vDelta*vDelta)
}

func detectAdjacentColorShifts(samples []ColorSample, threshold float64) []ColorShift {
	var shifts []ColorShift
	for index := 1; index < len(samples); index++ {
		before := colorMean{y: samples[index-1].YMean, u: samples[index-1].UMean, v: samples[index-1].VMean}
		after := colorMean{y: samples[index].YMean, u: samples[index].UMean, v: samples[index].VMean}
		delta := colorDistance(before, after)
		if delta >= threshold {
			shifts = append(shifts, ColorShift{Time: samples[index].Time, Delta: delta})
		}
	}
	return shifts
}

func collapseColorShifts(candidates []ColorShift, windowSeconds float64) []ColorShift {
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Time < candidates[j].Time
	})

	shifts := []ColorShift{candidates[0]}
	for _, candidate := range candidates[1:] {
		last := &shifts[len(shifts)-1]
		if candidate.Time-last.Time <= windowSeconds {
			if candidate.Delta > last.Delta {
				*last = candidate
			}
			continue
		}
		shifts = append(shifts, candidate)
	}

	return shifts
}

func ffmpegErrorText(stdout string, stderr string) string {
	message := strings.TrimSpace(stderr)
	if message != "" {
		return message
	}
	return strings.TrimSpace(stdout)
}
