package labels

import (
	"bytes"
	"regexp"
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
		`id="autodetect" class="primary mutating-control"`,
		"Auto-detect boundaries",
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

func TestLabelsPagePresentsSimplePrimaryAndAdvancedActions(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	primaryActions := regexp.MustCompile(`(?s)<div class="actions" aria-label="Label editor actions">(.*?)</div>`).FindString(out)
	if primaryActions == "" {
		t.Fatal("labels page should render the primary label editor actions")
	}
	for _, want := range []string{`id="add-boundary"`, `id="save"`} {
		if !strings.Contains(primaryActions, want) {
			t.Fatalf("primary label editor actions should contain %q", want)
		}
	}
	for _, notWant := range []string{`id="detect"`, `id="autodetect"`, `id="export"`, `id="import"`} {
		if strings.Contains(primaryActions, notWant) {
			t.Fatalf("primary label editor actions should not contain advanced or workflow action %q", notWant)
		}
	}

	wants := []string{
		`<details class="help advanced-tools" id="advanced-tools">`,
		"<summary>Advanced tools</summary>",
		`id="detect" class="secondary mutating-control">Scan silence only</button>`,
		`id="export" class="secondary mutating-control">Export timestamps</button>`,
		`id="import" class="secondary mutating-control">Import timestamps</button>`,
		`id="timestamps" name="timestamps"`,
		"Set up auto-detect",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q in the simplified workflow", want)
		}
	}
	for _, retired := range []string{">Detect silences</button>", ">Suggest boundaries</button>"} {
		if strings.Contains(out, retired) {
			t.Fatalf("labels page should not render retired primary terminology %q", retired)
		}
	}
}

func TestLabelsPageDetectionControlsKeepDistinctEndpointWiring(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		`id="autodetect" class="primary mutating-control"`,
		"Auto-detect boundaries",
		"fetch(api + '/autodetect', {",
		`id="detect" class="secondary mutating-control"`,
		"Scan silence only",
		"fetch(api + '/detect', { method: 'POST' })",
		"Auto-detecting…",
		"Scanning…",
		"Auto-detect failed:",
		"Silence scan failed:",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to preserve endpoint wiring and feedback", want)
		}
	}
}

func TestLabelsPageDefaultsVisualOnAndOCROff(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	colorInput := regexp.MustCompile(`<input[^>]*id="autodetect-use-color"[^>]*>`).FindString(out)
	if colorInput == "" {
		t.Fatal("labels page should render autodetect-use-color checkbox")
	}
	if !strings.Contains(colorInput, "checked") {
		t.Fatalf("autodetect-use-color should be checked by default, input = %q", colorInput)
	}
	if strings.Contains(out, "Use color (slow)") {
		t.Fatal("color autodetect label should no longer say slow")
	}

	ocrInput := regexp.MustCompile(`<input[^>]*id="autodetect-use-ocr"[^>]*>`).FindString(out)
	if ocrInput == "" {
		t.Fatal("labels page should render autodetect-use-ocr checkbox")
	}
	if strings.Contains(ocrInput, "checked") {
		t.Fatalf("autodetect-use-ocr should stay unchecked by default, input = %q", ocrInput)
	}
	if !strings.Contains(out, "Use OCR (slow)") {
		t.Fatal("OCR autodetect label should keep the slow warning")
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

func TestLabelsPageHighlightsReviewPriorityMetadata(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		"Review priority",
		"const sourceDisplayName = (source) =>",
		"black: 'Black'",
		"freeze: 'Freeze'",
		"source-badge--black",
		"source-badge--freeze",
		"const candidateLowConfidence = (candidate) =>",
		"Low confidence",
		"const candidateReviewPriority = (candidate) =>",
		"sortControl.value === 'review-priority'",
		"labels.candidates.map((candidate, index) => ({ candidate: candidate, index: index, key: candidateKey(candidate) }))",
		"|| a.index - b.index",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to prioritize uncertain/conflicting black/freeze candidates for review", want)
		}
	}
	if strings.Contains(out, "labels.candidates.sort") {
		t.Fatal("labels page should sort a copied candidate item list, not mutate saved/API candidate order")
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
		"!item.candidate.conflict",
		"promoteCandidate(item.candidate, candidateSuggestedName(item.candidate))",
		"Save to persist.",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to promote high-confidence suggestions without saving", want)
		}
	}
}

func TestLabelsPageReconnectsToBackgroundAnalysis(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		"fetch(api + '/detect', { method: 'POST' })",
		"fetch(api + '/autodetect', {",
		"fetch(api + '/' + operation)",
		"window.setTimeout(() => checkBackgroundStatus(operation), 3000)",
		"Auto-detecting boundaries in the background",
		"You can close this page; results will be saved for review.",
		"await resumeBackgroundJobs()",
		"await loadLabels()",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to resume background analysis after reload", want)
		}
	}

	if strings.Contains(out, "setDirty(true);\n        setStatus('Suggested ") {
		t.Fatal("completed background analysis should load persisted candidates as saved state")
	}
}

func TestLabelsPageHydratesLineupTextareaOnLoad(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		"labels.lineup = labels.lineup || []",
		"autodetectLineup.value = labels.lineup.join('\\n')",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to hydrate the lineup textarea on load", want)
		}
	}
}

func TestLabelsPageLineupDirtyTracking(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		"autodetectLineup.addEventListener('input'",
		"labels.lineup = autodetectLineup.value.split(/\\r?\\n/).map((n) => n.trim()).filter(Boolean)",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to sync lineup to labels and mark page dirty", want)
		}
	}
}

func TestLabelsPagePreservesLineupAcrossTimestampImport(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		"const prevLineup = (labels && labels.lineup) ? [...labels.lineup] : []",
		"labels.lineup = prevLineup",
		"autodetectLineup.value = prevLineup.join('\\n')",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q to preserve lineup when importing timestamp text", want)
		}
	}
}

// TestLabelsPageShowsGuidedOnboardingWhenEmpty verifies AC6: when boundaries
// and candidates are both zero, the label editor shows a guided onboarding
// panel and auto-expands the keyboard shortcuts <details> element.
func TestLabelsPageShowsGuidedOnboardingWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	wants := []string{
		`id="onboarding-panel"`,
		"class=\"onboarding-panel\"",
		"maybeShowOnboarding",
		"No labels yet",
		"shortcuts.open = true",
		"onboarding-panel",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("labels page should contain %q for guided onboarding (AC6)", want)
		}
	}
}

// TestLabelsPageOnboardingUsesInstructionalPlaceholder verifies AC6: the
// lineup textarea placeholder explains how to enter group names.
func TestLabelsPageOnboardingUsesInstructionalPlaceholder(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Enter one group name per line") {
		t.Fatal("labels page lineup textarea placeholder should contain instructional text (AC6)")
	}
}
