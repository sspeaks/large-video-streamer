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
