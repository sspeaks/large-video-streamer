package labels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/auth"
	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/detect"
)

func TestDetectRunsAfterStartRequestReturnsAndPersistsCandidates(t *testing.T) {
	cfg := config.Config{
		VideoDir: t.TempDir(),
		HLSDir:   t.TempDir(),
		StateDir: t.TempDir(),
		NoAuth:   true,
	}
	store := New(cfg)
	srv := NewServer(cfg, store)

	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	var releaseOnce sync.Once
	var calls atomic.Int32
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
	})

	srv.detections.detect = func(path string, noiseDB float64, minDur float64) ([]detect.Silence, error) {
		calls.Add(1)
		startOnce.Do(func() { close(started) })
		<-release
		return []detect.Silence{{Time: 42, Duration: 3.5}}, nil
	}
	mux := noAuthLabelMux(srv)

	startResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		startResponse <- serveNoAuthLabelRequest(mux, http.MethodPost, "/labels/api/sample_video/detect", "")
	}()

	var res *httptest.ResponseRecorder
	select {
	case res = <-startResponse:
	case <-time.After(time.Second):
		t.Fatal("POST detect did not return while the detector was still running")
	}
	if res.Code != http.StatusAccepted {
		t.Fatalf("POST detect status = %d, want %d; body %q", res.Code, http.StatusAccepted, res.Body.String())
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background detector did not start")
	}

	status := decodeDetectionStatus(t, serveNoAuthLabelRequest(mux, http.MethodGet, "/labels/api/sample_video/detect", ""))
	if status.State != detectionRunning || status.Operation != string(detectionOperationSilence) {
		t.Fatalf("silence-only status = %#v, want running detect operation", status)
	}

	duplicate := serveNoAuthLabelRequest(mux, http.MethodPost, "/labels/api/sample_video/detect", "")
	if duplicate.Code != http.StatusAccepted {
		t.Fatalf("duplicate POST detect status = %d, want %d", duplicate.Code, http.StatusAccepted)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("detector calls = %d, want 1 while the show already has a running job", got)
	}

	releaseOnce.Do(func() { close(release) })
	status = waitForDetectionState(t, mux, "sample_video", "detect", detectionCompleted)
	if status.CandidateCount != 1 {
		t.Fatalf("candidate count = %d, want 1", status.CandidateCount)
	}

	labelDoc, err := store.Load("sample_video")
	if err != nil {
		t.Fatalf("Load detected labels: %v", err)
	}
	if len(labelDoc.Candidates) != 1 || labelDoc.Candidates[0].Time != 42 || labelDoc.Candidates[0].Status != "candidate" {
		t.Fatalf("persisted candidates = %#v, want candidate at 42s", labelDoc.Candidates)
	}
	candidate := labelDoc.Candidates[0]
	if candidate.SuggestedName != "" || candidate.Confidence != 0 || len(candidate.Sources) != 0 {
		t.Fatalf("silence-only candidate = %#v, want raw candidate without auto-detect metadata", candidate)
	}
}

func TestDetectionFailureStatusAndLogAreSanitized(t *testing.T) {
	const show = "group-01"

	tests := []struct {
		name      string
		operation string
		context   string
		start     func(*Server, *http.ServeMux) *httptest.ResponseRecorder
	}{
		{
			name:      "silence detection",
			operation: "detect",
			context:   "ffmpeg audio detect failed",
			start: func(srv *Server, mux *http.ServeMux) *httptest.ResponseRecorder {
				sourcePath := filepath.Join(srv.cfg.VideoDir, show+".mkv")
				srv.detections.detect = func(path string, noiseDB float64, minDur float64) ([]detect.Silence, error) {
					return nil, fmt.Errorf("ffmpeg audio detect failed: exit status 1: %s: Permission denied", sourcePath)
				}
				return serveNoAuthLabelRequest(mux, http.MethodPost, "/labels/api/"+show+"/detect", "")
			},
		},
		{
			name:      "visual autodetect",
			operation: "autodetect",
			context:   "ffmpeg visual detection failed",
			start: func(srv *Server, mux *http.ServeMux) *httptest.ResponseRecorder {
				sourcePath := filepath.Join(srv.cfg.VideoDir, show+".mkv")
				sourceFilename := show + ".mkv"
				srv.autodetectSignals = &fakeAutodetectSignals{
					sceneErr: fmt.Errorf(
						"ffmpeg visual detection failed: exit status 1: Input #0 from '%s'; %s: Invalid data found for %s",
						sourcePath,
						sourcePath,
						sourceFilename,
					),
				}
				return serveNoAuthLabelRequest(
					mux,
					http.MethodPost,
					"/labels/api/"+show+"/autodetect",
					`{"lineup":[{"name":"group-01"}],"useSilence":false,"useColor":true}`,
				)
			},
		},
		{
			name:      "label persistence",
			operation: "detect",
			context:   "load detected labels",
			start: func(srv *Server, mux *http.ServeMux) *httptest.ResponseRecorder {
				statePath := filepath.Join(srv.cfg.StateDir, "labels", show+".labels.json")
				srv.detections.detect = func(path string, noiseDB float64, minDur float64) ([]detect.Silence, error) {
					return []detect.Silence{{Time: 10, Duration: 2}}, nil
				}
				srv.detections.store = detectionFailureStore{
					loadErr: fmt.Errorf("load detected labels from %s: permission denied", statePath),
				}
				return serveNoAuthLabelRequest(mux, http.MethodPost, "/labels/api/"+show+"/detect", "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				VideoDir: t.TempDir(),
				HLSDir:   t.TempDir(),
				StateDir: t.TempDir(),
				NoAuth:   true,
			}
			srv := NewServer(cfg, New(cfg))
			var logs bytes.Buffer
			srv.detections.logger = log.New(&logs, "", 0)
			mux := noAuthLabelMux(srv)

			res := tt.start(srv, mux)
			if res.Code != http.StatusAccepted {
				t.Fatalf("POST %s status = %d, want %d; body %q", tt.operation, res.Code, http.StatusAccepted, res.Body.String())
			}

			status := waitForDetectionState(t, mux, show, tt.operation, detectionFailed)
			if status.Error != detectionPublicError {
				t.Fatalf("detection error = %q, want %q", status.Error, detectionPublicError)
			}
			sourcePath := filepath.Join(cfg.VideoDir, show+".mkv")
			if strings.Contains(status.Error, sourcePath) || strings.Contains(status.Error, show) || strings.Contains(status.Error, "ffmpeg") {
				t.Fatalf("public detection error contains private diagnostics: %q", status.Error)
			}

			logged := logs.String()
			if count := strings.Count(logged, "detection job failed"); count != 1 {
				t.Fatalf("detection failure log count = %d, want 1; log %q", count, logged)
			}
			if strings.Contains(logged, sourcePath) || strings.Contains(logged, show+".mkv") || strings.Contains(logged, show) {
				t.Fatalf("detection failure log contains source identifier: %q", logged)
			}
			for _, root := range []string{cfg.VideoDir, cfg.StateDir, cfg.HLSDir} {
				if strings.Contains(logged, root) {
					t.Fatalf("detection failure log contains configured path %q: %q", root, logged)
				}
			}
			if !strings.Contains(logged, "[redacted-source]") {
				t.Fatalf("detection failure log = %q, want redaction marker", logged)
			}
			if !strings.Contains(logged, tt.context) {
				t.Fatalf("detection failure log = %q, want context %q", logged, tt.context)
			}
		})
	}
}

type detectionFailureStore struct {
	loadErr error
}

func (s detectionFailureStore) Load(string) (VideoLabels, error) {
	return VideoLabels{}, s.loadErr
}

func (detectionFailureStore) Save(VideoLabels) error {
	return nil
}

func TestAutodetectRunsAfterStartRequestReturnsAndPersistsCandidates(t *testing.T) {
	cfg := config.Config{
		VideoDir: t.TempDir(),
		HLSDir:   t.TempDir(),
		StateDir: t.TempDir(),
		NoAuth:   true,
	}
	store := New(cfg)
	srv := NewServer(cfg, store)

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
	})
	srv.autodetectSignals = &blockingAutodetectSignals{
		fakeAutodetectSignals: &fakeAutodetectSignals{
			silences: []detect.Silence{{Time: 42, Duration: 3.5}},
		},
		started: started,
		release: release,
	}
	mux := noAuthLabelMux(srv)

	res := serveNoAuthLabelRequest(
		mux,
		http.MethodPost,
		"/labels/api/sample_video/autodetect",
		`{"lineup":[{"name":"quartet-a"}]}`,
	)
	if res.Code != http.StatusAccepted {
		t.Fatalf("POST autodetect status = %d, want %d; body %q", res.Code, http.StatusAccepted, res.Body.String())
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background autodetect did not start")
	}

	status := decodeDetectionStatus(t, serveNoAuthLabelRequest(mux, http.MethodGet, "/labels/api/sample_video/autodetect", ""))
	if status.State != detectionRunning || status.Operation != string(detectionOperationAutodetect) {
		t.Fatalf("autodetect status = %#v, want running autodetect", status)
	}

	releaseOnce.Do(func() { close(release) })
	status = waitForDetectionState(t, mux, "sample_video", "autodetect", detectionCompleted)
	if status.CandidateCount != 1 {
		t.Fatalf("candidate count = %d, want 1", status.CandidateCount)
	}

	labelDoc, err := store.Load("sample_video")
	if err != nil {
		t.Fatalf("Load autodetect labels: %v", err)
	}
	if len(labelDoc.Candidates) != 1 || labelDoc.Candidates[0].Time != 42 || labelDoc.Candidates[0].SuggestedName != "quartet-a" {
		t.Fatalf("persisted candidates = %#v, want quartet-a candidate at 42s", labelDoc.Candidates)
	}
	candidate := labelDoc.Candidates[0]
	if candidate.Confidence == 0 || len(candidate.Sources) == 0 {
		t.Fatalf("auto-detect candidate = %#v, want ranked source metadata", candidate)
	}
}

type blockingAutodetectSignals struct {
	*fakeAutodetectSignals
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (f *blockingAutodetectSignals) DetectAudio(path string, noiseDB float64, minDur float64) (detect.AudioSignals, error) {
	f.once.Do(func() { close(f.started) })
	<-f.release
	return f.fakeAutodetectSignals.DetectAudio(path, noiseDB, minDur)
}

func noAuthLabelMux(srv *Server) *http.ServeMux {
	authn := auth.New(config.Config{NoAuth: true})
	mux := http.NewServeMux()
	authn.RegisterRoutes(mux)
	srv.RegisterRoutes(mux, authn)
	return mux
}

func serveNoAuthLabelRequest(mux *http.ServeMux, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

func decodeDetectionStatus(t *testing.T, res *httptest.ResponseRecorder) detectionStatus {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("GET detection status = %d, want %d; body %q", res.Code, http.StatusOK, res.Body.String())
	}
	var status detectionStatus
	if err := json.Unmarshal(res.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode detection status: %v", err)
	}
	return status
}

func waitForDetectionState(t *testing.T, mux *http.ServeMux, show, operation, want string) detectionStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		status := decodeDetectionStatus(t, serveNoAuthLabelRequest(mux, http.MethodGet, "/labels/api/"+show+"/"+operation, ""))
		if status.State == want {
			return status
		}
		if status.State == detectionFailed && want != detectionFailed {
			t.Fatalf("detection failed while waiting for %q: %s", want, status.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("detection state = %q, want %q", status.State, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
