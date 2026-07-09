package labels

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCandidateOldJSONDecodesWithoutMetadata(t *testing.T) {
	const oldSidecar = `{
  "video": "demo",
  "boundaries": [],
  "candidates": [
    {"time": 12.5, "duration": 1.25, "status": "candidate"}
  ]
}`

	var got VideoLabels
	if err := json.Unmarshal([]byte(oldSidecar), &got); err != nil {
		t.Fatalf("Unmarshal old sidecar returned error: %v", err)
	}

	if len(got.Candidates) != 1 {
		t.Fatalf("len(Candidates) = %d, want 1", len(got.Candidates))
	}
	candidate := got.Candidates[0]
	if candidate.Time != 12.5 || candidate.Duration != 1.25 || candidate.Status != "candidate" {
		t.Fatalf("Candidate = %#v, want old fields preserved", candidate)
	}
	if len(candidate.Sources) != 0 || candidate.Confidence != 0 || candidate.SuggestedName != "" || candidate.Conflict {
		t.Fatalf("Candidate metadata = %#v, want zero values for missing old JSON fields", candidate)
	}
}

func TestCandidateMetadataJSONNamesAndOmitEmpty(t *testing.T) {
	withoutMetadata, err := json.Marshal(Candidate{Time: 12.5, Duration: 1.25, Status: "candidate"})
	if err != nil {
		t.Fatalf("Marshal candidate without metadata returned error: %v", err)
	}
	for _, field := range []string{"sources", "confidence", "suggestedName", "conflict"} {
		if strings.Contains(string(withoutMetadata), field) {
			t.Fatalf("Marshal without metadata = %s, did not want field %q", withoutMetadata, field)
		}
	}

	withMetadata, err := json.Marshal(Candidate{
		Time:          12.5,
		Duration:      1.25,
		Status:        "candidate",
		Sources:       []string{"silence", "chapter-list"},
		Confidence:    0.875,
		SuggestedName: "Intro",
		Conflict:      true,
	})
	if err != nil {
		t.Fatalf("Marshal candidate with metadata returned error: %v", err)
	}
	for _, want := range []string{
		`"sources":["silence","chapter-list"]`,
		`"confidence":0.875`,
		`"suggestedName":"Intro"`,
		`"conflict":true`,
	} {
		if !strings.Contains(string(withMetadata), want) {
			t.Fatalf("Marshal with metadata = %s, want %s", withMetadata, want)
		}
	}
}
