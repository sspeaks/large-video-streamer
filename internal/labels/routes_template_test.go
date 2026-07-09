package labels

import (
	"bytes"
	"strings"
	"testing"
)

// TestLabelsPageEmbedsShowNameWithoutDoubleQuoting guards against a regression
// where the show name was double-quoted in the page's JavaScript (printf "%q"
// plus html/template's own JS-string escaping), which made the labels page
// operate on a differently-named sidecar than the player and pass a quoted path
// to ffmpeg.
func TestLabelsPageEmbedsShowNameWithoutDoubleQuoting(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, `const show = "quartet_finals";`) {
		t.Fatalf("labels page should embed the show as a plain JS string `const show = \"quartet_finals\";`")
	}
	if strings.Contains(out, `\"quartet_finals\"`) {
		t.Fatal("show name is double-quoted in the rendered page (regression)")
	}
}

// TestLabelsPageIncludesKeyboardShortcuts asserts the editor wires up the
// candidate-review keyboard layer (global keydown handler, current-candidate
// promote/reject/replay/nudge helpers, and the discoverable help legend) and no
// longer carries the retired boundary-navigation shortcuts.
func TestLabelsPageIncludesKeyboardShortcuts(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		"addEventListener('keydown'",
		"Keyboard shortcuts",
		"openShortcutsHelp",
		"stepCandidate",
		"promoteCurrentCandidate",
		"rejectCurrentCandidate",
		"replayCurrent",
		"nudgeCurrentCandidate",
		"candidate-current",
		"Nudged candidate",
		"Alt",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to wire up candidate keyboard shortcuts", want)
		}
	}

	for _, notWant := range []string{"jumpBoundary", "Nudged start", "boundary-start"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("labels page should no longer contain %q after moving shortcuts to candidates", notWant)
		}
	}
}

// TestLabelsPageKeyboardNavigationKeepsWindowSteady guards issue #12: pressing
// j/k to step candidates must not move the browser window (no "warp down" to the
// candidate row followed by a "slide back up" to the video). render() must scroll
// the highlighted row only inside the candidate table's own overflow region via
// scrollRowIntoView, and seekPreview must gate the window-moving video scroll
// behind an explicit opt-in so only the "Preview" buttons reveal the video.
func TestLabelsPageKeyboardNavigationKeepsWindowSteady(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		"const scrollRowIntoView",       // container-only scroll helper exists
		"scrollRowIntoView(currentRow)", // render() uses it instead of window scroll
		"if (opts.scroll)",              // seekPreview only scrolls when asked
		"{ scroll: true }",              // explicit Preview buttons opt into reveal-scroll
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q so j/k navigation keeps the window on the video", want)
		}
	}

	// The window-moving row scroll (the "warp down" half of the bug) used
	// block: 'nearest' and was the only such occurrence in the page.
	if strings.Contains(out, "block: 'nearest'") {
		t.Fatal("labels page should no longer contain \"block: 'nearest'\": keyboard navigation must not scroll the window to the candidate row")
	}
}

func TestLabelsPageMarksEditorInputsForPasswordManagers(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		`id="bulk-name" name="bulk-name" type="text"`,
		`id="timestamps" name="timestamps" autocomplete="off" data-lpignore="true"`,
		`name="boundary-name-' + index + '" autocomplete="off" data-lpignore="true"`,
		`name="candidate-boundary-name-' + item.index + '" autocomplete="off" data-lpignore="true"`,
		`name="candidate-select-' + item.index + '" aria-label=`,
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to keep password managers off editor controls", want)
		}
	}
}

func TestLabelsPageIncludesAutodetectControlsAndRequest(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		`id="autodetect"`,
		`id="autodetect-lineup" name="autodetect-lineup"`,
		`id="autodetect-use-silence" name="autodetect-use-silence"`,
		`id="autodetect-use-color" name="autodetect-use-color"`,
		`id="autodetect-use-ocr" name="autodetect-use-ocr"`,
		"const runAutodetect = async () =>",
		"api + '/autodetect'",
		"useSilence: autodetectUseSilence.checked",
		"useColor: autodetectUseColor.checked",
		"useOCR: autodetectUseOCR.checked",
		"Auto-detect failed:",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to wire up autodetect controls", want)
		}
	}
}

func TestLabelsPageRendersAutodetectCandidateMetadata(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		"<th>Sources</th>",
		"<th>Confidence</th>",
		"source-badge",
		"formatConfidence(candidate.confidence)",
		"candidate.conflict",
		"candidate-conflict",
		"candidateSuggestedName(candidate) || 'group-a'",
		"candidateDefaultName(item.candidate, bulkName.value)",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to show autodetect candidate metadata", want)
		}
	}
}

func TestLabelsPageIncludesHighConfidenceBulkApply(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		`id="promote-high-confidence"`,
		"const highConfidenceCandidateItems = () =>",
		"Number(item.candidate.confidence) >= 0.85",
		"candidateSuggestedName(item.candidate)",
		"promoteCandidate(item.candidate, candidateSuggestedName(item.candidate))",
		"Save to persist.",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to promote high-confidence suggestions without saving", want)
		}
	}
}
