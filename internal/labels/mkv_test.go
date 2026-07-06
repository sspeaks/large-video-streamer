package labels

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

func TestImportAndExportMKVChapters(t *testing.T) {
	requireTool(t, "ffmpeg")
	requireTool(t, "ffprobe")
	requireTool(t, "mkvpropedit")

	dir := t.TempDir()
	mkvPath := filepath.Join(dir, "sample_video.mkv")
	metadataPath := filepath.Join(dir, "chapters.ffmetadata")
	metadata := `;FFMETADATA1
[CHAPTER]
TIMEBASE=1/1000
START=0
END=1000
title=chapter-one
[CHAPTER]
TIMEBASE=1/1000
START=1000
END=2000
title=chapter-two
`
	if err := os.WriteFile(metadataPath, []byte(metadata), 0o644); err != nil {
		t.Fatalf("write ffmetadata: %v", err)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=16x16:d=2", "-i", metadataPath, "-map_metadata", "1", "-map_chapters", "1", "-c:v", "libx264", "-t", "2", "-y", mkvPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg create mkv: %v\n%s", err, out)
	}

	store := New(config.Config{})
	got, err := store.ImportMKVChapters(mkvPath)
	if err != nil {
		t.Fatalf("ImportMKVChapters returned error: %v", err)
	}
	assertBoundariesApprox(t, got, []Boundary{{Name: "chapter-one", Start: 0}, {Name: "chapter-two", Start: 1}})

	want := []Boundary{{Name: "group-b", Start: 1.5}, {Name: "group-a", Start: 0.25}}
	if err := store.ExportMKVChapters(mkvPath, want); err != nil {
		t.Fatalf("ExportMKVChapters returned error: %v", err)
	}
	reread, err := store.ImportMKVChapters(mkvPath)
	if err != nil {
		t.Fatalf("ImportMKVChapters after export returned error: %v", err)
	}
	assertBoundariesApprox(t, reread, []Boundary{{Name: "group-a", Start: 0.25}, {Name: "group-b", Start: 1.5}})
}

func TestFormatOGMChaptersRejectsNewlineNames(t *testing.T) {
	_, err := formatOGMChapters([]Boundary{{Name: "group-a\nCHAPTER02=00:00:00.000", Start: 0}})
	if err == nil {
		t.Fatal("formatOGMChapters returned nil error for newline name")
	}
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available: %v", name, err)
	}
}

func assertBoundariesApprox(t *testing.T, got, want []Boundary) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(boundaries) = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Fatalf("boundary %d name = %q, want %q; all %#v", i, got[i].Name, want[i].Name, got)
		}
		if math.Abs(got[i].Start-want[i].Start) > 0.02 {
			t.Fatalf("boundary %d start = %f, want %f; all %#v", i, got[i].Start, want[i].Start, got)
		}
	}
}
