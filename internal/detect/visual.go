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

// BlackSegment is a dark interval reported by ffmpeg's blackdetect filter.
type BlackSegment struct {
	Start    float64
	End      float64
	Duration float64
}

// FreezeSegment is a still-frame interval reported by ffmpeg's freezedetect filter.
type FreezeSegment struct {
	Start    float64
	End      float64
	Duration float64
}

// VisualSignals contains all visual raw signals produced by one ffmpeg video pass.
type VisualSignals struct {
	Scenes         []SceneChange
	ColorSamples   []ColorSample
	BlackSegments  []BlackSegment
	FreezeSegments []FreezeSegment
}

const visualFloatPattern = `[-+]?(?:\d+(?:\.\d*)?|\.\d+)`

var (
	frameTimeRE  = regexp.MustCompile(`(?:^|\s)pts_time:(` + visualFloatPattern + `)(?:\s|$)`)
	leadingFloat = regexp.MustCompile(`^` + visualFloatPattern)
)

// DetectSceneChanges returns scene changes from ffmpeg's scdet filter.
func DetectSceneChanges(path string, threshold float64) ([]SceneChange, error) {
	return DetectSceneChangesAtRate(path, threshold, 0)
}

// DetectSceneChangesAtRate returns scene changes from ffmpeg's scdet filter,
// optionally sampling frames before scdet when sampleRate is greater than zero.
func DetectSceneChangesAtRate(path string, threshold float64, sampleRate float64) ([]SceneChange, error) {
	return DetectSceneChangesWindow(path, threshold, sampleRate, 0, 0)
}

// DetectSceneChangesWindow returns scene changes from a bounded source window.
func DetectSceneChangesWindow(path string, threshold float64, sampleRate float64, start float64, duration float64) ([]SceneChange, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	var stdout, stderr bytes.Buffer
	args := []string{
		"-hide_banner",
		"-nostats",
	}
	args = appendWindowInputArgs(args, path, start, duration)
	args = append(args,
		"-an",
		"-vf", buildSceneFilterChain(threshold, sampleRate),
		"-f", "null",
		"-",
	)
	cmd := exec.Command(ffmpeg, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg scdet failed: %w: %s", err, ffmpegErrorText(stdout.String(), stderr.String()))
	}

	changes := parseSceneChanges(stdout.String() + "\n" + stderr.String())
	return offsetSceneChanges(filterSceneChangesByThreshold(changes, threshold), start), nil
}

func buildSceneFilterChain(threshold float64, sampleRate float64) string {
	var filters []string
	if sampleRate > 0 {
		filters = append(filters, "fps="+formatFloat(sampleRate))
	}
	filters = append(filters, "scdet=threshold="+formatFloat(threshold), "metadata=mode=print:file=-")
	return strings.Join(filters, ",")
}

// DetectVisual returns scene, color, black, and freeze signals from one full-source ffmpeg video pass.
func DetectVisual(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64) ([]SceneChange, []ColorSample, []BlackSegment, []FreezeSegment, error) {
	return DetectVisualWindow(path, sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration, 0, 0)
}

// DetectVisualWindow returns scene, color, black, and freeze signals from one bounded ffmpeg video pass.
func DetectVisualWindow(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64, start float64, duration float64) ([]SceneChange, []ColorSample, []BlackSegment, []FreezeSegment, error) {
	signals, err := detectVisualSignalsWindow(path, sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration, start, duration)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return signals.Scenes, signals.ColorSamples, signals.BlackSegments, signals.FreezeSegments, nil
}

func detectVisualSignalsWindow(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64, start float64, duration float64) (VisualSignals, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return VisualSignals{}, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	var stdout, stderr bytes.Buffer
	args := buildVisualFFmpegArgs(path, sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration, start, duration)
	cmd := exec.Command(ffmpeg, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return VisualSignals{}, fmt.Errorf("ffmpeg visual detection failed: %w: %s", err, ffmpegErrorText(stdout.String(), stderr.String()))
	}

	output := stdout.String() + "\n" + stderr.String()
	scenes := filterSceneChangesByThreshold(parseSceneChanges(output), sceneThreshold)
	signals := VisualSignals{
		Scenes:         offsetSceneChanges(scenes, start),
		ColorSamples:   offsetColorSamples(parseColorSamples(output), start),
		BlackSegments:  offsetBlackSegments(parseBlackSegments(output), start),
		FreezeSegments: offsetFreezeSegments(parseFreezeSegments(output), start),
	}
	return signals, nil
}

func buildVisualFFmpegArgs(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64, start float64, duration float64) []string {
	args := []string{
		"-hide_banner",
		"-nostats",
		"-loglevel", "info",
	}
	args = appendWindowInputArgs(args, path, start, duration)
	return append(args,
		"-an",
		"-filter_complex", buildVisualFilterGraph(sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration),
		"-map", "[fullout]",
		"-f", "null",
		"-",
	)
}

func buildVisualFilterGraph(sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64) string {
	return "[0:v]split=2[full][slow];" +
		"[slow]" + buildSceneColorFilterChain(sceneThreshold, sceneColorSampleRate) + ",nullsink;" +
		"[full]" + buildBlackFilterChain(blackMinDuration) + "," + buildFreezeFilterChain(freezeMinDuration) + "[fullout]"
}

func buildSceneColorFilterChain(threshold float64, sampleRate float64) string {
	var filters []string
	if sampleRate > 0 {
		filters = append(filters, "fps="+formatFloat(sampleRate))
	}
	filters = append(filters, "scdet=threshold="+formatFloat(threshold), "signalstats", "metadata=mode=print:file=-")
	return strings.Join(filters, ",")
}

// DetectBlackSegments returns dark intervals from ffmpeg's blackdetect filter.
func DetectBlackSegments(path string, minDuration float64) ([]BlackSegment, error) {
	return DetectBlackSegmentsWindow(path, minDuration, 0, 0)
}

// DetectBlackSegmentsWindow returns dark intervals from a bounded source window.
func DetectBlackSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]BlackSegment, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	var stdout, stderr bytes.Buffer
	args := []string{
		"-hide_banner",
		"-nostats",
		"-loglevel", "info",
	}
	args = appendWindowInputArgs(args, path, start, duration)
	args = append(args,
		"-an",
		"-vf", buildBlackFilterChain(minDuration),
		"-f", "null",
		"-",
	)
	cmd := exec.Command(ffmpeg, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg blackdetect failed: %w: %s", err, ffmpegErrorText(stdout.String(), stderr.String()))
	}

	return offsetBlackSegments(parseBlackSegments(stdout.String()+"\n"+stderr.String()), start), nil
}

func buildBlackFilterChain(minDuration float64) string {
	if minDuration < 0 {
		minDuration = 0
	}
	return "blackdetect=d=" + formatFloat(minDuration) + ":pic_th=0.98:pix_th=0.10"
}

// DetectFreezeSegments returns still-frame intervals from ffmpeg's freezedetect filter.
func DetectFreezeSegments(path string, minDuration float64) ([]FreezeSegment, error) {
	return DetectFreezeSegmentsWindow(path, minDuration, 0, 0)
}

// DetectFreezeSegmentsWindow returns still-frame intervals from a bounded source window.
func DetectFreezeSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]FreezeSegment, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	var stdout, stderr bytes.Buffer
	args := []string{
		"-hide_banner",
		"-nostats",
		"-loglevel", "info",
	}
	args = appendWindowInputArgs(args, path, start, duration)
	args = append(args,
		"-an",
		"-vf", buildFreezeFilterChain(minDuration),
		"-f", "null",
		"-",
	)
	cmd := exec.Command(ffmpeg, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg freezedetect failed: %w: %s", err, ffmpegErrorText(stdout.String(), stderr.String()))
	}

	return offsetFreezeSegments(parseFreezeSegments(stdout.String()+"\n"+stderr.String()), start), nil
}

func buildFreezeFilterChain(minDuration float64) string {
	if minDuration < 0 {
		minDuration = 0
	}
	return "freezedetect=n=-60dB:d=" + formatFloat(minDuration)
}

// SampleFrameColors returns ffmpeg signalstats YUV mean samples.
func SampleFrameColors(path string, sampleRate float64, crop string) ([]ColorSample, error) {
	return SampleFrameColorsWindow(path, sampleRate, crop, 0, 0)
}

// SampleFrameColorsWindow returns ffmpeg signalstats YUV mean samples from a bounded source window.
func SampleFrameColorsWindow(path string, sampleRate float64, crop string, start float64, duration float64) ([]ColorSample, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	var stdout, stderr bytes.Buffer
	args := []string{
		"-hide_banner",
		"-nostats",
	}
	args = appendWindowInputArgs(args, path, start, duration)
	args = append(args,
		"-an",
		"-vf", buildColorFilterChain(sampleRate, crop),
		"-f", "null",
		"-",
	)
	cmd := exec.Command(ffmpeg, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg signalstats failed: %w: %s", err, ffmpegErrorText(stdout.String(), stderr.String()))
	}

	return offsetColorSamples(parseColorSamples(stdout.String()+"\n"+stderr.String()), start), nil
}

func appendWindowInputArgs(args []string, path string, start float64, duration float64) []string {
	if start > 0 {
		args = append(args, "-ss", formatFloat(start))
	}
	args = append(args, "-i", path)
	if duration > 0 {
		args = append(args, "-t", formatFloat(duration))
	}
	return args
}

func offsetSceneChanges(changes []SceneChange, offset float64) []SceneChange {
	if offset <= 0 {
		return changes
	}
	for i := range changes {
		changes[i].Time += offset
	}
	return changes
}

func filterSceneChangesByThreshold(changes []SceneChange, threshold float64) []SceneChange {
	if threshold <= 0 {
		return changes
	}
	filtered := changes[:0]
	for _, change := range changes {
		if change.Score >= threshold {
			filtered = append(filtered, change)
		}
	}
	return filtered
}

func offsetColorSamples(samples []ColorSample, offset float64) []ColorSample {
	if offset <= 0 {
		return samples
	}
	for i := range samples {
		samples[i].Time += offset
	}
	return samples
}

func offsetBlackSegments(segments []BlackSegment, offset float64) []BlackSegment {
	if offset <= 0 {
		return segments
	}
	for i := range segments {
		segments[i].Start += offset
		segments[i].End += offset
	}
	return segments
}

func offsetFreezeSegments(segments []FreezeSegment, offset float64) []FreezeSegment {
	if offset <= 0 {
		return segments
	}
	for i := range segments {
		segments[i].Start += offset
		segments[i].End += offset
	}
	return segments
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

func parseBlackSegments(output string) []BlackSegment {
	var segments []BlackSegment
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		start, startOK := parseMetadataNumber(line, "black_start")
		end, endOK := parseMetadataNumber(line, "black_end")
		if !startOK || !endOK {
			continue
		}
		duration, durationOK := parseMetadataNumber(line, "black_duration")
		if !durationOK {
			duration = end - start
		}
		if end < start || duration < 0 {
			continue
		}
		segments = append(segments, BlackSegment{Start: start, End: end, Duration: duration})
	}
	sort.Slice(segments, func(i, j int) bool {
		if segments[i].End == segments[j].End {
			return segments[i].Start < segments[j].Start
		}
		return segments[i].End < segments[j].End
	})
	return segments
}

type freezeFrame struct {
	segment     FreezeSegment
	hasStart    bool
	hasEnd      bool
	hasDuration bool
}

func parseFreezeSegments(output string) []FreezeSegment {
	var segments []FreezeSegment
	var current freezeFrame

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if start, ok := parseMetadataNumber(line, "freeze_start"); ok {
			segments = appendFreezeFrame(segments, current)
			current = freezeFrame{segment: FreezeSegment{Start: start}, hasStart: true}
		}
		if duration, ok := parseMetadataNumber(line, "freeze_duration"); ok {
			current.segment.Duration = duration
			current.hasDuration = true
		}
		if end, ok := parseMetadataNumber(line, "freeze_end"); ok {
			current.segment.End = end
			current.hasEnd = true
			segments = appendFreezeFrame(segments, current)
			current = freezeFrame{}
		}
	}
	segments = appendFreezeFrame(segments, current)
	sort.Slice(segments, func(i, j int) bool {
		if segments[i].End == segments[j].End {
			return segments[i].Start < segments[j].Start
		}
		return segments[i].End < segments[j].End
	})
	return segments
}

func appendFreezeFrame(segments []FreezeSegment, frame freezeFrame) []FreezeSegment {
	if !frame.hasStart || !frame.hasEnd {
		return segments
	}
	segment := frame.segment
	if !frame.hasDuration {
		segment.Duration = segment.End - segment.Start
	}
	if segment.End < segment.Start || segment.Duration < 0 {
		return segments
	}
	return append(segments, segment)
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
