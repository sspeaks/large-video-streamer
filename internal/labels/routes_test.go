package labels

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspeaks/large-video-streamer/internal/auth"
	"github.com/sspeaks/large-video-streamer/internal/config"
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

func authenticatedLabelMux(t *testing.T, store *Store) *http.ServeMux {
	t.Helper()
	authn := auth.New(config.Config{LoginUser: "group-a", LoginPass: "group-b", CookieSecret: []byte("01234567890123456789012345678901")})
	mux := http.NewServeMux()
	authn.RegisterRoutes(mux)
	store.RegisterRoutes(mux, authn)
	return mux
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
