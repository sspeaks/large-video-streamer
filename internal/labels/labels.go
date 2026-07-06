package labels

import (
	"io"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

// Boundary is a named chapter start in seconds.
type Boundary struct {
	Name  string  `json:"name"`
	Start float64 `json:"start"`
}

// Candidate is a silence-detected possible chapter boundary.
type Candidate struct {
	Time     float64 `json:"time"`
	Duration float64 `json:"duration"`
	Status   string  `json:"status"`
}

// VideoLabels is the JSON sidecar model stored as <video>.labels.json.
type VideoLabels struct {
	Video      string      `json:"video"`
	Boundaries []Boundary  `json:"boundaries"`
	Candidates []Candidate `json:"candidates"`
}

// Store loads and saves per-video label sidecars.
type Store struct {
	cfg config.Config
}

// New returns a label store rooted in the configured video directory.
func New(cfg config.Config) *Store {
	return &Store{cfg: cfg}
}

// Load reads labels for video.
func (s *Store) Load(video string) (VideoLabels, error) {
	return s.load(video)
}

// Save writes labels to their JSON sidecar.
func (s *Store) Save(labels VideoLabels) error {
	return s.save(labels)
}

// ToWebVTT renders labels as WebVTT chapters.
func (s *Store) ToWebVTT(labels VideoLabels) string {
	return toWebVTT(labels)
}

// ImportTimestamps parses timestamp labels from r.
func (s *Store) ImportTimestamps(r io.Reader) (VideoLabels, error) {
	return importTimestamps(r)
}

// ExportTimestamps renders labels to timestamp text.
func (s *Store) ExportTimestamps(labels VideoLabels) string {
	return exportTimestamps(labels)
}
