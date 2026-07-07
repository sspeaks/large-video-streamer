package segment

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

func TestSegmentGeneratesHLSAndSkipsExistingPlaylist(t *testing.T) {
	requireTools(t)

	videoDir := t.TempDir()
	hlsDir := t.TempDir()
	generateSyntheticMKV(t, videoDir, "sample")

	cfg := config.Config{VideoDir: videoDir, HLSDir: hlsDir}
	if err := Segment(cfg, "sample"); err != nil {
		t.Fatalf("Segment() failed: %v", err)
	}

	playlist := filepath.Join(hlsDir, "sample", "playlist.m3u8")
	before := readNonEmptyFile(t, playlist)
	segments, err := filepath.Glob(filepath.Join(hlsDir, "sample", "seg_*.ts"))
	if err != nil {
		t.Fatalf("glob segments: %v", err)
	}
	if len(segments) == 0 {
		t.Fatalf("expected at least one HLS segment in %s", filepath.Dir(playlist))
	}

	if err := Segment(cfg, "sample"); err != nil {
		t.Fatalf("Segment() second call failed: %v", err)
	}
	after := readNonEmptyFile(t, playlist)
	if string(after) != string(before) {
		t.Fatalf("playlist changed on idempotent second Segment call")
	}
}

func TestSegmentAllProcessesTopLevelMKV(t *testing.T) {
	requireTools(t)

	videoDir := t.TempDir()
	hlsDir := t.TempDir()
	staleTmp := filepath.Join(hlsDir, ".batch.123.tmp")
	if err := os.MkdirAll(staleTmp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleTmp, "playlist.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	generateSyntheticMKV(t, videoDir, "batch")

	cfg := config.Config{VideoDir: videoDir, HLSDir: hlsDir}
	if err := SegmentAll(cfg); err != nil {
		t.Fatalf("SegmentAll() failed: %v", err)
	}

	playlist := filepath.Join(hlsDir, "batch", "playlist.m3u8")
	readNonEmptyFile(t, playlist)
	if _, err := os.Stat(staleTmp); !os.IsNotExist(err) {
		t.Fatalf("stale temp dir still exists or stat failed unexpectedly: %v", err)
	}
}

func requireTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found on PATH", tool)
		}
	}
}

func generateSyntheticMKV(t *testing.T, videoDir, name string) {
	t.Helper()
	out := filepath.Join(videoDir, name+".mkv")
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "testsrc=d=3:s=320x240",
		"-f", "lavfi", "-i", "sine=d=3",
		"-c:v", "libx264", "-c:a", "aac", out)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generate synthetic MKV: %v\n%s", err, output)
	}
}

func readNonEmptyFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(contents) == 0 {
		t.Fatalf("expected %s to be non-empty", path)
	}
	return contents
}
