package labels

import (
	"sort"
	"strings"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

func TestExportImportTimestampsRoundTripTable(t *testing.T) {
	cases := []struct {
		name string
		in   VideoLabels
	}{
		{
			name: "plain hyphen names out of order",
			in: VideoLabels{Video: "demo", Boundaries: []Boundary{
				{Name: "group-b", Start: 1200},
				{Name: "group-a", Start: 600},
			}},
		},
		{
			name: "single space name",
			in: VideoLabels{Video: "finals", Boundaries: []Boundary{
				{Name: "Quartet Finals", Start: 463},
			}},
		},
		{
			name: "multiple spaced names",
			in: VideoLabels{Video: "show", Boundaries: []Boundary{
				{Name: "Quartet Finals", Start: 463},
				{Name: "Semi Final Round", Start: 1800},
				{Name: "Grand Champions", Start: 3661},
			}},
		},
		{
			name: "mixed spaced hyphen and digit names",
			in: VideoLabels{Video: "mix", Boundaries: []Boundary{
				{Name: "Act 1 Scene 2", Start: 90},
				{Name: "intro-01", Start: 0},
				{Name: "Break Time", Start: 600},
			}},
		},
		{
			name: "empty video single spaced name",
			in: VideoLabels{Boundaries: []Boundary{
				{Name: "Only One", Start: 120},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := New(config.Config{})
			exported := store.ExportTimestamps(tc.in)
			got, err := store.ImportTimestamps(strings.NewReader(exported))
			if err != nil {
				t.Fatalf("ImportTimestamps exported text returned error: %v\n%s", err, exported)
			}
			if got.Video != tc.in.Video {
				t.Fatalf("Video = %q, want %q", got.Video, tc.in.Video)
			}

			want := append([]Boundary(nil), tc.in.Boundaries...)
			sort.SliceStable(want, func(i, j int) bool {
				return want[i].Start < want[j].Start
			})
			if len(got.Boundaries) != len(want) {
				t.Fatalf("len(Boundaries) = %d, want %d: %#v", len(got.Boundaries), len(want), got.Boundaries)
			}
			for i := range want {
				if got.Boundaries[i] != want[i] {
					t.Fatalf("boundary %d = %#v, want %#v", i, got.Boundaries[i], want[i])
				}
			}
		})
	}
}

func TestImportTimestampsAllowsSpacesInName(t *testing.T) {
	store := New(config.Config{})
	got, err := store.ImportTimestamps(strings.NewReader(`> demo.mkv
Quartet Finals 00:07:43
Semi Final 00:30:00
group-c 00:40:00
`))
	if err != nil {
		t.Fatalf("ImportTimestamps returned error: %v", err)
	}
	if got.Video != "demo" {
		t.Fatalf("Video = %q, want %q", got.Video, "demo")
	}
	want := []Boundary{
		{Name: "Quartet Finals", Start: 463},
		{Name: "Semi Final", Start: 1800},
		{Name: "group-c", Start: 2400},
	}
	if len(got.Boundaries) != len(want) {
		t.Fatalf("len(Boundaries) = %d, want %d: %#v", len(got.Boundaries), len(want), got.Boundaries)
	}
	for i := range want {
		if got.Boundaries[i] != want[i] {
			t.Fatalf("boundary %d = %#v, want %#v", i, got.Boundaries[i], want[i])
		}
	}
}
