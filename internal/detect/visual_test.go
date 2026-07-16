package detect

import (
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSceneChangesFromScdetMetadata(t *testing.T) {
	output := `
frame:0    pts:0       pts_time:0
lavfi.scd.mafd=0.000
lavfi.scd.score=0.000
frame:12   pts:12000   pts_time:4.2
lavfi.scd.mafd=23.125
lavfi.scd.score=23.125
lavfi.scd.time=4.2
frame:13   pts:13000   pts_time:4.6
lavfi.scd.mafd=bad
lavfi.scd.score=bad
lavfi.scd.time=4.6
frame:22   pts:22000   pts_time:9.2
lavfi.scd.mafd=17.5
lavfi.scd.score=17.5
lavfi.scd.time=9.2
`

	changes := parseSceneChanges(output)

	want := []SceneChange{
		{Time: 4.2, Score: 23.125},
		{Time: 9.2, Score: 17.5},
	}
	assertSceneChanges(t, changes, want)
}

func TestParseSceneChangesFromScdetLogLines(t *testing.T) {
	output := `
[scdet @ 0x55a] lavfi.scd.score: 31.250, lavfi.scd.time: 12.345
[scdet @ 0x55a] lavfi.scd.score: bad, lavfi.scd.time: 15
`

	changes := parseSceneChanges(output)

	want := []SceneChange{{Time: 12.345, Score: 31.250}}
	assertSceneChanges(t, changes, want)
}

func TestParseSceneChangesUsesFrameTimeWhenScdetTimeMissing(t *testing.T) {
	output := `
frame:42   pts:42000   pts_time:14.25
lavfi.scd.mafd=18.75
lavfi.scd.score=18.75
`

	changes := parseSceneChanges(output)

	want := []SceneChange{{Time: 14.25, Score: 18.75}}
	assertSceneChanges(t, changes, want)
}

func TestParseBlackSegmentsFromBlackdetectLogLines(t *testing.T) {
	output := `
[blackdetect @ 0x55a] black_start:12.125 black_end:14.5 black_duration:2.375
[blackdetect @ 0x55a] black_start:bad black_end:18 black_duration:2
[blackdetect @ 0x55a] black_start:20 black_end:20.75 black_duration:0.75
`

	segments := parseBlackSegments(output)

	want := []BlackSegment{
		{Start: 12.125, End: 14.5, Duration: 2.375},
		{Start: 20, End: 20.75, Duration: 0.75},
	}
	assertBlackSegments(t, segments, want)
}

func TestOffsetBlackSegmentsAddsWindowStart(t *testing.T) {
	got := offsetBlackSegments([]BlackSegment{{Start: 2, End: 3, Duration: 1}}, 90)
	want := []BlackSegment{{Start: 92, End: 93, Duration: 1}}
	assertBlackSegments(t, got, want)
}

func TestParseFreezeSegmentsFromFreezedetectLogLines(t *testing.T) {
	output := `
[freezedetect @ 0x55a] lavfi.freezedetect.freeze_start: 30.25
[freezedetect @ 0x55a] lavfi.freezedetect.freeze_duration: 2.50
[freezedetect @ 0x55a] lavfi.freezedetect.freeze_end: 32.75
[freezedetect @ 0x55a] lavfi.freezedetect.freeze_start: 40
[freezedetect @ 0x55a] lavfi.freezedetect.freeze_end: 42.25
`

	segments := parseFreezeSegments(output)

	want := []FreezeSegment{
		{Start: 30.25, End: 32.75, Duration: 2.50},
		{Start: 40, End: 42.25, Duration: 2.25},
	}
	assertFreezeSegments(t, segments, want)
}

func TestOffsetFreezeSegmentsAddsWindowStart(t *testing.T) {
	got := offsetFreezeSegments([]FreezeSegment{{Start: 2, End: 5, Duration: 3}}, 120)
	want := []FreezeSegment{{Start: 122, End: 125, Duration: 3}}
	assertFreezeSegments(t, got, want)
}

func TestOffsetSceneChangesAddsWindowStart(t *testing.T) {
	got := offsetSceneChanges([]SceneChange{{Time: 2.5, Score: 11}}, 100)
	want := []SceneChange{{Time: 102.5, Score: 11}}
	assertSceneChanges(t, got, want)
}

func TestFilterSceneChangesByThreshold(t *testing.T) {
	got := filterSceneChangesByThreshold([]SceneChange{
		{Time: 1, Score: 9.9},
		{Time: 2, Score: 10},
		{Time: 3, Score: 12},
	}, 10)
	want := []SceneChange{{Time: 2, Score: 10}, {Time: 3, Score: 12}}
	assertSceneChanges(t, got, want)
}

func TestParseColorSamplesFromSignalstatsMetadata(t *testing.T) {
	output := `
frame:0    pts:0       pts_time:0
lavfi.signalstats.YAVG=42.5
lavfi.signalstats.UAVG=110
lavfi.signalstats.VAVG=140.25
lavfi.signalstats.SATAVG=25
frame:1    pts:500     pts_time:0.5
lavfi.signalstats.YAVG=bad
lavfi.signalstats.UAVG=111
lavfi.signalstats.VAVG=141
frame:2    pts:1000    pts_time:1
lavfi.signalstats.YAVG=50
lavfi.signalstats.UAVG=112.5
lavfi.signalstats.VAVG=139
`

	samples := parseColorSamples(output)

	want := []ColorSample{
		{Time: 0, YMean: 42.5, UMean: 110, VMean: 140.25},
		{Time: 1, YMean: 50, UMean: 112.5, VMean: 139},
	}
	assertColorSamples(t, samples, want)
}

func TestOffsetColorSamplesAddsWindowStart(t *testing.T) {
	got := offsetColorSamples([]ColorSample{{Time: 2, YMean: 10, UMean: 20, VMean: 30}}, 90)
	want := []ColorSample{{Time: 92, YMean: 10, UMean: 20, VMean: 30}}
	assertColorSamples(t, got, want)
}

func TestDetectColorShiftsUsesSustainedWindowMeans(t *testing.T) {
	samples := []ColorSample{
		{Time: 0, YMean: 10, UMean: 128, VMean: 128},
		{Time: 1, YMean: 10, UMean: 128, VMean: 128},
		{Time: 2, YMean: 10, UMean: 128, VMean: 128},
		{Time: 3, YMean: 70, UMean: 150, VMean: 90},
		{Time: 4, YMean: 70, UMean: 150, VMean: 90},
		{Time: 5, YMean: 70, UMean: 150, VMean: 90},
	}

	shifts := DetectColorShifts(samples, 60, 2)

	wantDelta := math.Sqrt(60*60 + 22*22 + 38*38)
	want := []ColorShift{{Time: 3, Delta: wantDelta}}
	assertColorShifts(t, shifts, want)
}

func TestDetectColorShiftsIgnoresSingleSampleSpike(t *testing.T) {
	samples := []ColorSample{
		{Time: 0, YMean: 10, UMean: 128, VMean: 128},
		{Time: 1, YMean: 10, UMean: 128, VMean: 128},
		{Time: 2, YMean: 90, UMean: 40, VMean: 200},
		{Time: 3, YMean: 10, UMean: 128, VMean: 128},
		{Time: 4, YMean: 10, UMean: 128, VMean: 128},
		{Time: 5, YMean: 10, UMean: 128, VMean: 128},
	}

	shifts := DetectColorShifts(samples, 90, 2)

	if len(shifts) != 0 {
		t.Fatalf("DetectColorShifts returned spike as sustained shift: %#v", shifts)
	}
}

func TestVisualFFmpegIntegration(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg not found: %v", err)
	}

	path := "testdata/colors.ffconcat"

	scenes, err := DetectSceneChanges(path, 1)
	if err != nil {
		t.Fatalf("DetectSceneChanges returned error: %v", err)
	}
	if len(scenes) == 0 {
		t.Fatal("DetectSceneChanges returned no scenes")
	}

	samples, err := SampleFrameColors(path, 1, "crop=2:2:0:0")
	if err != nil {
		t.Fatalf("SampleFrameColors returned error: %v", err)
	}
	if len(samples) < 2 {
		t.Fatalf("SampleFrameColors returned too few samples: %#v", samples)
	}

	shifts := DetectColorShifts(samples, 20, 1)
	if len(shifts) == 0 {
		t.Fatalf("DetectColorShifts returned no shifts from samples: %#v", samples)
	}
}

func TestBuildVisualFFmpegArgsMapsOnlyFullRateBranch(t *testing.T) {
	args := buildVisualFFmpegArgs("synthetic.mkv", 10, 0.5, 0.2, 2, 1.5, 0.5)

	var mapped []string
	for i, arg := range args {
		if arg == "-map" && i+1 < len(args) {
			mapped = append(mapped, args[i+1])
		}
	}
	if len(mapped) != 1 || mapped[0] != "[fullout]" {
		t.Fatalf("mapped outputs = %#v, want only [fullout]", mapped)
	}

	graph := buildVisualFilterGraph(10, 0.5, 0.2, 2)
	if strings.Contains(graph, "[slowout]") {
		t.Fatalf("sampled analysis branch is still mapped: %s", graph)
	}
	if !strings.Contains(graph, "metadata=mode=print:file=-,nullsink") {
		t.Fatalf("sampled analysis branch does not terminate in nullsink: %s", graph)
	}
}

func TestDetectVisualWindowHandlesH264TailWithoutSampledFrames(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skipf("ffmpeg not found: %v", err)
	}
	encoders, err := exec.Command(ffmpeg, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil {
		t.Fatalf("list ffmpeg encoders: %v: %s", err, encoders)
	}
	if !strings.Contains(string(encoders), "libx264") {
		t.Skip("ffmpeg does not provide the libx264 encoder")
	}

	path := filepath.Join(t.TempDir(), "synthetic-h264.mkv")
	output, err := exec.Command(
		ffmpeg,
		"-hide_banner",
		"-loglevel", "error",
		"-f", "lavfi",
		"-i", "testsrc2=size=64x64:rate=60:duration=2",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-g", "60",
		path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("generate synthetic H.264 Matroska input: %v: %s", err, output)
	}

	scenes, samples, black, freeze, err := DetectVisualWindow(path, 10, 0.5, 0.2, 2, 1.5, 0.5)
	if err != nil {
		t.Fatalf("DetectVisualWindow returned error for short H.264 tail: %v", err)
	}
	if len(scenes) != 0 || len(samples) != 0 || len(black) != 0 || len(freeze) != 0 {
		t.Fatalf("short tail signals = scenes:%#v samples:%#v black:%#v freeze:%#v, want none", scenes, samples, black, freeze)
	}
}

func TestDetectVisualWindowMatchesSeparateDetectors(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg not found: %v", err)
	}

	path := "testdata/visual_equivalence.ffconcat"
	const (
		sceneThreshold = 10
		sampleRate     = 0.5
		blackDuration  = 0.2
		freezeDuration = 2
		start          = 0
		duration       = 0
	)

	wantScenes, err := DetectSceneChangesWindow(path, sceneThreshold, sampleRate, start, duration)
	if err != nil {
		t.Fatalf("DetectSceneChangesWindow returned error: %v", err)
	}
	if len(wantScenes) == 0 {
		t.Fatal("fixture produced no scene changes")
	}
	wantSamples, err := SampleFrameColorsWindow(path, sampleRate, "", start, duration)
	if err != nil {
		t.Fatalf("SampleFrameColorsWindow returned error: %v", err)
	}
	if len(wantSamples) == 0 {
		t.Fatal("fixture produced no color samples")
	}
	wantBlack, err := DetectBlackSegmentsWindow(path, blackDuration, start, duration)
	if err != nil {
		t.Fatalf("DetectBlackSegmentsWindow returned error: %v", err)
	}
	if len(wantBlack) == 0 {
		t.Fatal("fixture produced no black segments")
	}
	wantFreeze, err := DetectFreezeSegmentsWindow(path, freezeDuration, start, duration)
	if err != nil {
		t.Fatalf("DetectFreezeSegmentsWindow returned error: %v", err)
	}
	if len(wantFreeze) == 0 {
		t.Fatal("fixture produced no freeze segments")
	}

	gotScenes, gotSamples, gotBlack, gotFreeze, err := DetectVisualWindow(path, sceneThreshold, sampleRate, blackDuration, freezeDuration, start, duration)
	if err != nil {
		t.Fatalf("DetectVisualWindow returned error: %v", err)
	}

	assertSceneChanges(t, gotScenes, wantScenes)
	assertColorSamples(t, gotSamples, wantSamples)
	assertBlackSegments(t, gotBlack, wantBlack)
	assertFreezeSegments(t, gotFreeze, wantFreeze)
}

func assertSceneChanges(t *testing.T, got, want []SceneChange) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d scene changes, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if !closeFloat(got[i].Time, want[i].Time) || !closeFloat(got[i].Score, want[i].Score) {
			t.Fatalf("scene change %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func assertBlackSegments(t *testing.T, got, want []BlackSegment) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d black segments, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if !closeFloat(got[i].Start, want[i].Start) ||
			!closeFloat(got[i].End, want[i].End) ||
			!closeFloat(got[i].Duration, want[i].Duration) {
			t.Fatalf("black segment %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func assertFreezeSegments(t *testing.T, got, want []FreezeSegment) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d freeze segments, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if !closeFloat(got[i].Start, want[i].Start) ||
			!closeFloat(got[i].End, want[i].End) ||
			!closeFloat(got[i].Duration, want[i].Duration) {
			t.Fatalf("freeze segment %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func assertColorSamples(t *testing.T, got, want []ColorSample) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d color samples, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if !closeFloat(got[i].Time, want[i].Time) ||
			!closeFloat(got[i].YMean, want[i].YMean) ||
			!closeFloat(got[i].UMean, want[i].UMean) ||
			!closeFloat(got[i].VMean, want[i].VMean) {
			t.Fatalf("color sample %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func assertColorShifts(t *testing.T, got, want []ColorShift) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d color shifts, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if !closeFloat(got[i].Time, want[i].Time) || !closeFloat(got[i].Delta, want[i].Delta) {
			t.Fatalf("color shift %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func closeFloat(a, b float64) bool {
	return math.Abs(a-b) < 0.0001
}

func ExampleSampleFrameColors_filterOrder() {
	fmt.Println(buildColorFilterChain(2, "crop=iw/2:ih/2:0:0"))
	// Output: crop=iw/2:ih/2:0:0,fps=2,signalstats,metadata=mode=print:file=-
}
