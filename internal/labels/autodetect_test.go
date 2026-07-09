package labels

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/detect"
)

func TestNormalizeAutodetectRequestDefaults(t *testing.T) {
	got, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup:     []autodetectLineupEntry{{Name: "  opener  "}},
		UseSilence: true,
		UseColor:   true,
		UseOCR:     true,
	})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	if len(got.Lineup) != 1 {
		t.Fatalf("len(Lineup) = %d, want 1", len(got.Lineup))
	}
	if got.Lineup[0].Name != "opener" {
		t.Fatalf("Lineup[0].Name = %q, want %q", got.Lineup[0].Name, "opener")
	}
	if got.Lineup[0].SongCount != 2 {
		t.Fatalf("Lineup[0].SongCount = %d, want 2", got.Lineup[0].SongCount)
	}
	if got.NoiseDB == nil || *got.NoiseDB != detect.DefaultNoiseDB {
		t.Fatalf("NoiseDB = %v, want %v", got.NoiseDB, detect.DefaultNoiseDB)
	}
	if got.MinDur == nil || *got.MinDur != detect.DefaultMinDur {
		t.Fatalf("MinDur = %v, want %v", got.MinDur, detect.DefaultMinDur)
	}
	if !got.UseSilence || !got.UseColor || !got.UseOCR {
		t.Fatalf("flags were not preserved: %#v", got)
	}
}

func TestNormalizeAutodetectRequestTrimsAndDeduplicatesAliases(t *testing.T) {
	got, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup: []autodetectLineupEntry{{
			Name:      "artist",
			Aliases:   []string{"  ace  ", "beet ", "ace", " ace "},
			SongCount: 3,
		}},
	})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	wantAliases := []string{"ace", "beet"}
	if !slices.Equal(got.Lineup[0].Aliases, wantAliases) {
		t.Fatalf("Aliases = %#v, want %#v", got.Lineup[0].Aliases, wantAliases)
	}
	if got.Lineup[0].SongCount != 3 {
		t.Fatalf("SongCount = %d, want 3", got.Lineup[0].SongCount)
	}
}

func TestNormalizeAutodetectRequestRejectsEmptyLineup(t *testing.T) {
	_, err := normalizeAutodetectRequest(autodetectRequest{})
	if err == nil || !strings.Contains(err.Error(), "lineup is required") {
		t.Fatalf("error = %v, want lineup required error", err)
	}
}

func TestNormalizeAutodetectRequestRejectsEmptyName(t *testing.T) {
	_, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup: []autodetectLineupEntry{{Name: " \t "}},
	})
	if err == nil || !strings.Contains(err.Error(), "lineup[0].name is required") {
		t.Fatalf("error = %v, want empty name error", err)
	}
}

func TestNormalizeAutodetectRequestRejectsLineBreaks(t *testing.T) {
	tests := []struct {
		name string
		req  autodetectRequest
		want string
	}{
		{
			name: "lineup name",
			req:  autodetectRequest{Lineup: []autodetectLineupEntry{{Name: "artist\nname"}}},
			want: "lineup[0].name cannot contain line breaks",
		},
		{
			name: "alias",
			req: autodetectRequest{Lineup: []autodetectLineupEntry{{
				Name:    "artist",
				Aliases: []string{"ok", "bad\ralias"},
			}}},
			want: "lineup[0].aliases[1] cannot contain line breaks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeAutodetectRequest(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNormalizeAutodetectRequestRejectsInvalidNumericThresholds(t *testing.T) {
	invalidNoiseDB := math.Inf(1)
	zeroMinDur := 0.0
	tests := []struct {
		name string
		req  autodetectRequest
		want string
	}{
		{
			name: "noiseDB",
			req: autodetectRequest{
				Lineup:  []autodetectLineupEntry{{Name: "artist"}},
				NoiseDB: &invalidNoiseDB,
			},
			want: "noiseDB must be finite",
		},
		{
			name: "minDur",
			req: autodetectRequest{
				Lineup: []autodetectLineupEntry{{Name: "artist"}},
				MinDur: &zeroMinDur,
			},
			want: "minDur must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeAutodetectRequest(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNormalizeAutodetectRequestRejectsInvalidSongCount(t *testing.T) {
	_, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup: []autodetectLineupEntry{{Name: "artist", SongCount: -1}},
	})
	if err == nil || !strings.Contains(err.Error(), "lineup[0].songCount must be greater than 0") {
		t.Fatalf("error = %v, want invalid songCount error", err)
	}
}

func TestAssignLineupSuggestionsSingleQuartet(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1.5, Status: "candidate"},
		{Time: 30, Duration: 2.25, Status: "candidate"},
	}

	got := assignLineupSuggestions(lineup, candidates)

	wantNames := []string{"quartet-a", "quartet-a-song-2"}
	assertSuggestedNames(t, got, wantNames)
	for i := range got {
		if got[i].Time != candidates[i].Time || got[i].Duration != candidates[i].Duration || got[i].Status != candidates[i].Status {
			t.Fatalf("candidate %d = %#v, want time/duration/status preserved from %#v", i, got[i], candidates[i])
		}
		if !slices.Equal(got[i].Sources, []string{"silence", "lineup"}) {
			t.Fatalf("candidate %d Sources = %#v, want [silence lineup]", i, got[i].Sources)
		}
		if got[i].Confidence <= 0 {
			t.Fatalf("candidate %d Confidence = %v, want positive lineup confidence", i, got[i].Confidence)
		}
	}
}

func TestAssignLineupSuggestionsMultipleQuartetsInTimeOrder(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}, {Name: "quartet-b"}})
	candidates := []Candidate{
		{Time: 40, Duration: 4, Status: "candidate"},
		{Time: 10, Duration: 1, Status: "candidate"},
		{Time: 30, Duration: 3, Status: "candidate"},
		{Time: 20, Duration: 2, Status: "candidate"},
	}

	got := assignLineupSuggestions(lineup, candidates)

	wantTimes := []float64{10, 20, 30, 40}
	for i, wantTime := range wantTimes {
		if got[i].Time != wantTime {
			t.Fatalf("candidate %d Time = %v, want %v in time-sorted order: %#v", i, got[i].Time, wantTime, got)
		}
	}
	assertSuggestedNames(t, got, []string{"quartet-a", "quartet-a-song-2", "quartet-b", "quartet-b-song-2"})
}

func TestAssignLineupSuggestionsReturnsPartialWhenInsufficientCandidates(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}, {Name: "quartet-b"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate"},
		{Time: 20, Duration: 2, Status: "candidate"},
	}

	got := assignLineupSuggestions(lineup, candidates)

	assertSuggestedNames(t, got, []string{"quartet-a", "quartet-a-song-2"})
}

func TestAssignLineupSuggestionsLeavesExtraCandidatesSilenceOnly(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate"},
		{Time: 20, Duration: 2, Status: "candidate"},
		{Time: 30, Duration: 3, Status: "candidate", SuggestedName: "stale", Sources: []string{"lineup"}, Confidence: 0.99},
	}

	got := assignLineupSuggestions(lineup, candidates)

	assertSuggestedNames(t, got[:2], []string{"quartet-a", "quartet-a-song-2"})
	extra := got[2]
	if extra.Time != 30 || extra.Duration != 3 || extra.Status != "candidate" {
		t.Fatalf("extra candidate = %#v, want time/duration/status preserved", extra)
	}
	if extra.SuggestedName != "" {
		t.Fatalf("extra SuggestedName = %q, want empty", extra.SuggestedName)
	}
	if !slices.Equal(extra.Sources, []string{"silence"}) {
		t.Fatalf("extra Sources = %#v, want [silence]", extra.Sources)
	}
	if extra.Confidence <= 0 || extra.Confidence >= got[0].Confidence {
		t.Fatalf("extra Confidence = %v, want positive confidence lower than assigned %v", extra.Confidence, got[0].Confidence)
	}
}

func TestAssignLineupSuggestionsUsesCustomSongCount(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a", SongCount: 3}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate"},
		{Time: 20, Duration: 2, Status: "candidate"},
		{Time: 30, Duration: 3, Status: "candidate"},
	}

	got := assignLineupSuggestions(lineup, candidates)

	assertSuggestedNames(t, got, []string{"quartet-a", "quartet-a-song-2", "quartet-a-song-3"})
}

func TestAssignLineupSuggestionsHonorsSingleSongCount(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a", SongCount: 1}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate"},
		{Time: 20, Duration: 2, Status: "candidate", SuggestedName: "stale", Sources: []string{"lineup"}, Confidence: 0.9},
	}

	got := assignLineupSuggestions(lineup, candidates)

	assertSuggestedNames(t, got, []string{"quartet-a", ""})
	if got[1].Confidence != autodetectSilenceConfidence || !slices.Equal(got[1].Sources, []string{"silence"}) {
		t.Fatalf("extra candidate = %#v, want silence-only confidence after single-song lineup is exhausted", got[1])
	}
}

func TestAssignLineupSuggestionsKeepsNamesWithSpacesAndIgnoresAliases(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{
		Name:    "  Quartet A  ",
		Aliases: []string{"QA", "Quartet Alias"},
	}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate"},
		{Time: 20, Duration: 2, Status: "candidate"},
	}

	got := assignLineupSuggestions(lineup, candidates)

	assertSuggestedNames(t, got, []string{"Quartet A", "Quartet A-song-2"})
}

func TestBuildAutodetectCandidatesBoostsSong2WithVisualSignals(t *testing.T) {
	req, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup:     []autodetectLineupEntry{{Name: "quartet-a"}},
		UseSilence: true,
		UseColor:   true,
	})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	signals := &fakeAutodetectSignals{
		silences: []detect.Silence{{Time: 10, Duration: 2}, {Time: 20, Duration: 2}},
		scenes:   []detect.SceneChange{{Time: 20.5, Score: 12}},
		colorSamples: []detect.ColorSample{
			{Time: 18, YMean: 10, UMean: 10, VMean: 10},
			{Time: 19, YMean: 10, UMean: 10, VMean: 10},
			{Time: 20, YMean: 90, UMean: 90, VMean: 90},
			{Time: 21, YMean: 90, UMean: 90, VMean: 90},
		},
	}
	srv := NewServer(config.Config{StateDir: t.TempDir()}, nil)
	srv.autodetectSignals = signals

	got, err := srv.buildAutodetectCandidates("sample_video.mkv", req)
	if err != nil {
		t.Fatalf("buildAutodetectCandidates returned error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(candidates) = %d, want 2: %#v", len(got), got)
	}
	if !slices.Contains(got[1].Sources, "scene") || !slices.Contains(got[1].Sources, "color") {
		t.Fatalf("song-2 Sources = %#v, want scene and color", got[1].Sources)
	}
	if got[1].Confidence != autodetectVisualConfidence {
		t.Fatalf("song-2 Confidence = %v, want %v", got[1].Confidence, autodetectVisualConfidence)
	}
	if slices.Contains(got[0].Sources, "scene") || slices.Contains(got[0].Sources, "color") {
		t.Fatalf("song-1 Sources = %#v, want no visual boost", got[0].Sources)
	}
}

func TestBuildAutodetectCandidatesBoostsOCRAndFlagsConflictingLineup(t *testing.T) {
	req, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup: []autodetectLineupEntry{
			{Name: "quartet-a"},
			{Name: "quartet-b", Aliases: []string{"Q B"}},
		},
		UseSilence: true,
		UseOCR:     true,
	})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	signals := &fakeAutodetectSignals{
		silences: []detect.Silence{{Time: 10, Duration: 2}},
		ocrResults: map[float64]detect.OCRResult{
			10: {Time: 10, Text: "Q B", Confidence: 91},
		},
	}
	srv := NewServer(config.Config{StateDir: t.TempDir()}, nil)
	srv.autodetectSignals = signals

	got, err := srv.buildAutodetectCandidates("sample_video.mkv", req)
	if err != nil {
		t.Fatalf("buildAutodetectCandidates returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want 1: %#v", len(got), got)
	}
	if !slices.Contains(got[0].Sources, "ocr") {
		t.Fatalf("Sources = %#v, want ocr", got[0].Sources)
	}
	if got[0].Confidence != 0.91 {
		t.Fatalf("Confidence = %v, want 0.91", got[0].Confidence)
	}
	if !got[0].Conflict {
		t.Fatalf("Conflict = false, want true for OCR mismatch")
	}
	if got[0].SuggestedName != "quartet-a" {
		t.Fatalf("SuggestedName = %q, want lineup suggestion to remain for review", got[0].SuggestedName)
	}
}

func TestBoostCandidateWithOCRDoesNotLowerExistingConfidence(t *testing.T) {
	candidate := Candidate{
		Time:          10,
		Duration:      2,
		Status:        "candidate",
		Sources:       []string{"silence", "lineup"},
		Confidence:    autodetectLineupConfidence,
		SuggestedName: "quartet-a-song-2",
	}
	lineup := []autodetectLineupEntry{{Name: "quartet-a"}}

	got := boostCandidateWithOCR(candidate, detect.OCRResult{Text: "quartet-a", Confidence: 35}, lineup)

	if got.Confidence != autodetectLineupConfidence {
		t.Fatalf("Confidence = %v, want existing lineup confidence %v", got.Confidence, autodetectLineupConfidence)
	}
	if !slices.Contains(got.Sources, "ocr") {
		t.Fatalf("Sources = %#v, want OCR source appended", got.Sources)
	}
	if got.Conflict {
		t.Fatalf("Conflict = true, want compatible OCR text not to flag conflict")
	}
	if got.SuggestedName != "quartet-a-song-2" {
		t.Fatalf("SuggestedName = %q, want existing lineup song suggestion preserved", got.SuggestedName)
	}
}

func normalizedLineup(t *testing.T, entries []autodetectLineupEntry) []autodetectLineupEntry {
	t.Helper()
	req, err := normalizeAutodetectRequest(autodetectRequest{Lineup: entries})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	return req.Lineup
}

func assertSuggestedNames(t *testing.T, candidates []Candidate, want []string) {
	t.Helper()
	if len(candidates) != len(want) {
		t.Fatalf("len(candidates) = %d, want %d: %#v", len(candidates), len(want), candidates)
	}
	for i, wantName := range want {
		if candidates[i].SuggestedName != wantName {
			t.Fatalf("candidate %d SuggestedName = %q, want %q: %#v", i, candidates[i].SuggestedName, wantName, candidates)
		}
	}
}
