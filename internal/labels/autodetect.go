package labels

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sspeaks/large-video-streamer/internal/detect"
)

const (
	autodetectSourceSilence = "silence"
	autodetectSourceLineup  = "lineup"
	autodetectSourceScene   = "scene"
	autodetectSourceColor   = "color"
	autodetectSourceOCR     = "ocr"

	autodetectSilenceConfidence = 0.4
	autodetectLineupConfidence  = 0.8
	autodetectVisualConfidence  = 0.9

	autodetectSceneThreshold      = 10.0
	autodetectColorSampleRate     = 0.5
	autodetectColorShiftThreshold = 40.0
	autodetectColorWindowSeconds  = 2.0
	autodetectSignalWindowSeconds = 1.0
)

type autodetectSignals interface {
	DetectSilence(path string, noiseDB float64, minDur float64) ([]detect.Silence, error)
	DetectSceneChanges(path string, threshold float64) ([]detect.SceneChange, error)
	SampleFrameColors(path string, sampleRate float64, crop string) ([]detect.ColorSample, error)
	OCRLowerThird(path string, timestamp float64, options detect.OCROptions) (detect.OCRResult, error)
}

type detectAutodetectSignals struct{}

func (detectAutodetectSignals) DetectSilence(path string, noiseDB float64, minDur float64) ([]detect.Silence, error) {
	return detect.DetectSilence(path, noiseDB, minDur)
}

func (detectAutodetectSignals) DetectSceneChanges(path string, threshold float64) ([]detect.SceneChange, error) {
	return detect.DetectSceneChanges(path, threshold)
}

func (detectAutodetectSignals) SampleFrameColors(path string, sampleRate float64, crop string) ([]detect.ColorSample, error) {
	return detect.SampleFrameColors(path, sampleRate, crop)
}

func (detectAutodetectSignals) OCRLowerThird(path string, timestamp float64, options detect.OCROptions) (detect.OCRResult, error) {
	return detect.OCRLowerThird(path, timestamp, options)
}

type autodetectRequest struct {
	Lineup     []autodetectLineupEntry `json:"lineup"`
	UseSilence bool                    `json:"useSilence"`
	UseColor   bool                    `json:"useColor"`
	UseOCR     bool                    `json:"useOCR"`
	NoiseDB    *float64                `json:"noiseDB,omitempty"`
	MinDur     *float64                `json:"minDur,omitempty"`
}

type autodetectLineupEntry struct {
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases,omitempty"`
	SongCount int      `json:"songCount,omitempty"`
}

func decodeAutodetectRequest(r io.Reader) (autodetectRequest, error) {
	var wire struct {
		Lineup     []autodetectLineupEntry `json:"lineup"`
		UseSilence *bool                   `json:"useSilence"`
		UseColor   bool                    `json:"useColor"`
		UseOCR     bool                    `json:"useOCR"`
		NoiseDB    *float64                `json:"noiseDB,omitempty"`
		MinDur     *float64                `json:"minDur,omitempty"`
	}
	if err := json.NewDecoder(r).Decode(&wire); err != nil {
		return autodetectRequest{}, err
	}
	useSilence := true
	if wire.UseSilence != nil {
		useSilence = *wire.UseSilence
	}
	return normalizeAutodetectRequest(autodetectRequest{
		Lineup:     wire.Lineup,
		UseSilence: useSilence,
		UseColor:   wire.UseColor,
		UseOCR:     wire.UseOCR,
		NoiseDB:    wire.NoiseDB,
		MinDur:     wire.MinDur,
	})
}

func normalizeAutodetectRequest(req autodetectRequest) (autodetectRequest, error) {
	if len(req.Lineup) == 0 {
		return autodetectRequest{}, errors.New("lineup is required")
	}

	normalized := req
	normalized.Lineup = make([]autodetectLineupEntry, len(req.Lineup))
	for i, entry := range req.Lineup {
		if containsLineBreak(entry.Name) {
			return autodetectRequest{}, fmt.Errorf("lineup[%d].name cannot contain line breaks", i)
		}
		entry.Name = strings.TrimSpace(entry.Name)
		if entry.Name == "" {
			return autodetectRequest{}, fmt.Errorf("lineup[%d].name is required", i)
		}
		if entry.SongCount < 0 {
			return autodetectRequest{}, fmt.Errorf("lineup[%d].songCount must be greater than 0", i)
		}
		if entry.SongCount == 0 {
			entry.SongCount = 2
		}
		aliases, err := normalizeAutodetectAliases(entry.Aliases, i)
		if err != nil {
			return autodetectRequest{}, err
		}
		entry.Aliases = aliases
		normalized.Lineup[i] = entry
	}

	if normalized.NoiseDB == nil {
		normalized.NoiseDB = float64Ptr(detect.DefaultNoiseDB)
	} else {
		noiseDB := *normalized.NoiseDB
		if math.IsNaN(noiseDB) || math.IsInf(noiseDB, 0) {
			return autodetectRequest{}, errors.New("noiseDB must be finite")
		}
		normalized.NoiseDB = float64Ptr(noiseDB)
	}
	if normalized.MinDur == nil {
		normalized.MinDur = float64Ptr(detect.DefaultMinDur)
	} else {
		minDur := *normalized.MinDur
		if math.IsNaN(minDur) || math.IsInf(minDur, 0) {
			return autodetectRequest{}, errors.New("minDur must be finite")
		}
		if minDur <= 0 {
			return autodetectRequest{}, errors.New("minDur must be greater than 0")
		}
		normalized.MinDur = float64Ptr(minDur)
	}
	return normalized, nil
}

func normalizeAutodetectAliases(aliases []string, lineupIndex int) ([]string, error) {
	normalized := make([]string, 0, len(aliases))
	seen := make(map[string]struct{}, len(aliases))
	for i, alias := range aliases {
		if containsLineBreak(alias) {
			return nil, fmt.Errorf("lineup[%d].aliases[%d] cannot contain line breaks", lineupIndex, i)
		}
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		normalized = append(normalized, alias)
	}
	return normalized, nil
}

func assignLineupSuggestions(lineup []autodetectLineupEntry, candidates []Candidate) []Candidate {
	sorted := append([]Candidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Time < sorted[j].Time
	})

	names := lineupSuggestedNames(lineup)
	for i := range sorted {
		if i < len(names) {
			sorted[i].SuggestedName = names[i]
			sorted[i].Sources = []string{autodetectSourceSilence, autodetectSourceLineup}
			sorted[i].Confidence = autodetectLineupConfidence
			continue
		}
		sorted[i].SuggestedName = ""
		sorted[i].Sources = []string{autodetectSourceSilence}
		sorted[i].Confidence = autodetectSilenceConfidence
	}
	return sorted
}

func (srv *Server) buildAutodetectCandidates(sourcePath string, req autodetectRequest) ([]Candidate, error) {
	signals := srv.autodetectSignalRunner()
	var candidates []Candidate
	if req.UseSilence {
		silences, err := signals.DetectSilence(sourcePath, *req.NoiseDB, *req.MinDur)
		if err != nil {
			return nil, err
		}
		candidates = candidatesFromSilences(silences)
	}

	candidates = assignLineupSuggestions(req.Lineup, candidates)

	if req.UseColor {
		var err error
		candidates, err = srv.boostAutodetectVisualSignals(sourcePath, candidates)
		if err != nil {
			return nil, err
		}
	}
	if req.UseOCR {
		var err error
		candidates, err = srv.boostAutodetectOCR(sourcePath, candidates, req.Lineup)
		if err != nil {
			return nil, err
		}
	}
	return candidates, nil
}

func (srv *Server) autodetectSignalRunner() autodetectSignals {
	if srv.autodetectSignals != nil {
		return srv.autodetectSignals
	}
	return detectAutodetectSignals{}
}

func candidatesFromSilences(silences []detect.Silence) []Candidate {
	candidates := make([]Candidate, 0, len(silences))
	for _, sil := range silences {
		candidates = append(candidates, Candidate{
			Time:       sil.Time,
			Duration:   sil.Duration,
			Status:     "candidate",
			Sources:    []string{autodetectSourceSilence},
			Confidence: autodetectSilenceConfidence,
		})
	}
	return candidates
}

func (srv *Server) boostAutodetectVisualSignals(sourcePath string, candidates []Candidate) ([]Candidate, error) {
	signals := srv.autodetectSignalRunner()
	scenes, err := signals.DetectSceneChanges(sourcePath, autodetectSceneThreshold)
	if err != nil {
		return nil, fmt.Errorf("scene autodetect failed: %w", err)
	}
	samples, err := signals.SampleFrameColors(sourcePath, autodetectColorSampleRate, "")
	if err != nil {
		return nil, fmt.Errorf("color autodetect failed: %w", err)
	}
	shifts := detect.DetectColorShifts(samples, autodetectColorShiftThreshold, autodetectColorWindowSeconds)
	boosted := append([]Candidate(nil), candidates...)
	for i := range boosted {
		if !isSong2Suggestion(boosted[i].SuggestedName) {
			continue
		}
		if nearSceneChange(boosted[i].Time, scenes) {
			boosted[i] = boostCandidate(boosted[i], autodetectSourceScene, autodetectVisualConfidence)
		}
		if nearColorShift(boosted[i].Time, shifts) {
			boosted[i] = boostCandidate(boosted[i], autodetectSourceColor, autodetectVisualConfidence)
		}
	}
	return boosted, nil
}

func (srv *Server) boostAutodetectOCR(sourcePath string, candidates []Candidate, lineup []autodetectLineupEntry) ([]Candidate, error) {
	tempRoot, err := srv.autodetectOCRTempRoot()
	if err != nil {
		return nil, err
	}
	signals := srv.autodetectSignalRunner()
	boosted := append([]Candidate(nil), candidates...)
	for i := range boosted {
		result, err := signals.OCRLowerThird(sourcePath, boosted[i].Time, detect.OCROptions{TempRoot: tempRoot})
		if err != nil {
			return nil, fmt.Errorf("ocr autodetect at %.3fs failed: %w", boosted[i].Time, err)
		}
		boosted[i] = boostCandidateWithOCR(boosted[i], result, lineup)
	}
	return boosted, nil
}

func (srv *Server) autodetectOCRTempRoot() (string, error) {
	if strings.TrimSpace(srv.cfg.StateDir) == "" {
		return "", errors.New("state dir is required for OCR autodetect temp files")
	}
	root := filepath.Join(srv.cfg.StateDir, "ocr")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create OCR temp root: %w", err)
	}
	return root, nil
}

func isSong2Suggestion(name string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), "-song-2")
}

func nearSceneChange(time float64, scenes []detect.SceneChange) bool {
	for _, scene := range scenes {
		if math.Abs(scene.Time-time) <= autodetectSignalWindowSeconds {
			return true
		}
	}
	return false
}

func nearColorShift(time float64, shifts []detect.ColorShift) bool {
	for _, shift := range shifts {
		if math.Abs(shift.Time-time) <= autodetectSignalWindowSeconds {
			return true
		}
	}
	return false
}

func boostCandidate(candidate Candidate, source string, confidence float64) Candidate {
	candidate.Sources = unionSources(candidate.Sources, []string{source})
	if confidence > candidate.Confidence {
		candidate.Confidence = confidence
	}
	return candidate
}

func boostCandidateWithOCR(candidate Candidate, result detect.OCRResult, lineup []autodetectLineupEntry) Candidate {
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return candidate
	}
	candidate = boostCandidate(candidate, autodetectSourceOCR, normalizeOCRConfidence(result.Confidence))
	suggestedName, matched := lineupSuggestedNameFromOCR(text, lineup)
	if !matched {
		suggestedName = text
	}
	if candidate.SuggestedName == "" {
		candidate.SuggestedName = suggestedName
		return candidate
	}
	if matched && !sameLineupSuggestion(candidate.SuggestedName, suggestedName) {
		candidate.Conflict = true
	}
	return candidate
}

func normalizeOCRConfidence(confidence float64) float64 {
	if confidence > 1 {
		confidence = confidence / 100
	}
	if confidence < 0 {
		return 0
	}
	if confidence > 1 {
		return 1
	}
	return confidence
}

func lineupSuggestedNameFromOCR(text string, lineup []autodetectLineupEntry) (string, bool) {
	foldedText := strings.ToLower(text)
	for _, entry := range lineup {
		for _, name := range append([]string{entry.Name}, entry.Aliases...) {
			foldedName := strings.ToLower(strings.TrimSpace(name))
			if foldedName == "" {
				continue
			}
			if strings.Contains(foldedText, foldedName) || strings.Contains(foldedName, foldedText) {
				return entry.Name, true
			}
		}
	}
	return "", false
}

func sameLineupSuggestion(candidateSuggestion string, lineupName string) bool {
	candidateSuggestion = strings.ToLower(strings.TrimSpace(candidateSuggestion))
	lineupName = strings.ToLower(strings.TrimSpace(lineupName))
	return candidateSuggestion == lineupName || strings.HasPrefix(candidateSuggestion, lineupName+"-song-")
}

func lineupSuggestedNames(lineup []autodetectLineupEntry) []string {
	var names []string
	for _, entry := range lineup {
		songCount := entry.SongCount
		if songCount <= 0 {
			songCount = 2
		}
		for song := 1; song <= songCount; song++ {
			if song == 1 {
				names = append(names, entry.Name)
				continue
			}
			names = append(names, fmt.Sprintf("%s-song-%d", entry.Name, song))
		}
	}
	return names
}

func containsLineBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func float64Ptr(value float64) *float64 {
	return &value
}
