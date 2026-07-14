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
	Time          float64  `json:"time"`
	Duration      float64  `json:"duration"`
	Status        string   `json:"status"`
	Sources       []string `json:"sources,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	SuggestedName string   `json:"suggestedName,omitempty"`
	Conflict      bool     `json:"conflict,omitempty"`
	VisualAnchor  float64  `json:"-"`
	FusionAnchor  float64  `json:"-"`
}

// VideoLabels is the JSON sidecar model stored as <video>.labels.json.
type VideoLabels struct {
	Video      string      `json:"video"`
	Boundaries []Boundary  `json:"boundaries"`
	Candidates []Candidate `json:"candidates"`
}

// LabelStore persists per-video label sidecars.
type LabelStore interface {
	Load(video string) (VideoLabels, error)
	Save(labels VideoLabels) error
}

// Store loads and saves per-video label sidecars.
type Store struct {
	cfg config.Config
}

var _ LabelStore = (*Store)(nil)

// Server owns the label routes while delegating persistence to a LabelStore.
type Server struct {
	cfg               config.Config
	store             LabelStore
	autodetectSignals autodetectSignals
}

// New returns the flat-file label store rooted in the configured state directory.
func New(cfg config.Config) *Store {
	return &Store{cfg: cfg}
}

// NewServer returns a label route server using store for persistence. A nil
// store keeps the current flat-file behavior.
func NewServer(cfg config.Config, store LabelStore) *Server {
	if store == nil {
		store = New(cfg)
	}
	return &Server{cfg: cfg, store: store, autodetectSignals: detectAutodetectSignals{}}
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
