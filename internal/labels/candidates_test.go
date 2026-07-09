package labels

import "testing"

func TestMergeCandidatesPreservesDecisionsAndDedups(t *testing.T) {
	existing := []Candidate{
		{Time: 100, Duration: 3, Status: "rejected"},  // decision kept
		{Time: 200, Duration: 4, Status: "named"},     // decision kept
		{Time: 300, Duration: 2, Status: "candidate"}, // undecided -> dropped, superseded by fresh scan
	}
	detected := []Candidate{
		{Time: 100.4, Duration: 3, Status: "candidate"}, // within 1s of rejected(100) -> skipped
		{Time: 250, Duration: 5, Status: "candidate"},   // new -> kept
		{Time: 200.9, Duration: 4, Status: "candidate"}, // within 1s of named(200) -> skipped
	}

	got := mergeCandidates(existing, detected)

	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3: %#v", len(got), got)
	}
	if got[0].Time != 100 || got[0].Status != "rejected" {
		t.Fatalf("got[0] = %#v, want kept rejected@100", got[0])
	}
	if got[1].Time != 200 || got[1].Status != "named" {
		t.Fatalf("got[1] = %#v, want kept named@200", got[1])
	}
	if got[2].Time != 250 || got[2].Status != "candidate" {
		t.Fatalf("got[2] = %#v, want new candidate@250", got[2])
	}
}

func TestMergeCandidatesEmptyExisting(t *testing.T) {
	detected := []Candidate{{Time: 30, Duration: 2, Status: "candidate"}, {Time: 10, Duration: 2, Status: "candidate"}}
	got := mergeCandidates(nil, detected)
	if len(got) != 2 || got[0].Time != 10 || got[1].Time != 30 {
		t.Fatalf("got = %#v, want sorted [10,30]", got)
	}
}

func TestMergeCandidatesKeepsNearbySilenceOnlyDetectionsSeparate(t *testing.T) {
	detected := []Candidate{
		{Time: 10, Duration: 2, Status: "candidate"},
		{Time: 10.5, Duration: 3, Status: "candidate"},
	}

	got := mergeCandidates(nil, detected)

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %#v", len(got), got)
	}
}

func TestMergeCandidatesMergesMetadataForNearbyDetections(t *testing.T) {
	detected := []Candidate{
		{Time: 30, Duration: 2, Status: "candidate", Sources: []string{"silence", "ocr"}, Confidence: 0.4},
		{Time: 30.8, Duration: 3, Status: "candidate", Sources: []string{"ocr", "visual"}, Confidence: 0.9},
	}

	got := mergeCandidates(nil, detected)

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %#v", len(got), got)
	}
	if len(got[0].Sources) != 3 || got[0].Sources[0] != "silence" || got[0].Sources[1] != "ocr" || got[0].Sources[2] != "visual" {
		t.Fatalf("Sources = %#v, want union [silence ocr visual]", got[0].Sources)
	}
}

func TestMergeCandidatesUsesHighestConfidenceForNearbyDetections(t *testing.T) {
	detected := []Candidate{
		{Time: 40, Duration: 2, Status: "candidate", Sources: []string{"silence"}, Confidence: 0.35},
		{Time: 40.5, Duration: 2, Status: "candidate", Sources: []string{"ocr"}, Confidence: 0.82},
	}

	got := mergeCandidates(nil, detected)

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %#v", len(got), got)
	}
	if got[0].Confidence != 0.82 {
		t.Fatalf("Confidence = %v, want 0.82", got[0].Confidence)
	}
}

func TestMergeCandidatesPrefersNonEmptySuggestedNameForNearbyDetections(t *testing.T) {
	detected := []Candidate{
		{Time: 50, Duration: 2, Status: "candidate", Sources: []string{"silence"}},
		{Time: 50.2, Duration: 2, Status: "candidate", Sources: []string{"ocr"}, SuggestedName: "Opening"},
	}

	got := mergeCandidates(nil, detected)

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %#v", len(got), got)
	}
	if got[0].SuggestedName != "Opening" {
		t.Fatalf("SuggestedName = %q, want Opening", got[0].SuggestedName)
	}
}

func TestMergeCandidatesPropagatesConflictForNearbyDetections(t *testing.T) {
	detected := []Candidate{
		{Time: 60, Duration: 2, Status: "candidate", Sources: []string{"visual"}},
		{Time: 60.4, Duration: 2, Status: "candidate", Sources: []string{"ocr"}, Conflict: true},
	}

	got := mergeCandidates(nil, detected)

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %#v", len(got), got)
	}
	if !got[0].Conflict {
		t.Fatalf("Conflict = false, want true")
	}
}

func TestMergeCandidatesWithBoundariesSkipsDetectedNearExistingBoundary(t *testing.T) {
	boundaries := []Boundary{{Name: "Intro", Start: 90}}
	detected := []Candidate{
		{Time: 90.4, Duration: 2, Status: "candidate", Sources: []string{"silence"}},
		{Time: 120, Duration: 2, Status: "candidate", Sources: []string{"silence"}},
	}

	got := mergeCandidatesWithBoundaries(nil, detected, boundaries)

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %#v", len(got), got)
	}
	if got[0].Time != 120 {
		t.Fatalf("got[0].Time = %v, want 120", got[0].Time)
	}
}

func TestMergeCandidatesWithBoundariesKeepsDetectedAtOneSecondBoundary(t *testing.T) {
	boundaries := []Boundary{{Name: "Intro", Start: 90}}
	detected := []Candidate{
		{Time: 90.999, Duration: 2, Status: "candidate", Sources: []string{"silence"}},
		{Time: 91, Duration: 2, Status: "candidate", Sources: []string{"silence"}},
	}

	got := mergeCandidatesWithBoundaries(nil, detected, boundaries)

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want only candidate exactly 1s from boundary kept: %#v", len(got), got)
	}
	if got[0].Time != 91 {
		t.Fatalf("kept candidate Time = %v, want 91", got[0].Time)
	}
}

func TestMergeCandidatesExistingDecisionsWinOverDetectedMetadata(t *testing.T) {
	existing := []Candidate{
		{Time: 70, Duration: 2, Status: "named", SuggestedName: "Kept"},
		{Time: 80, Duration: 2, Status: "rejected"},
	}
	detected := []Candidate{
		{Time: 70.4, Duration: 3, Status: "candidate", Sources: []string{"ocr"}, Confidence: 1, SuggestedName: "Detected"},
		{Time: 80.4, Duration: 3, Status: "candidate", Sources: []string{"visual"}, Confidence: 1},
	}

	got := mergeCandidates(existing, detected)

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %#v", len(got), got)
	}
	if got[0].Time != 70 || got[0].Status != "named" || got[0].SuggestedName != "Kept" {
		t.Fatalf("got[0] = %#v, want original named decision", got[0])
	}
	if got[1].Time != 80 || got[1].Status != "rejected" {
		t.Fatalf("got[1] = %#v, want original rejected decision", got[1])
	}
}
