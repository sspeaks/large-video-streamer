package detect

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// OCRResult is recognized lower-third text near a video timestamp.
type OCRResult struct {
	Time       float64
	Text       string
	Confidence float64
}

// OCROptions controls lower-third frame extraction and word filtering.
type OCROptions struct {
	Crop          string
	MinConfidence float64
	TempRoot      string
	Language      string
}

const (
	DefaultOCRLowerThirdCrop = "crop=iw:ih*0.35:0:ih*0.65"
	DefaultOCRMinConfidence  = 50.0
)

// OCRLowerThird extracts a lower-third frame near timestamp and runs Tesseract TSV OCR.
func OCRLowerThird(path string, timestamp float64, options OCROptions) (OCRResult, error) {
	if timestamp < 0 {
		return OCRResult{}, fmt.Errorf("ocr timestamp must be non-negative: %s", formatFloat(timestamp))
	}

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return OCRResult{}, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}
	tesseract, err := exec.LookPath("tesseract")
	if err != nil {
		return OCRResult{}, fmt.Errorf("tesseract not found in PATH: %w", err)
	}

	options, err = normalizeOCROptions(path, options)
	if err != nil {
		return OCRResult{}, err
	}

	dir, err := os.MkdirTemp(options.TempRoot, "large-video-streamer-ocr-*")
	if err != nil {
		return OCRResult{}, fmt.Errorf("create OCR temp directory: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := ensurePathOutsideVideoDir(dir, path); err != nil {
		return OCRResult{}, err
	}

	framePath := filepath.Join(dir, "frame.png")
	if err := extractOCRFrame(ffmpeg, path, timestamp, framePath, options.Crop); err != nil {
		return OCRResult{}, err
	}

	tsv, err := runTesseractTSV(tesseract, framePath, options.Language)
	if err != nil {
		return OCRResult{}, err
	}

	text, confidence := parseTesseractTSV(tsv, options.MinConfidence)
	return OCRResult{Time: timestamp, Text: text, Confidence: confidence}, nil
}

func normalizeOCROptions(videoPath string, options OCROptions) (OCROptions, error) {
	if strings.TrimSpace(options.Crop) == "" {
		options.Crop = DefaultOCRLowerThirdCrop
	}
	if options.MinConfidence == 0 {
		options.MinConfidence = DefaultOCRMinConfidence
	}
	if strings.TrimSpace(options.TempRoot) == "" {
		options.TempRoot = os.TempDir()
	}

	if err := ensurePathOutsideVideoDir(options.TempRoot, videoPath); err != nil {
		return OCROptions{}, err
	}

	tempRootAbs, err := filepath.Abs(options.TempRoot)
	if err != nil {
		return OCROptions{}, fmt.Errorf("resolve OCR temp root: %w", err)
	}
	options.TempRoot = tempRootAbs
	return options, nil
}

func ensurePathOutsideVideoDir(path string, videoPath string) error {
	tempRoot, err := resolvedPath(path)
	if err != nil {
		return fmt.Errorf("resolve OCR temp root: %w", err)
	}
	videoAbs, err := filepath.Abs(videoPath)
	if err != nil {
		return fmt.Errorf("resolve video path: %w", err)
	}
	videoDir, err := resolvedPath(filepath.Dir(videoAbs))
	if err != nil {
		return fmt.Errorf("resolve video directory: %w", err)
	}
	if pathWithinDir(tempRoot, videoDir) {
		return fmt.Errorf("ocr temp root %q must not be under video directory %q", tempRoot, videoDir)
	}
	return nil
}

func resolvedPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	evaluated, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return evaluated, nil
	}
	if os.IsNotExist(err) {
		return abs, nil
	}
	return "", err
}

func pathWithinDir(path string, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func extractOCRFrame(ffmpeg string, videoPath string, timestamp float64, framePath string, crop string) error {
	args := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-ss", formatFloat(timestamp),
		"-i", videoPath,
		"-an",
		"-frames:v", "1",
	}
	if strings.TrimSpace(crop) != "" {
		args = append(args, "-vf", crop)
	}
	args = append(args, framePath)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(ffmpeg, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg OCR frame extraction failed: %w: %s", err, ocrCommandErrorText(stdout.String(), stderr.String()))
	}
	if _, err := os.Stat(framePath); err != nil {
		return fmt.Errorf("ffmpeg OCR frame extraction did not create frame: %w", err)
	}
	return nil
}

func runTesseractTSV(tesseract string, imagePath string, language string) (string, error) {
	args := []string{imagePath, "stdout"}
	if strings.TrimSpace(language) != "" {
		args = append(args, "-l", language)
	}
	args = append(args, "--psm", "6", "tsv")

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(tesseract, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tesseract TSV OCR failed: %w: %s", err, ocrCommandErrorText(stdout.String(), stderr.String()))
	}
	return stdout.String(), nil
}

func parseTesseractTSV(output string, minConfidence float64) (string, float64) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var columns []string
	var levelIndex, confIndex, textIndex int
	var words []string
	var confidenceSum float64

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		if columns == nil {
			columns = strings.Split(line, "\t")
			indexes := tesseractTSVIndexes(columns)
			levelIndex, confIndex, textIndex = indexes.level, indexes.conf, indexes.text
			if levelIndex < 0 || confIndex < 0 || textIndex < 0 {
				return "", 0
			}
			continue
		}

		fields := strings.SplitN(line, "\t", len(columns))
		if len(fields) <= levelIndex || len(fields) <= confIndex || len(fields) <= textIndex {
			continue
		}

		level, err := strconv.Atoi(strings.TrimSpace(fields[levelIndex]))
		if err != nil || level != 5 {
			continue
		}
		confidence, err := strconv.ParseFloat(strings.TrimSpace(fields[confIndex]), 64)
		if err != nil || confidence < minConfidence {
			continue
		}
		text := strings.TrimSpace(fields[textIndex])
		if text == "" {
			continue
		}

		words = append(words, text)
		confidenceSum += confidence
	}

	if len(words) == 0 {
		return "", 0
	}
	return strings.Join(words, " "), confidenceSum / float64(len(words))
}

type tesseractTSVColumnIndexes struct {
	level int
	conf  int
	text  int
}

func tesseractTSVIndexes(columns []string) tesseractTSVColumnIndexes {
	indexes := tesseractTSVColumnIndexes{level: -1, conf: -1, text: -1}
	for i, column := range columns {
		switch strings.TrimSpace(strings.ToLower(column)) {
		case "level":
			indexes.level = i
		case "conf":
			indexes.conf = i
		case "text":
			indexes.text = i
		}
	}
	return indexes
}

func ocrCommandErrorText(stdout string, stderr string) string {
	var parts []string
	if text := strings.TrimSpace(stderr); text != "" {
		parts = append(parts, text)
	}
	if text := strings.TrimSpace(stdout); text != "" {
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return "no command output"
	}
	return strings.Join(parts, "\n")
}
