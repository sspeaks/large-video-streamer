package share

import (
	"encoding/json"
	"fmt"
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
)

const testPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXTINF:6.000,
seg_0000.ts
#EXTINF:6.000,
seg_0001.ts
#EXTINF:6.000,
seg_0002.ts
#EXTINF:6.000,
seg_0003.ts
#EXTINF:6.000,
seg_0004.ts
#EXT-X-ENDLIST
`

// testConfig builds a config with auth enabled and unique temp state/hls/video
// dirs. The share server's own hls/labels helpers read from these same dirs.
func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		StateDir:     t.TempDir(),
		HLSDir:       t.TempDir(),
		VideoDir:     t.TempDir(),
		LoginUser:    "owner",
		LoginPass:    "pw",
		CookieSecret: []byte("01234567890123456789012345678901"),
	}
}

func newShareMux(t *testing.T, cfg config.Config) (*Server, *http.ServeMux) {
	t.Helper()
	srv := New(cfg)
	a := auth.New(cfg)
	mux := http.NewServeMux()
	a.RegisterRoutes(mux)
	srv.RegisterRoutes(mux, a)
	return srv, mux
}

func flatShareStore(t *testing.T, srv *Server) *Store {
	t.Helper()
	store, ok := srv.store.(*Store)
	if !ok {
		t.Fatalf("share server store is %T, want *Store", srv.store)
	}
	return store
}

// writeHLSShow writes a 5×6s VOD playlist and its segment files under HLSDir.
func writeHLSShow(t *testing.T, cfg config.Config, show string) {
	t.Helper()
	dir := filepath.Join(cfg.HLSDir, show)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "playlist.m3u8"), []byte(testPlaylist), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("seg_%04d.ts", i)), []byte("segdata"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeLabels(t *testing.T, cfg config.Config, show, json string) {
	t.Helper()
	dir := filepath.Join(cfg.StateDir, "labels")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, show+".labels.json"), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ownerCookies(t *testing.T, mux *http.ServeMux) []*http.Cookie {
	t.Helper()
	body := strings.NewReader(url.Values{"user": {"owner"}, "pass": {"pw"}}.Encode())
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login set no cookie: status %d body %q", rec.Code, rec.Body.String())
	}
	return cookies
}

func do(t *testing.T, mux *http.ServeMux, method, target, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var r *strings.Reader
	if body == "" {
		r = strings.NewReader("")
	} else {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func shareCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == shareCookieName {
			return c
		}
	}
	t.Fatalf("response set no %s cookie (status %d)", shareCookieName, rec.Code)
	return nil
}

func hasShareCookie(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == shareCookieName {
			return true
		}
	}
	return false
}

// --- owner create ---------------------------------------------------------

func TestCreateRequiresAuth(t *testing.T) {
	cfg := testConfig(t)
	_, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	writeLabels(t, cfg, "demo", `{"video":"demo","boundaries":[{"name":"intro","start":0},{"name":"two","start":12}]}`)

	rec := do(t, mux, http.MethodPost, "/shares", `{"show":"demo","boundaryIndex":1,"start":12,"name":"two","mode":"single"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d, want 401", rec.Code)
	}
}

func TestCreateAuthenticatedReturnsToken(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	writeLabels(t, cfg, "demo", `{"video":"demo","boundaries":[{"name":"intro","start":0},{"name":"two","start":12}]}`)
	cookies := ownerCookies(t, mux)

	rec := do(t, mux, http.MethodPost, "/shares", `{"show":"demo","boundaryIndex":1,"start":12,"name":"two","mode":"single"}`, cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body %q", rec.Code, rec.Body.String())
	}
	var resp createResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" || resp.Path != "/s/"+resp.Token {
		t.Fatalf("unexpected response %#v", resp)
	}
	sh, ok := srv.store.Get(resp.Token)
	if !ok {
		t.Fatal("created share not found in store")
	}
	// "two" is the last boundary (start 12), so End is the total duration (30).
	if sh.Mode != ModeSingle || sh.Start != 12 || sh.End != 30 || sh.ChapterName != "two" {
		t.Fatalf("stored share = %#v", sh)
	}
	if len(sh.Segments) != 3 { // seg_0002, seg_0003, seg_0004 overlap [12,30)
		t.Fatalf("segments = %v, want 3", sh.Segments)
	}
}

// --- owner management ------------------------------------------------------

func TestAdminSharesRequireAuth(t *testing.T) {
	cfg := testConfig(t)
	_, mux := newShareMux(t, cfg)
	hash := strings.Repeat("a", 64)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/admin/shares"},
		{http.MethodPost, "/admin/shares/" + hash + "/revoke"},
		{http.MethodPost, "/admin/shares/" + hash + "/delete"},
	} {
		rec := do(t, mux, tc.method, tc.path, "")
		if rec.Code != http.StatusFound {
			t.Fatalf("%s %s status = %d, want 302", tc.method, tc.path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Fatalf("%s %s redirect = %q, want /login", tc.method, tc.path, loc)
		}
	}
}

func TestAdminSharesListTimestampsAndStatus(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	cookies := ownerCookies(t, mux)

	base := time.Date(2026, 7, 7, 12, 30, 0, 0, time.UTC)
	claimed := base.Add(time.Hour)
	expiredAt := base.Add(-time.Hour)
	activeExpiresAt := base.Add(24 * time.Hour)
	revokedAt := base.Add(2 * time.Hour)

	activeToken, err := srv.store.Create(CreateParams{
		Show: "demo", ChapterName: "Active Chapter", Mode: ModeSingle, ExpiresAt: &activeExpiresAt,
	})
	if err != nil {
		t.Fatalf("create active share: %v", err)
	}
	expiredToken, err := srv.store.Create(CreateParams{
		Show: "demo", ChapterName: "Expired Chapter", Mode: ModePublic, ExpiresAt: &expiredAt,
	})
	if err != nil {
		t.Fatalf("create expired share: %v", err)
	}
	revokedToken, err := srv.store.Create(CreateParams{
		Show: "demo", ChapterName: "Revoked Chapter", Mode: ModePublic,
	})
	if err != nil {
		t.Fatalf("create revoked share: %v", err)
	}

	store := flatShareStore(t, srv)
	store.mu.Lock()
	store.byHash[sha256hex(activeToken)].CreatedAt = base
	store.byHash[sha256hex(activeToken)].ClaimedAt = &claimed
	store.byHash[sha256hex(expiredToken)].CreatedAt = base.Add(time.Minute)
	store.byHash[sha256hex(revokedToken)].CreatedAt = base.Add(2 * time.Minute)
	store.byHash[sha256hex(revokedToken)].RevokedAt = &revokedAt
	store.mu.Unlock()

	rec := do(t, mux, http.MethodGet, "/admin/shares", "", cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin shares status = %d, want 200; body %q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Active Chapter", "Expired Chapter", "Revoked Chapter",
		"demo", "single", "public",
		"2026-07-07 12:30 UTC", "2026-07-07 13:30 UTC", "2026-07-07 11:30 UTC", "2026-07-08 12:30 UTC",
		"Claimed/device registered", "Active", "Expired", "Revoked", "Revoke", "Delete",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin page missing %q in body: %q", want, body)
		}
	}
}

func TestAdminShareMutationsRevokeDeleteAndPRG(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	cookies := ownerCookies(t, mux)

	revokeToken := newPublicShare(t, srv, "demo")
	if rec := do(t, mux, http.MethodGet, "/s/"+revokeToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("pre-revoke viewer status = %d, want 200", rec.Code)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+revokeToken+"/playlist.m3u8", ""); rec.Code != http.StatusOK {
		t.Fatalf("pre-revoke playlist status = %d, want 200", rec.Code)
	}
	rec := do(t, mux, http.MethodPost, "/admin/shares/"+sha256hex(revokeToken)+"/revoke", "", cookies...)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("revoke status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/shares" {
		t.Fatalf("revoke redirect = %q, want /admin/shares", loc)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+revokeToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("revoked viewer status = %d, want 404", rec.Code)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+revokeToken+"/playlist.m3u8", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("revoked playlist status = %d, want 404", rec.Code)
	}

	deleteToken := newPublicShare(t, srv, "demo")
	if rec := do(t, mux, http.MethodGet, "/s/"+deleteToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("pre-delete viewer status = %d, want 200", rec.Code)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+deleteToken+"/playlist.m3u8", ""); rec.Code != http.StatusOK {
		t.Fatalf("pre-delete playlist status = %d, want 200", rec.Code)
	}
	rec = do(t, mux, http.MethodPost, "/admin/shares/"+sha256hex(deleteToken)+"/delete", "", cookies...)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/shares" {
		t.Fatalf("delete redirect = %q, want /admin/shares", loc)
	}
	if _, ok := srv.store.Get(deleteToken); ok {
		t.Fatal("deleted share still present in store")
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+deleteToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted viewer status = %d, want 404", rec.Code)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+deleteToken+"/playlist.m3u8", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted playlist status = %d, want 404", rec.Code)
	}
}

func TestAdminSharesOmitSensitiveFields(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	cookies := ownerCookies(t, mux)

	token, err := srv.store.Create(CreateParams{
		Show:        "demo",
		ChapterName: "Sensitive Chapter",
		Mode:        ModeSingle,
		Segments:    []string{"secret-segment.ts"},
		Playlist:    "secret-playlist-text",
	})
	if err != nil {
		t.Fatalf("create sensitive share: %v", err)
	}
	if !srv.store.Claim(token, "device-secret-value") {
		t.Fatal("claim sensitive share failed")
	}
	sh, ok := srv.store.Get(token)
	if !ok {
		t.Fatal("claimed share not found")
	}

	rec := do(t, mux, http.MethodGet, "/admin/shares", "", cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin shares status = %d, want 200; body %q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Sensitive Chapter") {
		t.Fatalf("admin page missing share row: %q", body)
	}
	for _, forbidden := range []string{
		token,
		sh.DeviceHash,
		"device-secret-value",
		"secret-segment.ts",
		"secret-playlist-text",
		"DeviceHash",
		"Segments",
		"Playlist",
	} {
		if forbidden != "" && strings.Contains(body, forbidden) {
			t.Fatalf("admin page exposed sensitive value %q in body: %q", forbidden, body)
		}
	}
}

func TestCreatePublicWithExpiry(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	writeLabels(t, cfg, "demo", `{"video":"demo","boundaries":[{"name":"intro","start":0},{"name":"two","start":12}]}`)
	cookies := ownerCookies(t, mux)

	exp := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{"show":"demo","boundaryIndex":0,"start":0,"name":"intro","mode":"public","expiresAt":%q}`, exp)
	rec := do(t, mux, http.MethodPost, "/shares", body, cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body %q", rec.Code, rec.Body.String())
	}
	var resp createResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	sh, _ := srv.store.Get(resp.Token)
	if sh.Mode != ModePublic || sh.ExpiresAt == nil {
		t.Fatalf("stored share = %#v, want public with expiry", sh)
	}
	// intro is the first of two boundaries; End is the next boundary (12).
	if sh.End != 12 {
		t.Fatalf("End = %v, want 12", sh.End)
	}
}

func TestCreateNotSegmentedReturns409(t *testing.T) {
	cfg := testConfig(t)
	_, mux := newShareMux(t, cfg)
	// Labels exist but the show has never been segmented (no playlist.m3u8).
	writeLabels(t, cfg, "demo", `{"video":"demo","boundaries":[{"name":"intro","start":0}]}`)
	cookies := ownerCookies(t, mux)

	rec := do(t, mux, http.MethodPost, "/shares", `{"show":"demo","boundaryIndex":0,"start":0,"name":"intro","mode":"single"}`, cookies...)
	if rec.Code != http.StatusConflict {
		t.Fatalf("create for unsegmented show status = %d, want 409; body %q", rec.Code, rec.Body.String())
	}
}

func TestCreateBadBoundaryReturns400(t *testing.T) {
	cfg := testConfig(t)
	_, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	writeLabels(t, cfg, "demo", `{"video":"demo","boundaries":[{"name":"intro","start":0},{"name":"two","start":12}]}`)
	cookies := ownerCookies(t, mux)

	cases := map[string]string{
		"index out of range": `{"show":"demo","boundaryIndex":9,"start":0,"name":"intro","mode":"single"}`,
		"name mismatch":      `{"show":"demo","boundaryIndex":0,"start":0,"name":"wrong","mode":"single"}`,
		"start mismatch":     `{"show":"demo","boundaryIndex":0,"start":500,"name":"intro","mode":"single"}`,
		"bad mode":           `{"show":"demo","boundaryIndex":0,"start":0,"name":"intro","mode":"bogus"}`,
		"bad show":           `{"show":"../etc","boundaryIndex":0,"start":0,"name":"intro","mode":"single"}`,
	}
	for name, body := range cases {
		rec := do(t, mux, http.MethodPost, "/shares", body, cookies...)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body %q", name, rec.Code, rec.Body.String())
		}
	}
}

// --- single-device flow ---------------------------------------------------

func newSingleShare(t *testing.T, cfg config.Config, srv *Server, show string) string {
	t.Helper()
	token, err := srv.store.Create(CreateParams{
		Show: show, ChapterName: "Chapter Two", Start: 12, End: 30, StartOffset: 0, EndOffset: 18,
		Segments: []string{"seg_0002.ts", "seg_0003.ts", "seg_0004.ts"}, Playlist: testPlaylist, Mode: ModeSingle,
	})
	if err != nil {
		t.Fatalf("create single share: %v", err)
	}
	return token
}

func TestSingleDeviceClaimBindAndReject(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newSingleShare(t, cfg, srv, "demo")

	// First view with no cookie: bot-safe interstitial, no claim, no cookie.
	rec := do(t, mux, http.MethodGet, "/s/"+token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("interstitial status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Watch now") {
		t.Fatalf("interstitial missing Watch now button: %q", rec.Body.String())
	}
	if hasShareCookie(rec) {
		t.Fatal("GET must not set a device cookie")
	}
	if sh, _ := srv.store.Get(token); sh.ClaimedAt != nil {
		t.Fatal("GET must not claim the share")
	}

	// POST claims and binds the device.
	rec = do(t, mux, http.MethodPost, "/s/"+token, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("claim status = %d, want 303", rec.Code)
	}
	cookie := shareCookie(t, rec)
	if cookie.Path != "/s/"+token || !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("device cookie attributes = %#v", cookie)
	}

	// Bound device can view and fetch media.
	rec = do(t, mux, http.MethodGet, "/s/"+token, "", cookie)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Watch now") {
		t.Fatalf("bound viewer status = %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Chapter Two") || !strings.Contains(rec.Body.String(), "<video") {
		t.Fatalf("viewer missing title/video: %q", rec.Body.String())
	}
	rec = do(t, mux, http.MethodGet, "/s/"+token+"/playlist.m3u8", "", cookie)
	if rec.Code != http.StatusOK || rec.Body.String() != testPlaylist {
		t.Fatalf("playlist status=%d body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != mpegURLType {
		t.Fatalf("playlist content-type = %q", ct)
	}
	rec = do(t, mux, http.MethodGet, "/s/"+token+"/seg_0002.ts", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("segment status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Fatalf("segment content-type = %q", ct)
	}

	// A second device (no cookie) is rejected on both the viewer and media.
	if rec := do(t, mux, http.MethodGet, "/s/"+token, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("second-device viewer status = %d, want 404", rec.Code)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/playlist.m3u8", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("second-device playlist status = %d, want 404", rec.Code)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/seg_0002.ts", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("second-device segment status = %d, want 404", rec.Code)
	}
}

func TestDenyMediaBeforeClaim(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newSingleShare(t, cfg, srv, "demo")

	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/playlist.m3u8", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unclaimed playlist status = %d, want 404", rec.Code)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/seg_0002.ts", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unclaimed segment status = %d, want 404", rec.Code)
	}
}

func TestBotSafeGetThenDifferentDeviceClaims(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newSingleShare(t, cfg, srv, "demo")

	// A preview bot GETs the link: no cookie, no claim.
	rec := do(t, mux, http.MethodGet, "/s/"+token, "")
	if hasShareCookie(rec) {
		t.Fatal("preview GET must not set a cookie")
	}
	if sh, _ := srv.store.Get(token); sh.ClaimedAt != nil {
		t.Fatal("preview GET must not claim the share")
	}
	// The real recipient can still claim it afterwards.
	rec = do(t, mux, http.MethodPost, "/s/"+token, "")
	if rec.Code != http.StatusSeeOther || !hasShareCookie(rec) {
		t.Fatalf("subsequent claim failed: status %d", rec.Code)
	}
}

func TestFirstClaimRaceOverHTTP(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newSingleShare(t, cfg, srv, "demo")

	// Two independent devices race to POST; exactly one gets a cookie + redirect.
	rec1 := do(t, mux, http.MethodPost, "/s/"+token, "")
	rec2 := do(t, mux, http.MethodPost, "/s/"+token, "")
	cookies, redirects := 0, 0
	for _, rec := range []*httptest.ResponseRecorder{rec1, rec2} {
		if hasShareCookie(rec) {
			cookies++
		}
		if rec.Code == http.StatusSeeOther {
			redirects++
		}
	}
	if cookies != 1 {
		t.Fatalf("device cookies issued = %d, want exactly 1", cookies)
	}
	// The loser (already claimed by another device, no cookie) gets 404.
	if rec2.Code != http.StatusNotFound && rec1.Code != http.StatusNotFound {
		t.Fatalf("expected one 404 for the losing device; got %d and %d", rec1.Code, rec2.Code)
	}
	_ = redirects
}

// --- public mode ----------------------------------------------------------

func newPublicShare(t *testing.T, srv *Server, show string) string {
	t.Helper()
	token, err := srv.store.Create(CreateParams{
		Show: show, ChapterName: "Public Chapter", Start: 0, End: 12, StartOffset: 0, EndOffset: 12,
		Segments: []string{"seg_0000.ts", "seg_0001.ts"}, Playlist: testPlaylist, Mode: ModePublic,
	})
	if err != nil {
		t.Fatalf("create public share: %v", err)
	}
	return token
}

func TestPublicModeAnyDeviceUnlimited(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newPublicShare(t, srv, "demo")

	for i := 0; i < 3; i++ {
		if rec := do(t, mux, http.MethodGet, "/s/"+token, ""); rec.Code != http.StatusOK {
			t.Fatalf("public viewer #%d status = %d, want 200", i, rec.Code)
		}
		if rec := do(t, mux, http.MethodGet, "/s/"+token+"/playlist.m3u8", ""); rec.Code != http.StatusOK {
			t.Fatalf("public playlist #%d status = %d, want 200", i, rec.Code)
		}
		if rec := do(t, mux, http.MethodGet, "/s/"+token+"/seg_0000.ts", ""); rec.Code != http.StatusOK {
			t.Fatalf("public segment #%d status = %d, want 200", i, rec.Code)
		}
	}
	// POST on a public share just redirects to the viewer without a cookie.
	rec := do(t, mux, http.MethodPost, "/s/"+token, "")
	if rec.Code != http.StatusSeeOther || hasShareCookie(rec) {
		t.Fatalf("public POST status = %d, cookie=%v", rec.Code, hasShareCookie(rec))
	}
}

// --- expiry (server-side authoritative) -----------------------------------

func TestExpiredShareDeniedEvenWithCookie(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newSingleShare(t, cfg, srv, "demo")

	// Claim to obtain a valid device cookie.
	rec := do(t, mux, http.MethodPost, "/s/"+token, "")
	cookie := shareCookie(t, rec)

	// Expire the share server-side.
	store := flatShareStore(t, srv)
	store.mu.Lock()
	past := time.Now().Add(-time.Hour).UTC()
	store.byHash[sha256hex(token)].ExpiresAt = &past
	store.mu.Unlock()

	if rec := do(t, mux, http.MethodGet, "/s/"+token, "", cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("expired viewer status = %d, want 404", rec.Code)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/playlist.m3u8", "", cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("expired playlist status = %d, want 404", rec.Code)
	}
}

// --- scoping / IDOR --------------------------------------------------------

func TestScopingSegmentNotInListAndOwnerSessionNoAccess(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newSingleShare(t, cfg, srv, "demo") // frozen segments: seg_0002..seg_0004

	// Claim so media would otherwise be reachable for the bound device.
	rec := do(t, mux, http.MethodPost, "/s/"+token, "")
	cookie := shareCookie(t, rec)

	// A segment that exists on disk but is NOT in the frozen list -> 404.
	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/seg_0000.ts", "", cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("out-of-chapter segment status = %d, want 404", rec.Code)
	}
	// A non-media file is never reachable.
	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/playlist.m3u8x", "", cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("bogus file status = %d, want 404", rec.Code)
	}

	// The owner's vid_sess session does NOT by itself unlock a single share's media.
	ownerSess := ownerCookies(t, mux)
	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/seg_0002.ts", "", ownerSess...); rec.Code != http.StatusNotFound {
		t.Fatalf("owner-session media access status = %d, want 404 (needs device cookie)", rec.Code)
	}
}

// --- headers, content, and routing ----------------------------------------

func TestViewerHeadersAndNoChrome(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newPublicShare(t, srv, "demo")

	rec := do(t, mux, http.MethodGet, "/s/"+token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer status = %d", rec.Code)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Fatalf("CSP = %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "media-src 'self' blob:") {
		t.Fatalf("CSP media-src = %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "worker-src 'self' blob:") {
		t.Fatalf("CSP worker-src = %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Public Chapter") || !strings.Contains(body, "hls.min.js") || !strings.Contains(body, "<video") {
		t.Fatalf("viewer body missing expected content: %q", body)
	}
	for _, forbidden := range []string{"Back to library", "Sign out", "label editor", "Mark start here"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("viewer body unexpectedly contains %q", forbidden)
		}
	}
}

func TestInterstitialContentOnly(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newSingleShare(t, cfg, srv, "demo")

	rec := do(t, mux, http.MethodGet, "/s/"+token, "")
	body := rec.Body.String()
	if !strings.Contains(body, "Watch now") || !strings.Contains(body, `action="/s/`+token+`"`) {
		t.Fatalf("interstitial missing Watch now form: %q", body)
	}
	if strings.Contains(body, "<video") {
		t.Fatal("interstitial must not embed a video before the device is bound")
	}
}

func TestUnknownAndRevokedTokensReturn404(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")

	if rec := do(t, mux, http.MethodGet, "/s/does-not-exist", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown token status = %d, want 404", rec.Code)
	}
	token := newPublicShare(t, srv, "demo")
	if ok, err := srv.store.RevokeByHash(sha256hex(token)); err != nil || !ok {
		t.Fatalf("RevokeByHash ok=%v err=%v, want ok with no error", ok, err)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+token, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("revoked viewer status = %d, want 404", rec.Code)
	}
	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/playlist.m3u8", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("revoked media status = %d, want 404", rec.Code)
	}
}

func TestRoutingTrailingSlashAndEncodedSlash(t *testing.T) {
	cfg := testConfig(t)
	srv, mux := newShareMux(t, cfg)
	writeHLSShow(t, cfg, "demo")
	token := newPublicShare(t, srv, "demo")

	// Trailing slash (empty file segment) must not serve media.
	if rec := do(t, mux, http.MethodGet, "/s/"+token+"/", ""); rec.Code == http.StatusOK {
		t.Fatalf("trailing-slash media status = %d, want non-200", rec.Code)
	}
	// Encoded-slash / traversal attempts must not serve media.
	for _, target := range []string{
		"/s/" + token + "/..%2fseg_0000.ts",
		"/s/" + token + "/%2e%2e%2fplaylist.m3u8",
		"/s/" + token + "/sub%2fseg_0000.ts",
	} {
		rec := do(t, mux, http.MethodGet, target, "")
		if rec.Code == http.StatusOK {
			t.Fatalf("%s served media (status 200), want rejection", target)
		}
	}
}
