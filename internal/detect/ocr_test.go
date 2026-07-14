package detect

import (
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseTesseractTSVFiltersLowConfidenceWords(t *testing.T) {
	tsv := strings.Join([]string{
		"level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext",
		"1\t1\t0\t0\t0\t0\t0\t0\t640\t360\t-1\t",
		"5\t1\t1\t1\t1\t1\t120\t260\t80\t24\t96.5\tAlice",
		"5\t1\t1\t1\t1\t2\t205\t260\t30\t24\t49\tnoise",
		"5\t1\t1\t1\t1\t3\t240\t260\t90\t24\t83.5\tCooper",
		"5\t1\t1\t1\t1\t4\t335\t260\t20\t24\tbad\tbadconf",
		"5\t1\t1\t1\t1\t5\t360\t260\t20\t24\t90\t   ",
	}, "\n")

	text, confidence := parseTesseractTSV(tsv, 50)

	if text != "Alice Cooper" {
		t.Fatalf("text = %q, want %q", text, "Alice Cooper")
	}
	if !closeFloat(confidence, 90) {
		t.Fatalf("confidence = %v, want 90", confidence)
	}
}

func TestParseTesseractTSVUsesHeaderColumns(t *testing.T) {
	tsv := strings.Join([]string{
		"text\tconf\tlevel",
		"ignored\t-1\t4",
		"Director\t88\t5",
		"credit\t12\t5",
		"Cut\t92\t5",
	}, "\n")

	text, confidence := parseTesseractTSV(tsv, DefaultOCRMinConfidence)

	if text != "Director Cut" {
		t.Fatalf("text = %q, want %q", text, "Director Cut")
	}
	if !closeFloat(confidence, 90) {
		t.Fatalf("confidence = %v, want 90", confidence)
	}
}

func TestParseTesseractTSVPreservesTabsInsideTextField(t *testing.T) {
	tsv := strings.Join([]string{
		"level\tconf\ttext",
		"5\t93\tAlice\tCooper",
	}, "\n")

	text, confidence := parseTesseractTSV(tsv, DefaultOCRMinConfidence)

	if text != "Alice\tCooper" {
		t.Fatalf("text = %q, want tab inside OCR text preserved", text)
	}
	if !closeFloat(confidence, 93) {
		t.Fatalf("confidence = %v, want 93", confidence)
	}
}

func TestParseTesseractTSVReturnsZeroConfidenceWhenNoWordsPass(t *testing.T) {
	tsv := strings.Join([]string{
		"level\tconf\ttext",
		"5\t12\tblur",
		"5\t-1\t",
	}, "\n")

	text, confidence := parseTesseractTSV(tsv, 80)

	if text != "" {
		t.Fatalf("text = %q, want empty string", text)
	}
	if confidence != 0 {
		t.Fatalf("confidence = %v, want 0", confidence)
	}
}

func TestNormalizeOCROptionsRejectsTempRootSymlinkUnderVideoDir(t *testing.T) {
	root := t.TempDir()
	videoDir := filepath.Join(root, "videos")
	outsideDir := filepath.Join(root, "scratch")
	if err := os.Mkdir(videoDir, 0o755); err != nil {
		t.Fatalf("create video dir: %v", err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatalf("create scratch dir: %v", err)
	}
	videoPath := filepath.Join(videoDir, "sample.mkv")
	if err := os.WriteFile(videoPath, nil, 0o644); err != nil {
		t.Fatalf("create video file: %v", err)
	}

	tempRoot := filepath.Join(outsideDir, "linked-temp")
	if err := os.Symlink(videoDir, tempRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := normalizeOCROptions(videoPath, OCROptions{TempRoot: tempRoot})

	if err == nil {
		t.Fatal("normalizeOCROptions accepted temp root symlinked under video directory")
	}
}

func TestNormalizeOCROptionsDefaultsTimingAndPreprocessing(t *testing.T) {
	root := t.TempDir()
	videoDir := filepath.Join(root, "videos")
	tempRoot := filepath.Join(root, "ocr-temp")
	if err := os.Mkdir(videoDir, 0o755); err != nil {
		t.Fatalf("create video dir: %v", err)
	}
	if err := os.Mkdir(tempRoot, 0o755); err != nil {
		t.Fatalf("create OCR temp root: %v", err)
	}
	videoPath := filepath.Join(videoDir, "sample.mkv")
	if err := os.WriteFile(videoPath, nil, 0o644); err != nil {
		t.Fatalf("create video file: %v", err)
	}

	got, err := normalizeOCROptions(videoPath, OCROptions{TempRoot: tempRoot})
	if err != nil {
		t.Fatalf("normalizeOCROptions returned error: %v", err)
	}

	if got.Crop != DefaultOCRLowerThirdCrop {
		t.Fatalf("Crop = %q, want default %q", got.Crop, DefaultOCRLowerThirdCrop)
	}
	if got.MinConfidence != DefaultOCRMinConfidence {
		t.Fatalf("MinConfidence = %v, want %v", got.MinConfidence, DefaultOCRMinConfidence)
	}
	if got.PageSegMode != DefaultOCRPageSegMode {
		t.Fatalf("PageSegMode = %d, want %d", got.PageSegMode, DefaultOCRPageSegMode)
	}
	if got.PreprocessFilter != "" {
		t.Fatalf("PreprocessFilter = %q, want empty default", got.PreprocessFilter)
	}
	if !slices.Equal(got.ProbeOffsets, []float64{0}) {
		t.Fatalf("ProbeOffsets = %#v, want [0]", got.ProbeOffsets)
	}
}

func TestNormalizeOCROptionsRejectsInvalidPSMAndProbeOffsets(t *testing.T) {
	tests := []struct {
		name    string
		options OCROptions
		want    string
	}{
		{
			name:    "negative PSM",
			options: OCROptions{PageSegMode: -1},
			want:    "OCR PSM",
		},
		{
			name:    "too large PSM",
			options: OCROptions{PageSegMode: 14},
			want:    "OCR PSM",
		},
		{
			name:    "non-finite probe offset",
			options: OCROptions{ProbeOffsets: []float64{math.Inf(1)}},
			want:    "OCR probe offset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeOCROptions("sample.mkv", tt.options)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestOCRFrameFilterComposesPreprocessingAfterCrop(t *testing.T) {
	got := ocrFrameFilter("crop=iw:ih*0.5:0:ih*0.5", "scale=iw*2:ih*2,unsharp=5:5:1")
	want := "crop=iw:ih*0.5:0:ih*0.5,scale=iw*2:ih*2,unsharp=5:5:1"
	if got != want {
		t.Fatalf("filter = %q, want %q", got, want)
	}

	got = ocrFrameFilter("crop=iw:ih*0.5:0:ih*0.5", " \t ")
	want = "crop=iw:ih*0.5:0:ih*0.5"
	if got != want {
		t.Fatalf("filter without preprocessing = %q, want %q", got, want)
	}
}

func TestBuildOCRFrameArgsUsesSeekPairForLargeTimestamps(t *testing.T) {
	args := buildOCRFrameArgs("video.mkv", 42.25, "frame.png", "crop=iw:ih")
	wantPrefix := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-ss", "37.25",
		"-i", "video.mkv",
		"-ss", "5",
	}
	if !slices.Equal(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args prefix = %#v, want %#v; full args %#v", args[:len(wantPrefix)], wantPrefix, args)
	}
	if !slices.Contains(args, "-vf") || args[len(args)-1] != "frame.png" {
		t.Fatalf("args = %#v, want filter flag and output frame", args)
	}
}

func TestBuildOCRFrameArgsUsesSingleSeekForSmallTimestamps(t *testing.T) {
	args := buildOCRFrameArgs("video.mkv", 3.5, "frame.png", "")
	wantPrefix := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-ss", "3.5",
		"-i", "video.mkv",
		"-an",
		"-frames:v", "1",
	}
	if !slices.Equal(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args prefix = %#v, want %#v; full args %#v", args[:len(wantPrefix)], wantPrefix, args)
	}
	if slices.Contains(args, "-vf") {
		t.Fatalf("args = %#v, want no filter flag for empty filter", args)
	}
}

func TestRunOCRProbesSelectsHighestConfidenceAndSkipsNoFrame(t *testing.T) {
	var probes []float64
	result, err := runOCRProbes(10, OCROptions{ProbeOffsets: []float64{0, 2, -1}}, "ocr-work", func(probeTime float64, framePath string) (OCRResult, error) {
		probes = append(probes, probeTime)
		switch probeTime {
		case 10:
			return OCRResult{}, ErrOCRFrameNotFound
		case 12:
			return OCRResult{Time: 12, Text: "low", Confidence: 45}, nil
		case 9:
			return OCRResult{Time: 9, Text: "high", Confidence: 92}, nil
		default:
			t.Fatalf("unexpected probe time %v with frame path %q", probeTime, framePath)
			return OCRResult{}, nil
		}
	})
	if err != nil {
		t.Fatalf("runOCRProbes returned error: %v", err)
	}
	if !slices.Equal(probes, []float64{10, 12, 9}) {
		t.Fatalf("probes = %#v, want [10 12 9]", probes)
	}
	if result.Text != "high" || result.Time != 9 || result.Confidence != 92 {
		t.Fatalf("result = %#v, want high-confidence probe at 9s", result)
	}
}

func TestRunOCRProbesReturnsNoFrameWhenAllProbesMiss(t *testing.T) {
	calls := 0
	_, err := runOCRProbes(1, OCROptions{ProbeOffsets: []float64{-2, 0, 2}}, "ocr-work", func(probeTime float64, framePath string) (OCRResult, error) {
		calls++
		return OCRResult{}, ErrOCRFrameNotFound
	})
	if !errors.Is(err, ErrOCRFrameNotFound) {
		t.Fatalf("error = %v, want ErrOCRFrameNotFound", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 non-negative probes", calls)
	}
}

func TestRunOCRProbesReturnsToolErrorsImmediately(t *testing.T) {
	boom := errors.New("tesseract exploded")
	_, err := runOCRProbes(10, OCROptions{ProbeOffsets: []float64{0, 2}}, "ocr-work", func(probeTime float64, framePath string) (OCRResult, error) {
		return OCRResult{}, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want wrapped tool error", err)
	}
}

func TestOCRLowerThirdIntegration(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg not found: %v", err)
	}
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skipf("tesseract not found: %v", err)
	}

	videoRoot := filepath.Join("testdata", "ocr-integration-video")
	tempRoot := filepath.Join("testdata", "ocr-integration-temp")
	if err := os.MkdirAll(videoRoot, 0o755); err != nil {
		t.Fatalf("create integration video root: %v", err)
	}
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		t.Fatalf("create integration temp root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tempRoot)
		_ = os.RemoveAll(videoRoot)
	})

	videoPath := filepath.Join(videoRoot, "lower-third.mkv")
	synthesizeLowerThirdVideo(t, videoPath)

	result, err := OCRLowerThird(videoPath, 0.5, OCROptions{
		Crop:          "crop=iw:ih*0.5:0:ih*0.5",
		MinConfidence: 20,
		TempRoot:      tempRoot,
	})
	if err != nil {
		t.Fatalf("OCRLowerThird returned error: %v", err)
	}

	upperText := strings.ToUpper(result.Text)
	if !strings.Contains(upperText, "ALICE") {
		t.Fatalf("OCR text = %q, want it to contain ALICE; full result: %#v", result.Text, result)
	}
	if result.Time != 0.5 {
		t.Fatalf("time = %v, want 0.5", result.Time)
	}
	if result.Confidence <= 0 {
		t.Fatalf("confidence = %v, want positive", result.Confidence)
	}
}

func synthesizeLowerThirdVideo(t *testing.T, path string) {
	t.Helper()

	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-hide_banner",
		"-nostats",
		"-f", "lavfi",
		"-i", "color=c=black:size=640x360:rate=2:duration=2",
		"-vf", "drawtext=text=ALICE:fontcolor=white:fontsize=56:x=60:y=h-110",
		"-frames:v", "4",
		"-c:v", "ffv1",
		path,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return
	}
	output := string(out)
	if strings.Contains(output, "No such filter: 'drawtext'") ||
		strings.Contains(output, "Cannot find a valid font") ||
		strings.Contains(output, "Error initializing filter 'drawtext'") {
		t.Skipf("ffmpeg drawtext unavailable: %v\n%s", err, output)
	}
	t.Fatalf("synthesize lower-third video: %v\n%s", err, output)
}
