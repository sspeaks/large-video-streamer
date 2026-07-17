package labels

import (
	"bytes"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/detect"
)

func setAutodetectLineupOutputMinScoreForTest(t *testing.T, score float64) func() {
	t.Helper()
	previous := autodetectLineupOutputMinScore
	autodetectLineupOutputMinScore = score
	return func() {
		autodetectLineupOutputMinScore = previous
	}
}

func setAutodetectSilenceOutputMinDurForTest(t *testing.T, duration float64) func() {
	t.Helper()
	previous := autodetectSilenceOutputMinDur
	autodetectSilenceOutputMinDur = duration
	return func() {
		autodetectSilenceOutputMinDur = previous
	}
}

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

func TestDecodeAutodetectRequestDefaultsVisualOnAndOCROff(t *testing.T) {
	got, err := decodeAutodetectRequest(bytes.NewBufferString(`{"lineup":[{"name":"artist"}]}`))
	if err != nil {
		t.Fatalf("decodeAutodetectRequest returned error: %v", err)
	}
	if !got.UseColor {
		t.Fatal("absent useColor should default to true")
	}
	if got.UseOCR {
		t.Fatal("absent useOCR should default to false")
	}
}

func TestDecodeAutodetectRequestHonorsExplicitVisualOff(t *testing.T) {
	got, err := decodeAutodetectRequest(bytes.NewBufferString(`{"lineup":[{"name":"artist"}],"useColor":false}`))
	if err != nil {
		t.Fatalf("decodeAutodetectRequest returned error: %v", err)
	}
	if got.UseColor {
		t.Fatal("explicit useColor=false should be honored")
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

func TestCandidatesFromSilencesUseStartEdgeAndPreserveDuration(t *testing.T) {
	got := candidatesFromSilences([]detect.Silence{{Start: 12.5, Time: 20.5, Duration: 8}})

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %#v", len(got), got)
	}
	if got[0].Time != 12.5 {
		t.Fatalf("Time = %v, want silence start 12.5", got[0].Time)
	}
	if got[0].Duration != 8 {
		t.Fatalf("Duration = %v, want 8", got[0].Duration)
	}
	if !slices.Equal(got[0].Sources, []string{autodetectSourceSilence}) {
		t.Fatalf("Sources = %#v, want [silence]", got[0].Sources)
	}
}

func TestCandidatesFromSilencesFallBackToEndForLeadingSilence(t *testing.T) {
	got := candidatesFromSilences([]detect.Silence{{Start: 0, Time: 7.25, Duration: 7.25}})

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %#v", len(got), got)
	}
	if got[0].Time != 7.25 {
		t.Fatalf("Time = %v, want silence end fallback 7.25", got[0].Time)
	}
	if got[0].Duration != 7.25 {
		t.Fatalf("Duration = %v, want 7.25", got[0].Duration)
	}
}

func TestCandidatesFromBlackAndFreezeSegmentsUseStartEdge(t *testing.T) {
	black := candidatesFromBlackSegments([]detect.BlackSegment{{Start: 31, End: 35, Duration: 4}})
	freeze := candidatesFromFreezeSegments([]detect.FreezeSegment{{Start: 42, End: 49, Duration: 7}})

	if len(black) != 1 || black[0].Time != 31 || black[0].Duration != 4 {
		t.Fatalf("black candidates = %#v, want one candidate at start 31 with duration 4", black)
	}
	if len(freeze) != 1 || freeze[0].Time != 42 || freeze[0].Duration != 7 {
		t.Fatalf("freeze candidates = %#v, want one candidate at start 42 with duration 7", freeze)
	}
}

func TestVisualWindowAnchorsKeepSilenceEndWindowStart(t *testing.T) {
	anchors := visualWindowAnchors(candidatesFromSilences([]detect.Silence{{Start: 12.5, Time: 20.5, Duration: 8}}))

	if len(anchors) != 1 {
		t.Fatalf("len(anchors) = %d, want 1: %#v", len(anchors), anchors)
	}
	if anchors[0].Time != 20.5 {
		t.Fatalf("anchor Time = %v, want silence end 20.5", anchors[0].Time)
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

func TestAssignLineupSuggestionsDropsLowEvidenceUnassignedCandidates(t *testing.T) {
	restore := setAutodetectLineupOutputMinScoreForTest(t, 1.21)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate"},
		{Time: 20, Duration: 2, Status: "candidate"},
		{Time: 30, Duration: 3, Status: "candidate", SuggestedName: "stale", Sources: []string{"lineup"}, Confidence: 0.99},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 2 {
		t.Fatalf("len(candidates) = %d, want low-evidence unassigned candidate dropped: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a", "quartet-a-song-2"})
}

func TestAssignLineupSuggestionsKeepsAssignedCandidateBelowOutputGate(t *testing.T) {
	restore := setAutodetectLineupOutputMinScoreForTest(t, 2.0)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want assigned candidate kept below output gate: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a"})
}

func TestAssignLineupSuggestionsKeepsProtectedOCRBelowOutputGate(t *testing.T) {
	restore := setAutodetectLineupOutputMinScoreForTest(t, 4.0)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: autodetectVisualConfidence},
		{Time: 20, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: autodetectSilenceConfidence, SuggestedName: "quartet-b"},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 2 {
		t.Fatalf("len(candidates) = %d, want protected OCR candidate kept below output gate: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a", "quartet-b"})
}

func TestAssignLineupSuggestionsDropsShortUnassignedSilenceOnlyCandidate(t *testing.T) {
	restore := setAutodetectSilenceOutputMinDurForTest(t, 5)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a", SongCount: 1}})
	candidates := []Candidate{
		{Time: 10, Duration: 8, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: autodetectVisualConfidence},
		{Time: 30, Duration: 3, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want short unassigned silence-only candidate dropped: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a"})
}

func TestAssignLineupSuggestionsDropsShortSurplusSilenceOnlyCandidateAfterFullLineup(t *testing.T) {
	restore := setAutodetectSilenceOutputMinDurForTest(t, 5)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a", SongCount: 1}})
	candidates := []Candidate{
		{Time: 10, Duration: 8, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 30, Duration: 3, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want surplus silence-only candidate dropped: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a"})
}

func TestAssignLineupSuggestionsDropsLongSurplusSilenceOnlyCandidateAfterFullLineup(t *testing.T) {
	restore := setAutodetectSilenceOutputMinDurForTest(t, 5)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a", SongCount: 1}})
	candidates := []Candidate{
		{Time: 10, Duration: 8, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: autodetectVisualConfidence},
		{Time: 30, Duration: 6, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want long surplus silence-only candidate dropped: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a"})
}

func TestAssignLineupSuggestionsKeepsCorroboratedShortUnassignedSilenceCandidate(t *testing.T) {
	restore := setAutodetectSilenceOutputMinDurForTest(t, 5)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a", SongCount: 1}})
	candidates := []Candidate{
		{Time: 10, Duration: 8, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: autodetectVisualConfidence},
		{Time: 30, Duration: 3, Status: "candidate", Sources: []string{autodetectSourceSilence, autodetectSourceColor}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 2 {
		t.Fatalf("len(candidates) = %d, want corroborated short candidate kept: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a", ""})
}

func TestSurplusSuppressionFullLineup(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{
		{Name: "group-01", SongCount: 1},
		{Name: "group-02", SongCount: 1},
	})
	candidates := []Candidate{
		{Time: 10, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-01"},
		{Time: 30, Duration: 12, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 45, Status: "candidate", Sources: []string{autodetectSourceAudio}, Confidence: autodetectAudioConfidence},
		{Time: 90, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-02"},
	}

	got, stats := assignLineupSuggestionsWithStats(lineup, candidates)

	if stats.surplusSuppressed != 2 {
		t.Fatalf("surplusSuppressed = %d, want 2", stats.surplusSuppressed)
	}
	assertSuggestedNames(t, got, []string{"group-01", "group-02"})
}

func TestSurplusSuppressionPartialLineup(t *testing.T) {
	restore := setAutodetectLineupOutputMinScoreForTest(t, 0.7)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{
		{Name: "group-01", SongCount: 1},
		{Name: "group-02", SongCount: 1},
		{Name: "group-03", SongCount: 1},
	})
	candidates := []Candidate{
		{Time: 10, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-01"},
		{Time: 30, Duration: 12, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: 0.3},
		{Time: 90, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-02"},
	}

	got, stats := assignLineupSuggestionsWithStats(lineup, candidates)

	if stats.surplusSuppressed != 0 {
		t.Fatalf("surplusSuppressed = %d, want 0 for partial lineup", stats.surplusSuppressed)
	}
	assertSuggestedNames(t, got, []string{"group-01", "", "group-02"})
}

func TestSurplusSuppressionMultiSource(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{
		{Name: "group-01", SongCount: 1},
		{Name: "group-02", SongCount: 1},
	})
	candidates := []Candidate{
		{Time: 10, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-01"},
		{Time: 30, Duration: 3, Status: "candidate", Sources: []string{autodetectSourceSilence, autodetectSourceColor}, Confidence: autodetectSilenceConfidence},
		{Time: 90, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-02"},
	}

	got, stats := assignLineupSuggestionsWithStats(lineup, candidates)

	if stats.surplusSuppressed != 0 {
		t.Fatalf("surplusSuppressed = %d, want 0 for visual-corroborated surplus", stats.surplusSuppressed)
	}
	assertSuggestedNames(t, got, []string{"group-01", "", "group-02"})
}

func TestSurplusSuppressionOCR(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{
		{Name: "group-01", SongCount: 1},
		{Name: "group-02", SongCount: 1},
	})
	candidates := []Candidate{
		{Time: 10, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-01"},
		{Time: 30, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "unknown-group"},
		{Time: 90, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-02"},
	}

	got, stats := assignLineupSuggestionsWithStats(lineup, candidates)

	if stats.surplusSuppressed != 0 {
		t.Fatalf("surplusSuppressed = %d, want 0 for OCR-bearing surplus", stats.surplusSuppressed)
	}
	assertSuggestedNames(t, got, []string{"group-01", "unknown-group", "group-02"})
}

func TestSurplusSuppressionProtectedDecisions(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{
		{Name: "group-01", SongCount: 1},
		{Name: "group-02", SongCount: 1},
	})
	candidates := []Candidate{
		{Time: 10, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-01"},
		{Time: 30, Status: "named", Sources: []string{autodetectSourceSilence}, Confidence: 0.05},
		{Time: 40, Status: "rejected", Sources: []string{autodetectSourceSilence}, Confidence: 0.05},
		{Time: 50, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: 0.05, Conflict: true},
		{Time: 90, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-02"},
	}

	got, stats := assignLineupSuggestionsWithStats(lineup, candidates)

	if stats.surplusSuppressed != 0 {
		t.Fatalf("surplusSuppressed = %d, want 0 for protected decisions", stats.surplusSuppressed)
	}
	if len(got) != 5 {
		t.Fatalf("len(candidates) = %d, want protected decisions preserved: %#v", len(got), got)
	}
}

func TestIntraPerformanceGapSuppression(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "group-01", SongCount: 2}})
	candidates := []Candidate{
		{Time: 10, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-01"},
		{Time: 30, Duration: 12, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 60, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-01-song-2"},
	}

	got, stats := assignLineupSuggestionsWithStats(lineup, candidates)

	if stats.surplusSuppressed != 1 {
		t.Fatalf("surplusSuppressed = %d, want 1 intra-performance surplus candidate", stats.surplusSuppressed)
	}
	assertSuggestedNames(t, got, []string{"group-01", "group-01-song-2"})
}

func TestIntraPerformanceGapPreserved(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{
		{Name: "group-01", SongCount: 1},
		{Name: "group-02", SongCount: 1},
	})
	candidates := []Candidate{
		{Time: 10, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-01"},
		{Time: 30, Duration: 12, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 60, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "group-02"},
	}
	suppressor := newSurplusCandidateSuppressor(lineup, candidates, map[int]string{0: "group-01", 2: "group-02"})

	if suppressor.isIntraPerformanceCandidate(1, candidates[1]) {
		t.Fatal("candidate between different performers was treated as intra-performance")
	}
}

func TestSurplusSuppressionEmptyLineup(t *testing.T) {
	candidates := []Candidate{
		{Time: 10, Duration: 12, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got, stats := rankLineupSuggestionsWithStats(nil, candidates)

	if stats.surplusSuppressed != 0 {
		t.Fatalf("surplusSuppressed = %d, want 0 for empty lineup", stats.surplusSuppressed)
	}
	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want empty lineup behavior unchanged: %#v", len(got), got)
	}
}

func TestAssignLineupSuggestionsKeepsAssignedShortSilenceOnlyCandidate(t *testing.T) {
	restore := setAutodetectSilenceOutputMinDurForTest(t, 5)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a", SongCount: 1}})
	candidates := []Candidate{
		{Time: 10, Duration: 3, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want assigned short silence-only candidate kept: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a"})
}

func TestAssignLineupSuggestionsDropsEarlySurplusSilenceCandidate(t *testing.T) {
	restore := setAutodetectSilenceOutputMinDurForTest(t, 0)
	defer restore()
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 5, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 25, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceScene, autodetectSourceColor}, Confidence: autodetectVisualConfidence},
		{Time: 90, Duration: 2, Status: "candidate", Sources: []string{autodetectSourceSilence, autodetectSourceColor}, Confidence: 0.86},
	}

	got := assignLineupSuggestions(lineup, candidates)

	assertSuggestedNames(t, got, []string{"quartet-a", "quartet-a-song-2"})
}

func TestAssignLineupSuggestionsPreservesMismatchedOCRWhenBetterSequenceExists(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 5, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "quartet-b"},
		{Time: 25, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceScene, autodetectSourceColor}, Confidence: autodetectVisualConfidence},
		{Time: 90, Duration: 2, Status: "candidate", Sources: []string{autodetectSourceSilence, autodetectSourceColor}, Confidence: 0.86},
	}

	got := assignLineupSuggestions(lineup, candidates)

	assertSuggestedNames(t, got, []string{"quartet-b", "quartet-a", "quartet-a-song-2"})
	if got[0].Conflict {
		t.Fatalf("early OCR Conflict = true, want unassigned OCR suggestion preserved without a lineup conflict")
	}
	if slices.Contains(got[0].Sources, autodetectSourceLineup) {
		t.Fatalf("early OCR Sources = %#v, want no lineup source", got[0].Sources)
	}
}

func TestCandidatesFromLoudnessOnsetsEmitsStrongStandaloneAudio(t *testing.T) {
	got := candidatesFromLoudnessOnsets([]detect.LoudnessOnset{
		{Time: 100, Floor: -50, Delta: autodetectAudioStandaloneDelta + 1},
	}, nil, true)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want strong standalone audio candidate: %#v", len(got), got)
	}
	if got[0].Time != 100 || !slices.Equal(got[0].Sources, []string{autodetectSourceAudio}) {
		t.Fatalf("candidate = %#v, want standalone audio at onset time", got[0])
	}
	if got[0].Confidence != autodetectAudioConfidence {
		t.Fatalf("confidence = %v, want %v", got[0].Confidence, autodetectAudioConfidence)
	}
}

func TestCandidatesFromLoudnessOnsetsGatesWeakStandaloneAudio(t *testing.T) {
	got := candidatesFromLoudnessOnsets([]detect.LoudnessOnset{
		{Time: 100, Floor: -50, Delta: autodetectAudioStandaloneDelta - 0.1},
	}, nil, true)

	if len(got) != 0 {
		t.Fatalf("candidates = %#v, want weak standalone onset suppressed", got)
	}
}

func TestCandidatesFromLoudnessOnsetsRequiresStrongAnchorCorroboration(t *testing.T) {
	anchors := []Candidate{{Time: 100, FusionAnchor: 105, Sources: []string{autodetectSourceSilence}}}
	got := candidatesFromLoudnessOnsets([]detect.LoudnessOnset{
		{Time: 104, Floor: -50, Delta: autodetectAudioStandaloneDelta - 0.1},
		{Time: 105, Floor: -50, Delta: autodetectAudioStandaloneDelta + 1},
	}, anchors, false)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want only strong anchor corroboration: %#v", len(got), got)
	}
	if got[0].Time != anchors[0].Time || got[0].FusionAnchor != anchors[0].FusionAnchor {
		t.Fatalf("candidate = %#v, want audio corroboration on anchor", got[0])
	}
}

func TestAssignLineupSuggestionsDoesNotConflictOCRBaseNameForSongSlot(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: autodetectVisualConfidence},
		{Time: 70, Duration: 2, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "quartet-a"},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if got[1].Conflict {
		t.Fatalf("song-2 OCR Conflict = true, want base lineup OCR text treated as compatible")
	}
	if got[1].SuggestedName != "quartet-a" {
		t.Fatalf("song-2 SuggestedName = %q, want OCR suggestion preserved", got[1].SuggestedName)
	}
	if !slices.Contains(got[1].Sources, autodetectSourceLineup) {
		t.Fatalf("song-2 Sources = %#v, want compatible lineup source added", got[1].Sources)
	}
}

func TestAssignLineupSuggestionsPrunesNearbyDuplicateExtrasAfterRanking(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: autodetectVisualConfidence},
		{Time: 70, Duration: 2, Status: "candidate", Sources: []string{autodetectSourceSilence, autodetectSourceColor}, Confidence: 0.86},
		{Time: 71.1, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 2 {
		t.Fatalf("len(candidates) = %d, want nearby duplicate pruned: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a", "quartet-a-song-2"})
	if got[1].Time != 70 {
		t.Fatalf("song-2 candidate Time = %v, want higher-confidence boundary at 70", got[1].Time)
	}
}

func TestAssignLineupSuggestionsPrunesNearDuplicatesBeforeTheyFillLineupSlots(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: autodetectVisualConfidence},
		{Time: 11.2, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want near duplicate pruned before assigning song-2: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a"})
}

func TestAssignLineupSuggestionsPrunesEvenlySpacedDuplicateCluster(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: autodetectVisualConfidence},
		{Time: 12, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 14, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want evenly-spaced cluster collapsed: %#v", len(got), got)
	}
	if got[0].Time != 10 {
		t.Fatalf("kept Time = %v, want highest-ranked cluster representative at 10", got[0].Time)
	}
}

func TestAssignLineupSuggestionsPrunesCompatibleOCRDuplicatesBeforeTheyFillLineupSlots(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.95, SuggestedName: "quartet-a"},
		{Time: 11.2, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceOCR}, Confidence: 0.93, SuggestedName: "quartet-a"},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 1 {
		t.Fatalf("len(candidates) = %d, want compatible OCR duplicate pruned before assigning song-2: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a"})
	if !slices.Contains(got[0].Sources, autodetectSourceOCR) {
		t.Fatalf("Sources = %#v, want OCR source preserved", got[0].Sources)
	}
}

func TestAssignLineupSuggestionsLeavesVeryLowConfidenceCandidatesUnnamedWhenCountMatchesLineup(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: 0.05},
		{Time: 30, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: 0.05},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 0 {
		t.Fatalf("len(candidates) = %d, want very low confidence unassigned candidates dropped: %#v", len(got), got)
	}
}

func TestAssignLineupSuggestionsKeepsPlausibleSong2Boundary(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, Duration: 1, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: autodetectVisualConfidence},
		{Time: 70, Duration: 2, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := assignLineupSuggestions(lineup, candidates)

	if len(got) != 2 {
		t.Fatalf("len(candidates) = %d, want both plausible song boundaries preserved: %#v", len(got), got)
	}
	assertSuggestedNames(t, got, []string{"quartet-a", "quartet-a-song-2"})
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

	assertSuggestedNames(t, got, []string{"quartet-a"})
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
		silences: []detect.Silence{{Time: 10, Duration: 6}, {Time: 20, Duration: 6}},
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
	if got[1].Confidence != autodetectLineupConfidence {
		t.Fatalf("song-2 Confidence = %v, want %v", got[1].Confidence, autodetectLineupConfidence)
	}
	if slices.Contains(got[0].Sources, "scene") || slices.Contains(got[0].Sources, "color") {
		t.Fatalf("song-1 Sources = %#v, want no visual boost", got[0].Sources)
	}
}

func TestBuildAutodetectCandidatesCreatesSceneOnlyCandidates(t *testing.T) {
	req, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup:   []autodetectLineupEntry{{Name: "quartet-a"}},
		UseColor: true,
	})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	signals := &fakeAutodetectSignals{
		scenes: []detect.SceneChange{{Time: 42, Score: 15}},
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
	if got[0].Time != 42 || !slices.Contains(got[0].Sources, "scene") {
		t.Fatalf("candidate = %#v, want scene candidate at 42s", got[0])
	}
	if got[0].SuggestedName != "quartet-a" {
		t.Fatalf("SuggestedName = %q, want lineup-assigned name", got[0].SuggestedName)
	}
}

func TestBuildAutodetectCandidatesUsesWindowedBlackAndFreezeCandidates(t *testing.T) {
	req, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup:     []autodetectLineupEntry{{Name: "quartet-a", SongCount: 3}},
		UseSilence: true,
		UseColor:   true,
	})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	signals := &fakeAutodetectSignals{
		silences:       []detect.Silence{{Time: 40, Duration: detect.DefaultMinDur}},
		blackSegments:  []detect.BlackSegment{{Start: 78, End: 80, Duration: 2}},
		freezeSegments: []detect.FreezeSegment{{Start: 86, End: 90, Duration: 4}},
	}
	srv := NewServer(config.Config{StateDir: t.TempDir()}, nil)
	srv.autodetectSignals = signals

	got, err := srv.buildAutodetectCandidates("sample_video.mkv", req)
	if err != nil {
		t.Fatalf("buildAutodetectCandidates returned error: %v", err)
	}

	if !containsCandidateNear(got, 78, autodetectSourceBlack) {
		t.Fatalf("candidates = %#v, want windowed black candidate", got)
	}
	if !containsCandidateNear(got, 86, autodetectSourceFreeze) {
		t.Fatalf("candidates = %#v, want windowed freeze candidate", got)
	}
	if signals.blackCalls != 0 || signals.freezeCalls != 0 {
		t.Fatalf("blackCalls=%d freezeCalls=%d, want no full-source transition scans when silence anchors bound visual work", signals.blackCalls, signals.freezeCalls)
	}
	if len(signals.visualWindows) != 1 || signals.visualWindows[0].start != 40 || signals.visualWindows[0].duration != autodetectVisualAnchorWindow {
		t.Fatalf("visualWindows=%#v, want one combined visual window at the silence anchor", signals.visualWindows)
	}
	if len(signals.blackWindows) != 0 || len(signals.freezeWindows) != 0 || len(signals.sceneWindows) != 0 || len(signals.colorWindows) != 0 {
		t.Fatalf("separate visual windows black=%#v freeze=%#v scene=%#v color=%#v, want combined visual pass only", signals.blackWindows, signals.freezeWindows, signals.sceneWindows, signals.colorWindows)
	}
}

func TestAutodetectVisualCandidatesWindowUsesOneCombinedVisualSignalPass(t *testing.T) {
	signals := &fakeAutodetectSignals{
		scenes: []detect.SceneChange{{Time: 12, Score: 15}},
		colorSamples: []detect.ColorSample{
			{Time: 8, YMean: 10, UMean: 10, VMean: 10},
			{Time: 9, YMean: 10, UMean: 10, VMean: 10},
			{Time: 10, YMean: 90, UMean: 90, VMean: 90},
			{Time: 11, YMean: 90, UMean: 90, VMean: 90},
		},
		blackSegments:  []detect.BlackSegment{{Start: 20, End: 21, Duration: 1}},
		freezeSegments: []detect.FreezeSegment{{Start: 30, End: 33, Duration: 3}},
	}
	srv := NewServer(config.Config{}, nil)
	srv.autodetectSignals = signals

	got, err := srv.autodetectVisualCandidatesWindow("sample_video.mkv", 5, 60)
	if err != nil {
		t.Fatalf("autodetectVisualCandidatesWindow returned error: %v", err)
	}

	if len(signals.visualWindows) != 1 || signals.visualWindows[0] != (autodetectSignalWindow{start: 5, duration: 60}) {
		t.Fatalf("visualWindows = %#v, want exactly one combined visual pass for the window", signals.visualWindows)
	}
	if len(signals.blackWindows) != 0 || len(signals.freezeWindows) != 0 || len(signals.sceneWindows) != 0 || len(signals.colorWindows) != 0 {
		t.Fatalf("separate visual calls black=%#v freeze=%#v scene=%#v color=%#v, want none", signals.blackWindows, signals.freezeWindows, signals.sceneWindows, signals.colorWindows)
	}
	for _, source := range []string{autodetectSourceBlack, autodetectSourceFreeze, autodetectSourceScene, autodetectSourceColor} {
		if len(candidateTimesBySource(got, source)) == 0 {
			t.Fatalf("candidates = %#v, want source %q from combined visual result", got, source)
		}
	}
}

func TestBuildAutodetectCandidatesPreservesShortSilenceWithVisualCandidates(t *testing.T) {
	minDur := 1.0
	req, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup:     []autodetectLineupEntry{{Name: "quartet-a", SongCount: 3}},
		UseSilence: true,
		UseColor:   true,
		MinDur:     &minDur,
	})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	signals := &fakeAutodetectSignals{
		silences: []detect.Silence{{Time: 10, Duration: 1.5}, {Time: 100, Duration: 6}},
		scenes:   []detect.SceneChange{{Time: 130, Score: 15}},
	}
	srv := NewServer(config.Config{}, nil)
	srv.autodetectSignals = signals

	got, err := srv.buildAutodetectCandidates("sample_video.mkv", req)
	if err != nil {
		t.Fatalf("buildAutodetectCandidates returned error: %v", err)
	}

	if !containsCandidateNear(got, 10, autodetectSourceSilence) {
		t.Fatalf("candidates = %#v, want short silence candidate at 10s preserved alongside visual candidates", got)
	}
	if !containsCandidateNear(got, 130, autodetectSourceScene) {
		t.Fatalf("candidates = %#v, want visual scene candidate so the preservation case exercises visual mode", got)
	}
}

func TestBuildAutodetectCandidatesUsesDefaultMinDurationWindowsForVisualSignals(t *testing.T) {
	req, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup:     []autodetectLineupEntry{{Name: "quartet-a"}},
		UseSilence: true,
		UseColor:   true,
	})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	signals := &fakeAutodetectSignals{
		silences: []detect.Silence{{Time: 10, Duration: 2}, {Time: 100, Duration: 6}},
		scenes:   []detect.SceneChange{{Time: 130, Score: 15}},
	}
	srv := NewServer(config.Config{}, nil)
	srv.autodetectSignals = signals

	if _, err := srv.buildAutodetectCandidates("sample_video.mkv", req); err != nil {
		t.Fatalf("buildAutodetectCandidates returned error: %v", err)
	}

	if len(signals.visualWindows) != 2 {
		t.Fatalf("visual windows = %#v, want windows for silence candidates at default min duration or longer", signals.visualWindows)
	}
	if signals.visualWindows[0].start != 10 || signals.visualWindows[0].duration != autodetectVisualAnchorWindow {
		t.Fatalf("first visual window = %#v, want start 10 duration %v", signals.visualWindows[0], autodetectVisualAnchorWindow)
	}
	if signals.visualWindows[1].start != 100 || signals.visualWindows[1].duration != autodetectVisualAnchorWindow {
		t.Fatalf("second visual window = %#v, want start 100 duration %v", signals.visualWindows[1], autodetectVisualAnchorWindow)
	}
	if len(signals.sceneWindows) != 0 || len(signals.colorWindows) != 0 {
		t.Fatalf("scene windows = %#v color windows = %#v, want combined visual windows only", signals.sceneWindows, signals.colorWindows)
	}
}

func TestAutodetectVisualCandidatesWindowKeepsMultipleSceneAndColorCandidates(t *testing.T) {
	signals := &fakeAutodetectSignals{
		scenes: []detect.SceneChange{
			{Time: 12, Score: 12},
			{Time: 20, Score: 16},
			{Time: 30, Score: 9},
		},
		colorSamples: []detect.ColorSample{
			{Time: 8, YMean: 10, UMean: 10, VMean: 10},
			{Time: 9, YMean: 10, UMean: 10, VMean: 10},
			{Time: 10, YMean: 90, UMean: 90, VMean: 90},
			{Time: 11, YMean: 90, UMean: 90, VMean: 90},
			{Time: 18, YMean: 90, UMean: 90, VMean: 90},
			{Time: 19, YMean: 90, UMean: 90, VMean: 90},
			{Time: 20, YMean: 10, UMean: 10, VMean: 10},
			{Time: 21, YMean: 10, UMean: 10, VMean: 10},
		},
	}
	srv := NewServer(config.Config{}, nil)
	srv.autodetectSignals = signals

	got, err := srv.autodetectVisualCandidatesWindow("sample_video.mkv", 0, autodetectVisualAnchorWindow)
	if err != nil {
		t.Fatalf("autodetectVisualCandidatesWindow returned error: %v", err)
	}

	sceneTimes := candidateTimesBySource(got, autodetectSourceScene)
	slices.Sort(sceneTimes)
	if !slices.Equal(sceneTimes, []float64{12, 20}) {
		t.Fatalf("scene candidate times = %#v, want multiple above-threshold scene scores retained", sceneTimes)
	}
	colorTimes := candidateTimesBySource(got, autodetectSourceColor)
	slices.Sort(colorTimes)
	if !slices.Equal(colorTimes, []float64{10, 20}) {
		t.Fatalf("color candidate times = %#v, want two color shifts retained", colorTimes)
	}
}

func TestFuseAutodetectCandidatesClustersNearbySources(t *testing.T) {
	raw := []Candidate{
		{Time: 10, Duration: 3, Status: "candidate", Sources: []string{"silence"}, Confidence: autodetectSilenceConfidence},
		{Time: 10.5, Status: "candidate", Sources: []string{"scene"}, Confidence: autodetectSceneConfidence},
		{Time: 11, Status: "candidate", Sources: []string{"color"}, Confidence: autodetectColorConfidence},
		{Time: 30, Status: "candidate", Sources: []string{"scene"}, Confidence: autodetectSceneConfidence},
	}

	got := fuseAutodetectCandidates(raw, 2)

	if len(got) != 2 {
		t.Fatalf("len(fused) = %d, want 2: %#v", len(got), got)
	}
	if got[0].Duration != 3 {
		t.Fatalf("cluster Duration = %v, want silence duration preserved", got[0].Duration)
	}
	for _, source := range []string{"silence", "scene", "color"} {
		if !slices.Contains(got[0].Sources, source) {
			t.Fatalf("cluster Sources = %#v, want %q", got[0].Sources, source)
		}
	}
	if got[0].Confidence != autodetectVisualStopBoost {
		t.Fatalf("cluster Confidence = %v, want %v", got[0].Confidence, autodetectVisualStopBoost)
	}
}

func TestFuseAutodetectCandidatesClustersNearbyBlackAndFreezeSources(t *testing.T) {
	raw := []Candidate{
		{Time: 10, Duration: 3, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 10.5, Duration: 0.5, Status: "candidate", Sources: []string{autodetectSourceBlack}, Confidence: autodetectBlackConfidence},
		{Time: 11.5, Duration: 2.5, Status: "candidate", Sources: []string{autodetectSourceFreeze}, Confidence: autodetectFreezeConfidence},
	}

	got := fuseAutodetectCandidates(raw, 2)

	if len(got) != 1 {
		t.Fatalf("len(fused) = %d, want 1: %#v", len(got), got)
	}
	for _, source := range []string{autodetectSourceSilence, autodetectSourceBlack, autodetectSourceFreeze} {
		if !slices.Contains(got[0].Sources, source) {
			t.Fatalf("cluster Sources = %#v, want %q", got[0].Sources, source)
		}
	}
	if got[0].Time != 10.5 {
		t.Fatalf("cluster Time = %v, want black transition time from highest-confidence source", got[0].Time)
	}
	if got[0].Confidence != autodetectBlackConfidence {
		t.Fatalf("cluster Confidence = %v, want black confidence %v", got[0].Confidence, autodetectBlackConfidence)
	}
}

func TestFuseAutodetectCandidatesUsesFusionAnchorForClustering(t *testing.T) {
	raw := []Candidate{
		{Time: 10, FusionAnchor: 20, Duration: 3, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 13, FusionAnchor: 21, Duration: 2, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := fuseAutodetectCandidates(raw, 2)

	if len(got) != 1 {
		t.Fatalf("len(fused) = %d, want 1 clustered by fusion anchor: %#v", len(got), got)
	}
	if got[0].Time != 10 {
		t.Fatalf("fused Time = %v, want emitted start edge 10 preserved", got[0].Time)
	}
}

func TestRankLineupSuggestionsPrunesDuplicatesByFusionAnchor(t *testing.T) {
	lineup := normalizedLineup(t, []autodetectLineupEntry{{Name: "quartet-a"}})
	candidates := []Candidate{
		{Time: 10, FusionAnchor: 20, Duration: 3, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 14, FusionAnchor: 21, Duration: 2, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
	}

	got := rankLineupSuggestions(lineup, candidates)

	if len(got) != 1 {
		t.Fatalf("len(ranked) = %d, want 1 duplicate pruned by fusion anchor: %#v", len(got), got)
	}
	if got[0].Time != 10 {
		t.Fatalf("ranked Time = %v, want emitted start edge 10 preserved", got[0].Time)
	}
}

func TestAutodetectSceneCandidateConfidenceScalesWithScore(t *testing.T) {
	below := autodetectSceneCandidateConfidence(autodetectSceneThreshold - 1)
	if below != autodetectSceneConfidence {
		t.Fatalf("below-threshold confidence = %v, want base %v", below, autodetectSceneConfidence)
	}
	atThreshold := autodetectSceneCandidateConfidence(autodetectSceneThreshold)
	if atThreshold != autodetectSceneConfidence {
		t.Fatalf("threshold confidence = %v, want base %v", atThreshold, autodetectSceneConfidence)
	}
	above := autodetectSceneCandidateConfidence(autodetectSceneThreshold * 1.5)
	if above <= autodetectSceneConfidence || above >= autodetectVisualConfidence {
		t.Fatalf("above-threshold confidence = %v, want between base %v and cap %v", above, autodetectSceneConfidence, autodetectVisualConfidence)
	}
	capped := autodetectSceneCandidateConfidence(autodetectSceneThreshold * 3)
	if capped != autodetectVisualConfidence {
		t.Fatalf("capped confidence = %v, want visual confidence cap %v", capped, autodetectVisualConfidence)
	}
}

func TestFuseAutodetectCandidatesKeepsHighSceneConfidence(t *testing.T) {
	sceneConfidence := autodetectSceneCandidateConfidence(autodetectSceneThreshold * 3)
	raw := []Candidate{
		{Time: 10, Duration: 2, Status: "candidate", Sources: []string{autodetectSourceSilence}, Confidence: autodetectSilenceConfidence},
		{Time: 10.5, Status: "candidate", Sources: []string{autodetectSourceScene}, Confidence: sceneConfidence},
	}

	got := fuseAutodetectCandidates(raw, 2)

	if len(got) != 1 {
		t.Fatalf("len(fused) = %d, want 1: %#v", len(got), got)
	}
	if got[0].Confidence != sceneConfidence {
		t.Fatalf("fused Confidence = %v, want high scene confidence %v preserved", got[0].Confidence, sceneConfidence)
	}
	if got[0].Time != 10.5 {
		t.Fatalf("fused Time = %v, want highest-confidence scene time 10.5", got[0].Time)
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
	if got[0].SuggestedName != "quartet-b" {
		t.Fatalf("SuggestedName = %q, want OCR suggestion to remain for review", got[0].SuggestedName)
	}
}

func TestBuildAutodetectCandidatesSkipsOCRMissingFrame(t *testing.T) {
	req, err := normalizeAutodetectRequest(autodetectRequest{
		Lineup:     []autodetectLineupEntry{{Name: "quartet-a"}},
		UseSilence: true,
		UseOCR:     true,
	})
	if err != nil {
		t.Fatalf("normalizeAutodetectRequest returned error: %v", err)
	}
	signals := &fakeAutodetectSignals{
		silences: []detect.Silence{{Time: 10, Duration: 2}},
		ocrErr:   detect.ErrOCRFrameNotFound,
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
	if slices.Contains(got[0].Sources, "ocr") {
		t.Fatalf("Sources = %#v, want missing OCR frame not to add OCR source", got[0].Sources)
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

func containsCandidateNear(candidates []Candidate, wantTime float64, source string) bool {
	for _, candidate := range candidates {
		if math.Abs(candidate.Time-wantTime) <= 0.000001 && slices.Contains(candidate.Sources, source) {
			return true
		}
	}
	return false
}

func candidateTimesBySource(candidates []Candidate, source string) []float64 {
	var times []float64
	for _, candidate := range candidates {
		if slices.Contains(candidate.Sources, source) {
			times = append(times, candidate.Time)
		}
	}
	return times
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
