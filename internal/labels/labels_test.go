package labels

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

// sampleTimestampExample uses dummy names but preserves the clipTrimmer
// syntax quirks the parser must tolerate: leading spaces before the filename,
// bare group-label lines with trailing spaces (skipped), and a double space
// between the start and stop timestamps.
const sampleTimestampExample = `>                   sample_video.mkv
section-one              
group-a          00:10:00 00:18:00
group-b          00:20:00 00:28:00
group-c          00:30:00  00:38:00
group-d          00:40:00 00:48:00
group-e          00:50:00 00:58:00
group-f          01:00:00 01:08:00
group-g          01:10:00 01:18:00
group-h          01:20:00 01:28:00
group-i          01:30:00 01:38:00
group-j          01:40:00 01:48:00
section-two 
group-k          02:00:00 02:08:00
`

func TestImportTimestampsParsesClipTrimmerExample(t *testing.T) {
	store := New(config.Config{})
	labels, err := store.ImportTimestamps(strings.NewReader(sampleTimestampExample))
	if err != nil {
		t.Fatalf("ImportTimestamps returned error: %v", err)
	}

	if labels.Video != "sample_video" {
		t.Fatalf("Video = %q, want %q", labels.Video, "sample_video")
	}
	if got, want := len(labels.Boundaries), 11; got != want {
		t.Fatalf("len(Boundaries) = %d, want %d: %#v", got, want, labels.Boundaries)
	}
	if got, want := labels.Boundaries[0], (Boundary{Name: "group-a", Start: 600}); got != want {
		t.Fatalf("first boundary = %#v, want %#v", got, want)
	}
	last := labels.Boundaries[len(labels.Boundaries)-1]
	if want := (Boundary{Name: "group-k", Start: 7200}); last != want {
		t.Fatalf("last boundary = %#v, want %#v", last, want)
	}
	for _, boundary := range labels.Boundaries {
		if boundary.Name == "section-one" || boundary.Name == "section-two" {
			t.Fatalf("bare group label %q was imported as a boundary", boundary.Name)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	store := New(config.Config{VideoDir: t.TempDir(), StateDir: t.TempDir()})
	want := VideoLabels{
		Video: "sample_video",
		Boundaries: []Boundary{
			{Name: "group-a", Start: 600},
			{Name: "group-b", Start: 1200},
		},
		Candidates: []Candidate{{
			Time:          12.5,
			Duration:      1.25,
			Status:        "candidate",
			Sources:       []string{"silence", "lineup"},
			Confidence:    0.8,
			SuggestedName: "group-a",
		}},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	got, err := store.Load("sample_video")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got.Video != want.Video {
		t.Fatalf("Video = %q, want %q", got.Video, want.Video)
	}
	if len(got.Boundaries) != len(want.Boundaries) || got.Boundaries[0] != want.Boundaries[0] || got.Boundaries[1] != want.Boundaries[1] {
		t.Fatalf("Boundaries = %#v, want %#v", got.Boundaries, want.Boundaries)
	}
	if len(got.Candidates) != 1 || !reflect.DeepEqual(got.Candidates[0], want.Candidates[0]) {
		t.Fatalf("Candidates = %#v, want %#v", got.Candidates, want.Candidates)
	}
}

func TestLoadMissingSidecarReturnsFreshLabels(t *testing.T) {
	store := New(config.Config{VideoDir: t.TempDir(), StateDir: t.TempDir()})
	got, err := store.Load("missing_video")
	if err != nil {
		t.Fatalf("Load missing sidecar returned error: %v", err)
	}
	if got.Video != "missing_video" || len(got.Boundaries) != 0 || len(got.Candidates) != 0 {
		t.Fatalf("Load missing sidecar = %#v, want fresh labels for missing_video", got)
	}
}

func TestToWebVTTFormatsSortedChapters(t *testing.T) {
	store := New(config.Config{})
	got := store.ToWebVTT(VideoLabels{Boundaries: []Boundary{
		{Name: "second", Start: 765},
		{Name: "first", Start: 0},
	}})
	want := "WEBVTT\n\n" +
		"1\n" +
		"00:00:00.000 --> 00:12:45.000\n" +
		"first\n\n" +
		"2\n" +
		"00:12:45.000 --> 00:22:45.000\n" +
		"second\n\n"
	if got != want {
		t.Fatalf("ToWebVTT() = %q, want %q", got, want)
	}
}

func TestToWebVTTNoBoundaries(t *testing.T) {
	store := New(config.Config{})
	if got, want := store.ToWebVTT(VideoLabels{}), "WEBVTT\n\n"; got != want {
		t.Fatalf("ToWebVTT empty = %q, want %q", got, want)
	}
}

func TestExportImportTimestampsRoundTrip(t *testing.T) {
	store := New(config.Config{})
	want := VideoLabels{Video: "sample_video", Boundaries: []Boundary{
		{Name: "group-b", Start: 1200},
		{Name: "group-a", Start: 600},
	}}

	exported := store.ExportTimestamps(want)
	got, err := store.ImportTimestamps(strings.NewReader(exported))
	if err != nil {
		t.Fatalf("ImportTimestamps exported text returned error: %v\n%s", err, exported)
	}

	if got.Video != want.Video {
		t.Fatalf("Video = %q, want %q", got.Video, want.Video)
	}
	wantBoundaries := []Boundary{{Name: "group-a", Start: 600}, {Name: "group-b", Start: 1200}}
	if len(got.Boundaries) != len(wantBoundaries) {
		t.Fatalf("len(Boundaries) = %d, want %d: %#v", len(got.Boundaries), len(wantBoundaries), got.Boundaries)
	}
	for i := range wantBoundaries {
		if got.Boundaries[i] != wantBoundaries[i] {
			t.Fatalf("boundary %d = %#v, want %#v", i, got.Boundaries[i], wantBoundaries[i])
		}
	}
}
