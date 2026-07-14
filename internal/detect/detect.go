package detect

import (
	"bytes"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Silence is a detected quiet interval. Start is the moment audio becomes
// silent; Time is the moment audio resumes (the silence end), which aligns with
// a performance start; Duration is the length of the silence in seconds.
type Silence struct {
	Start    float64
	Time     float64
	Duration float64
}

type LoudnessSample struct {
	Time  float64
	Level float64
}

type LoudnessOnset struct {
	Time  float64
	Level float64
	Floor float64
	Delta float64
}

type AudioSignals struct {
	Silences        []Silence
	LoudnessSamples []LoudnessSample
	LoudnessOnsets  []LoudnessOnset
}

const (
	DefaultNoiseDB = -35.0
	DefaultMinDur  = 2.0

	DefaultLoudnessWindowSeconds = 0.2
	DefaultLoudnessOnsetDeltaDB  = 12.0
	DefaultLoudnessOnsetFloorDB  = -40.0
	DefaultLoudnessSustainDB     = -38.0
	DefaultLoudnessSustainSec    = 0.6
	DefaultLoudnessRefractorySec = 3.0
)

var (
	silenceStartRE = regexp.MustCompile(`silence_start:\s*([-+]?(?:\d+(?:\.\d*)?|\.\d+))`)
	silenceEndRE   = regexp.MustCompile(`silence_end:\s*([-+]?(?:\d+(?:\.\d*)?|\.\d+))\s*\|\s*silence_duration:\s*([-+]?(?:\d+(?:\.\d*)?|\.\d+))`)
	metadataTimeRE = regexp.MustCompile(`\bpts_time:([-+]?(?:\d+(?:\.\d*)?|\.\d+))\b`)
	rmsLevelRE     = regexp.MustCompile(`lavfi\.astats\.Overall\.RMS_level=([-+]?(?:\d+(?:\.\d*)?|\.\d+)|-inf|inf)`)
)

// DetectSilence returns detected silence intervals from ffmpeg silencedetect.
func DetectSilence(path string, noiseDB float64, minDur float64) ([]Silence, error) {
	audio, err := DetectAudio(path, noiseDB, minDur)
	if err != nil {
		return nil, err
	}
	return audio.Silences, nil
}

// DetectAudio returns silence intervals and loudness onsets from a single ffmpeg
// audio pass. silencedetect logs to stderr while astats/ametadata prints RMS
// windows to stdout.
func DetectAudio(path string, noiseDB float64, minDur float64) (AudioSignals, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return AudioSignals{}, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	windowSamples := int(DefaultLoudnessWindowSeconds * 10000)
	if windowSamples <= 0 {
		windowSamples = 2000
	}
	cmd := exec.Command(
		ffmpeg,
		"-hide_banner",
		"-nostats",
		"-i", path,
		"-vn",
		"-af", fmt.Sprintf("silencedetect=noise=%sdB:d=%s,aresample=10000,asetnsamples=n=%d,astats=metadata=1:reset=1,ametadata=mode=print:file=-:key=lavfi.astats.Overall.RMS_level", formatFloat(noiseDB), formatFloat(minDur), windowSamples),
		"-f", "null",
		"-",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return AudioSignals{}, fmt.Errorf("ffmpeg audio detect failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	samples := parseLoudnessMetadata(stdout.String())
	return AudioSignals{
		Silences:        parseSilenceDetect(stderr.String()),
		LoudnessSamples: samples,
		LoudnessOnsets:  DetectLoudnessOnsets(samples),
	}, nil
}

func parseSilenceDetect(output string) []Silence {
	var silences []Silence
	var pendingStart float64
	hasPendingStart := false

	for _, line := range strings.Split(output, "\n") {
		if matches := silenceStartRE.FindStringSubmatch(line); matches != nil {
			start, err := strconv.ParseFloat(matches[1], 64)
			if err != nil {
				hasPendingStart = false
				continue
			}
			pendingStart = start
			hasPendingStart = true
			continue
		}

		matches := silenceEndRE.FindStringSubmatch(line)
		if matches == nil || !hasPendingStart {
			continue
		}

		end, endErr := strconv.ParseFloat(matches[1], 64)
		duration, durationErr := strconv.ParseFloat(matches[2], 64)
		if endErr != nil || durationErr != nil {
			hasPendingStart = false
			continue
		}

		silences = append(silences, Silence{Start: pendingStart, Time: end, Duration: duration})
		hasPendingStart = false
	}

	sort.Slice(silences, func(i, j int) bool {
		return silences[i].Time < silences[j].Time
	})

	return silences
}

func parseLoudnessMetadata(output string) []LoudnessSample {
	var samples []LoudnessSample
	var pendingTime float64
	hasPendingTime := false
	for _, line := range strings.Split(output, "\n") {
		if matches := metadataTimeRE.FindStringSubmatch(line); matches != nil {
			time, err := strconv.ParseFloat(matches[1], 64)
			if err != nil {
				hasPendingTime = false
				continue
			}
			pendingTime = time
			hasPendingTime = true
			continue
		}
		matches := rmsLevelRE.FindStringSubmatch(line)
		if matches == nil || !hasPendingTime {
			continue
		}
		level := parseLoudnessLevel(matches[1])
		samples = append(samples, LoudnessSample{Time: pendingTime, Level: level})
		hasPendingTime = false
	}
	sort.Slice(samples, func(i, j int) bool {
		return samples[i].Time < samples[j].Time
	})
	return samples
}

func parseLoudnessLevel(raw string) float64 {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "-inf":
		return -120
	case "inf", "+inf":
		return 0
	}
	level, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(level) {
		return -120
	}
	if math.IsInf(level, -1) {
		return -120
	}
	if math.IsInf(level, 1) {
		return 0
	}
	return level
}

func DetectLoudnessOnsets(samples []LoudnessSample) []LoudnessOnset {
	if len(samples) == 0 {
		return nil
	}
	sorted := append([]LoudnessSample(nil), samples...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Time < sorted[j].Time
	})

	var onsets []LoudnessOnset
	lastOnset := math.Inf(-1)
	for i := 1; i < len(sorted); i++ {
		current := sorted[i]
		if current.Time-lastOnset < DefaultLoudnessRefractorySec {
			continue
		}
		floor, ok := precedingLoudnessFloor(sorted, i, 2.0)
		if !ok || floor > DefaultLoudnessOnsetFloorDB {
			continue
		}
		delta := current.Level - floor
		if delta < DefaultLoudnessOnsetDeltaDB || current.Level < DefaultLoudnessSustainDB {
			continue
		}
		if !loudnessSustains(sorted, i, DefaultLoudnessSustainSec, DefaultLoudnessSustainDB) {
			continue
		}
		onsets = append(onsets, LoudnessOnset{Time: current.Time, Level: current.Level, Floor: floor, Delta: delta})
		lastOnset = current.Time
	}
	return onsets
}

func precedingLoudnessFloor(samples []LoudnessSample, index int, lookbackSeconds float64) (float64, bool) {
	if index <= 0 {
		return 0, false
	}
	start := samples[index].Time - lookbackSeconds
	sum := 0.0
	count := 0
	for i := index - 1; i >= 0; i-- {
		if samples[i].Time < start {
			break
		}
		sum += samples[i].Level
		count++
	}
	if count == 0 {
		return 0, false
	}
	return sum / float64(count), true
}

func loudnessSustains(samples []LoudnessSample, index int, sustainSeconds float64, minLevel float64) bool {
	end := samples[index].Time + sustainSeconds
	count := 0
	for i := index; i < len(samples) && samples[i].Time < end; i++ {
		if samples[i].Level < minLevel {
			return false
		}
		count++
	}
	return count >= 2
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
