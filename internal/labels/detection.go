package labels

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
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

	detectionPublicError = "Detection failed. Check the source video and try again."
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

type detectionLogger interface {
	Printf(format string, args ...any)
}

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
	logger     detectionLogger

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
		logger:     log.Default(),
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

	m.finish(key, PendingReviewCount(labelDoc.Candidates), nil)
}

func (m *detectionManager) finish(key detectionJobKey, candidateCount int, err error) {
	now := time.Now().UTC()
	if err != nil {
		m.logger.Printf(
			"detection job failed (operation=%s): %s",
			key.operation,
			redactDetectionDiagnostic(err.Error(), m.cfg, key.show),
		)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	status := m.jobs[key]
	status.FinishedAt = &now
	status.CandidateCount = candidateCount
	if err != nil {
		status.State = detectionFailed
		status.Error = detectionPublicError
	} else {
		status.State = detectionCompleted
		status.Error = ""
	}
	m.jobs[key] = status
}

type diagnosticRedaction struct {
	value       string
	replacement string
}

func redactDetectionDiagnostic(message string, cfg config.Config, show string) string {
	sourceFilename := show + ".mkv"
	sourcePath := filepath.Join(cfg.VideoDir, sourceFilename)

	var redactions []diagnosticRedaction
	redactions = appendPathRedactions(redactions, sourcePath, "[redacted-source]")
	redactions = append(redactions,
		diagnosticRedaction{value: sourceFilename, replacement: "[redacted-source]"},
		diagnosticRedaction{value: show, replacement: "[redacted-source]"},
	)
	for _, root := range []string{cfg.VideoDir, cfg.StateDir, cfg.HLSDir} {
		if safeDiagnosticRoot(root) {
			redactions = appendPathRedactions(redactions, root, "[redacted-path]")
		}
	}
	sort.SliceStable(redactions, func(i, j int) bool {
		return len(redactions[i].value) > len(redactions[j].value)
	})

	seen := make(map[string]struct{}, len(redactions))
	replacements := make([]string, 0, len(redactions)*2)
	for _, redaction := range redactions {
		if redaction.value == "" {
			continue
		}
		if _, ok := seen[redaction.value]; ok {
			continue
		}
		seen[redaction.value] = struct{}{}
		replacements = append(replacements, redaction.value, redaction.replacement)
	}
	return strings.NewReplacer(replacements...).Replace(message)
}

func appendPathRedactions(redactions []diagnosticRedaction, path, replacement string) []diagnosticRedaction {
	slashPath := filepath.ToSlash(path)
	return append(redactions,
		diagnosticRedaction{value: path, replacement: replacement},
		diagnosticRedaction{value: slashPath, replacement: replacement},
		diagnosticRedaction{value: strings.ReplaceAll(slashPath, "/", `\`), replacement: replacement},
	)
}

func safeDiagnosticRoot(path string) bool {
	if path == "" {
		return false
	}
	clean := filepath.Clean(path)
	volumeRoot := filepath.VolumeName(clean) + string(filepath.Separator)
	return filepath.IsAbs(clean) && clean != volumeRoot
}
