package labels

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/detect"
)

const (
	autodetectSampleDirEnv         = "VIDSTREAMER_AUTODETECT_SAMPLE_DIR"
	autodetectSampleSourceEnv      = "VIDSTREAMER_AUTODETECT_SAMPLE_SOURCE"
	autodetectSignalModeEnv        = "VIDSTREAMER_AUTODETECT_SIGNALS"
	autodetectCacheDirEnv          = "VIDSTREAMER_AUTODETECT_CACHE_DIR"
	autodetectMinStartRecallEnv    = "VIDSTREAMER_AUTODETECT_MIN_START_RECALL"
	autodetectMinStartPrecisionEnv = "VIDSTREAMER_AUTODETECT_MIN_START_PRECISION"
	autodetectMaxCandidatesEnv     = "VIDSTREAMER_AUTODETECT_MAX_CANDIDATES"
	autodetectVisualSceneTopNEnv   = "VIDSTREAMER_AUTODETECT_VISUAL_SCENE_TOP_N"
	autodetectVisualColorTopNEnv   = "VIDSTREAMER_AUTODETECT_VISUAL_COLOR_TOP_N"
	autodetectVisualAnchorMinEnv   = "VIDSTREAMER_AUTODETECT_VISUAL_ANCHOR_MIN_DUR"
	autodetectMinOutputScoreEnv    = "VIDSTREAMER_AUTODETECT_MIN_OUTPUT_SCORE"
	autodetectMinSilenceOutputEnv  = "VIDSTREAMER_AUTODETECT_MIN_SILENCE_OUTPUT_DUR"
	benchmarkRecallAtReviewLimit   = 30
	benchmarkRegressionGateEpsilon = 0.0000001
	benchmarkSignalCacheVersion    = "v3"
	benchmarkSourceSilenceStart    = "silence_start"
	benchmarkAudioOnsetActiveNote  = "audio_rms_loudness_active"
)

var (
	benchmarkTimestampRE              = regexp.MustCompile(`\b\d+:\d{2}:\d{2}\b`)
	benchmarkOrderRE                  = regexp.MustCompile(`^\s*\d+[\).]?\s+(.+?)\s*$`)
	benchmarkToleranceSweepTolerances = []float64{5, 10, 15, 20, 30}
)

type benchmarkTruth struct {
	Name  string
	Start float64
	Stop  float64
}

type benchmarkScore struct {
	Tolerance          float64
	TruthCount         int
	CandidateCount     int
	Matches            []benchmarkMatch
	UnmatchedTruth     []int
	UnmatchedCandidate []int
	Duplicates         int
}

type benchmarkMatch struct {
	TruthIndex     int
	CandidateIndex int
	TruthTime      float64
	CandidateTime  float64
	Delta          float64
}

type benchmarkPair struct {
	truthIndex     int
	candidateIndex int
	delta          float64
}

type benchmarkRawSource struct {
	Name string
	Hits []benchmarkSourceHit
}

type benchmarkSourceHit struct {
	Time float64
}

type benchmarkOracleReport struct {
	Tolerance     float64
	Scope         string
	Notes         []string
	TruthCount    int
	SourceNames   []string
	SourceMatched map[string]int
	AnyMatched    int
	MissingTruth  []int
	SelectedTruth []int
	TruthSources  map[int][]string
}

type benchmarkRegressionGates struct {
	minStartRecall    *float64
	minStartPrecision *float64
	maxCandidates     *int
}

type benchmarkVisualSettings struct {
	sceneCandidateLimit *int
	colorCandidateLimit *int
	anchorMinDur        *float64
	minOutputScore      *float64
	minSilenceOutputDur *float64
}

func TestAutodetectSampleBenchmark(t *testing.T) {
	sampleDir := strings.TrimSpace(os.Getenv(autodetectSampleDirEnv))
	if sampleDir == "" {
		t.Skipf("set %s to run the local auto-detect benchmark", autodetectSampleDirEnv)
	}
	sampleDir, err := filepath.Abs(sampleDir)
	if err != nil {
		t.Fatalf("resolve sample dir: %v", err)
	}

	sourcePath := strings.TrimSpace(os.Getenv(autodetectSampleSourceEnv))
	if sourcePath == "" {
		sourcePath = filepath.Join(sampleDir, "semi_finals.mkv")
	}
	truth, err := loadBenchmarkTimestamps(filepath.Join(sampleDir, "timestamps.txt"))
	if err != nil {
		t.Fatalf("load benchmark timestamps: %v", err)
	}
	lineup, err := loadBenchmarkLineup(filepath.Join(sampleDir, "semi-finals order.txt"))
	if err != nil {
		t.Fatalf("load benchmark lineup: %v", err)
	}
	if len(lineup) == 0 {
		lineup = lineupFromTruth(truth)
	}

	req, err := benchmarkAutodetectRequest(lineup, strings.TrimSpace(os.Getenv(autodetectSignalModeEnv)))
	if err != nil {
		t.Fatal(err)
	}
	cacheDir, err := benchmarkCacheDir(sourcePath)
	if err != nil {
		t.Fatalf("resolve benchmark cache dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cacheDir, "state"), 0o755); err != nil {
		t.Fatalf("create benchmark state dir: %v", err)
	}
	gates, err := parseBenchmarkRegressionGates(os.LookupEnv)
	if err != nil {
		t.Fatalf("parse benchmark regression gates: %v", err)
	}
	visualSettings, err := parseBenchmarkVisualSettings(os.LookupEnv)
	if err != nil {
		t.Fatalf("parse benchmark visual settings: %v", err)
	}
	restoreVisualSettings := applyBenchmarkVisualSettings(visualSettings)
	defer restoreVisualSettings()

	srv := NewServer(config.Config{StateDir: filepath.Join(cacheDir, "state")}, nil)
	srv.autodetectSignals = &cachedAutodetectSignals{
		inner: detectAutodetectSignals{},
		dir:   filepath.Join(cacheDir, "signals"),
	}

	rawSources, rawScope, rawNotes, err := benchmarkRawSignalSources(sourcePath, req, srv.autodetectSignalRunner())
	if err != nil {
		t.Fatalf("build raw signal ceiling: %v", err)
	}
	rawReport := benchmarkRawSignalOracle(truth, benchmarkTruthStartTime, rawSources, 20, rawScope, rawNotes)

	candidates, rankingStats, err := srv.buildAutodetectCandidatesWithStats(sourcePath, req)
	if err != nil {
		t.Fatalf("build auto-detect candidates: %v", err)
	}

	startScore := scoreAutodetectBenchmark(truth, benchmarkTruthStartTime, candidates, 20)
	t.Log(formatBenchmarkRunMetadata(sampleDir, sourcePath, cacheDir, benchmarkSignalMode(req)))
	t.Log(formatBenchmarkRawSignalCeiling(rawReport))
	t.Log(formatBenchmarkRawSignalMatrix(rawReport))
	t.Logf("ranking surplus_suppressed=%d", rankingStats.surplusSuppressed)
	t.Logf("start benchmark:\n%s", startScore.format(truth, benchmarkTruthStartTime, candidates))
	t.Logf("start tolerance_sweep=%s", formatBenchmarkToleranceSweep(benchmarkToleranceSweep(truth, benchmarkTruthStartTime, candidates, benchmarkToleranceSweepTolerances)))
	if err := gates.checkStart(startScore); err != nil {
		t.Fatalf("start regression gate failed: %v", err)
	}
	if hasBenchmarkStops(truth) {
		rawStopReport := benchmarkRawSignalOracle(truth, benchmarkTruthStopTime, rawSources, 20, rawScope, rawNotes)
		t.Logf("stop %s", formatBenchmarkRawSignalCeiling(rawStopReport))
		t.Logf("stop %s", formatBenchmarkRawSignalMatrix(rawStopReport))

		stopScore := scoreAutodetectBenchmark(truth, benchmarkTruthStopTime, candidates, 20)
		t.Logf("stop benchmark:\n%s", stopScore.format(truth, benchmarkTruthStopTime, candidates))
		t.Logf("stop tolerance_sweep=%s", formatBenchmarkToleranceSweep(benchmarkToleranceSweep(truth, benchmarkTruthStopTime, candidates, benchmarkToleranceSweepTolerances)))
	}
}

func TestLoadBenchmarkTimestampsIgnoresHeadersAndUsesDummyNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timestamps.txt")
	if err := os.WriteFile(path, []byte("Sample Header\nNotes\nGroup A 00:01:00 00:03:00\nGroup B 00:05:00\n00:07:00\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := loadBenchmarkTimestamps(path)
	if err != nil {
		t.Fatalf("loadBenchmarkTimestamps: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3: %#v", len(got), got)
	}
	if got[0].Start != 60 || got[0].Stop != 180 || got[1].Start != 300 || got[1].Stop != 0 || got[2].Start != 420 || got[2].Stop != 0 {
		t.Fatalf("truth = %#v, want start/stop values from fixture", got)
	}
	if got[2].Name != "group-03" {
		t.Fatalf("got[2].Name = %q, want dummy group-03", got[2].Name)
	}
}

func TestScoreAutodetectBenchmark(t *testing.T) {
	truth := []benchmarkTruth{
		{Name: "group-a", Start: 100},
		{Name: "group-b", Start: 200},
		{Name: "group-c", Start: 300},
	}
	candidates := []Candidate{
		{Time: 83, Duration: 2, Status: "candidate"},
		{Time: 102, Duration: 3, Status: "candidate"},
		{Time: 219, Duration: 4, Status: "candidate"},
		{Time: 220, Duration: 5, Status: "candidate"},
		{Time: 400, Duration: 6, Status: "candidate"},
	}

	score := scoreAutodetectBenchmark(truth, benchmarkTruthStartTime, candidates, 20)
	if len(score.Matches) != 2 {
		t.Fatalf("matches = %#v, want 2", score.Matches)
	}
	if len(score.UnmatchedTruth) != 1 || score.UnmatchedTruth[0] != 2 {
		t.Fatalf("UnmatchedTruth = %#v, want [2]", score.UnmatchedTruth)
	}
	if score.Duplicates != 2 {
		t.Fatalf("Duplicates = %d, want 2", score.Duplicates)
	}
	if precision := score.precision(); math.Abs(precision-0.4) > 0.0001 {
		t.Fatalf("precision = %v, want 0.4", precision)
	}
	if recall := score.recall(); math.Abs(recall-(2.0/3.0)) > 0.0001 {
		t.Fatalf("recall = %v, want 2/3", recall)
	}
}

func TestScoreAutodetectBenchmarkCountsOnlySelectedTruthTimes(t *testing.T) {
	truth := []benchmarkTruth{
		{Name: "group-a", Start: 100, Stop: 0},
		{Name: "group-b", Start: 200, Stop: 240},
		{Name: "group-c", Start: 300, Stop: 0},
	}
	candidates := []Candidate{
		{Time: 240, Status: "candidate"},
	}

	score := scoreAutodetectBenchmark(truth, benchmarkTruthStopTime, candidates, 5)
	if score.TruthCount != 1 {
		t.Fatalf("TruthCount = %d, want 1 selected stop time", score.TruthCount)
	}
	if len(score.UnmatchedTruth) != 0 {
		t.Fatalf("UnmatchedTruth = %#v, want no missing selected stop times", score.UnmatchedTruth)
	}
	assertBenchmarkFloatNear(t, score.recall(), 1)
	assertBenchmarkFloatNear(t, score.f1(), 1)
}

func TestBenchmarkScoreFBetaCalculations(t *testing.T) {
	score := benchmarkScore{
		TruthCount:     4,
		CandidateCount: 5,
		Matches:        []benchmarkMatch{{}, {}, {}},
	}

	assertBenchmarkFloatNear(t, score.f1(), 2*0.6*0.75/(0.6+0.75))
	assertBenchmarkFloatNear(t, score.fBeta(2), 5*0.6*0.75/(4*0.6+0.75))
	assertBenchmarkFloatNear(t, score.fBeta(0.5), 1.25*0.6*0.75/(0.25*0.6+0.75))

	empty := benchmarkScore{}
	assertBenchmarkFloatNear(t, empty.f1(), 0)
	assertBenchmarkFloatNear(t, empty.fBeta(2), 0)
}

func TestBenchmarkRecallAtKUsesCandidateOrder(t *testing.T) {
	truth := []benchmarkTruth{
		{Name: "group-a", Start: 100},
		{Name: "group-b", Start: 200},
		{Name: "group-c", Start: 300},
	}
	candidates := []Candidate{
		{Time: 500, Status: "candidate"},
		{Time: 101, Status: "candidate"},
		{Time: 203, Status: "candidate"},
		{Time: 299, Status: "candidate"},
		{Time: 199, Status: "candidate"},
	}

	assertBenchmarkFloatNear(t, benchmarkRecallAtK(truth, benchmarkTruthStartTime, candidates, 10, 1), 0)
	assertBenchmarkFloatNear(t, benchmarkRecallAtK(truth, benchmarkTruthStartTime, candidates, 10, 2), 1.0/3.0)
	assertBenchmarkFloatNear(t, benchmarkRecallAtK(truth, benchmarkTruthStartTime, candidates, 10, 3), 2.0/3.0)
	assertBenchmarkFloatNear(t, benchmarkRecallAtK(truth, benchmarkTruthStartTime, candidates, 10, 4), 1)
	assertBenchmarkFloatNear(t, benchmarkRecallAtK(truth, benchmarkTruthStartTime, candidates, 10, 30), 1)
	assertBenchmarkFloatNear(t, benchmarkRecallAtK(truth, benchmarkTruthStartTime, candidates, 10, 0), 0)
}

func TestBenchmarkToleranceSweepScoresInOrder(t *testing.T) {
	truth := []benchmarkTruth{
		{Name: "group-a", Start: 100},
		{Name: "group-b", Start: 200},
		{Name: "group-c", Start: 300},
	}
	candidates := []Candidate{
		{Time: 106, Status: "candidate"},
		{Time: 188, Status: "candidate"},
		{Time: 330, Status: "candidate"},
	}

	scores := benchmarkToleranceSweep(truth, benchmarkTruthStartTime, candidates, []float64{5, 10, 15, 20, 30})
	wantMatches := []int{0, 1, 2, 2, 3}
	if len(scores) != len(wantMatches) {
		t.Fatalf("len(scores) = %d, want %d", len(scores), len(wantMatches))
	}
	for i, want := range wantMatches {
		if scores[i].Tolerance != []float64{5, 10, 15, 20, 30}[i] {
			t.Fatalf("scores[%d].Tolerance = %v", i, scores[i].Tolerance)
		}
		if got := len(scores[i].Matches); got != want {
			t.Fatalf("scores[%d] matches = %d, want %d", i, got, want)
		}
	}
}

func TestFormatNearestUnmatchedTruthAnonymizesNames(t *testing.T) {
	truth := []benchmarkTruth{
		{Name: "group-a", Start: 100},
		{Name: "group-b", Start: 200},
	}
	candidates := []Candidate{
		{Time: 102, Status: "candidate", SuggestedName: "group-a"},
		{Time: 240, Status: "candidate", SuggestedName: "group-b"},
	}
	score := scoreAutodetectBenchmark(truth, benchmarkTruthStartTime, candidates, 10)

	got := formatNearestUnmatchedTruth(truth, benchmarkTruthStartTime, candidates, score.UnmatchedTruth)
	if got != "group-02@truth->candidate(+40.0s)" {
		t.Fatalf("nearest misses = %q, want anonymized nearest candidate", got)
	}
	if strings.Contains(got, "group-b") {
		t.Fatalf("nearest misses = %q, should not include truth names", got)
	}

	got = formatNearestUnmatchedTruth(truth, benchmarkTruthStartTime, nil, []int{0})
	if got != "group-01@truth->none" {
		t.Fatalf("nearest misses without candidates = %q, want none", got)
	}
}

func TestFormatBenchmarkSourceAttributionUsesSourcesOnly(t *testing.T) {
	candidates := []Candidate{
		{Sources: []string{"silence", "ocr"}, SuggestedName: "group-a"},
		{Sources: []string{"silence", "color"}, SuggestedName: "group-b"},
		{SuggestedName: "sample_video"},
	}

	got := formatBenchmarkSourceAttribution(candidates)
	if got != "color=1,none=1,ocr=1,silence=2" {
		t.Fatalf("source attribution = %q", got)
	}
	for _, disallowed := range []string{"group-a", "group-b", "sample_video"} {
		if strings.Contains(got, disallowed) {
			t.Fatalf("source attribution = %q, should not include %q", got, disallowed)
		}
	}
}

func TestBenchmarkRawSignalOracleCountsSourcesAndAny(t *testing.T) {
	truth := []benchmarkTruth{
		{Name: "real group alpha", Start: 100},
		{Name: "real group beta", Start: 200},
		{Name: "real group gamma", Start: 300},
	}
	sources := []benchmarkRawSource{
		{Name: "silence", Hits: []benchmarkSourceHit{{Time: 105}, {Time: 220}}},
		{Name: "scene", Hits: []benchmarkSourceHit{{Time: 90}, {Time: 260}}},
		{Name: "color", Hits: []benchmarkSourceHit{{Time: 281}}},
	}

	report := benchmarkRawSignalOracle(truth, benchmarkTruthStartTime, sources, 20, "current-search-window", nil)

	if report.TruthCount != 3 {
		t.Fatalf("TruthCount = %d, want 3", report.TruthCount)
	}
	if report.SourceMatched["silence"] != 2 || report.SourceMatched["scene"] != 1 || report.SourceMatched["color"] != 1 {
		t.Fatalf("SourceMatched = %#v, want silence=2 scene=1 color=1", report.SourceMatched)
	}
	if report.AnyMatched != 3 {
		t.Fatalf("AnyMatched = %d, want 3", report.AnyMatched)
	}
	if len(report.MissingTruth) != 0 {
		t.Fatalf("MissingTruth = %#v, want none", report.MissingTruth)
	}
}

func TestFormatBenchmarkRawSignalOracleAnonymizesNames(t *testing.T) {
	truth := []benchmarkTruth{
		{Name: "real group alpha", Start: 100},
		{Name: "real group beta", Start: 200},
		{Name: "real group gamma", Start: 300},
	}
	sources := []benchmarkRawSource{
		{Name: "silence", Hits: []benchmarkSourceHit{{Time: 105}, {Time: 220}}},
		{Name: "scene", Hits: []benchmarkSourceHit{{Time: 90}}},
		{Name: "color", Hits: []benchmarkSourceHit{{Time: 400}}},
	}
	report := benchmarkRawSignalOracle(truth, benchmarkTruthStartTime, sources, 20, "current-search-window", []string{"ocr=candidate-bound"})

	summary := formatBenchmarkRawSignalCeiling(report)
	matrix := formatBenchmarkRawSignalMatrix(report)
	if summary != "raw_signal_ceiling tolerance=20s scope=current-search-window silence=2/3 scene=1/3 color=0/3 any=2/3 missing=group-03 notes=ocr=candidate-bound" {
		t.Fatalf("summary = %q", summary)
	}
	if matrix != "raw_signal_matrix group-01=silence+scene group-02=silence group-03=none" {
		t.Fatalf("matrix = %q", matrix)
	}
	for _, disallowed := range []string{"real group alpha", "real group beta", "real group gamma"} {
		if strings.Contains(summary, disallowed) || strings.Contains(matrix, disallowed) {
			t.Fatalf("oracle output leaked truth name %q: summary=%q matrix=%q", disallowed, summary, matrix)
		}
	}
}

func TestFormatBenchmarkMissingTruthUsesGroupIndexes(t *testing.T) {
	got := formatBenchmarkMissingTruth([]int{0, 2, 4}, 2)
	if got != "group-01,group-03,...+1" {
		t.Fatalf("missing truth = %q", got)
	}

	got = formatBenchmarkMissingTruth(nil, 2)
	if got != "none" {
		t.Fatalf("missing truth with no indexes = %q", got)
	}
}

func TestBenchmarkRawSignalOracleHandlesEmptyInputs(t *testing.T) {
	noTruth := benchmarkRawSignalOracle(nil, benchmarkTruthStartTime, []benchmarkRawSource{
		{Name: "silence", Hits: []benchmarkSourceHit{{Time: 10}}},
	}, 20, "current-search-window", nil)
	if got := formatBenchmarkRawSignalCeiling(noTruth); got != "raw_signal_ceiling tolerance=20s scope=current-search-window silence=0/0 any=0/0 missing=none" {
		t.Fatalf("empty truth summary = %q", got)
	}
	if got := formatBenchmarkRawSignalMatrix(noTruth); got != "raw_signal_matrix none" {
		t.Fatalf("empty truth matrix = %q", got)
	}

	noSources := benchmarkRawSignalOracle([]benchmarkTruth{{Name: "real group", Start: 100}}, benchmarkTruthStartTime, nil, 20, "current-search-window", nil)
	if got := formatBenchmarkRawSignalCeiling(noSources); got != "raw_signal_ceiling tolerance=20s scope=current-search-window any=0/1 missing=group-01" {
		t.Fatalf("empty sources summary = %q", got)
	}
	if got := formatBenchmarkRawSignalMatrix(noSources); got != "raw_signal_matrix group-01=none" {
		t.Fatalf("empty sources matrix = %q", got)
	}
}

func TestBenchmarkSourceHitsFromSilencesUsesSilenceEndTime(t *testing.T) {
	hits := benchmarkSourceHitsFromSilences([]detect.Silence{
		{Start: 115, Time: 125, Duration: 10},
		{Start: 235, Time: 240, Duration: 5},
	})

	if len(hits) != 2 || hits[0].Time != 125 || hits[1].Time != 240 {
		t.Fatalf("hits = %#v, want silence end times", hits)
	}
}

func TestBenchmarkSourceHitsFromSilenceStartsUsesPositiveStartTimes(t *testing.T) {
	hits := benchmarkSourceHitsFromSilenceStarts([]detect.Silence{
		{Start: 115, Time: 125, Duration: 10},
		{Time: 240, Duration: 5},
		{Start: 300, Time: 305, Duration: 5},
	})

	if len(hits) != 2 || hits[0].Time != 115 || hits[1].Time != 300 {
		t.Fatalf("hits = %#v, want positive silence_start times only", hits)
	}
}

func TestBenchmarkSourceHitsFromBlackAndFreezeSegmentsUseSegmentEndTime(t *testing.T) {
	blackHits := benchmarkSourceHitsFromBlackSegments([]detect.BlackSegment{
		{Start: 120, End: 125, Duration: 5},
	})
	freezeHits := benchmarkSourceHitsFromFreezeSegments([]detect.FreezeSegment{
		{Start: 220, End: 240, Duration: 20},
	})

	if len(blackHits) != 1 || blackHits[0].Time != 125 {
		t.Fatalf("black hits = %#v, want black_end time", blackHits)
	}
	if len(freezeHits) != 1 || freezeHits[0].Time != 240 {
		t.Fatalf("freeze hits = %#v, want freeze_end time", freezeHits)
	}
}

func TestBenchmarkRawSignalSourcesUsesWindowedVisualSignals(t *testing.T) {
	noiseDB := detect.DefaultNoiseDB
	minDur := detect.DefaultMinDur
	signals := &benchmarkFakeRawSignals{
		silences: []detect.Silence{
			{Start: 90, Time: 100, Duration: autodetectVisualAnchorMinDur},
			{Start: 240, Time: 250, Duration: autodetectVisualAnchorMinDur - 1},
		},
		loudnessOnsets: []detect.LoudnessOnset{{Time: 101}},
		windowBlackSegments: map[float64][]detect.BlackSegment{
			100: {{Start: 90, End: 95, Duration: 5}},
		},
		windowFreezeSegments: map[float64][]detect.FreezeSegment{
			100: {{Start: 130, End: 135, Duration: 5}},
		},
		windowScenes: map[float64][]detect.SceneChange{
			100: {{Time: 112, Score: autodetectSceneThreshold}},
		},
		windowSamples: map[float64][]detect.ColorSample{
			100: {
				{Time: 120, YMean: 10, UMean: 10, VMean: 10},
				{Time: 121, YMean: 60, UMean: 10, VMean: 10},
			},
		},
	}
	req := autodetectRequest{
		UseSilence: true,
		UseColor:   true,
		UseOCR:     true,
		NoiseDB:    &noiseDB,
		MinDur:     &minDur,
	}

	sources, scope, notes, err := benchmarkRawSignalSources("sample_video", req, signals)
	if err != nil {
		t.Fatalf("benchmarkRawSignalSources returned error: %v", err)
	}

	if scope != "current-search-window" {
		t.Fatalf("scope = %q, want current-search-window", scope)
	}
	if len(sources) != 7 || sources[0].Name != "silence" || sources[1].Name != "silence_start" || sources[2].Name != "audio" || sources[3].Name != "black" || sources[4].Name != "freeze" || sources[5].Name != "scene" || sources[6].Name != "color" {
		t.Fatalf("sources = %#v, want silence/silence_start/audio/black/freeze/scene/color", sources)
	}
	if len(sources[0].Hits) != 2 || sources[0].Hits[0].Time != 100 || sources[0].Hits[1].Time != 250 {
		t.Fatalf("silence hits = %#v, want raw silence end times", sources[0].Hits)
	}
	if len(sources[1].Hits) != 2 || sources[1].Hits[0].Time != 90 || sources[1].Hits[1].Time != 240 {
		t.Fatalf("silence_start hits = %#v, want raw silence start times", sources[1].Hits)
	}
	if len(sources[2].Hits) != 1 || sources[2].Hits[0].Time != 101 {
		t.Fatalf("audio hits = %#v, want loudness onset hit", sources[2].Hits)
	}
	if len(sources[3].Hits) != 1 || sources[3].Hits[0].Time != 95 {
		t.Fatalf("black hits = %#v, want full-source black_end hit", sources[3].Hits)
	}
	if len(sources[4].Hits) != 1 || sources[4].Hits[0].Time != 135 {
		t.Fatalf("freeze hits = %#v, want full-source freeze_end hit", sources[4].Hits)
	}
	if len(sources[5].Hits) != 1 || sources[5].Hits[0].Time != 112 {
		t.Fatalf("scene hits = %#v, want window scene hit", sources[5].Hits)
	}
	if len(sources[6].Hits) != 1 || sources[6].Hits[0].Time != 121 {
		t.Fatalf("color hits = %#v, want window color shift hit", sources[6].Hits)
	}
	if len(signals.visualWindows) != 1 || signals.visualWindows[0].start != 100 || signals.visualWindows[0].duration != autodetectVisualAnchorWindow {
		t.Fatalf("visualWindows = %#v, want a single combined visual scan for the long-silence anchor", signals.visualWindows)
	}
	if len(signals.sceneWindows) != 0 || len(signals.colorWindows) != 0 || len(signals.blackWindows) != 0 || len(signals.freezeWindows) != 0 {
		t.Fatalf("sceneWindows=%#v colorWindows=%#v blackWindows=%#v freezeWindows=%#v, want the per-component window detectors unused", signals.sceneWindows, signals.colorWindows, signals.blackWindows, signals.freezeWindows)
	}
	if signals.fullBlackCalls != 0 || signals.fullFreezeCalls != 0 || signals.fullSceneCalls != 0 || signals.fullColorCalls != 0 || signals.ocrCalls != 0 {
		t.Fatalf("fullBlackCalls=%d fullFreezeCalls=%d fullSceneCalls=%d fullColorCalls=%d ocrCalls=%d, want no full-source or OCR calls", signals.fullBlackCalls, signals.fullFreezeCalls, signals.fullSceneCalls, signals.fullColorCalls, signals.ocrCalls)
	}
	if got := strings.Join(notes, "+"); got != "audio_rms_loudness_active+visual_windowed_around_silence_anchors+ocr_candidate_bound_not_raw" {
		t.Fatalf("notes = %q", got)
	}
}

func TestBenchmarkRawSignalSourcesSkipsVisualWhenNoWindowAnchors(t *testing.T) {
	noiseDB := detect.DefaultNoiseDB
	minDur := detect.DefaultMinDur
	signals := &benchmarkFakeRawSignals{
		silences: []detect.Silence{
			{Start: 95, Time: 100, Duration: autodetectVisualAnchorMinDur - 1},
		},
		scenes: []detect.SceneChange{{Time: 112, Score: autodetectSceneThreshold}},
		colorSamples: []detect.ColorSample{
			{Time: 120, YMean: 10, UMean: 10, VMean: 10},
			{Time: 121, YMean: 60, UMean: 10, VMean: 10},
		},
	}
	req := autodetectRequest{
		UseSilence: true,
		UseColor:   true,
		NoiseDB:    &noiseDB,
		MinDur:     &minDur,
	}

	sources, scope, notes, err := benchmarkRawSignalSources("sample_video", req, signals)
	if err != nil {
		t.Fatalf("benchmarkRawSignalSources returned error: %v", err)
	}

	if scope != "current-search-window" {
		t.Fatalf("scope = %q, want current-search-window", scope)
	}
	if len(sources) != 7 || sources[1].Name != "silence_start" || len(sources[1].Hits) != 1 ||
		sources[2].Name != "audio" || len(sources[2].Hits) != 0 ||
		sources[3].Name != "black" || len(sources[3].Hits) != 0 ||
		sources[4].Name != "freeze" || len(sources[4].Hits) != 0 ||
		sources[5].Name != "scene" || len(sources[5].Hits) != 0 ||
		sources[6].Name != "color" || len(sources[6].Hits) != 0 {
		t.Fatalf("sources = %#v, want empty black/freeze/scene/color sources", sources)
	}
	if signals.fullBlackCalls != 0 || signals.fullFreezeCalls != 0 || signals.fullSceneCalls != 0 || signals.fullColorCalls != 0 ||
		len(signals.visualWindows) != 0 || len(signals.blackWindows) != 0 || len(signals.freezeWindows) != 0 || len(signals.sceneWindows) != 0 || len(signals.colorWindows) != 0 {
		t.Fatalf("signal calls fullBlack=%d fullFreeze=%d fullScene=%d fullColor=%d visualWindows=%#v blackWindows=%#v freezeWindows=%#v sceneWindows=%#v colorWindows=%#v, want no visual scans", signals.fullBlackCalls, signals.fullFreezeCalls, signals.fullSceneCalls, signals.fullColorCalls, signals.visualWindows, signals.blackWindows, signals.freezeWindows, signals.sceneWindows, signals.colorWindows)
	}
	if got := strings.Join(notes, "+"); got != "audio_rms_loudness_active+visual_skipped_no_silence_anchors_current_window_only" {
		t.Fatalf("notes = %q", got)
	}
}

// countingVisualWindowSignals wraps benchmarkFakeRawSignals and counts calls
// to DetectVisualWindow, so a test can prove the underlying visual detector
// is invoked once per anchor even when both the raw-signal ceiling phase and
// production candidate generation request visual signals for that anchor.
type countingVisualWindowSignals struct {
	*benchmarkFakeRawSignals
	visualWindowCalls int
}

func (c *countingVisualWindowSignals) DetectVisualWindow(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64, start float64, duration float64) (detect.VisualSignals, error) {
	c.visualWindowCalls++
	return c.benchmarkFakeRawSignals.DetectVisualWindow(path, sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration, start, duration)
}

// TestCachedAutodetectSignalsReuseVisualWindowAcrossBenchmarkPhases proves
// the AC10 benchmark performance fix: the raw-signal ceiling phase
// (benchmarkRawSignalSources) and production candidate generation
// (buildAutodetectCandidatesWithStats) share one DetectVisualWindow
// result/cache entry per identical anchor+params, instead of each phase
// performing its own ffmpeg pass. Running both phases against the same
// cache-backed signals runner must invoke the underlying visual detector
// exactly once per anchor, not twice.
func TestCachedAutodetectSignalsReuseVisualWindowAcrossBenchmarkPhases(t *testing.T) {
	noiseDB := detect.DefaultNoiseDB
	minDur := detect.DefaultMinDur
	fake := &countingVisualWindowSignals{benchmarkFakeRawSignals: &benchmarkFakeRawSignals{
		silences: []detect.Silence{
			{Start: 90, Time: 100, Duration: autodetectVisualAnchorMinDur},
		},
		windowBlackSegments: map[float64][]detect.BlackSegment{
			100: {{Start: 90, End: 95, Duration: 5}},
		},
		windowFreezeSegments: map[float64][]detect.FreezeSegment{
			100: {{Start: 130, End: 135, Duration: 5}},
		},
		windowScenes: map[float64][]detect.SceneChange{
			100: {{Time: 112, Score: autodetectSceneThreshold + 1}},
		},
		windowSamples: map[float64][]detect.ColorSample{
			100: {
				{Time: 120, YMean: 10, UMean: 10, VMean: 10},
				{Time: 121, YMean: 60, UMean: 10, VMean: 10},
			},
		},
	}}
	cached := &cachedAutodetectSignals{inner: fake, dir: t.TempDir()}
	req := autodetectRequest{UseSilence: true, UseColor: true, NoiseDB: &noiseDB, MinDur: &minDur}

	if _, _, _, err := benchmarkRawSignalSources("sample_video", req, cached); err != nil {
		t.Fatalf("benchmarkRawSignalSources returned error: %v", err)
	}

	srv := &Server{autodetectSignals: cached}
	if _, _, err := srv.buildAutodetectCandidatesWithStats("sample_video", req); err != nil {
		t.Fatalf("buildAutodetectCandidatesWithStats returned error: %v", err)
	}

	if fake.visualWindowCalls != 1 {
		t.Fatalf("visualWindowCalls = %d, want 1 (shared visual-window cache entry across raw-signal and candidate-generation phases)", fake.visualWindowCalls)
	}
}

func TestFormatBenchmarkStageCounts(t *testing.T) {
	candidates := []Candidate{
		{Status: "candidate"},
		{Status: "named"},
		{Status: "candidate"},
		{},
	}

	got := formatBenchmarkStageCounts(candidates)
	if got != "candidate=2,named=1,unknown=1" {
		t.Fatalf("stage counts = %q", got)
	}
}

func TestBenchmarkRegressionGatesParseAndCheck(t *testing.T) {
	gates, err := parseBenchmarkRegressionGates(func(key string) (string, bool) {
		values := map[string]string{
			autodetectMinStartRecallEnv:    "0.50",
			autodetectMinStartPrecisionEnv: "0.40",
			autodetectMaxCandidatesEnv:     "5",
		}
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("parseBenchmarkRegressionGates returned error: %v", err)
	}

	passing := benchmarkScore{TruthCount: 4, CandidateCount: 5, Matches: []benchmarkMatch{{}, {}}}
	if err := gates.checkStart(passing); err != nil {
		t.Fatalf("checkStart returned error for passing score: %v", err)
	}

	failing := benchmarkScore{TruthCount: 4, CandidateCount: 6, Matches: []benchmarkMatch{{}}}
	err = gates.checkStart(failing)
	if err == nil || !strings.Contains(err.Error(), autodetectMinStartRecallEnv) {
		t.Fatalf("checkStart error = %v, want recall gate failure", err)
	}

	failingPrecision := benchmarkScore{TruthCount: 4, CandidateCount: 10, Matches: []benchmarkMatch{{}, {}}}
	err = gates.checkStart(failingPrecision)
	if err == nil || !strings.Contains(err.Error(), autodetectMinStartPrecisionEnv) {
		t.Fatalf("checkStart error = %v, want precision gate failure", err)
	}

	failingCandidateCount := benchmarkScore{TruthCount: 4, CandidateCount: 6, Matches: []benchmarkMatch{{}, {}, {}}}
	err = gates.checkStart(failingCandidateCount)
	if err == nil || !strings.Contains(err.Error(), autodetectMaxCandidatesEnv) {
		t.Fatalf("checkStart error = %v, want max candidate gate failure", err)
	}
}

func TestBenchmarkRegressionGatesRejectInvalidEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
		raw  string
	}{
		{name: "float", env: autodetectMinStartRecallEnv, raw: "not-a-number"},
		{name: "precision", env: autodetectMinStartPrecisionEnv, raw: "not-a-number"},
		{name: "int", env: autodetectMaxCandidatesEnv, raw: "1.5"},
		{name: "zero max candidates", env: autodetectMaxCandidatesEnv, raw: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseBenchmarkRegressionGates(func(key string) (string, bool) {
				if key == tt.env {
					return tt.raw, true
				}
				return "", false
			})
			if err == nil || !strings.Contains(err.Error(), tt.env) {
				t.Fatalf("parseBenchmarkRegressionGates error = %v, want env-specific parse error", err)
			}
		})
	}
}

func TestBenchmarkVisualSettingsParseAndApply(t *testing.T) {
	settings, err := parseBenchmarkVisualSettings(func(key string) (string, bool) {
		values := map[string]string{
			autodetectVisualSceneTopNEnv:  "4",
			autodetectVisualColorTopNEnv:  "5",
			autodetectVisualAnchorMinEnv:  "1.25",
			autodetectMinOutputScoreEnv:   "1.35",
			autodetectMinSilenceOutputEnv: "4.5",
		}
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("parseBenchmarkVisualSettings returned error: %v", err)
	}

	restore := applyBenchmarkVisualSettings(settings)
	defer restore()

	if autodetectSceneCandidateLimit != 4 {
		t.Fatalf("autodetectSceneCandidateLimit = %d, want 4", autodetectSceneCandidateLimit)
	}
	if autodetectColorCandidateLimit != 5 {
		t.Fatalf("autodetectColorCandidateLimit = %d, want 5", autodetectColorCandidateLimit)
	}
	if autodetectVisualAnchorMinDur != 1.25 {
		t.Fatalf("autodetectVisualAnchorMinDur = %v, want 1.25", autodetectVisualAnchorMinDur)
	}
	if autodetectLineupOutputMinScore != 1.35 {
		t.Fatalf("autodetectLineupOutputMinScore = %v, want 1.35", autodetectLineupOutputMinScore)
	}
	if autodetectSilenceOutputMinDur != 4.5 {
		t.Fatalf("autodetectSilenceOutputMinDur = %v, want 4.5", autodetectSilenceOutputMinDur)
	}
}

func TestBenchmarkVisualSettingsRejectInvalidEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
		raw  string
	}{
		{name: "scene top n", env: autodetectVisualSceneTopNEnv, raw: "0"},
		{name: "color top n", env: autodetectVisualColorTopNEnv, raw: "-1"},
		{name: "anchor min duration", env: autodetectVisualAnchorMinEnv, raw: "not-a-number"},
		{name: "zero anchor min duration", env: autodetectVisualAnchorMinEnv, raw: "0"},
		{name: "min output score", env: autodetectMinOutputScoreEnv, raw: "not-a-number"},
		{name: "negative min output score", env: autodetectMinOutputScoreEnv, raw: "-0.1"},
		{name: "min silence output duration", env: autodetectMinSilenceOutputEnv, raw: "not-a-number"},
		{name: "negative min silence output duration", env: autodetectMinSilenceOutputEnv, raw: "-0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseBenchmarkVisualSettings(func(key string) (string, bool) {
				if key == tt.env {
					return tt.raw, true
				}
				return "", false
			})
			if err == nil || !strings.Contains(err.Error(), tt.env) {
				t.Fatalf("parseBenchmarkVisualSettings error = %v, want env-specific parse error", err)
			}
		})
	}
}

func TestFormatBenchmarkOmitsRealTimesAndNames(t *testing.T) {
	truth := []benchmarkTruth{
		{Name: "group-a", Start: 60},
		{Name: "group-b", Start: 120},
	}
	candidates := []Candidate{
		{Time: 60, Duration: 2, Status: "candidate", Sources: []string{"silence"}, SuggestedName: "group-a"},
		{Time: 180, Duration: 3, Status: "candidate", Sources: []string{"ocr"}, SuggestedName: "dummy-ocr-text"},
	}

	got := scoreAutodetectBenchmark(truth, benchmarkTruthStartTime, candidates, 5).format(truth, benchmarkTruthStartTime, candidates)
	for _, disallowed := range []string{"00:01:00", "00:02:00", "00:03:00", "group-a", "group-b", "dummy-ocr-text"} {
		if strings.Contains(got, disallowed) {
			t.Fatalf("format output = %q, should not contain %q", got, disallowed)
		}
	}
}

func TestFormatBenchmarkRunMetadataAnonymizesPaths(t *testing.T) {
	got := formatBenchmarkRunMetadata(
		"/Users/alice/private-samples/2026-finals",
		"/Users/alice/private-samples/2026-finals/Semi Finals Secret.mkv",
		"/Users/alice/.cache/vid-streamer/autodetect-eval/private-cache-key",
		"silence+visual",
	)

	for _, want := range []string{"sample=sample_video", "source=sample_video", "sampleDir=sample-", "sourceID=source-", "cache=cache-", "signalMode=silence+visual"} {
		if !strings.Contains(got, want) {
			t.Fatalf("metadata = %q, want %q", got, want)
		}
	}
	for _, disallowed := range []string{
		"/Users/alice",
		"private-samples",
		"2026-finals",
		"Semi Finals Secret.mkv",
		"private-cache-key",
	} {
		if strings.Contains(got, disallowed) {
			t.Fatalf("metadata = %q, should not include %q", got, disallowed)
		}
	}
}

func TestBenchmarkCacheDirIncludesSignalCacheVersion(t *testing.T) {
	const cacheRoot = "benchmark-cache-root"
	t.Setenv(autodetectCacheDirEnv, cacheRoot)

	sourcePath := "autodetect_eval_test.go"
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		t.Fatalf("resolve source: %v", err)
	}
	stat, err := os.Stat(absSource)
	if err != nil {
		t.Fatalf("stat source: %v", err)
	}

	keyMaterial := fmt.Sprintf("%s\x00%d\x00%d\x00%s", absSource, stat.Size(), stat.ModTime().UnixNano(), benchmarkSignalCacheVersion)
	hash := sha256.Sum256([]byte(keyMaterial))
	want := filepath.Join(cacheRoot, hex.EncodeToString(hash[:8]))

	got, err := benchmarkCacheDir(sourcePath)
	if err != nil {
		t.Fatalf("benchmarkCacheDir: %v", err)
	}
	if got != want {
		t.Fatalf("benchmarkCacheDir() = %q, want %q", got, want)
	}
}

func assertBenchmarkFloatNear(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0001 {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func loadBenchmarkTimestamps(path string) ([]benchmarkTruth, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var truth []benchmarkTruth
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		timeIndex := benchmarkTimestampRE.FindStringIndex(line)
		if timeIndex == nil {
			continue
		}
		timeFields := benchmarkTimestampRE.FindAllString(line, -1)
		start, err := parseClockSeconds(timeFields[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		var stop float64
		if len(timeFields) > 1 {
			stop, err = parseClockSeconds(timeFields[1])
			if err != nil {
				return nil, fmt.Errorf("line %d stop: %w", lineNo, err)
			}
		}
		name := strings.TrimSpace(line[:timeIndex[0]])
		if name == "" {
			name = fmt.Sprintf("group-%02d", len(truth)+1)
		}
		truth = append(truth, benchmarkTruth{Name: name, Start: start, Stop: stop})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(truth) == 0 {
		return nil, fmt.Errorf("%s did not contain timestamp rows", path)
	}
	sort.SliceStable(truth, func(i, j int) bool {
		return truth[i].Start < truth[j].Start
	})
	return truth, nil
}

func loadBenchmarkLineup(path string) ([]autodetectLineupEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lineup []autodetectLineupEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		matches := benchmarkOrderRE.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		name := strings.TrimSpace(matches[1])
		if name != "" {
			lineup = append(lineup, autodetectLineupEntry{Name: name})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lineup, nil
}

func lineupFromTruth(truth []benchmarkTruth) []autodetectLineupEntry {
	lineup := make([]autodetectLineupEntry, 0, len(truth))
	seen := make(map[string]struct{}, len(truth))
	for _, entry := range truth {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		lineup = append(lineup, autodetectLineupEntry{Name: name})
	}
	return lineup
}

func benchmarkAutodetectRequest(lineup []autodetectLineupEntry, signalMode string) (autodetectRequest, error) {
	req := autodetectRequest{Lineup: lineup, UseSilence: true}
	switch strings.ToLower(signalMode) {
	case "", "silence", "silence-only":
	case "visual", "silence+visual":
		req.UseColor = true
	case "ocr", "silence+ocr":
		req.UseOCR = true
	case "all", "default", "silence+visual+ocr":
		req.UseColor = true
		req.UseOCR = true
	default:
		return autodetectRequest{}, fmt.Errorf("unknown %s %q; use silence, visual, ocr, or all", autodetectSignalModeEnv, signalMode)
	}
	return normalizeAutodetectRequest(req)
}

func benchmarkSignalMode(req autodetectRequest) string {
	var parts []string
	if req.UseSilence {
		parts = append(parts, "silence")
	}
	if req.UseColor {
		parts = append(parts, "visual")
	}
	if req.UseOCR {
		parts = append(parts, "ocr")
	}
	return strings.Join(parts, "+")
}

type benchmarkTimeSelector func(benchmarkTruth) float64

func benchmarkTruthStartTime(entry benchmarkTruth) float64 {
	return entry.Start
}

func benchmarkTruthStopTime(entry benchmarkTruth) float64 {
	return entry.Stop
}

func hasBenchmarkStops(truth []benchmarkTruth) bool {
	for _, entry := range truth {
		if entry.Stop > 0 {
			return true
		}
	}
	return false
}

func scoreAutodetectBenchmark(truth []benchmarkTruth, truthTime benchmarkTimeSelector, candidates []Candidate, tolerance float64) benchmarkScore {
	selectedTruth := make(map[int]struct{}, len(truth))
	for truthIndex, truthEntry := range truth {
		// Treat zero as not recorded; stop times commonly omit a value.
		if truthTime(truthEntry) > 0 {
			selectedTruth[truthIndex] = struct{}{}
		}
	}
	var pairs []benchmarkPair
	for truthIndex, truthEntry := range truth {
		selectedTruthTime := truthTime(truthEntry)
		if selectedTruthTime <= 0 {
			continue
		}
		for candidateIndex, candidate := range candidates {
			delta := candidate.Time - selectedTruthTime
			if math.Abs(delta) <= tolerance {
				pairs = append(pairs, benchmarkPair{truthIndex: truthIndex, candidateIndex: candidateIndex, delta: delta})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return math.Abs(pairs[i].delta) < math.Abs(pairs[j].delta)
	})

	matchedTruth := make(map[int]struct{}, len(truth))
	matchedCandidate := make(map[int]struct{}, len(candidates))
	score := benchmarkScore{Tolerance: tolerance, TruthCount: len(selectedTruth), CandidateCount: len(candidates)}
	for _, pair := range pairs {
		if _, ok := matchedTruth[pair.truthIndex]; ok {
			continue
		}
		if _, ok := matchedCandidate[pair.candidateIndex]; ok {
			continue
		}
		matchedTruth[pair.truthIndex] = struct{}{}
		matchedCandidate[pair.candidateIndex] = struct{}{}
		score.Matches = append(score.Matches, benchmarkMatch{
			TruthIndex:     pair.truthIndex,
			CandidateIndex: pair.candidateIndex,
			TruthTime:      truthTime(truth[pair.truthIndex]),
			CandidateTime:  candidates[pair.candidateIndex].Time,
			Delta:          pair.delta,
		})
	}

	for index := range truth {
		if _, ok := selectedTruth[index]; !ok {
			continue
		}
		if _, ok := matchedTruth[index]; !ok {
			score.UnmatchedTruth = append(score.UnmatchedTruth, index)
		}
	}
	for index, candidate := range candidates {
		if _, ok := matchedCandidate[index]; ok {
			continue
		}
		score.UnmatchedCandidate = append(score.UnmatchedCandidate, index)
		if nearMatchedTruth(candidate.Time, score.Matches, tolerance) {
			score.Duplicates++
		}
	}
	sort.SliceStable(score.Matches, func(i, j int) bool {
		return score.Matches[i].TruthTime < score.Matches[j].TruthTime
	})
	return score
}

func nearMatchedTruth(candidateTime float64, matches []benchmarkMatch, tolerance float64) bool {
	for _, match := range matches {
		if math.Abs(candidateTime-match.TruthTime) <= tolerance {
			return true
		}
	}
	return false
}

func (s benchmarkScore) precision() float64 {
	if s.CandidateCount == 0 {
		return 0
	}
	return float64(len(s.Matches)) / float64(s.CandidateCount)
}

func (s benchmarkScore) recall() float64 {
	if s.TruthCount == 0 {
		return 0
	}
	return float64(len(s.Matches)) / float64(s.TruthCount)
}

func (s benchmarkScore) f1() float64 {
	return s.fBeta(1)
}

func (s benchmarkScore) fBeta(beta float64) float64 {
	if beta <= 0 || math.IsNaN(beta) || math.IsInf(beta, 0) {
		return 0
	}
	precision := s.precision()
	recall := s.recall()
	if precision == 0 || recall == 0 {
		return 0
	}
	betaSquared := beta * beta
	return (1 + betaSquared) * precision * recall / (betaSquared*precision + recall)
}

func benchmarkRecallAtK(truth []benchmarkTruth, truthTime benchmarkTimeSelector, candidates []Candidate, tolerance float64, k int) float64 {
	if k <= 0 {
		return 0
	}
	if k > len(candidates) {
		k = len(candidates)
	}
	return scoreAutodetectBenchmark(truth, truthTime, candidates[:k], tolerance).recall()
}

func benchmarkToleranceSweep(truth []benchmarkTruth, truthTime benchmarkTimeSelector, candidates []Candidate, tolerances []float64) []benchmarkScore {
	scores := make([]benchmarkScore, 0, len(tolerances))
	for _, tolerance := range tolerances {
		scores = append(scores, scoreAutodetectBenchmark(truth, truthTime, candidates, tolerance))
	}
	return scores
}

func benchmarkRawSignalSources(sourcePath string, req autodetectRequest, signals autodetectSignals) ([]benchmarkRawSource, string, []string, error) {
	audio, err := signals.DetectAudio(sourcePath, *req.NoiseDB, *req.MinDur)
	if err != nil {
		return nil, "", nil, fmt.Errorf("audio raw signal failed: %w", err)
	}
	silences := audio.Silences

	sources := []benchmarkRawSource{{
		Name: autodetectSourceSilence,
		Hits: benchmarkSourceHitsFromSilences(silences),
	}, {
		Name: benchmarkSourceSilenceStart,
		Hits: benchmarkSourceHitsFromSilenceStarts(silences),
	}, {
		Name: autodetectSourceAudio,
		Hits: benchmarkSourceHitsFromLoudnessOnsets(audio.LoudnessOnsets),
	}}
	scope := "current-search-window"
	notes := []string{benchmarkAudioOnsetActiveNote}

	if req.UseColor {
		anchors := visualWindowAnchors(candidatesFromSilences(silences))
		if len(anchors) == 0 {
			notes = append(notes, "visual_skipped_no_silence_anchors_current_window_only")
			sources = append(sources,
				benchmarkRawSource{Name: autodetectSourceBlack},
				benchmarkRawSource{Name: autodetectSourceFreeze},
				benchmarkRawSource{Name: autodetectSourceScene},
				benchmarkRawSource{Name: autodetectSourceColor},
			)
		} else {
			blackSegments, freezeSegments, scenes, shifts, err := benchmarkWindowedVisualRawSignals(sourcePath, anchors, signals)
			if err != nil {
				return nil, "", nil, err
			}
			sources = append(sources,
				benchmarkRawSource{Name: autodetectSourceBlack, Hits: benchmarkSourceHitsFromBlackSegments(blackSegments)},
				benchmarkRawSource{Name: autodetectSourceFreeze, Hits: benchmarkSourceHitsFromFreezeSegments(freezeSegments)},
				benchmarkRawSource{Name: autodetectSourceScene, Hits: benchmarkSourceHitsFromScenes(scenes)},
				benchmarkRawSource{Name: autodetectSourceColor, Hits: benchmarkSourceHitsFromColorShifts(shifts)},
			)
			notes = append(notes, "visual_windowed_around_silence_anchors")
		}
	} else {
		notes = append(notes, "visual_disabled")
	}
	if req.UseOCR {
		notes = append(notes, "ocr_candidate_bound_not_raw")
	}

	return sources, scope, notes, nil
}

// benchmarkWindowedVisualRawSignals computes the raw-signal ceiling report's
// black/freeze/scene/color hits per silence anchor. It calls the same
// combined signals.DetectVisualWindow(...) that production candidate
// generation uses (autodetectVisualCandidatesWindow in autodetect.go), with
// identical parameter values (sceneThreshold, sceneColorSampleRate,
// blackMinDuration, freezeMinDuration, anchor.Time, autodetectVisualAnchorWindow).
// Because both phases now call the exact same function with the exact same
// arguments, cachedAutodetectSignals resolves them to the same
// "visual-window" cache key/anchor, so a benchmark run reuses whichever
// phase (raw ceiling or candidate generation) scans a given anchor first
// instead of performing a separate ffmpeg pass per phase.
func benchmarkWindowedVisualRawSignals(sourcePath string, anchors []Candidate, signals autodetectSignals) ([]detect.BlackSegment, []detect.FreezeSegment, []detect.SceneChange, []detect.ColorShift, error) {
	var blackSegments []detect.BlackSegment
	var freezeSegments []detect.FreezeSegment
	var scenes []detect.SceneChange
	var shifts []detect.ColorShift
	for _, anchor := range anchors {
		windowSignals, err := signals.DetectVisualWindow(sourcePath, autodetectSceneThreshold, autodetectColorSampleRate, autodetectBlackMinDuration, autodetectFreezeMinDuration, anchor.Time, autodetectVisualAnchorWindow)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("visual raw signal failed: %w", err)
		}
		blackSegments = append(blackSegments, windowSignals.BlackSegments...)
		freezeSegments = append(freezeSegments, windowSignals.FreezeSegments...)
		scenes = append(scenes, windowSignals.Scenes...)
		shifts = append(shifts, detect.DetectColorShifts(windowSignals.ColorSamples, autodetectColorShiftThreshold, autodetectColorWindowSeconds)...)
	}
	return blackSegments, freezeSegments, scenes, shifts, nil
}

func benchmarkSourceHitsFromSilences(silences []detect.Silence) []benchmarkSourceHit {
	hits := make([]benchmarkSourceHit, 0, len(silences))
	for _, sil := range silences {
		hits = append(hits, benchmarkSourceHit{Time: sil.Time})
	}
	return hits
}

func benchmarkSourceHitsFromSilenceStarts(silences []detect.Silence) []benchmarkSourceHit {
	hits := make([]benchmarkSourceHit, 0, len(silences))
	for _, sil := range silences {
		if sil.Start <= 0 {
			continue
		}
		hits = append(hits, benchmarkSourceHit{Time: sil.Start})
	}
	return hits
}

func benchmarkSourceHitsFromLoudnessOnsets(onsets []detect.LoudnessOnset) []benchmarkSourceHit {
	hits := make([]benchmarkSourceHit, 0, len(onsets))
	for _, onset := range onsets {
		hits = append(hits, benchmarkSourceHit{Time: onset.Time})
	}
	return hits
}

func benchmarkSourceHitsFromBlackSegments(segments []detect.BlackSegment) []benchmarkSourceHit {
	hits := make([]benchmarkSourceHit, 0, len(segments))
	for _, segment := range segments {
		hits = append(hits, benchmarkSourceHit{Time: segment.End})
	}
	return hits
}

func benchmarkSourceHitsFromFreezeSegments(segments []detect.FreezeSegment) []benchmarkSourceHit {
	hits := make([]benchmarkSourceHit, 0, len(segments))
	for _, segment := range segments {
		hits = append(hits, benchmarkSourceHit{Time: segment.End})
	}
	return hits
}

func benchmarkSourceHitsFromScenes(scenes []detect.SceneChange) []benchmarkSourceHit {
	hits := make([]benchmarkSourceHit, 0, len(scenes))
	for _, scene := range scenes {
		if scene.Score < autodetectSceneThreshold {
			continue
		}
		hits = append(hits, benchmarkSourceHit{Time: scene.Time})
	}
	return hits
}

func benchmarkSourceHitsFromColorShifts(shifts []detect.ColorShift) []benchmarkSourceHit {
	hits := make([]benchmarkSourceHit, 0, len(shifts))
	for _, shift := range shifts {
		hits = append(hits, benchmarkSourceHit{Time: shift.Time})
	}
	return hits
}

func benchmarkRawSignalOracle(truth []benchmarkTruth, truthTime benchmarkTimeSelector, sources []benchmarkRawSource, tolerance float64, scope string, notes []string) benchmarkOracleReport {
	if strings.TrimSpace(scope) == "" {
		scope = "unknown"
	}
	report := benchmarkOracleReport{
		Tolerance:     tolerance,
		Scope:         scope,
		Notes:         append([]string(nil), notes...),
		SourceMatched: make(map[string]int, len(sources)),
		TruthSources:  make(map[int][]string, len(truth)),
	}
	for index, entry := range truth {
		if truthTime(entry) > 0 {
			report.SelectedTruth = append(report.SelectedTruth, index)
		}
	}
	report.TruthCount = len(report.SelectedTruth)

	seenSources := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			continue
		}
		if _, ok := seenSources[name]; !ok {
			seenSources[name] = struct{}{}
			report.SourceNames = append(report.SourceNames, name)
		}
		matched := benchmarkMatchedTruthIndexes(truth, truthTime, report.SelectedTruth, source.Hits, tolerance)
		report.SourceMatched[name] += len(matched)
		for _, truthIndex := range matched {
			report.TruthSources[truthIndex] = append(report.TruthSources[truthIndex], name)
		}
	}

	anyMatched := make(map[int]struct{}, len(report.SelectedTruth))
	for _, truthIndex := range report.SelectedTruth {
		sources := report.TruthSources[truthIndex]
		if len(sources) == 0 {
			report.MissingTruth = append(report.MissingTruth, truthIndex)
			continue
		}
		report.TruthSources[truthIndex] = compactSourcesByOrder(sources, report.SourceNames)
		anyMatched[truthIndex] = struct{}{}
	}
	report.AnyMatched = len(anyMatched)
	return report
}

func benchmarkMatchedTruthIndexes(truth []benchmarkTruth, truthTime benchmarkTimeSelector, selectedTruth []int, hits []benchmarkSourceHit, tolerance float64) []int {
	matched := make(map[int]struct{}, len(selectedTruth))
	for _, truthIndex := range selectedTruth {
		selectedTruthTime := truthTime(truth[truthIndex])
		for _, hit := range hits {
			if math.Abs(hit.Time-selectedTruthTime) <= tolerance {
				matched[truthIndex] = struct{}{}
				break
			}
		}
	}
	indexes := make([]int, 0, len(matched))
	for truthIndex := range matched {
		indexes = append(indexes, truthIndex)
	}
	sort.Ints(indexes)
	return indexes
}

func compactSourcesByOrder(values []string, sourceOrder []string) []string {
	if len(values) == 0 {
		return nil
	}
	present := make(map[string]struct{}, len(values))
	for _, value := range values {
		present[value] = struct{}{}
	}
	compact := make([]string, 0, len(present))
	for _, source := range sourceOrder {
		if _, ok := present[source]; ok {
			compact = append(compact, source)
			delete(present, source)
		}
	}
	if len(present) > 0 {
		extra := make([]string, 0, len(present))
		for source := range present {
			extra = append(extra, source)
		}
		sort.Strings(extra)
		compact = append(compact, extra...)
	}
	return compact
}

func formatBenchmarkToleranceSweep(scores []benchmarkScore) string {
	if len(scores) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(scores))
	for _, score := range scores {
		parts = append(parts, fmt.Sprintf("%.0fs=%d/%d P=%.1f%% R=%.1f%% F1=%.1f%%",
			score.Tolerance,
			len(score.Matches),
			score.TruthCount,
			score.precision()*100,
			score.recall()*100,
			score.f1()*100,
		))
	}
	return strings.Join(parts, ",")
}

func formatBenchmarkRunMetadata(sampleDir, sourcePath, cacheDir, signalMode string) string {
	return fmt.Sprintf("sample=sample_video sampleDir=%s source=sample_video sourceID=%s cache=%s signalMode=%s",
		formatBenchmarkPathLabel("sample", sampleDir),
		formatBenchmarkPathLabel("source", sourcePath),
		formatBenchmarkPathLabel("cache", cacheDir),
		signalMode,
	)
}

func formatBenchmarkPathLabel(prefix, raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return prefix + "-" + hex.EncodeToString(hash[:6])
}

func (s benchmarkScore) format(truth []benchmarkTruth, truthTime benchmarkTimeSelector, candidates []Candidate) string {
	var b strings.Builder
	errors := s.absoluteErrors()
	reviewRecallLimit := benchmarkRecallAtReviewLimit
	fmt.Fprintf(&b, "truth=%d candidates=%d matched=%d tolerance=%.0fs precision=%.1f%% recall=%.1f%% f1=%.1f%% f2=%.1f%% recall@%d=%.1f%% recall@%d=%.1f%% duplicates=%d median_error=%s p90_error=%s",
		s.TruthCount,
		s.CandidateCount,
		len(s.Matches),
		s.Tolerance,
		s.precision()*100,
		s.recall()*100,
		s.f1()*100,
		s.fBeta(2)*100,
		s.TruthCount,
		benchmarkRecallAtK(truth, truthTime, candidates, s.Tolerance, s.TruthCount)*100,
		reviewRecallLimit,
		benchmarkRecallAtK(truth, truthTime, candidates, s.Tolerance, reviewRecallLimit)*100,
		s.Duplicates,
		formatOptionalSeconds(percentile(errors, 0.50)),
		formatOptionalSeconds(percentile(errors, 0.90)),
	)
	if len(candidates) > 0 {
		fmt.Fprintf(&b, "\nstage_counts=%s", formatBenchmarkStageCounts(candidates))
		fmt.Fprintf(&b, "\nsource_counts=%s", formatBenchmarkSourceAttribution(candidates))
	}
	if len(s.UnmatchedTruth) > 0 {
		fmt.Fprintf(&b, "\nnearest_misses=%s", formatNearestUnmatchedTruth(truth, truthTime, candidates, s.UnmatchedTruth))
	}
	if len(s.UnmatchedCandidate) > 0 {
		fmt.Fprintf(&b, "\nunmatched_candidates=%s", formatUnmatchedCandidates(candidates, s.UnmatchedCandidate, 30))
	}
	if len(s.Matches) > 0 {
		fmt.Fprintf(&b, "\nmatches=%s", formatBenchmarkMatches(s.Matches, 30))
	}
	return b.String()
}

func parseBenchmarkRegressionGates(lookup func(string) (string, bool)) (benchmarkRegressionGates, error) {
	minRecall, err := parseOptionalBenchmarkGateFloat(lookup, autodetectMinStartRecallEnv)
	if err != nil {
		return benchmarkRegressionGates{}, err
	}
	minPrecision, err := parseOptionalBenchmarkGateFloat(lookup, autodetectMinStartPrecisionEnv)
	if err != nil {
		return benchmarkRegressionGates{}, err
	}
	maxCandidates, err := parseOptionalBenchmarkGateInt(lookup, autodetectMaxCandidatesEnv)
	if err != nil {
		return benchmarkRegressionGates{}, err
	}
	return benchmarkRegressionGates{
		minStartRecall:    minRecall,
		minStartPrecision: minPrecision,
		maxCandidates:     maxCandidates,
	}, nil
}

func parseBenchmarkVisualSettings(lookup func(string) (string, bool)) (benchmarkVisualSettings, error) {
	sceneLimit, err := parseOptionalBenchmarkGateInt(lookup, autodetectVisualSceneTopNEnv)
	if err != nil {
		return benchmarkVisualSettings{}, err
	}
	colorLimit, err := parseOptionalBenchmarkGateInt(lookup, autodetectVisualColorTopNEnv)
	if err != nil {
		return benchmarkVisualSettings{}, err
	}
	anchorMinDur, err := parseOptionalBenchmarkPositiveFloat(lookup, autodetectVisualAnchorMinEnv)
	if err != nil {
		return benchmarkVisualSettings{}, err
	}
	minOutputScore, err := parseOptionalBenchmarkNonNegativeFloat(lookup, autodetectMinOutputScoreEnv)
	if err != nil {
		return benchmarkVisualSettings{}, err
	}
	minSilenceOutputDur, err := parseOptionalBenchmarkNonNegativeFloat(lookup, autodetectMinSilenceOutputEnv)
	if err != nil {
		return benchmarkVisualSettings{}, err
	}
	return benchmarkVisualSettings{
		sceneCandidateLimit: sceneLimit,
		colorCandidateLimit: colorLimit,
		anchorMinDur:        anchorMinDur,
		minOutputScore:      minOutputScore,
		minSilenceOutputDur: minSilenceOutputDur,
	}, nil
}

func applyBenchmarkVisualSettings(settings benchmarkVisualSettings) func() {
	previousSceneLimit := autodetectSceneCandidateLimit
	previousColorLimit := autodetectColorCandidateLimit
	previousAnchorMinDur := autodetectVisualAnchorMinDur
	previousMinOutputScore := autodetectLineupOutputMinScore
	previousMinSilenceOutputDur := autodetectSilenceOutputMinDur
	if settings.sceneCandidateLimit != nil {
		autodetectSceneCandidateLimit = *settings.sceneCandidateLimit
	}
	if settings.colorCandidateLimit != nil {
		autodetectColorCandidateLimit = *settings.colorCandidateLimit
	}
	if settings.anchorMinDur != nil {
		autodetectVisualAnchorMinDur = *settings.anchorMinDur
	}
	if settings.minOutputScore != nil {
		autodetectLineupOutputMinScore = *settings.minOutputScore
	}
	if settings.minSilenceOutputDur != nil {
		autodetectSilenceOutputMinDur = *settings.minSilenceOutputDur
	}
	return func() {
		autodetectSceneCandidateLimit = previousSceneLimit
		autodetectColorCandidateLimit = previousColorLimit
		autodetectVisualAnchorMinDur = previousAnchorMinDur
		autodetectLineupOutputMinScore = previousMinOutputScore
		autodetectSilenceOutputMinDur = previousMinSilenceOutputDur
	}
}

func parseOptionalBenchmarkGateFloat(lookup func(string) (string, bool), envName string) (*float64, error) {
	raw, ok := lookup(envName)
	if !ok {
		return nil, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return nil, fmt.Errorf("%s must be a number between 0 and 1, got %q", envName, raw)
	}
	return &value, nil
}

func parseOptionalBenchmarkPositiveFloat(lookup func(string) (string, bool), envName string) (*float64, error) {
	raw, ok := lookup(envName)
	if !ok {
		return nil, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return nil, fmt.Errorf("%s must be a positive number, got %q", envName, raw)
	}
	return &value, nil
}

func parseOptionalBenchmarkNonNegativeFloat(lookup func(string) (string, bool), envName string) (*float64, error) {
	raw, ok := lookup(envName)
	if !ok {
		return nil, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil, fmt.Errorf("%s must be a non-negative number, got %q", envName, raw)
	}
	return &value, nil
}

func parseOptionalBenchmarkGateInt(lookup func(string) (string, bool), envName string) (*int, error) {
	raw, ok := lookup(envName)
	if !ok {
		return nil, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer, got %q", envName, raw)
	}
	return &value, nil
}

func (g benchmarkRegressionGates) checkStart(score benchmarkScore) error {
	if g.minStartRecall != nil && score.recall()+benchmarkRegressionGateEpsilon < *g.minStartRecall {
		return fmt.Errorf("%s requires start recall >= %.4f, got %.4f", autodetectMinStartRecallEnv, *g.minStartRecall, score.recall())
	}
	if g.minStartPrecision != nil && score.precision()+benchmarkRegressionGateEpsilon < *g.minStartPrecision {
		return fmt.Errorf("%s requires start precision >= %.4f, got %.4f", autodetectMinStartPrecisionEnv, *g.minStartPrecision, score.precision())
	}
	if g.maxCandidates != nil && score.CandidateCount > *g.maxCandidates {
		return fmt.Errorf("%s requires candidates <= %d, got %d", autodetectMaxCandidatesEnv, *g.maxCandidates, score.CandidateCount)
	}
	return nil
}

func (s benchmarkScore) absoluteErrors() []float64 {
	errors := make([]float64, 0, len(s.Matches))
	for _, match := range s.Matches {
		errors = append(errors, math.Abs(match.Delta))
	}
	sort.Float64s(errors)
	return errors
}

func percentile(sorted []float64, p float64) (float64, bool) {
	if len(sorted) == 0 {
		return 0, false
	}
	if p <= 0 {
		return sorted[0], true
	}
	if p >= 1 {
		return sorted[len(sorted)-1], true
	}
	index := int(math.Ceil(p*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index], true
}

func formatOptionalSeconds(value float64, ok bool) string {
	if !ok {
		return "n/a"
	}
	return fmt.Sprintf("%.1fs", value)
}

func formatUnmatchedCandidates(candidates []Candidate, indexes []int, limit int) string {
	parts := make([]string, 0, min(len(indexes), limit))
	for _, index := range indexes {
		if len(parts) >= limit {
			break
		}
		parts = append(parts, fmt.Sprintf("candidate-%02d(%.1fs)", index+1, candidates[index].Duration))
	}
	if len(indexes) > limit {
		parts = append(parts, fmt.Sprintf("...+%d", len(indexes)-limit))
	}
	return strings.Join(parts, ",")
}

func formatBenchmarkMatches(matches []benchmarkMatch, limit int) string {
	parts := make([]string, 0, min(len(matches), limit))
	for _, match := range matches {
		if len(parts) >= limit {
			break
		}
		parts = append(parts, fmt.Sprintf("group-%02d(%+.1fs)", match.TruthIndex+1, match.Delta))
	}
	if len(matches) > limit {
		parts = append(parts, fmt.Sprintf("...+%d", len(matches)-limit))
	}
	return strings.Join(parts, ",")
}

func formatNearestUnmatchedTruth(truth []benchmarkTruth, truthTime benchmarkTimeSelector, candidates []Candidate, indexes []int) string {
	parts := make([]string, 0, len(indexes))
	for _, index := range indexes {
		selectedTruthTime := truthTime(truth[index])
		if selectedTruthTime <= 0 {
			continue
		}
		delta, ok := nearestCandidateDelta(selectedTruthTime, candidates)
		if !ok {
			parts = append(parts, fmt.Sprintf("group-%02d@truth->none", index+1))
			continue
		}
		parts = append(parts, fmt.Sprintf("group-%02d@truth->candidate(%+.1fs)", index+1, delta))
	}
	return strings.Join(parts, ",")
}

func formatBenchmarkRawSignalCeiling(report benchmarkOracleReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "raw_signal_ceiling tolerance=%.0fs scope=%s", report.Tolerance, report.Scope)
	for _, source := range report.SourceNames {
		fmt.Fprintf(&b, " %s=%d/%d", source, report.SourceMatched[source], report.TruthCount)
	}
	fmt.Fprintf(&b, " any=%d/%d missing=%s", report.AnyMatched, report.TruthCount, formatBenchmarkMissingTruth(report.MissingTruth, 12))
	if len(report.Notes) > 0 {
		fmt.Fprintf(&b, " notes=%s", strings.Join(report.Notes, "+"))
	}
	return b.String()
}

func formatBenchmarkRawSignalMatrix(report benchmarkOracleReport) string {
	if len(report.SelectedTruth) == 0 {
		return "raw_signal_matrix none"
	}
	parts := make([]string, 0, len(report.SelectedTruth))
	for _, truthIndex := range report.SelectedTruth {
		sources := report.TruthSources[truthIndex]
		value := "none"
		if len(sources) > 0 {
			value = strings.Join(sources, "+")
		}
		parts = append(parts, fmt.Sprintf("group-%02d=%s", truthIndex+1, value))
	}
	return "raw_signal_matrix " + strings.Join(parts, " ")
}

func formatBenchmarkMissingTruth(indexes []int, limit int) string {
	if len(indexes) == 0 {
		return "none"
	}
	if limit <= 0 {
		limit = len(indexes)
	}
	parts := make([]string, 0, min(len(indexes), limit)+1)
	for _, index := range indexes {
		if len(parts) >= limit {
			break
		}
		parts = append(parts, fmt.Sprintf("group-%02d", index+1))
	}
	if len(indexes) > limit {
		parts = append(parts, fmt.Sprintf("...+%d", len(indexes)-limit))
	}
	return strings.Join(parts, ",")
}

func nearestCandidateDelta(truthTime float64, candidates []Candidate) (float64, bool) {
	if len(candidates) == 0 {
		return 0, false
	}
	nearestDelta := candidates[0].Time - truthTime
	for _, candidate := range candidates[1:] {
		delta := candidate.Time - truthTime
		if math.Abs(delta) < math.Abs(nearestDelta) {
			nearestDelta = delta
		}
	}
	return nearestDelta, true
}

func formatBenchmarkSourceAttribution(candidates []Candidate) string {
	counts := make(map[string]int)
	for _, candidate := range candidates {
		seen := make(map[string]struct{}, len(candidate.Sources))
		counted := false
		for _, source := range candidate.Sources {
			source = strings.TrimSpace(source)
			if source == "" {
				continue
			}
			if _, ok := seen[source]; ok {
				continue
			}
			seen[source] = struct{}{}
			counts[source]++
			counted = true
		}
		if !counted {
			counts["none"]++
		}
	}
	return formatBenchmarkCounts(counts)
}

func formatBenchmarkStageCounts(candidates []Candidate) string {
	counts := make(map[string]int)
	for _, candidate := range candidates {
		status := strings.TrimSpace(candidate.Status)
		if status == "" {
			status = "unknown"
		}
		counts[status]++
	}
	return formatBenchmarkCounts(counts)
}

func formatBenchmarkCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}

type benchmarkFakeRawSignals struct {
	silences             []detect.Silence
	loudnessOnsets       []detect.LoudnessOnset
	scenes               []detect.SceneChange
	blackSegments        []detect.BlackSegment
	freezeSegments       []detect.FreezeSegment
	colorSamples         []detect.ColorSample
	windowBlackSegments  map[float64][]detect.BlackSegment
	windowFreezeSegments map[float64][]detect.FreezeSegment
	windowScenes         map[float64][]detect.SceneChange
	windowSamples        map[float64][]detect.ColorSample
	visualWindows        []autodetectSignalWindow
	blackWindows         []autodetectSignalWindow
	freezeWindows        []autodetectSignalWindow
	sceneWindows         []autodetectSignalWindow
	colorWindows         []autodetectSignalWindow
	fullBlackCalls       int
	fullFreezeCalls      int
	fullSceneCalls       int
	fullColorCalls       int
	ocrCalls             int
}

func (f *benchmarkFakeRawSignals) DetectSilence(path string, noiseDB float64, minDur float64) ([]detect.Silence, error) {
	return f.silences, nil
}

func (f *benchmarkFakeRawSignals) DetectAudio(path string, noiseDB float64, minDur float64) (detect.AudioSignals, error) {
	return detect.AudioSignals{Silences: f.silences, LoudnessOnsets: f.loudnessOnsets}, nil
}

func (f *benchmarkFakeRawSignals) DetectVisual(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64) (detect.VisualSignals, error) {
	return detect.VisualSignals{
		Scenes:         f.scenes,
		ColorSamples:   f.colorSamples,
		BlackSegments:  f.blackSegments,
		FreezeSegments: f.freezeSegments,
	}, nil
}

func (f *benchmarkFakeRawSignals) DetectVisualWindow(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64, start float64, duration float64) (detect.VisualSignals, error) {
	f.visualWindows = append(f.visualWindows, autodetectSignalWindow{start: start, duration: duration})
	return detect.VisualSignals{
		Scenes:         f.windowScenes[start],
		ColorSamples:   f.windowSamples[start],
		BlackSegments:  f.windowBlackSegments[start],
		FreezeSegments: f.windowFreezeSegments[start],
	}, nil
}

func (f *benchmarkFakeRawSignals) DetectSceneChanges(path string, threshold float64) ([]detect.SceneChange, error) {
	f.fullSceneCalls++
	return f.scenes, nil
}

func (f *benchmarkFakeRawSignals) DetectSceneChangesWindow(path string, threshold float64, sampleRate float64, start float64, duration float64) ([]detect.SceneChange, error) {
	f.sceneWindows = append(f.sceneWindows, autodetectSignalWindow{start: start, duration: duration})
	return f.windowScenes[start], nil
}

func (f *benchmarkFakeRawSignals) DetectBlackSegments(path string, minDuration float64) ([]detect.BlackSegment, error) {
	f.fullBlackCalls++
	return f.blackSegments, nil
}

func (f *benchmarkFakeRawSignals) DetectBlackSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.BlackSegment, error) {
	f.blackWindows = append(f.blackWindows, autodetectSignalWindow{start: start, duration: duration})
	return f.windowBlackSegments[start], nil
}

func (f *benchmarkFakeRawSignals) DetectFreezeSegments(path string, minDuration float64) ([]detect.FreezeSegment, error) {
	f.fullFreezeCalls++
	return f.freezeSegments, nil
}

func (f *benchmarkFakeRawSignals) DetectFreezeSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.FreezeSegment, error) {
	f.freezeWindows = append(f.freezeWindows, autodetectSignalWindow{start: start, duration: duration})
	return f.windowFreezeSegments[start], nil
}

func (f *benchmarkFakeRawSignals) SampleFrameColors(path string, sampleRate float64, crop string) ([]detect.ColorSample, error) {
	f.fullColorCalls++
	return f.colorSamples, nil
}

func (f *benchmarkFakeRawSignals) SampleFrameColorsWindow(path string, sampleRate float64, crop string, start float64, duration float64) ([]detect.ColorSample, error) {
	f.colorWindows = append(f.colorWindows, autodetectSignalWindow{start: start, duration: duration})
	return f.windowSamples[start], nil
}

func (f *benchmarkFakeRawSignals) OCRLowerThird(path string, timestamp float64, options detect.OCROptions) (detect.OCRResult, error) {
	f.ocrCalls++
	return detect.OCRResult{}, nil
}

type cachedAutodetectSignals struct {
	inner autodetectSignals
	dir   string
}

func (c *cachedAutodetectSignals) DetectAudio(path string, noiseDB float64, minDur float64) (detect.AudioSignals, error) {
	cachePath := c.cachePath("audio", path, formatBenchmarkFloat(noiseDB), formatBenchmarkFloat(minDur))
	var audio detect.AudioSignals
	if ok, err := readBenchmarkCache(cachePath, &audio); err != nil || ok {
		return audio, err
	}
	audio, err := c.inner.DetectAudio(path, noiseDB, minDur)
	if err != nil {
		return detect.AudioSignals{}, err
	}
	return audio, writeBenchmarkCache(cachePath, audio)
}

func (c *cachedAutodetectSignals) DetectSilence(path string, noiseDB float64, minDur float64) ([]detect.Silence, error) {
	cachePath := c.cachePath("silence", path, formatBenchmarkFloat(noiseDB), formatBenchmarkFloat(minDur))
	var silences []detect.Silence
	if ok, err := readBenchmarkCache(cachePath, &silences); err != nil || ok {
		return silences, err
	}
	silences, err := c.inner.DetectSilence(path, noiseDB, minDur)
	if err != nil {
		return nil, err
	}
	return silences, writeBenchmarkCache(cachePath, silences)
}

func (c *cachedAutodetectSignals) DetectVisual(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64) (detect.VisualSignals, error) {
	cachePath := c.cachePath("visual", path, formatBenchmarkFloat(sceneThreshold), formatBenchmarkFloat(sceneColorSampleRate), formatBenchmarkFloat(blackMinDuration), formatBenchmarkFloat(freezeMinDuration))
	var signals detect.VisualSignals
	if ok, err := readBenchmarkCache(cachePath, &signals); err != nil || ok {
		return signals, err
	}
	signals, err := c.inner.DetectVisual(path, sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration)
	if err != nil {
		return detect.VisualSignals{}, err
	}
	return signals, writeBenchmarkCache(cachePath, signals)
}

func (c *cachedAutodetectSignals) DetectVisualWindow(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64, start float64, duration float64) (detect.VisualSignals, error) {
	cachePath := c.cachePath("visual-window", path, formatBenchmarkFloat(sceneThreshold), formatBenchmarkFloat(sceneColorSampleRate), formatBenchmarkFloat(blackMinDuration), formatBenchmarkFloat(freezeMinDuration), formatBenchmarkFloat(start), formatBenchmarkFloat(duration))
	var signals detect.VisualSignals
	if ok, err := readBenchmarkCache(cachePath, &signals); err != nil || ok {
		return signals, err
	}
	signals, err := c.inner.DetectVisualWindow(path, sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration, start, duration)
	if err != nil {
		return detect.VisualSignals{}, err
	}
	return signals, writeBenchmarkCache(cachePath, signals)
}

func (c *cachedAutodetectSignals) DetectSceneChanges(path string, threshold float64) ([]detect.SceneChange, error) {
	cachePath := c.cachePath("scenes", path, formatBenchmarkFloat(threshold))
	var scenes []detect.SceneChange
	if ok, err := readBenchmarkCache(cachePath, &scenes); err != nil || ok {
		return scenes, err
	}
	scenes, err := c.inner.DetectSceneChanges(path, threshold)
	if err != nil {
		return nil, err
	}
	return scenes, writeBenchmarkCache(cachePath, scenes)
}

func (c *cachedAutodetectSignals) DetectSceneChangesWindow(path string, threshold float64, sampleRate float64, start float64, duration float64) ([]detect.SceneChange, error) {
	cachePath := c.cachePath("scenes-window", path, formatBenchmarkFloat(threshold), formatBenchmarkFloat(sampleRate), formatBenchmarkFloat(start), formatBenchmarkFloat(duration))
	var scenes []detect.SceneChange
	if ok, err := readBenchmarkCache(cachePath, &scenes); err != nil || ok {
		return scenes, err
	}
	scenes, err := c.inner.DetectSceneChangesWindow(path, threshold, sampleRate, start, duration)
	if err != nil {
		return nil, err
	}
	return scenes, writeBenchmarkCache(cachePath, scenes)
}

func (c *cachedAutodetectSignals) DetectBlackSegments(path string, minDuration float64) ([]detect.BlackSegment, error) {
	cachePath := c.cachePath("black", path, formatBenchmarkFloat(minDuration))
	var segments []detect.BlackSegment
	if ok, err := readBenchmarkCache(cachePath, &segments); err != nil || ok {
		return segments, err
	}
	segments, err := c.inner.DetectBlackSegments(path, minDuration)
	if err != nil {
		return nil, err
	}
	return segments, writeBenchmarkCache(cachePath, segments)
}

func (c *cachedAutodetectSignals) DetectBlackSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.BlackSegment, error) {
	cachePath := c.cachePath("black-window", path, formatBenchmarkFloat(minDuration), formatBenchmarkFloat(start), formatBenchmarkFloat(duration))
	var segments []detect.BlackSegment
	if ok, err := readBenchmarkCache(cachePath, &segments); err != nil || ok {
		return segments, err
	}
	segments, err := c.inner.DetectBlackSegmentsWindow(path, minDuration, start, duration)
	if err != nil {
		return nil, err
	}
	return segments, writeBenchmarkCache(cachePath, segments)
}

func (c *cachedAutodetectSignals) DetectFreezeSegments(path string, minDuration float64) ([]detect.FreezeSegment, error) {
	cachePath := c.cachePath("freeze", path, formatBenchmarkFloat(minDuration))
	var segments []detect.FreezeSegment
	if ok, err := readBenchmarkCache(cachePath, &segments); err != nil || ok {
		return segments, err
	}
	segments, err := c.inner.DetectFreezeSegments(path, minDuration)
	if err != nil {
		return nil, err
	}
	return segments, writeBenchmarkCache(cachePath, segments)
}

func (c *cachedAutodetectSignals) DetectFreezeSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.FreezeSegment, error) {
	cachePath := c.cachePath("freeze-window", path, formatBenchmarkFloat(minDuration), formatBenchmarkFloat(start), formatBenchmarkFloat(duration))
	var segments []detect.FreezeSegment
	if ok, err := readBenchmarkCache(cachePath, &segments); err != nil || ok {
		return segments, err
	}
	segments, err := c.inner.DetectFreezeSegmentsWindow(path, minDuration, start, duration)
	if err != nil {
		return nil, err
	}
	return segments, writeBenchmarkCache(cachePath, segments)
}

func (c *cachedAutodetectSignals) SampleFrameColors(path string, sampleRate float64, crop string) ([]detect.ColorSample, error) {
	cachePath := c.cachePath("colors", path, formatBenchmarkFloat(sampleRate), crop)
	var samples []detect.ColorSample
	if ok, err := readBenchmarkCache(cachePath, &samples); err != nil || ok {
		return samples, err
	}
	samples, err := c.inner.SampleFrameColors(path, sampleRate, crop)
	if err != nil {
		return nil, err
	}
	return samples, writeBenchmarkCache(cachePath, samples)
}

func (c *cachedAutodetectSignals) SampleFrameColorsWindow(path string, sampleRate float64, crop string, start float64, duration float64) ([]detect.ColorSample, error) {
	cachePath := c.cachePath("colors-window", path, formatBenchmarkFloat(sampleRate), crop, formatBenchmarkFloat(start), formatBenchmarkFloat(duration))
	var samples []detect.ColorSample
	if ok, err := readBenchmarkCache(cachePath, &samples); err != nil || ok {
		return samples, err
	}
	samples, err := c.inner.SampleFrameColorsWindow(path, sampleRate, crop, start, duration)
	if err != nil {
		return nil, err
	}
	return samples, writeBenchmarkCache(cachePath, samples)
}

func (c *cachedAutodetectSignals) OCRLowerThird(path string, timestamp float64, options detect.OCROptions) (detect.OCRResult, error) {
	cachePath, err := c.ocrCachePath(path, timestamp, options)
	if err != nil {
		return detect.OCRResult{}, err
	}
	var result detect.OCRResult
	if ok, err := readBenchmarkCache(cachePath, &result); err != nil || ok {
		return result, err
	}
	result, err = c.inner.OCRLowerThird(path, timestamp, options)
	if err != nil {
		return detect.OCRResult{}, err
	}
	return result, writeBenchmarkCache(cachePath, result)
}

func (c *cachedAutodetectSignals) ocrCachePath(path string, timestamp float64, options detect.OCROptions) (string, error) {
	options, err := normalizeBenchmarkOCROptions(options)
	if err != nil {
		return "", err
	}
	return c.cachePath(
		"ocr",
		path,
		formatBenchmarkFloat(timestamp),
		options.Crop,
		formatBenchmarkFloat(options.MinConfidence),
		options.Language,
		strconv.Itoa(options.PageSegMode),
		options.PreprocessFilter,
		formatBenchmarkFloatSlice(options.ProbeOffsets),
	), nil
}

func TestCachedAutodetectSignalsOCRCachePathNormalizesDefaults(t *testing.T) {
	cache := &cachedAutodetectSignals{dir: "cache-root"}

	got, err := cache.ocrCachePath("video.mkv", 12, detect.OCROptions{})
	if err != nil {
		t.Fatalf("ocrCachePath returned error: %v", err)
	}
	want, err := cache.ocrCachePath("video.mkv", 12, detect.OCROptions{
		Crop:          detect.DefaultOCRLowerThirdCrop,
		MinConfidence: detect.DefaultOCRMinConfidence,
		PageSegMode:   detect.DefaultOCRPageSegMode,
		ProbeOffsets:  []float64{0},
	})
	if err != nil {
		t.Fatalf("ocrCachePath with explicit defaults returned error: %v", err)
	}
	if got != want {
		t.Fatalf("default cache path = %q, want explicit defaults path %q", got, want)
	}
}

func TestCachedAutodetectSignalsOCRCachePathIncludesProcessingOptions(t *testing.T) {
	cache := &cachedAutodetectSignals{dir: "cache-root"}
	base := detect.OCROptions{
		Crop:             "crop=iw:ih*0.35:0:ih*0.65",
		MinConfidence:    50,
		Language:         "eng",
		PageSegMode:      6,
		PreprocessFilter: "scale=iw*2:ih*2",
		ProbeOffsets:     []float64{0, 2},
	}
	basePath, err := cache.ocrCachePath("video.mkv", 12, base)
	if err != nil {
		t.Fatalf("base ocrCachePath returned error: %v", err)
	}

	variants := []struct {
		name    string
		options detect.OCROptions
	}{
		{
			name: "crop",
			options: detect.OCROptions{
				Crop:             "crop=iw:ih*0.5:0:ih*0.5",
				MinConfidence:    50,
				Language:         "eng",
				PageSegMode:      6,
				PreprocessFilter: "scale=iw*2:ih*2",
				ProbeOffsets:     []float64{0, 2},
			},
		},
		{
			name: "minimum confidence",
			options: detect.OCROptions{
				Crop:             "crop=iw:ih*0.35:0:ih*0.65",
				MinConfidence:    65,
				Language:         "eng",
				PageSegMode:      6,
				PreprocessFilter: "scale=iw*2:ih*2",
				ProbeOffsets:     []float64{0, 2},
			},
		},
		{
			name: "language",
			options: detect.OCROptions{
				Crop:             "crop=iw:ih*0.35:0:ih*0.65",
				MinConfidence:    50,
				Language:         "spa",
				PageSegMode:      6,
				PreprocessFilter: "scale=iw*2:ih*2",
				ProbeOffsets:     []float64{0, 2},
			},
		},
		{
			name: "PSM",
			options: detect.OCROptions{
				Crop:             "crop=iw:ih*0.35:0:ih*0.65",
				MinConfidence:    50,
				Language:         "eng",
				PageSegMode:      7,
				PreprocessFilter: "scale=iw*2:ih*2",
				ProbeOffsets:     []float64{0, 2},
			},
		},
		{
			name: "preprocessing",
			options: detect.OCROptions{
				Crop:             "crop=iw:ih*0.35:0:ih*0.65",
				MinConfidence:    50,
				Language:         "eng",
				PageSegMode:      6,
				PreprocessFilter: "scale=iw*2:ih*2,unsharp=5:5:1",
				ProbeOffsets:     []float64{0, 2},
			},
		},
		{
			name: "probe offsets",
			options: detect.OCROptions{
				Crop:             "crop=iw:ih*0.35:0:ih*0.65",
				MinConfidence:    50,
				Language:         "eng",
				PageSegMode:      6,
				PreprocessFilter: "scale=iw*2:ih*2",
				ProbeOffsets:     []float64{0, 4},
			},
		},
	}

	for _, tt := range variants {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cache.ocrCachePath("video.mkv", 12, tt.options)
			if err != nil {
				t.Fatalf("ocrCachePath returned error: %v", err)
			}
			if got == basePath {
				t.Fatalf("cache path for %s did not change: %q", tt.name, got)
			}
		})
	}
}

func normalizeBenchmarkOCROptions(options detect.OCROptions) (detect.OCROptions, error) {
	if strings.TrimSpace(options.Crop) == "" {
		options.Crop = detect.DefaultOCRLowerThirdCrop
	}
	if options.MinConfidence == 0 {
		options.MinConfidence = detect.DefaultOCRMinConfidence
	}
	if options.PageSegMode == 0 {
		options.PageSegMode = detect.DefaultOCRPageSegMode
	}
	if options.PageSegMode < 1 || options.PageSegMode > 13 {
		return detect.OCROptions{}, fmt.Errorf("OCR PSM must be between 1 and 13: %d", options.PageSegMode)
	}
	options.PreprocessFilter = strings.TrimSpace(options.PreprocessFilter)
	if len(options.ProbeOffsets) == 0 {
		options.ProbeOffsets = []float64{0}
	} else {
		probeOffsets := append([]float64(nil), options.ProbeOffsets...)
		for i, offset := range probeOffsets {
			if math.IsNaN(offset) || math.IsInf(offset, 0) {
				return detect.OCROptions{}, fmt.Errorf("OCR probe offset must be finite: %s", formatBenchmarkFloat(offset))
			}
			if offset == 0 {
				probeOffsets[i] = 0
			}
		}
		options.ProbeOffsets = probeOffsets
	}
	return options, nil
}

func formatBenchmarkFloatSlice(values []float64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, formatBenchmarkFloat(value))
	}
	return strings.Join(parts, ",")
}

func (c *cachedAutodetectSignals) cachePath(prefix string, parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return filepath.Join(c.dir, prefix+"-"+hex.EncodeToString(hash[:8])+".json")
}

func benchmarkCacheDir(sourcePath string) (string, error) {
	root := strings.TrimSpace(os.Getenv(autodetectCacheDirEnv))
	if root == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(userCache, "vid-streamer", "autodetect-eval")
	}
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", err
	}
	stat, err := os.Stat(absSource)
	if err != nil {
		return "", err
	}
	keyMaterial := fmt.Sprintf("%s\x00%d\x00%d\x00%s", absSource, stat.Size(), stat.ModTime().UnixNano(), benchmarkSignalCacheVersion)
	hash := sha256.Sum256([]byte(keyMaterial))
	return filepath.Join(root, hex.EncodeToString(hash[:8])), nil
}

func readBenchmarkCache(path string, out any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return false, err
	}
	return true, nil
}

func writeBenchmarkCache(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func formatBenchmarkFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
