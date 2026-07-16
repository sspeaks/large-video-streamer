package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndexServesHTMLWithShowsFetch(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	Index().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Index() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	if body := rec.Body.String(); !strings.Contains(body, "/api/shows") {
		t.Fatalf("Index() body does not contain /api/shows")
	}
}

func TestIndexIncludesAccessiblePendingReviewIndicator(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	Index().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Index() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		"show.pendingReviews",
		"className = 'reviewBadge'",
		"plural(pendingReviews, 'pending review')",
		"titleText + ', ready to play'",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Index() body does not contain %q for the pending-review indicator", want)
		}
	}
}

func TestPlayerServesHTMLWithHLSAndChapters(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "hls") {
		t.Fatalf("Player() body does not reference hls")
	}
	if !strings.Contains(body, "chapters.vtt") {
		t.Fatalf("Player() body does not reference chapters.vtt")
	}
}

func TestPlayerIncludesKeyboardShortcuts(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		"addEventListener('keydown'",
		"Keyboard shortcuts",
		"seekBy",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body does not contain %q for keyboard shortcuts", want)
		}
	}
}

func TestPlayerIncludesBoundaryHotkey(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		"pendingBoundaryStart",
		"startBoundaryMark",
		"cancelBoundaryMark",
		"offerBoundaryUndo",
		"Mark a boundary at the current spot",
		"<kbd>b</kbd>",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body does not contain %q for the boundary hotkey", want)
		}
	}
}

func TestPlayerBoundaryFocusKeepsVideoInFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		"function focusWithoutScroll",       // helper that suppresses focus-driven scroll exists
		"preventScroll: true",               // uses the standard scroll-suppression option
		"focusWithoutScroll(boundaryNameEl", // boundary focus routes through the helper
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body should contain %q so marking a boundary keeps the video in frame", want)
		}
	}
	// The bare focus() calls that panned the video off-screen must all be rerouted.
	if strings.Contains(body, "boundaryNameEl.focus(") {
		t.Fatal("Player() body should no longer contain \"boundaryNameEl.focus(\": boundary focus must not scroll the video out of frame")
	}
}

func TestPlayerShowsAlwaysVisibleShortcutsAffordance(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		`id="shortcutsToggle"`, // always-visible affordance near the video
		"getElementById('shortcutsToggle').addEventListener('click', openShortcutsHelp)", // wired to reveal the legend
		`id="markStatus"`, // near-video status keeps the user oriented while marking
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body should contain %q so keyboard shortcuts are discoverable without knowing ?", want)
		}
	}
}

func TestPlayerShowsChaptersBelowVideoWithoutRemove(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{`id="chaptersPanel"`, "shareChapter"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body does not contain %q for the chapters section", want)
		}
	}
	if strings.Contains(body, "removeChapter") {
		t.Fatalf("Player() body still references removeChapter; delete is handled on the labels page")
	}
}

func TestPlayerMarksNonCredentialInputsForPasswordManagers(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		`id="boundaryName" name="boundaryName"`,
		`autocomplete="off" data-lpignore="true"`,
		`expiryInput.setAttribute('data-lpignore', 'true')`,
		`urlInput.setAttribute('data-lpignore', 'true')`,
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body does not contain %q for non-credential inputs", want)
		}
	}
}

func TestPlayerAllowsRenamingChaptersInline(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		"renameChapter",
		"beginRename",
		"commitRename",
		"'Rename ' + cue.label + ' chapter'",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body does not contain %q for inline chapter rename", want)
		}
	}
	if strings.Contains(body, "removeChapter") {
		t.Fatalf("Player() body still references removeChapter; delete is handled on the labels page")
	}
}

func TestHandlerServesVendoredHLS(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/hls.min.js", nil)

	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Handler() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() == 0 {
		t.Fatalf("Handler() served an empty hls.min.js body")
	}
}

// TestPlayerShowsGuidedEmptyStateWithDualCTAs verifies AC1–AC3: guided empty
// state with "Add your first chapter" (triggers openChapterEditor) and
// "Auto-detect" link (navigates to /labels/{show}).
func TestPlayerShowsGuidedEmptyStateWithDualCTAs(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		"renderGuidedEmptyState",
		"lastCandidatesEmpty",
		"Add your first chapter",
		"Auto-detect",
		"openChapterEditor",
		"chapterGuideCtas",
		"/labels/",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body does not contain %q for guided empty state (AC1–AC3)", want)
		}
	}
}

// TestPlayerIncludesFirstVisitLabelingTip verifies AC4: one-time tooltip on
// the "Add chapter" button, dismissed via "Got it" / Escape / click-outside,
// persisted in localStorage.
func TestPlayerIncludesFirstVisitLabelingTip(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		`id="labelingTip"`,
		`id="labelingTipDismiss"`,
		"Got it",
		"labeling-tip-dismissed",
		"localStorage",
		"dismissLabelingTip",
		"initLabelingTip",
		"handleTipClickOutside",
		`role="note"`,
		"aria-describedby",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body does not contain %q for first-visit tip (AC4)", want)
		}
	}
	// WAI-ARIA: tooltip role must not contain interactive descendants.
	// Verify the callout uses role="note" (not role="tooltip").
	if strings.Contains(body, `role="tooltip"`) {
		t.Fatal("Player() body must not use role=\"tooltip\" for an interactive callout — use role=\"note\" instead (WAI-ARIA #25)")
	}
}

// TestPlayerLabelingCalloutUsesValidARIARole is a regression guard for issue
// #25: WAI-ARIA forbids interactive descendants inside role="tooltip".
// The labeling tip must use role="note" (an informational callout that can
// contain interactive elements) so its "Got it" button is accessible.
func TestPlayerLabelingCalloutUsesValidARIARole(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if strings.Contains(body, `role="tooltip"`) {
		t.Fatal("Player() must not use role=\"tooltip\" for interactive callout — WAI-ARIA tooltip role forbids interactive descendants (issue #25)")
	}
	if !strings.Contains(body, `role="note"`) {
		t.Fatal("Player() labeling callout should use role=\"note\" for an interactive informational region")
	}
	if !strings.Contains(body, `id="labelingTipDismiss"`) {
		t.Fatal("Player() labeling callout must retain the interactive 'Got it' dismiss button")
	}
}

// TestPlayerLabelingTipDismissesOnEscape verifies AC4/AC7: Escape key wired to
// dismissLabelingTip so the tip can be closed via keyboard.
func TestPlayerLabelingTipDismissesOnEscape(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "dismissLabelingTip") {
		t.Fatal("Player() body missing dismissLabelingTip — Escape cannot dismiss the tip")
	}
	// Both dismissLabelingTip and cancelBoundaryMark must be called in Escape handler.
	if !strings.Contains(body, "didDismissTip") {
		t.Fatal("Player() Escape handler should capture dismissLabelingTip result")
	}
}

// TestPlayerHasMobileResponsiveGuidedState verifies AC8: CSS stacks CTAs
// vertically at ≤640 px and renders tooltip as a block element.
func TestPlayerHasMobileResponsiveGuidedState(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	body := rec.Body.String()
	wants := []string{
		"chapterGuideCtas",
		"labelingTip",
		"flex-direction: column",
		"@media (max-width: 640px)",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Player() body does not contain %q for mobile responsive layout (AC8)", want)
		}
	}
}

// TestIndexIncludesEnhancedLibraryBadges verifies AC5: "No chapters" badge for
// unlabeled shows, "✓ Labeled" for fully-handled shows, plus the existing
// "N pending reviews" badge.
func TestIndexIncludesEnhancedLibraryBadges(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	Index().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Index() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	wants := []string{
		"reviewBadge--neutral",
		"reviewBadge--success",
		"No chapters",
		"✓ Labeled",
		"boundaryCount",
		"candidateCount",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("Index() body does not contain %q for enhanced library badges (AC5)", want)
		}
	}
	// Existing pending-review badge must still be present.
	if !strings.Contains(body, "reviewBadge") {
		t.Fatal("Index() body missing reviewBadge class — existing badge regressed")
	}
}
