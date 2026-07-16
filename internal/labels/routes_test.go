package labels

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/auth"
	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/detect"
)

func TestRoutesSaveWritesSidecarAndChaptersVTT(t *testing.T) {
	videoDir := t.TempDir()
	hlsDir := t.TempDir()
	store := New(config.Config{VideoDir: videoDir, HLSDir: hlsDir, StateDir: t.TempDir()})
	mux := authenticatedLabelMux(t, store)

	body := `{"video":"ignored","boundaries":[{"name":"group-a","start":0},{"name":"group-b","start":90}],"candidates":[{"time":12.5,"duration":1.5,"status":"candidate"}]}`
	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video", body)
	if res.Code != http.StatusNoContent {
		t.Fatalf("POST save status = %d, want %d; body %q", res.Code, http.StatusNoContent, res.Body.String())
	}

	labels, err := store.Load("sample_video")
	if err != nil {
		t.Fatalf("Load saved labels: %v", err)
	}
	if labels.Video != "sample_video" || len(labels.Boundaries) != 2 || labels.Boundaries[0].Name != "group-a" {
		t.Fatalf("saved labels = %#v", labels)
	}
	chaptersPath := filepath.Join(hlsDir, "sample_video", "chapters.vtt")
	chapters, err := os.ReadFile(chaptersPath)
	if err != nil {
		t.Fatalf("read chapters.vtt: %v", err)
	}
	if !strings.Contains(string(chapters), "WEBVTT") || !strings.Contains(string(chapters), "group-b") {
		t.Fatalf("chapters.vtt = %q", chapters)
	}
}

func TestRoutesExportReturnsTimestampText(t *testing.T) {
	store := New(config.Config{VideoDir: t.TempDir(), HLSDir: t.TempDir(), StateDir: t.TempDir()})
	if err := store.Save(VideoLabels{Video: "sample_video", Boundaries: []Boundary{{Name: "group-b", Start: 120}, {Name: "group-a", Start: 60}}}); err != nil {
		t.Fatalf("Save fixture: %v", err)
	}
	mux := authenticatedLabelMux(t, store)

	res := serveLabelRequest(t, mux, http.MethodGet, "/labels/api/sample_video/export", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET export status = %d, want %d; body %q", res.Code, http.StatusOK, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
	want := "> sample_video.mkv\ngroup-a 00:01:00\ngroup-b 00:02:00\n"
	if res.Body.String() != want {
		t.Fatalf("export body = %q, want %q", res.Body.String(), want)
	}
}

func TestRoutesRejectUnsafeShowNames(t *testing.T) {
	store := New(config.Config{VideoDir: t.TempDir(), HLSDir: t.TempDir()})
	mux := authenticatedLabelMux(t, store)

	for _, path := range []string{"/labels/api/sample..video", "/labels/api/" + url.PathEscape("group-a/sample_video")} {
		res := serveLabelRequest(t, mux, http.MethodGet, path, "")
		if res.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want %d; body %q", path, res.Code, http.StatusBadRequest, res.Body.String())
		}
	}
}

func TestRoutesAutodetectStartsBackgroundJobAndSavesCandidatesOnly(t *testing.T) {
	videoDir := t.TempDir()
	hlsDir := t.TempDir()
	store := New(config.Config{VideoDir: videoDir, HLSDir: hlsDir, StateDir: t.TempDir()})
	if err := store.Save(VideoLabels{Video: "sample_video", Boundaries: []Boundary{{Name: "existing", Start: 42}}}); err != nil {
		t.Fatalf("Save fixture: %v", err)
	}
	signals := &fakeAutodetectSignals{
		silences: []detect.Silence{{Time: 10, Duration: 2.5}, {Time: 20, Duration: 2}},
	}
	srv := NewServer(store.cfg, store)
	mux := authenticatedLabelServerMux(t, srv, signals)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video/autodetect", `{"lineup":[{"name":"quartet-a"}]}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("POST autodetect status = %d, want %d; body %q", res.Code, http.StatusAccepted, res.Body.String())
	}
	status := waitForServerDetectionState(t, srv, "sample_video", detectionOperationAutodetect, detectionCompleted)
	if status.CandidateCount != 2 {
		t.Fatalf("candidate count = %d, want 2", status.CandidateCount)
	}

	saved, err := store.Load("sample_video")
	if err != nil {
		t.Fatalf("Load saved labels: %v", err)
	}
	if saved.Video != "sample_video" || len(saved.Boundaries) != 1 || saved.Boundaries[0].Name != "existing" {
		t.Fatalf("labels = %#v, want existing labels preserved", saved)
	}
	if len(saved.Candidates) != 2 {
		t.Fatalf("len(Candidates) = %d, want 2: %#v", len(saved.Candidates), saved.Candidates)
	}
	if saved.Candidates[0].SuggestedName != "quartet-a" || saved.Candidates[1].SuggestedName != "quartet-a-song-2" {
		t.Fatalf("SuggestedName values = %#v, want lineup suggestions", saved.Candidates)
	}
	if got, want := signals.silencePath, filepath.Join(videoDir, "sample_video.mkv"); got != want {
		t.Fatalf("silence path = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(hlsDir, "sample_video", "chapters.vtt")); !os.IsNotExist(err) {
		t.Fatalf("chapters.vtt exists after autodetect or stat failed: %v", err)
	}
	if len(saved.Candidates) != 2 || saved.Candidates[0].SuggestedName != "quartet-a" {
		t.Fatalf("saved candidates = %#v, want autodetect candidates persisted", saved.Candidates)
	}
}

func TestRoutesAutodetectDoesNotRewriteExistingChaptersVTT(t *testing.T) {
	videoDir := t.TempDir()
	hlsDir := t.TempDir()
	store := New(config.Config{VideoDir: videoDir, HLSDir: hlsDir, StateDir: t.TempDir()})
	if err := store.Save(VideoLabels{Video: "sample_video", Boundaries: []Boundary{{Name: "existing", Start: 42}}}); err != nil {
		t.Fatalf("Save fixture: %v", err)
	}
	chaptersDir := filepath.Join(hlsDir, "sample_video")
	if err := os.MkdirAll(chaptersDir, 0o755); err != nil {
		t.Fatalf("create chapters dir: %v", err)
	}
	chaptersPath := filepath.Join(chaptersDir, "chapters.vtt")
	const sentinel = "WEBVTT\n\nsentinel\n"
	if err := os.WriteFile(chaptersPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write sentinel chapters.vtt: %v", err)
	}
	signals := &fakeAutodetectSignals{
		silences: []detect.Silence{{Time: 10, Duration: 2.5}},
	}
	srv := NewServer(store.cfg, store)
	mux := authenticatedLabelServerMux(t, srv, signals)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video/autodetect", `{"lineup":[{"name":"quartet-a"}]}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("POST autodetect status = %d, want %d; body %q", res.Code, http.StatusAccepted, res.Body.String())
	}
	waitForServerDetectionState(t, srv, "sample_video", detectionOperationAutodetect, detectionCompleted)

	chapters, err := os.ReadFile(chaptersPath)
	if err != nil {
		t.Fatalf("read chapters.vtt: %v", err)
	}
	if string(chapters) != sentinel {
		t.Fatalf("chapters.vtt = %q, want existing sentinel unchanged because autodetect only saves candidates", chapters)
	}
}

func TestRoutesAutodetectPersistsBlackFreezeCandidatesOnly(t *testing.T) {
	videoDir := t.TempDir()
	hlsDir := t.TempDir()
	store := New(config.Config{VideoDir: videoDir, HLSDir: hlsDir, StateDir: t.TempDir()})
	if err := store.Save(VideoLabels{Video: "sample_video", Boundaries: []Boundary{{Name: "existing", Start: 5}}}); err != nil {
		t.Fatalf("Save fixture: %v", err)
	}
	chaptersDir := filepath.Join(hlsDir, "sample_video")
	if err := os.MkdirAll(chaptersDir, 0o755); err != nil {
		t.Fatalf("create chapters dir: %v", err)
	}
	chaptersPath := filepath.Join(chaptersDir, "chapters.vtt")
	const sentinel = "WEBVTT\n\nexisting chapter file\n"
	if err := os.WriteFile(chaptersPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write sentinel chapters.vtt: %v", err)
	}
	signals := &fakeAutodetectSignals{
		blackSegments:  []detect.BlackSegment{{Start: 12, End: 13, Duration: 1}},
		freezeSegments: []detect.FreezeSegment{{Start: 39, End: 42, Duration: 3}},
	}
	srv := NewServer(store.cfg, store)
	mux := authenticatedLabelServerMux(t, srv, signals)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video/autodetect", `{"lineup":[{"name":"quartet-a","songCount":2}],"useSilence":false,"useColor":true}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("POST autodetect status = %d, want %d; body %q", res.Code, http.StatusAccepted, res.Body.String())
	}
	waitForServerDetectionState(t, srv, "sample_video", detectionOperationAutodetect, detectionCompleted)

	saved, err := store.Load("sample_video")
	if err != nil {
		t.Fatalf("Load saved labels: %v", err)
	}
	if len(saved.Boundaries) != 1 || saved.Boundaries[0].Name != "existing" {
		t.Fatalf("boundaries = %#v, want existing boundary only", saved.Boundaries)
	}
	if len(saved.Candidates) != 2 {
		t.Fatalf("candidates = %#v, want black and freeze candidates", saved.Candidates)
	}
	if !candidateHasSource(saved.Candidates, autodetectSourceBlack) || !candidateHasSource(saved.Candidates, autodetectSourceFreeze) {
		t.Fatalf("candidates = %#v, want black and freeze source metadata", saved.Candidates)
	}
	for _, candidate := range saved.Candidates {
		if candidate.Status != "candidate" {
			t.Fatalf("candidate = %#v, want status candidate until a reviewer promotes it", candidate)
		}
	}
	chapters, err := os.ReadFile(chaptersPath)
	if err != nil {
		t.Fatalf("read chapters.vtt: %v", err)
	}
	if string(chapters) != sentinel {
		t.Fatalf("chapters.vtt = %q, want existing sentinel unchanged because autodetect only saves candidates", chapters)
	}
	if len(saved.Boundaries) != 1 || len(saved.Candidates) != 2 || !candidateHasSource(saved.Candidates, autodetectSourceBlack) || !candidateHasSource(saved.Candidates, autodetectSourceFreeze) {
		t.Fatalf("saved labels = %#v, want boundary unchanged and black/freeze candidates persisted", saved)
	}
}

func TestRoutesAutodetectRejectsUnsafeShowBeforeSignals(t *testing.T) {
	store := New(config.Config{VideoDir: t.TempDir(), HLSDir: t.TempDir(), StateDir: t.TempDir()})
	signals := &fakeAutodetectSignals{}
	mux := authenticatedLabelServerMux(t, NewServer(store.cfg, store), signals)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample..video/autodetect", `{"lineup":[{"name":"quartet-a"}]}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("POST autodetect status = %d, want %d; body %q", res.Code, http.StatusBadRequest, res.Body.String())
	}
	if signals.silenceCalls != 0 {
		t.Fatalf("silenceCalls = %d, want 0 for rejected show", signals.silenceCalls)
	}
}

func TestRoutesAutodetectReturnsSafeErrorAndLogsOCRDiagnostic(t *testing.T) {
	store := New(config.Config{VideoDir: t.TempDir(), HLSDir: t.TempDir(), StateDir: t.TempDir()})
	signals := &fakeAutodetectSignals{
		silences: []detect.Silence{{Time: 10, Duration: 2}},
		ocrErr:   errors.New("tesseract not found in PATH"),
	}
	srv := NewServer(store.cfg, store)
	var logs bytes.Buffer
	srv.detections.logger = log.New(&logs, "", 0)
	mux := authenticatedLabelServerMux(t, srv, signals)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/group-01/autodetect", `{"lineup":[{"name":"group-01"}],"useOCR":true}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("POST autodetect status = %d, want %d; body %q", res.Code, http.StatusAccepted, res.Body.String())
	}
	waitForServerDetectionState(t, srv, "group-01", detectionOperationAutodetect, detectionFailed)
	status := decodeDetectionStatus(
		t,
		serveLabelRequest(t, mux, http.MethodGet, "/labels/api/group-01/autodetect", ""),
	)
	if status.Error != detectionPublicError {
		t.Fatalf("autodetect error = %q, want %q", status.Error, detectionPublicError)
	}
	if count := strings.Count(logs.String(), "detection job failed"); count != 1 {
		t.Fatalf("detection failure log count = %d, want 1; log %q", count, logs.String())
	}
	if !strings.Contains(logs.String(), "tesseract not found in PATH") {
		t.Fatalf("detection failure log = %q, want OCR diagnostic", logs.String())
	}
}

func TestRoutesRejectBoundaryNamesWithNewlines(t *testing.T) {
	store := New(config.Config{VideoDir: t.TempDir(), HLSDir: t.TempDir()})
	mux := authenticatedLabelMux(t, store)

	body := `{"video":"sample_video","boundaries":[{"name":"group-a\nCHAPTER02=00:00:00.000","start":0}]}`
	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video", body)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("POST save status = %d, want %d; body %q", res.Code, http.StatusBadRequest, res.Body.String())
	}
	if _, err := os.Stat(filepath.Join(store.cfg.HLSDir, "sample_video", "chapters.vtt")); !os.IsNotExist(err) {
		t.Fatalf("chapters.vtt exists after rejected save or stat failed: %v", err)
	}
}

func TestRoutesImportPersistsAndWritesChaptersVTT(t *testing.T) {
	videoDir := t.TempDir()
	hlsDir := t.TempDir()
	store := New(config.Config{VideoDir: videoDir, HLSDir: hlsDir, StateDir: t.TempDir()})
	mux := authenticatedLabelMux(t, store)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video/import", "group-a 00:01:00\ngroup-b 00:02:00\n")
	if res.Code != http.StatusOK {
		t.Fatalf("POST import status = %d, want %d; body %q", res.Code, http.StatusOK, res.Body.String())
	}
	var labels VideoLabels
	if err := json.Unmarshal(res.Body.Bytes(), &labels); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if labels.Video != "sample_video" || len(labels.Boundaries) != 2 {
		t.Fatalf("imported labels = %#v", labels)
	}
	if _, err := os.Stat(filepath.Join(hlsDir, "sample_video", "chapters.vtt")); err != nil {
		t.Fatalf("stat chapters.vtt: %v", err)
	}
}

func TestRoutesImportExportImportRoundTripWithSpaces(t *testing.T) {
	videoDir, hlsDir, stateDir := t.TempDir(), t.TempDir(), t.TempDir()
	store := New(config.Config{VideoDir: videoDir, HLSDir: hlsDir, StateDir: stateDir})
	mux := authenticatedLabelMux(t, store)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/demo/import", "Quartet Finals 00:07:43\ngroup-b 00:20:00\n")
	if res.Code != http.StatusOK {
		t.Fatalf("POST import status = %d, want %d; body %q", res.Code, http.StatusOK, res.Body.String())
	}
	var labels VideoLabels
	if err := json.Unmarshal(res.Body.Bytes(), &labels); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	assertHasBoundary(t, labels, Boundary{Name: "Quartet Finals", Start: 463})
	assertHasBoundary(t, labels, Boundary{Name: "group-b", Start: 1200})

	if _, err := os.Stat(filepath.Join(stateDir, "labels", "demo.labels.json")); err != nil {
		t.Fatalf("stat state sidecar: %v", err)
	}
	if _, err := os.Stat(filepath.Join(videoDir, "demo.labels.json")); !os.IsNotExist(err) {
		t.Fatalf("video dir sidecar exists or stat failed with unexpected error: %v", err)
	}

	res = serveLabelRequest(t, mux, http.MethodGet, "/labels/api/demo/export", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET export status = %d, want %d; body %q", res.Code, http.StatusOK, res.Body.String())
	}
	exported := res.Body.String()
	if !strings.Contains(exported, "Quartet Finals 00:07:43") {
		t.Fatalf("exported text = %q, want Quartet Finals timestamp", exported)
	}

	res = serveLabelRequest(t, mux, http.MethodPost, "/labels/api/demo/import", exported)
	if res.Code != http.StatusOK {
		t.Fatalf("POST re-import status = %d, want %d; body %q", res.Code, http.StatusOK, res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &labels); err != nil {
		t.Fatalf("decode re-import response: %v", err)
	}
	assertHasBoundary(t, labels, Boundary{Name: "Quartet Finals", Start: 463})
	assertHasBoundary(t, labels, Boundary{Name: "group-b", Start: 1200})
}

func assertHasBoundary(t *testing.T, labels VideoLabels, want Boundary) {
	t.Helper()
	for _, got := range labels.Boundaries {
		if got == want {
			return
		}
	}
	t.Fatalf("Boundaries = %#v, want to contain %#v", labels.Boundaries, want)
}

func TestRoutesImportPreservesPersistedLineupFlatFile(t *testing.T) {
	cfg := config.Config{VideoDir: t.TempDir(), HLSDir: t.TempDir(), StateDir: t.TempDir()}
	store := New(cfg)
	if err := store.Save(VideoLabels{
		Video:  "demo",
		Lineup: []string{"group-01", "group-02"},
	}); err != nil {
		t.Fatalf("Save fixture: %v", err)
	}
	mux := authenticatedLabelMux(t, store)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/demo/import", "chapter-a 00:01:00\nchapter-b 00:02:00\n")
	if res.Code != http.StatusOK {
		t.Fatalf("POST import status = %d, want %d; body %q", res.Code, http.StatusOK, res.Body.String())
	}
	var importedLabels VideoLabels
	if err := json.Unmarshal(res.Body.Bytes(), &importedLabels); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if len(importedLabels.Lineup) != 2 || importedLabels.Lineup[0] != "group-01" || importedLabels.Lineup[1] != "group-02" {
		t.Fatalf("import response Lineup = %v, want [group-01 group-02]", importedLabels.Lineup)
	}

	// Simulate a reload: fresh GET without any additional save.
	res = serveLabelRequest(t, mux, http.MethodGet, "/labels/api/demo", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET labels status = %d, want %d; body %q", res.Code, http.StatusOK, res.Body.String())
	}
	var reloadedLabels VideoLabels
	if err := json.Unmarshal(res.Body.Bytes(), &reloadedLabels); err != nil {
		t.Fatalf("decode reload response: %v", err)
	}
	if len(reloadedLabels.Lineup) != 2 || reloadedLabels.Lineup[0] != "group-01" || reloadedLabels.Lineup[1] != "group-02" {
		t.Fatalf("reloaded Lineup = %v, want [group-01 group-02] — lineup was erased by import", reloadedLabels.Lineup)
	}
	if len(reloadedLabels.Boundaries) != 2 {
		t.Fatalf("reloaded Boundaries = %v, want 2 boundaries from import", reloadedLabels.Boundaries)
	}
}

func TestRoutesImportWithNoExistingLineupPersistsNilLineup(t *testing.T) {
	cfg := config.Config{VideoDir: t.TempDir(), HLSDir: t.TempDir(), StateDir: t.TempDir()}
	store := New(cfg)
	mux := authenticatedLabelMux(t, store)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/demo/import", "chapter-a 00:01:00\n")
	if res.Code != http.StatusOK {
		t.Fatalf("POST import status = %d, want %d; body %q", res.Code, http.StatusOK, res.Body.String())
	}
	reloaded, err := store.Load("demo")
	if err != nil {
		t.Fatalf("Load after import: %v", err)
	}
	if len(reloaded.Lineup) != 0 {
		t.Fatalf("Lineup with no prior lineup = %v, want empty", reloaded.Lineup)
	}
}

func TestSafeHTTPErrorLabelsImportLoadFailure(t *testing.T) {
	videoDir := t.TempDir()
	sentinel := videoDir
	store := &errLabelStore{loadErr: errors.New("disk error at " + sentinel)}
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, logs := serverWithTestLogger(t, srv)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/demo/import", "chapter-a 00:01:00\n")

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("POST import (load failure) status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	got := strings.TrimSpace(res.Body.String())
	if got != errPublicLoadFailed {
		t.Fatalf("body = %q, want %q", got, errPublicLoadFailed)
	}
	if strings.Contains(got, sentinel) {
		t.Fatalf("response body exposes sentinel path %q", sentinel)
	}
	if logs.Len() == 0 {
		t.Fatal("expected server-side log entry; got none")
	}
}

func candidateHasSource(candidates []Candidate, source string) bool {
	for _, candidate := range candidates {
		for _, got := range candidate.Sources {
			if got == source {
				return true
			}
		}
	}
	return false
}

func waitForServerDetectionState(t *testing.T, srv *Server, show string, operation detectionOperation, want string) detectionStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		status := srv.detections.status(show, operation)
		if status.State == want {
			return status
		}
		if status.State == detectionFailed && want != detectionFailed {
			t.Fatalf("%s failed while waiting for %q: %s", operation, want, status.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s state = %q, want %q", operation, status.State, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func authenticatedLabelMux(t *testing.T, store *Store) *http.ServeMux {
	t.Helper()
	authn := auth.New(config.Config{LoginUser: "group-a", LoginPass: "group-b", CookieSecret: []byte("01234567890123456789012345678901")})
	mux := http.NewServeMux()
	authn.RegisterRoutes(mux)
	store.RegisterRoutes(mux, authn)
	return mux
}

func authenticatedLabelServerMux(t *testing.T, srv *Server, signals *fakeAutodetectSignals) *http.ServeMux {
	t.Helper()
	srv.autodetectSignals = signals
	authn := auth.New(config.Config{LoginUser: "group-a", LoginPass: "group-b", CookieSecret: []byte("01234567890123456789012345678901")})
	mux := http.NewServeMux()
	authn.RegisterRoutes(mux)
	srv.RegisterRoutes(mux, authn)
	return mux
}

type fakeAutodetectSignals struct {
	silences       []detect.Silence
	audio          detect.AudioSignals
	silenceErr     error
	silencePath    string
	silenceCalls   int
	scenes         []detect.SceneChange
	sceneErr       error
	sceneWindows   []autodetectSignalWindow
	visualWindows  []autodetectSignalWindow
	blackSegments  []detect.BlackSegment
	blackErr       error
	blackCalls     int
	blackWindows   []autodetectSignalWindow
	freezeSegments []detect.FreezeSegment
	freezeErr      error
	freezeCalls    int
	freezeWindows  []autodetectSignalWindow
	colorSamples   []detect.ColorSample
	colorErr       error
	colorWindows   []autodetectSignalWindow
	ocrResults     map[float64]detect.OCRResult
	ocrErr         error
}

type autodetectSignalWindow struct {
	start    float64
	duration float64
}

func (f *fakeAutodetectSignals) DetectSilence(path string, noiseDB float64, minDur float64) ([]detect.Silence, error) {
	f.silenceCalls++
	f.silencePath = path
	if f.silenceErr != nil {
		return nil, f.silenceErr
	}
	return f.silences, nil
}

func (f *fakeAutodetectSignals) DetectAudio(path string, noiseDB float64, minDur float64) (detect.AudioSignals, error) {
	f.silenceCalls++
	f.silencePath = path
	if f.silenceErr != nil {
		return detect.AudioSignals{}, f.silenceErr
	}
	if len(f.audio.Silences) != 0 || len(f.audio.LoudnessSamples) != 0 || len(f.audio.LoudnessOnsets) != 0 {
		return f.audio, nil
	}
	return detect.AudioSignals{Silences: f.silences}, nil
}

func (f *fakeAutodetectSignals) DetectVisual(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64) (detect.VisualSignals, error) {
	if f.sceneErr != nil {
		return detect.VisualSignals{}, f.sceneErr
	}
	if f.blackErr != nil {
		return detect.VisualSignals{}, f.blackErr
	}
	if f.freezeErr != nil {
		return detect.VisualSignals{}, f.freezeErr
	}
	if f.colorErr != nil {
		return detect.VisualSignals{}, f.colorErr
	}
	return detect.VisualSignals{Scenes: f.scenes, ColorSamples: f.colorSamples, BlackSegments: f.blackSegments, FreezeSegments: f.freezeSegments}, nil
}

func (f *fakeAutodetectSignals) DetectVisualWindow(path string, sceneThreshold float64, sceneColorSampleRate float64, blackMinDuration float64, freezeMinDuration float64, start float64, duration float64) (detect.VisualSignals, error) {
	f.visualWindows = append(f.visualWindows, autodetectSignalWindow{start: start, duration: duration})
	return f.DetectVisual(path, sceneThreshold, sceneColorSampleRate, blackMinDuration, freezeMinDuration)
}

func (f *fakeAutodetectSignals) DetectSceneChanges(path string, threshold float64) ([]detect.SceneChange, error) {
	if f.sceneErr != nil {
		return nil, f.sceneErr
	}
	return f.scenes, nil
}

func (f *fakeAutodetectSignals) DetectSceneChangesWindow(path string, threshold float64, sampleRate float64, start float64, duration float64) ([]detect.SceneChange, error) {
	f.sceneWindows = append(f.sceneWindows, autodetectSignalWindow{start: start, duration: duration})
	if f.sceneErr != nil {
		return nil, f.sceneErr
	}
	return f.scenes, nil
}

func (f *fakeAutodetectSignals) DetectBlackSegments(path string, minDuration float64) ([]detect.BlackSegment, error) {
	f.blackCalls++
	if f.blackErr != nil {
		return nil, f.blackErr
	}
	return f.blackSegments, nil
}

func (f *fakeAutodetectSignals) DetectBlackSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.BlackSegment, error) {
	f.blackWindows = append(f.blackWindows, autodetectSignalWindow{start: start, duration: duration})
	if f.blackErr != nil {
		return nil, f.blackErr
	}
	return f.blackSegments, nil
}

func (f *fakeAutodetectSignals) DetectFreezeSegments(path string, minDuration float64) ([]detect.FreezeSegment, error) {
	f.freezeCalls++
	if f.freezeErr != nil {
		return nil, f.freezeErr
	}
	return f.freezeSegments, nil
}

func (f *fakeAutodetectSignals) DetectFreezeSegmentsWindow(path string, minDuration float64, start float64, duration float64) ([]detect.FreezeSegment, error) {
	f.freezeWindows = append(f.freezeWindows, autodetectSignalWindow{start: start, duration: duration})
	if f.freezeErr != nil {
		return nil, f.freezeErr
	}
	return f.freezeSegments, nil
}

func (f *fakeAutodetectSignals) SampleFrameColors(path string, sampleRate float64, crop string) ([]detect.ColorSample, error) {
	if f.colorErr != nil {
		return nil, f.colorErr
	}
	return f.colorSamples, nil
}

func (f *fakeAutodetectSignals) SampleFrameColorsWindow(path string, sampleRate float64, crop string, start float64, duration float64) ([]detect.ColorSample, error) {
	f.colorWindows = append(f.colorWindows, autodetectSignalWindow{start: start, duration: duration})
	if f.colorErr != nil {
		return nil, f.colorErr
	}
	return f.colorSamples, nil
}

func (f *fakeAutodetectSignals) OCRLowerThird(path string, timestamp float64, options detect.OCROptions) (detect.OCRResult, error) {
	if f.ocrErr != nil {
		return detect.OCRResult{}, f.ocrErr
	}
	if f.ocrResults != nil {
		if result, ok := f.ocrResults[timestamp]; ok {
			return result, nil
		}
	}
	return detect.OCRResult{}, nil
}

func serveLabelRequest(t *testing.T, mux *http.ServeMux, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	loginBody := strings.NewReader(url.Values{"user": {"group-a"}, "pass": {"group-b"}}.Encode())
	loginReq := httptest.NewRequest(http.MethodPost, "/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRes := httptest.NewRecorder()
	mux.ServeHTTP(loginRes, loginReq)
	cookies := loginRes.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login did not set session cookie; status %d body %q", loginRes.Code, loginRes.Body.String())
	}

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

// errLabelStore is a test double for LabelStore that returns configurable errors.
type errLabelStore struct {
	loadResult VideoLabels
	loadErr    error
	saveErr    error
}

func (s *errLabelStore) Load(_ string) (VideoLabels, error) {
	return s.loadResult, s.loadErr
}

func (s *errLabelStore) Save(_ VideoLabels) error {
	return s.saveErr
}

// serverWithTestLogger wires an authenticated mux around srv, captures log
// output into the returned buffer, and registers a fake autodetect signals stub
// so the server is fully ready without real detection tools.
func serverWithTestLogger(t *testing.T, srv *Server) (*http.ServeMux, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	srv.logger = log.New(&logs, "", 0)
	srv.autodetectSignals = &fakeAutodetectSignals{}
	authn := auth.New(config.Config{LoginUser: "group-a", LoginPass: "group-b", CookieSecret: []byte("01234567890123456789012345678901")})
	mux := http.NewServeMux()
	authn.RegisterRoutes(mux)
	srv.RegisterRoutes(mux, authn)
	return mux, &logs
}

func TestSafeHTTPErrorLabelsGetStoreFailure(t *testing.T) {
	videoDir := t.TempDir()
	sentinel := videoDir
	store := &errLabelStore{loadErr: errors.New("disk error at " + sentinel)}
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, logs := serverWithTestLogger(t, srv)

	res := serveLabelRequest(t, mux, http.MethodGet, "/labels/api/sample_video", "")

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("GET labels status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	body := strings.TrimSpace(res.Body.String())
	if body != errPublicLoadFailed {
		t.Fatalf("body = %q, want %q", body, errPublicLoadFailed)
	}
	if strings.Contains(body, sentinel) {
		t.Fatalf("response body exposes sentinel path %q", sentinel)
	}
	if logs.Len() == 0 {
		t.Fatal("expected server-side log entry for the failure; got none")
	}
}

func TestSafeHTTPErrorLabelsPostSaveFailure(t *testing.T) {
	videoDir := t.TempDir()
	sentinel := videoDir
	store := &errLabelStore{saveErr: errors.New("write error at " + sentinel)}
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, logs := serverWithTestLogger(t, srv)

	body := `{"video":"sample_video","boundaries":[],"candidates":[]}`
	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video", body)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("POST labels status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	got := strings.TrimSpace(res.Body.String())
	if got != errPublicSaveFailed {
		t.Fatalf("body = %q, want %q", got, errPublicSaveFailed)
	}
	if strings.Contains(got, sentinel) {
		t.Fatalf("response body exposes sentinel path %q", sentinel)
	}
	if logs.Len() == 0 {
		t.Fatal("expected server-side log entry; got none")
	}
}

func TestSafeHTTPErrorLabelsImportSaveFailure(t *testing.T) {
	videoDir := t.TempDir()
	sentinel := videoDir
	store := &errLabelStore{saveErr: errors.New("write error at " + sentinel)}
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, logs := serverWithTestLogger(t, srv)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video/import", "group-a 00:01:00\n")

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("POST import status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	got := strings.TrimSpace(res.Body.String())
	if got != errPublicSaveFailed {
		t.Fatalf("body = %q, want %q", got, errPublicSaveFailed)
	}
	if strings.Contains(got, sentinel) {
		t.Fatalf("response body exposes sentinel path %q", sentinel)
	}
	if logs.Len() == 0 {
		t.Fatal("expected server-side log entry; got none")
	}
}

func TestSafeHTTPErrorLabelsExportStoreFailure(t *testing.T) {
	videoDir := t.TempDir()
	sentinel := videoDir
	store := &errLabelStore{loadErr: errors.New("disk error at " + sentinel)}
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, logs := serverWithTestLogger(t, srv)

	res := serveLabelRequest(t, mux, http.MethodGet, "/labels/api/sample_video/export", "")

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("GET export status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	got := strings.TrimSpace(res.Body.String())
	if got != errPublicLoadFailed {
		t.Fatalf("body = %q, want %q", got, errPublicLoadFailed)
	}
	if strings.Contains(got, sentinel) {
		t.Fatalf("response body exposes sentinel path %q", sentinel)
	}
	if logs.Len() == 0 {
		t.Fatal("expected server-side log entry; got none")
	}
}

func TestSafeHTTPErrorMKVImportLoadFailure(t *testing.T) {
	videoDir := t.TempDir()
	sentinel := videoDir
	store := &errLabelStore{loadErr: errors.New("disk error at " + sentinel)}
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, logs := serverWithTestLogger(t, srv)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video/mkv/import", "")

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("POST mkv/import (load) status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	got := strings.TrimSpace(res.Body.String())
	if got != errPublicLoadFailed {
		t.Fatalf("body = %q, want %q", got, errPublicLoadFailed)
	}
	if strings.Contains(got, sentinel) {
		t.Fatalf("response body exposes sentinel path %q", sentinel)
	}
	if logs.Len() == 0 {
		t.Fatal("expected server-side log entry; got none")
	}
}

func TestSafeHTTPErrorMKVImportToolFailure(t *testing.T) {
	// importMKVChapters calls ffprobe; it will fail (tool absent or file missing).
	// Regardless of the internal error, the response must be the bounded public message
	// and must not expose the VideoDir path.
	videoDir := t.TempDir()
	store := &errLabelStore{} // Load succeeds, returns empty labels
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, _ := serverWithTestLogger(t, srv)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video/mkv/import", "")

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("POST mkv/import (tool) status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	got := strings.TrimSpace(res.Body.String())
	if got != errPublicMKVImport {
		t.Fatalf("body = %q, want %q", got, errPublicMKVImport)
	}
	if strings.Contains(got, videoDir) {
		t.Fatalf("response body exposes VideoDir path %q", videoDir)
	}
}

func TestSafeHTTPErrorMKVEmbedLoadFailure(t *testing.T) {
	videoDir := t.TempDir()
	sentinel := videoDir
	store := &errLabelStore{loadErr: errors.New("disk error at " + sentinel)}
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, logs := serverWithTestLogger(t, srv)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video/mkv/embed", "")

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("POST mkv/embed (load) status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	got := strings.TrimSpace(res.Body.String())
	if got != errPublicLoadFailed {
		t.Fatalf("body = %q, want %q", got, errPublicLoadFailed)
	}
	if strings.Contains(got, sentinel) {
		t.Fatalf("response body exposes sentinel path %q", sentinel)
	}
	if logs.Len() == 0 {
		t.Fatal("expected server-side log entry; got none")
	}
}

func TestSafeHTTPErrorMKVEmbedToolFailure(t *testing.T) {
	// exportMKVChapters calls mkvpropedit; it will fail (tool absent or file missing).
	// Regardless of the internal error, the response must be the bounded public message
	// and must not expose the VideoDir path.
	videoDir := t.TempDir()
	store := &errLabelStore{} // Load succeeds, returns empty labels
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, _ := serverWithTestLogger(t, srv)

	res := serveLabelRequest(t, mux, http.MethodPost, "/labels/api/sample_video/mkv/embed", "")

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("POST mkv/embed (tool) status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	got := strings.TrimSpace(res.Body.String())
	if got != errPublicMKVEmbed {
		t.Fatalf("body = %q, want %q", got, errPublicMKVEmbed)
	}
	if strings.Contains(got, videoDir) {
		t.Fatalf("response body exposes VideoDir path %q", videoDir)
	}
}

func TestSafeHTTPErrorLogsUsefulDiagnostics(t *testing.T) {
	// Verifies that safeHTTPError writes useful (redacted) diagnostics to the
	// server log while keeping the response bounded.
	videoDir := t.TempDir()
	internalMsg := "storage failure: " + videoDir + "/sample_video.labels.json"
	store := &errLabelStore{loadErr: errors.New(internalMsg)}
	srv := NewServer(config.Config{VideoDir: videoDir, StateDir: t.TempDir(), HLSDir: t.TempDir()}, store)
	mux, logs := serverWithTestLogger(t, srv)

	res := serveLabelRequest(t, mux, http.MethodGet, "/labels/api/sample_video", "")

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	// Response body must be the bounded public message.
	got := strings.TrimSpace(res.Body.String())
	if got != errPublicLoadFailed {
		t.Fatalf("body = %q, want bounded public message %q", got, errPublicLoadFailed)
	}
	// Internal path must not reach the client.
	if strings.Contains(got, videoDir) {
		t.Fatalf("response body exposes internal path %q", videoDir)
	}
	// Server log must contain a diagnostic entry for the failure.
	logOut := logs.String()
	if !strings.Contains(logOut, "label api error") {
		t.Fatalf("server log = %q, want it to contain \"label api error\"", logOut)
	}
	// The internal path should be redacted in the log.
	if strings.Contains(logOut, videoDir) {
		t.Fatalf("server log exposes unredacted path %q", videoDir)
	}
}

