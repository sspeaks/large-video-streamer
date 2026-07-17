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
	autodetectSourceBlack   = "black"
	autodetectSourceFreeze  = "freeze"
	autodetectSourceOCR     = "ocr"
	autodetectSourceAudio   = "audio"

	autodetectSilenceConfidence = 0.4
	autodetectAudioConfidence   = 0.4
	autodetectSceneConfidence   = 0.55
	autodetectColorConfidence   = 0.55
	autodetectBlackConfidence   = 0.82
	autodetectFreezeConfidence  = 0.78
	autodetectVisualPairBoost   = 0.7
	autodetectVisualStopBoost   = 0.75
	autodetectLineupConfidence  = 0.8
	autodetectVisualConfidence  = 0.9

	autodetectSceneThreshold          = 10.0
	autodetectSceneSampleRate         = 0.5
	autodetectBlackMinDuration        = 0.2
	autodetectFreezeMinDuration       = 2.0
	autodetectColorSampleRate         = 0.5
	autodetectColorShiftThreshold     = 40.0
	autodetectColorWindowSeconds      = 2.0
	autodetectFusionWindowSeconds     = 2.0
	autodetectSignalWindowSeconds     = 1.0
	autodetectVisualAnchorWindow      = 220.0
	autodetectAudioStandaloneFloor    = -40.0
	autodetectAudioStandaloneDelta    = 32.0
	autodetectAudioStandaloneMaxDelta = 100.0
	autodetectAudioStandaloneLimit    = 10
)

var (
	autodetectVisualAnchorMinDur  = detect.DefaultMinDur
	autodetectSceneCandidateLimit = 3
	autodetectColorCandidateLimit = 3
)

type autodetectSignals interface {
	DetectAudio(path string, noiseDB float64, minDur float64) (detect.AudioSignals, error)
	DetectSilence(path string, noiseDB float64, minDur float64) ([]detect.Silence, error)
	DetectVisual(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64) (detect.VisualSignals, error)
	DetectVisualWindow(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64, start float64, duration float64) (detect.VisualSignals, error)
	DetectSceneChanges(path string, threshold float64) ([]detect.SceneChange, error)
	DetectSceneChangesWindow(path string, threshold float64, sampleRate float64, start float64, duration float64) ([]detect.SceneChange, error)
	DetectBlackSegments(path string, minDuration float64) ([]detect.BlackSegment, error)
	DetectBlackSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.BlackSegment, error)
	DetectFreezeSegments(path string, minDuration float64) ([]detect.FreezeSegment, error)
	DetectFreezeSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.FreezeSegment, error)
	SampleFrameColors(path string, sampleRate float64, crop string) ([]detect.ColorSample, error)
	SampleFrameColorsWindow(path string, sampleRate float64, crop string, start float64, duration float64) ([]detect.ColorSample, error)
	OCRLowerThird(path string, timestamp float64, options detect.OCROptions) (detect.OCRResult, error)
}

type detectAutodetectSignals struct{}

func (detectAutodetectSignals) DetectAudio(path string, noiseDB float64, minDur float64) (detect.AudioSignals, error) {
	return detect.DetectAudio(path, noiseDB, minDur)
}

func (detectAutodetectSignals) DetectSilence(path string, noiseDB float64, minDur float64) ([]detect.Silence, error) {
	return detect.DetectSilence(path, noiseDB, minDur)
}

func (detectAutodetectSignals) DetectVisual(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64) (detect.VisualSignals, error) {
	scenes, samples, blackSegments, freezeSegments, err := detect.DetectVisual(path, sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration)
	return detect.VisualSignals{Scenes: scenes, ColorSamples: samples, BlackSegments: blackSegments, FreezeSegments: freezeSegments}, err
}

func (detectAutodetectSignals) DetectVisualWindow(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64, start float64, duration float64) (detect.VisualSignals, error) {
	scenes, samples, blackSegments, freezeSegments, err := detect.DetectVisualWindow(path, sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration, start, duration)
	return detect.VisualSignals{Scenes: scenes, ColorSamples: samples, BlackSegments: blackSegments, FreezeSegments: freezeSegments}, err
}

func (detectAutodetectSignals) DetectSceneChanges(path string, threshold float64) ([]detect.SceneChange, error) {
	return detect.DetectSceneChangesAtRate(path, threshold, autodetectSceneSampleRate)
}

func (detectAutodetectSignals) DetectSceneChangesWindow(path string, threshold float64, sampleRate float64, start float64, duration float64) ([]detect.SceneChange, error) {
	return detect.DetectSceneChangesWindow(path, threshold, sampleRate, start, duration)
}

func (detectAutodetectSignals) DetectBlackSegments(path string, minDuration float64) ([]detect.BlackSegment, error) {
	return detect.DetectBlackSegments(path, minDuration)
}

func (detectAutodetectSignals) DetectBlackSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.BlackSegment, error) {
	return detect.DetectBlackSegmentsWindow(path, minDuration, start, duration)
}

func (detectAutodetectSignals) DetectFreezeSegments(path string, minDuration float64) ([]detect.FreezeSegment, error) {
	return detect.DetectFreezeSegments(path, minDuration)
}

func (detectAutodetectSignals) DetectFreezeSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.FreezeSegment, error) {
	return detect.DetectFreezeSegmentsWindow(path, minDuration, start, duration)
}

func (detectAutodetectSignals) SampleFrameColors(path string, sampleRate float64, crop string) ([]detect.ColorSample, error) {
	return detect.SampleFrameColors(path, sampleRate, crop)
}

func (detectAutodetectSignals) SampleFrameColorsWindow(path string, sampleRate float64, crop string, start float64, duration float64) ([]detect.ColorSample, error) {
	return detect.SampleFrameColorsWindow(path, sampleRate, crop, start, duration)
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
		UseColor   *bool                   `json:"useColor"`
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
	useColor := true
	if wire.UseColor != nil {
		useColor = *wire.UseColor
	}
	return normalizeAutodetectRequest(autodetectRequest{
		Lineup:     wire.Lineup,
		UseSilence: useSilence,
		UseColor:   useColor,
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
	ranked, _ := assignLineupSuggestionsWithStats(lineup, candidates)
	return ranked
}

func assignLineupSuggestionsWithStats(lineup []autodetectLineupEntry, candidates []Candidate) ([]Candidate, autodetectRankingStats) {
	return rankLineupSuggestionsWithStats(lineup, candidates)
}

func assignLineupSuggestion(candidate Candidate, lineupName string) Candidate {
	lineupName = strings.TrimSpace(lineupName)
	if lineupName == "" {
		return candidate
	}
	if len(candidate.Sources) == 0 {
		candidate.Sources = []string{autodetectSourceSilence}
	}
	if candidate.SuggestedName == "" {
		candidate.SuggestedName = lineupName
		candidate.Sources = unionSources(candidate.Sources, []string{autodetectSourceLineup})
		if candidate.Confidence < autodetectLineupConfidence {
			candidate.Confidence = autodetectLineupConfidence
		}
		return candidate
	}
	if compatibleLineupSuggestion(candidate.SuggestedName, lineupName) {
		candidate.Sources = unionSources(candidate.Sources, []string{autodetectSourceLineup})
		if candidate.Confidence < autodetectLineupConfidence {
			candidate.Confidence = autodetectLineupConfidence
		}
		return candidate
	}
	if sourceContains(candidate.Sources, autodetectSourceOCR) {
		candidate.Conflict = true
	}
	return candidate
}

func (srv *Server) buildAutodetectCandidates(sourcePath string, req autodetectRequest) ([]Candidate, error) {
	candidates, _, err := srv.buildAutodetectCandidatesWithStats(sourcePath, req)
	return candidates, err
}

func (srv *Server) buildAutodetectCandidatesWithStats(sourcePath string, req autodetectRequest) ([]Candidate, autodetectRankingStats, error) {
	signals := srv.autodetectSignalRunner()
	var raw []Candidate
	var silenceCandidates []Candidate
	if req.UseSilence {
		audio, err := signals.DetectAudio(sourcePath, *req.NoiseDB, *req.MinDur)
		if err != nil {
			return nil, autodetectRankingStats{}, err
		}
		silenceCandidates = candidatesFromSilences(audio.Silences)
		raw = append(raw, silenceCandidates...)
		raw = append(raw, candidatesFromLoudnessOnsets(audio.LoudnessOnsets, silenceCandidates, true)...)
	}

	if req.UseColor {
		visualCandidates, err := srv.autodetectVisualCandidates(sourcePath, visualWindowAnchors(silenceCandidates))
		if err != nil {
			return nil, autodetectRankingStats{}, err
		}
		raw = append(raw, visualCandidates...)
	}

	candidates := fuseAutodetectCandidates(raw, autodetectFusionWindowSeconds)
	if req.UseOCR {
		var err error
		candidates, err = srv.boostAutodetectOCR(sourcePath, candidates, req.Lineup)
		if err != nil {
			return nil, autodetectRankingStats{}, err
		}
	}
	candidates, rankingStats := assignLineupSuggestionsWithStats(req.Lineup, candidates)
	return candidates, rankingStats, nil
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
		time := sil.Start
		if time <= 0 {
			time = sil.Time
		}

		candidates = append(candidates, Candidate{
			Time:         time,
			Duration:     sil.Duration,
			Status:       "candidate",
			Sources:      []string{autodetectSourceSilence},
			Confidence:   autodetectSilenceConfidence,
			VisualAnchor: sil.Time,
			FusionAnchor: sil.Time,
		})
	}
	return candidates
}

func candidatesFromLoudnessOnsets(onsets []detect.LoudnessOnset, anchors []Candidate, includeStandalone bool) []Candidate {
	candidates := make([]Candidate, 0, len(anchors))
	var standalone []detect.LoudnessOnset
	for _, onset := range onsets {
		anchor, ok := nearestAudioOnsetAnchor(onset.Time, anchors)
		if ok && strongAudioOnset(onset) {
			candidates = append(candidates, Candidate{
				Time:         anchor.Time,
				Status:       "candidate",
				Sources:      []string{autodetectSourceAudio},
				Confidence:   autodetectAudioConfidence,
				FusionAnchor: anchor.FusionAnchor,
			})
			continue
		}
		if includeStandalone &&
			strongAudioOnset(onset) {
			standalone = append(standalone, onset)
		}
	}
	sort.SliceStable(standalone, func(i, j int) bool {
		return standalone[i].Delta > standalone[j].Delta
	})
	if len(standalone) > autodetectAudioStandaloneLimit {
		standalone = standalone[:autodetectAudioStandaloneLimit]
	}
	for _, onset := range standalone {
		candidates = append(candidates, Candidate{
			Time:       onset.Time,
			Status:     "candidate",
			Sources:    []string{autodetectSourceAudio},
			Confidence: autodetectAudioConfidence,
		})
	}
	return candidates
}

func strongAudioOnset(onset detect.LoudnessOnset) bool {
	return onset.Floor <= autodetectAudioStandaloneFloor &&
		onset.Delta >= autodetectAudioStandaloneDelta &&
		onset.Delta <= autodetectAudioStandaloneMaxDelta
}

func nearestAudioOnsetAnchor(onsetTime float64, anchors []Candidate) (Candidate, bool) {
	for _, anchor := range anchors {
		if math.Abs(onsetTime-anchor.Time) <= autodetectFusionWindowSeconds ||
			(anchor.FusionAnchor > 0 && math.Abs(onsetTime-anchor.FusionAnchor) <= autodetectFusionWindowSeconds) {
			return anchor, true
		}
	}
	return Candidate{}, false
}

func (srv *Server) autodetectVisualCandidates(sourcePath string, anchors []Candidate) ([]Candidate, error) {
	if len(anchors) == 0 {
		return srv.autodetectVisualCandidatesFullSource(sourcePath)
	}
	var candidates []Candidate
	for _, anchor := range anchors {
		windowCandidates, err := srv.autodetectVisualCandidatesWindow(sourcePath, anchor.Time, autodetectVisualAnchorWindow)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, windowCandidates...)
	}
	return candidates, nil
}

func (srv *Server) autodetectVisualCandidatesFullSource(sourcePath string) ([]Candidate, error) {
	signals := srv.autodetectSignalRunner()
	visualSignals, err := signals.DetectVisual(sourcePath, autodetectSceneThreshold, autodetectColorSampleRate, autodetectBlackMinDuration, autodetectFreezeMinDuration)
	if err != nil {
		return nil, fmt.Errorf("visual autodetect failed: %w", err)
	}
	return candidatesFromVisualSignals(visualSignals), nil
}

func (srv *Server) autodetectVisualCandidatesWindow(sourcePath string, start float64, duration float64) ([]Candidate, error) {
	signals := srv.autodetectSignalRunner()
	visualSignals, err := signals.DetectVisualWindow(sourcePath, autodetectSceneThreshold, autodetectColorSampleRate, autodetectBlackMinDuration, autodetectFreezeMinDuration, start, duration)
	if err != nil {
		return nil, fmt.Errorf("visual autodetect failed: %w", err)
	}
	return candidatesFromVisualSignals(visualSignals), nil
}

func candidatesFromVisualSignals(signals detect.VisualSignals) []Candidate {
	shifts := detect.DetectColorShifts(signals.ColorSamples, autodetectColorShiftThreshold, autodetectColorWindowSeconds)

	candidates := make([]Candidate, 0, len(signals.BlackSegments)+len(signals.FreezeSegments)+len(signals.Scenes)+len(shifts))
	candidates = append(candidates, candidatesFromBlackSegments(signals.BlackSegments)...)
	candidates = append(candidates, candidatesFromFreezeSegments(signals.FreezeSegments)...)
	for _, scene := range topSceneChanges(signals.Scenes, autodetectSceneCandidateLimit) {
		if scene.Score < autodetectSceneThreshold {
			continue
		}
		candidates = append(candidates, Candidate{
			Time:       scene.Time,
			Status:     "candidate",
			Sources:    []string{autodetectSourceScene},
			Confidence: autodetectSceneCandidateConfidence(scene.Score),
		})
	}
	for _, shift := range topColorShifts(shifts, autodetectColorCandidateLimit) {
		candidates = append(candidates, Candidate{
			Time:       shift.Time,
			Status:     "candidate",
			Sources:    []string{autodetectSourceColor},
			Confidence: autodetectColorConfidence,
		})
	}
	return candidates
}

func candidatesFromBlackSegments(segments []detect.BlackSegment) []Candidate {
	candidates := make([]Candidate, 0, len(segments))
	for _, segment := range segments {
		candidates = append(candidates, Candidate{
			Time:         segment.Start,
			Duration:     segment.Duration,
			Status:       "candidate",
			Sources:      []string{autodetectSourceBlack},
			Confidence:   autodetectBlackConfidence,
			FusionAnchor: segment.End,
		})
	}
	return candidates
}

func candidatesFromFreezeSegments(segments []detect.FreezeSegment) []Candidate {
	candidates := make([]Candidate, 0, len(segments))
	for _, segment := range segments {
		candidates = append(candidates, Candidate{
			Time:         segment.Start,
			Duration:     segment.Duration,
			Status:       "candidate",
			Sources:      []string{autodetectSourceFreeze},
			Confidence:   autodetectFreezeConfidence,
			FusionAnchor: segment.End,
		})
	}
	return candidates
}

func visualWindowAnchors(candidates []Candidate) []Candidate {
	var anchors []Candidate
	for _, candidate := range candidates {
		if candidate.Duration >= autodetectVisualAnchorMinDur {
			if candidate.VisualAnchor > 0 {
				candidate.Time = candidate.VisualAnchor
			}
			anchors = append(anchors, candidate)
		}
	}
	return anchors
}

func autodetectSceneCandidateConfidence(score float64) float64 {
	if math.IsNaN(score) || score <= autodetectSceneThreshold {
		return autodetectSceneConfidence
	}
	scaled := autodetectSceneConfidence + ((score-autodetectSceneThreshold)/autodetectSceneThreshold)*(autodetectVisualConfidence-autodetectSceneConfidence)
	if scaled > autodetectVisualConfidence {
		return autodetectVisualConfidence
	}
	if scaled < autodetectSceneConfidence {
		return autodetectSceneConfidence
	}
	return scaled
}

func topSceneChanges(scenes []detect.SceneChange, limit int) []detect.SceneChange {
	if limit <= 0 || len(scenes) <= limit {
		return scenes
	}
	sorted := append([]detect.SceneChange(nil), scenes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})
	return sorted[:limit]
}

func topColorShifts(shifts []detect.ColorShift, limit int) []detect.ColorShift {
	if limit <= 0 || len(shifts) <= limit {
		return shifts
	}
	sorted := append([]detect.ColorShift(nil), shifts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Delta > sorted[j].Delta
	})
	return sorted[:limit]
}

func fuseAutodetectCandidates(raw []Candidate, windowSeconds float64) []Candidate {
	if len(raw) == 0 {
		return nil
	}
	sorted := append([]Candidate(nil), raw...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return autodetectCandidateFusionTime(sorted[i]) < autodetectCandidateFusionTime(sorted[j])
	})

	var fused []Candidate
	for _, candidate := range sorted {
		candidate = normalizeRawAutodetectCandidate(candidate)
		if len(fused) == 0 || math.Abs(autodetectCandidateFusionTime(fused[len(fused)-1])-autodetectCandidateFusionTime(candidate)) > windowSeconds {
			fused = append(fused, candidate)
			continue
		}
		fused[len(fused)-1] = mergeAutodetectCluster(fused[len(fused)-1], candidate)
	}
	for i := range fused {
		fused[i] = scoreAutodetectCluster(fused[i])
	}
	return fused
}

func autodetectCandidateFusionTime(candidate Candidate) float64 {
	if candidate.FusionAnchor > 0 {
		return candidate.FusionAnchor
	}
	return candidate.Time
}

func normalizeRawAutodetectCandidate(candidate Candidate) Candidate {
	candidate.Status = "candidate"
	if candidate.Confidence <= 0 {
		candidate.Confidence = autodetectSilenceConfidence
	}
	return candidate
}

func mergeAutodetectCluster(cluster Candidate, candidate Candidate) Candidate {
	if candidate.Confidence > cluster.Confidence {
		cluster.Time = candidate.Time
	}
	if candidate.Duration > cluster.Duration {
		cluster.Duration = candidate.Duration
	}
	cluster.Sources = unionSources(cluster.Sources, candidate.Sources)
	if candidate.Confidence > cluster.Confidence {
		cluster.Confidence = candidate.Confidence
	}
	if cluster.SuggestedName == "" {
		cluster.SuggestedName = candidate.SuggestedName
	} else if candidate.SuggestedName != "" && !sameLineupSuggestion(cluster.SuggestedName, candidate.SuggestedName) {
		cluster.Conflict = true
	}
	cluster.Conflict = cluster.Conflict || candidate.Conflict
	return cluster
}

func scoreAutodetectCluster(candidate Candidate) Candidate {
	hasSilence := sourceContains(candidate.Sources, autodetectSourceSilence)
	hasScene := sourceContains(candidate.Sources, autodetectSourceScene)
	hasColor := sourceContains(candidate.Sources, autodetectSourceColor)
	if hasScene && hasColor && candidate.Confidence < autodetectVisualPairBoost {
		candidate.Confidence = autodetectVisualPairBoost
	}
	if hasSilence && (hasScene || hasColor) && candidate.Confidence < autodetectVisualStopBoost {
		candidate.Confidence = autodetectVisualStopBoost
	}
	return candidate
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
			if errors.Is(err, detect.ErrOCRFrameNotFound) {
				continue
			}
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

func sourceContains(sources []string, source string) bool {
	for _, existing := range sources {
		if existing == source {
			return true
		}
	}
	return false
}

func removeSource(sources []string, source string) []string {
	var out []string
	for _, existing := range sources {
		if existing != source {
			out = append(out, existing)
		}
	}
	return out
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
