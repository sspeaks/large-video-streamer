package detect

import (
	"fmt"
	"math"
	"os/exec"
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
