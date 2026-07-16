package labels

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	if status.State != detectionRunning {
		t.Fatalf("detection state = %q, want %q", status.State, detectionRunning)
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
}

func TestDetectFailureIsAvailableFromStatusEndpoint(t *testing.T) {
	cfg := config.Config{
		VideoDir: t.TempDir(),
		HLSDir:   t.TempDir(),
		StateDir: t.TempDir(),
		NoAuth:   true,
	}
	srv := NewServer(cfg, New(cfg))
	srv.detections.detect = func(path string, noiseDB float64, minDur float64) ([]detect.Silence, error) {
		return nil, errors.New("ffmpeg exited")
	}
	mux := noAuthLabelMux(srv)

	res := serveNoAuthLabelRequest(mux, http.MethodPost, "/labels/api/sample_video/detect", "")
	if res.Code != http.StatusAccepted {
		t.Fatalf("POST detect status = %d, want %d; body %q", res.Code, http.StatusAccepted, res.Body.String())
	}

	status := waitForDetectionState(t, mux, "sample_video", "detect", detectionFailed)
	if !strings.Contains(status.Error, "ffmpeg exited") {
		t.Fatalf("detection error = %q, want ffmpeg failure", status.Error)
	}
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
