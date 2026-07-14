package detect

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseSilenceDetectPreservesStartEndAndDuration(t *testing.T) {
	output := `
[silencedetect @ 0x1] silence_start: 12.345
[silencedetect @ 0x1] silence_end: 16.789 | silence_duration: 4.444
[silencedetect @ 0x1] silence_start: 30
[silencedetect @ 0x1] silence_end: 32.5 | silence_duration: 2.5
`

	got := parseSilenceDetect(output)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %#v", len(got), got)
	}
	if got[0].Start != 12.345 || got[0].Time != 16.789 || got[0].Duration != 4.444 {
		t.Fatalf("first silence = %#v, want start/end/duration preserved", got[0])
	}
	if got[1].Start != 30 || got[1].Time != 32.5 || got[1].Duration != 2.5 {
		t.Fatalf("second silence = %#v, want start/end/duration preserved", got[1])
	}
}

func TestSilenceJSONBackwardsCompatibleWithoutStart(t *testing.T) {
	var got []Silence
	if err := json.Unmarshal([]byte(`[{"Time":125,"Duration":10}]`), &got); err != nil {
		t.Fatalf("unmarshal cached silence JSON without Start: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Start != 0 || got[0].Time != 125 || got[0].Duration != 10 {
		t.Fatalf("silence = %#v, want zero Start with cached Time/Duration preserved", got[0])
	}
}

func TestDetectSilenceReportsAudioResumeAfterKnownGap(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg not found: %v", err)
	}

	generatedDir := filepath.Join("testdata", ".generated")
	if err := os.MkdirAll(generatedDir, 0o755); err != nil {
		t.Fatalf("create generated fixture dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(generatedDir) })

	path := filepath.Join(generatedDir, "silence-gap.wav")
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
			if silence.Start < 2.5 || silence.Start > 3.5 {
				t.Fatalf("silence.Start = %v, want silence start near 3s in %#v", silence.Start, silence)
			}
			return
		}
	}

	t.Fatalf("no silence near audio resume at 7s with 3-4s duration: %#v", candidates)
}

func TestParseLoudnessMetadata(t *testing.T) {
	output := `
frame:0    pts:0       pts_time:0
lavfi.astats.Overall.RMS_level=-inf
frame:1    pts:2000    pts_time:0.2
lavfi.astats.Overall.RMS_level=-21.5
`

	got := parseLoudnessMetadata(output)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %#v", len(got), got)
	}
	if got[0].Time != 0 || got[0].Level != -120 {
		t.Fatalf("first sample = %#v, want -inf normalized to -120 dB at 0s", got[0])
	}
	if got[1].Time != 0.2 || got[1].Level != -21.5 {
		t.Fatalf("second sample = %#v, want -21.5 dB at 0.2s", got[1])
	}
}

func TestDetectLoudnessOnsetsRequiresSustainedRise(t *testing.T) {
	samples := []LoudnessSample{
		{Time: 0.0, Level: -70},
		{Time: 0.2, Level: -68},
		{Time: 0.4, Level: -20},
		{Time: 0.6, Level: -19},
		{Time: 0.8, Level: -20},
		{Time: 1.0, Level: -68},
		{Time: 3.0, Level: -70},
		{Time: 3.2, Level: -18},
		{Time: 3.4, Level: -70},
		{Time: 3.6, Level: -70},
	}

	got := DetectLoudnessOnsets(samples)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want only sustained onset: %#v", len(got), got)
	}
	if math.Abs(got[0].Time-0.4) > 0.0001 {
		t.Fatalf("onset time = %v, want 0.4", got[0].Time)
	}
	if got[0].Delta < DefaultLoudnessOnsetDeltaDB {
		t.Fatalf("onset delta = %v, want >= %v", got[0].Delta, DefaultLoudnessOnsetDeltaDB)
	}
}

func TestDetectAudioReportsLoudnessOnsetOnSyntheticStep(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg not found: %v", err)
	}

	generatedDir := filepath.Join("testdata", ".generated")
	if err := os.MkdirAll(generatedDir, 0o755); err != nil {
		t.Fatalf("create generated fixture dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(generatedDir) })

	path := filepath.Join(generatedDir, "loudness-step.wav")
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-hide_banner",
		"-nostats",
		"-f", "lavfi",
		"-i", "anullsrc=r=48000:cl=mono:d=1.2[s0];sine=frequency=440:sample_rate=48000:d=1.2[s1];[s0][s1]concat=n=2:v=0:a=1",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthesize audio: %v\n%s", err, out)
	}

	audio, err := DetectAudio(path, -35.0, 0.2)
	if err != nil {
		t.Fatalf("DetectAudio returned error: %v", err)
	}
	if len(audio.Silences) == 0 {
		t.Fatal("DetectAudio returned no silences")
	}
	if len(audio.LoudnessSamples) == 0 {
		t.Fatal("DetectAudio returned no loudness samples")
	}
	if len(audio.LoudnessOnsets) == 0 {
		t.Fatalf("DetectAudio returned no loudness onsets; samples=%#v", audio.LoudnessSamples)
	}
	if onset := audio.LoudnessOnsets[0].Time; onset < 1.0 || onset > 1.4 {
		t.Fatalf("first onset time = %v, want near 1.2s", onset)
	}
}
