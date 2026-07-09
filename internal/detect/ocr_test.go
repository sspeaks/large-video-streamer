package detect

import (
	"os"
	"os/exec"
	"path/filepath"
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
