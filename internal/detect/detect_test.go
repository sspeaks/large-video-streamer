package detect

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectSilenceReportsAudioResumeAfterKnownGap(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg not found: %v", err)
	}

	path := filepath.Join(t.TempDir(), "sample.wav")
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-hide_banner",
		"-nostats",
		"-f", "lavfi",
		"-i", "sine=frequency=440:duration=3",
		"-f", "lavfi",
		"-i", "anullsrc=channel_layout=mono:sample_rate=44100",
		"-f", "lavfi",
		"-i", "sine=frequency=440:duration=3",
		"-filter_complex", "[1]atrim=duration=4[s];[0][s][2]concat=n=3:v=0:a=1[a]",
		"-map", "[a]",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthesize audio: %v\n%s", err, out)
	}

	candidates, err := DetectSilence(path, -35.0, 2.0)
	if err != nil {
		t.Fatalf("DetectSilence returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("DetectSilence returned no candidates")
	}

	for _, silence := range candidates {
		if silence.Time >= 6.0 && silence.Time <= 8.0 &&
			silence.Duration >= 3.0 && silence.Duration <= 4.5 {
			return
		}
	}

	t.Fatalf("no silence near audio resume at 7s with 3-4s duration: %#v", candidates)
}
