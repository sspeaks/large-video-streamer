package labels

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/detect"
)

const (
	detectionIdle      = "idle"
	detectionRunning   = "running"
	detectionCompleted = "completed"
	detectionFailed    = "failed"

	detectionOperationSilence    detectionOperation = "detect"
	detectionOperationAutodetect detectionOperation = "autodetect"
)

type detectionStatus struct {
	State          string     `json:"state"`
	Operation      string     `json:"operation"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	CandidateCount int        `json:"candidateCount,omitempty"`
	Error          string     `json:"error,omitempty"`
}

type detectionOperation string

type silenceDetector func(path string, noiseDB float64, minDur float64) ([]detect.Silence, error)
type candidateBuilder func() ([]Candidate, error)

type detectionJobKey struct {
	show      string
	operation detectionOperation
}

type detectionManager struct {
	srv        *Server
	cfg        config.Config
	store      LabelStore
	mutationMu *sync.Mutex
	detect     silenceDetector

	mu   sync.Mutex
	jobs map[detectionJobKey]detectionStatus
}

func newDetectionManager(srv *Server, cfg config.Config, store LabelStore, mutationMu *sync.Mutex) *detectionManager {
	return &detectionManager{
		srv:        srv,
		cfg:        cfg,
		store:      store,
		mutationMu: mutationMu,
		detect:     detect.DetectSilence,
		jobs:       make(map[detectionJobKey]detectionStatus),
	}
}

func (m *detectionManager) startSilence(show string) detectionStatus {
	return m.start(show, detectionOperationSilence, func() ([]Candidate, error) {
		silences, err := m.detect(
			filepath.Join(m.cfg.VideoDir, show+".mkv"),
			detect.DefaultNoiseDB,
			detect.DefaultMinDur,
		)
		if err != nil {
			return nil, fmt.Errorf("detect silences: %w", err)
		}

		candidates := make([]Candidate, 0, len(silences))
		for _, silence := range silences {
			candidates = append(candidates, Candidate{
				Time:     silence.Time,
				Duration: silence.Duration,
				Status:   "candidate",
			})
		}
		return candidates, nil
	})
}

func (m *detectionManager) startAutodetect(show string, req autodetectRequest) detectionStatus {
	return m.start(show, detectionOperationAutodetect, func() ([]Candidate, error) {
		candidates, err := m.srv.buildAutodetectCandidates(
			filepath.Join(m.cfg.VideoDir, show+".mkv"),
			req,
		)
		if err != nil {
			return nil, fmt.Errorf("suggest boundaries: %w", err)
		}
		return candidates, nil
	})
}

func (m *detectionManager) start(show string, operation detectionOperation, build candidateBuilder) detectionStatus {
	key := detectionJobKey{show: show, operation: operation}

	m.mu.Lock()
	if status, ok := m.jobs[key]; ok && status.State == detectionRunning {
		m.mu.Unlock()
		return status
	}

	now := time.Now().UTC()
	status := detectionStatus{
		State:     detectionRunning,
		Operation: string(operation),
		StartedAt: &now,
	}
	m.jobs[key] = status
	m.mu.Unlock()

	go m.run(key, build)
	return status
}

func (m *detectionManager) status(show string, operation detectionOperation) detectionStatus {
	key := detectionJobKey{show: show, operation: operation}

	m.mu.Lock()
	defer m.mu.Unlock()

	if status, ok := m.jobs[key]; ok {
		return status
	}
	return detectionStatus{State: detectionIdle, Operation: string(operation)}
}

func (m *detectionManager) run(key detectionJobKey, build candidateBuilder) {
	detected, err := build()
	if err != nil {
		m.finish(key, 0, err)
		return
	}

	m.mutationMu.Lock()
	labelDoc, err := m.store.Load(key.show)
	if err == nil {
		labelDoc.Candidates = mergeCandidatesWithBoundaries(labelDoc.Candidates, detected, labelDoc.Boundaries)
		labelDoc.Video = key.show
		err = m.store.Save(labelDoc)
	}
	m.mutationMu.Unlock()
	if err != nil {
		m.finish(key, 0, fmt.Errorf("save detected candidates: %w", err))
		return
	}

	pending := 0
	for _, candidate := range labelDoc.Candidates {
		if candidate.Status != "named" && candidate.Status != "rejected" {
			pending++
		}
	}
	m.finish(key, pending, nil)
}

func (m *detectionManager) finish(key detectionJobKey, candidateCount int, err error) {
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	status := m.jobs[key]
	status.FinishedAt = &now
	status.CandidateCount = candidateCount
	if err != nil {
		status.State = detectionFailed
		status.Error = err.Error()
	} else {
		status.State = detectionCompleted
		status.Error = ""
	}
	m.jobs[key] = status
}
