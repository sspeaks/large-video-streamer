package share

import (
	"encoding/json"
	"errors"
	"html/template"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/auth"
	"github.com/sspeaks/large-video-streamer/internal/config"
	"github.com/sspeaks/large-video-streamer/internal/hls"
	"github.com/sspeaks/large-video-streamer/internal/labels"
)

const (
	shareCookieName = "vid_share"
	// shareCookieMaxAge (~10 years) keeps a claimed non-expiring share usable on
	// the bound device; the server-side ExpiresAt remains authoritative.
	shareCookieMaxAge = 10 * 365 * 24 * 60 * 60
	playlistFile      = "playlist.m3u8"
	mpegURLType       = "application/vnd.apple.mpegurl"
)

// Server owns share creation, claiming, and the no-chrome viewer routes.
type Server struct {
	store  ShareStore
	hls    *hls.Server
	labels labels.LabelStore
}

// New returns a share server using the current flat-file share and label stores.
func New(cfg config.Config) *Server {
	return NewWithStores(cfg, nil, nil)
}

// NewWithStores returns a share server using supplied persistence seams. Nil
// stores keep the current flat-file behavior.
func NewWithStores(cfg config.Config, store ShareStore, labelStore labels.LabelStore) *Server {
	if store == nil {
		store = NewStore(filepath.Join(cfg.StateDir, "shares.json"))
	}
	if labelStore == nil {
		labelStore = labels.New(cfg)
	}
	return &Server{
		store:  store,
		hls:    hls.New(cfg),
		labels: labelStore,
	}
}

// RegisterRoutes wires share endpoints into mux. Owner creation and management
// are gated; the /s/ recipient routes must work with no session in every mode.
func (srv *Server) RegisterRoutes(mux *http.ServeMux, a *auth.Authenticator) {
	mux.Handle("POST /shares", a.RequireMedia(http.HandlerFunc(srv.handleCreate)))
	mux.Handle("GET /admin/shares", a.RequirePage(http.HandlerFunc(srv.handleAdminShares)))
	mux.Handle("POST /admin/shares/{hash}/revoke", a.RequirePage(http.HandlerFunc(srv.handleAdminRevoke)))
	mux.Handle("POST /admin/shares/{hash}/delete", a.RequirePage(http.HandlerFunc(srv.handleAdminDelete)))
	mux.HandleFunc("GET /s/{token}", srv.handleView)
	mux.HandleFunc("POST /s/{token}", srv.handleClaim)
	mux.HandleFunc("GET /s/{token}/{file}", srv.handleMedia)
}

type createRequest struct {
	Show          string  `json:"show"`
	BoundaryIndex int     `json:"boundaryIndex"`
	Start         float64 `json:"start"`
	Name          string  `json:"name"`
	Mode          string  `json:"mode"`
	ExpiresAt     *string `json:"expiresAt"`
}

type createResponse struct {
	Token string `json:"token"`
	Path  string `json:"path"`
}

type adminSharesPageData struct {
	Shares []adminShareRow
}

type adminShareRow struct {
	TokenHash   string
	Show        string
	ChapterName string
	Mode        string
	CreatedAt   string
	ClaimedAt   string
	ExpiresAt   string
	Status      string
	StatusClass string
	CanRevoke   bool
	DeleteLabel string
	RevokeLabel string
}

// handleCreate validates the owner's request, freezes the chapter's derived
// sub-playlist/segments/offsets, stores a new share, and returns its token.
func (srv *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	defer r.Body.Close()

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid share JSON", http.StatusBadRequest)
		return
	}
	if !validShowName(req.Show) {
		http.Error(w, "invalid show name", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "chapter name is required", http.StatusBadRequest)
		return
	}

	mode := Mode(req.Mode)
	if mode == "" {
		mode = ModeSingle
	}
	if mode != ModeSingle && mode != ModePublic {
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}

	expiresAt, err := parseExpiresAt(req.ExpiresAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	vl, err := srv.labels.Load(req.Show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	chapter, rawEnd, ok := locateChapter(vl.Boundaries, req.BoundaryIndex, req.Name, req.Start)
	if !ok {
		http.Error(w, "boundary not found or does not match", http.StatusBadRequest)
		return
	}

	playlist, segments, startOffset, endOffset, total, err := srv.hls.BuildChapterPlaylist(req.Show, chapter.Start, rawEnd)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "segment this video before sharing", http.StatusConflict)
			return
		}
		http.Error(w, "could not build chapter", http.StatusInternalServerError)
		return
	}
	if len(segments) == 0 {
		http.Error(w, "chapter has no media", http.StatusBadRequest)
		return
	}
	end := rawEnd
	if end <= 0 {
		end = total
	}

	token, err := srv.store.Create(CreateParams{
		Show:        req.Show,
		ChapterName: chapter.Name,
		Start:       chapter.Start,
		End:         end,
		StartOffset: startOffset,
		EndOffset:   endOffset,
		Segments:    segments,
		Playlist:    string(playlist),
		Mode:        mode,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		http.Error(w, "could not create share", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(createResponse{Token: token, Path: "/s/" + token})
}

func (srv *Server) handleAdminShares(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	now := time.Now().UTC()
	summaries := srv.store.List()
	rows := make([]adminShareRow, 0, len(summaries))
	for _, sh := range summaries {
		rows = append(rows, adminRow(sh, now))
	}

	if err := adminSharesTemplate.Execute(w, adminSharesPageData{Shares: rows}); err != nil {
		http.Error(w, "failed to render shares page", http.StatusInternalServerError)
	}
}

func (srv *Server) handleAdminRevoke(w http.ResponseWriter, r *http.Request) {
	srv.handleAdminMutation(w, r, srv.store.RevokeByHash)
}

func (srv *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	srv.handleAdminMutation(w, r, srv.store.DeleteByHash)
}

func (srv *Server) handleAdminMutation(w http.ResponseWriter, r *http.Request, mutate func(string) (bool, error)) {
	w.Header().Set("Cache-Control", "no-store")
	hash := r.PathValue("hash")
	if !validTokenHash(hash) {
		http.NotFound(w, r)
		return
	}
	if _, err := mutate(hash); err != nil {
		http.Error(w, "could not update share", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/shares", http.StatusSeeOther)
}

// handleView serves the recipient entry point: the minimal viewer for public
// shares and for the bound device of a claimed single share; a bot-safe
// interstitial for an unclaimed single share (no cookie set, no claim recorded);
// and 404 otherwise (missing/revoked/expired/other device).
func (srv *Server) handleView(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	setShareHTMLHeaders(w)

	sh, ok := srv.store.Get(token)
	if !ok || !usable(sh, time.Now().UTC()) {
		http.NotFound(w, r)
		return
	}

	switch sh.Mode {
	case ModePublic:
		srv.renderViewer(w, token, sh)
	case ModeSingle:
		if sh.ClaimedAt != nil {
			if deviceMatchesCookie(r, sh) {
				srv.renderViewer(w, token, sh)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		srv.renderInterstitial(w, token, sh)
	default:
		http.NotFound(w, r)
	}
}

// handleClaim binds an unclaimed single share to the requesting device (setting
// the vid_share cookie), or redirects to the viewer for public / already-bound
// devices. Non-owning devices on a claimed single share get 404.
func (srv *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	w.Header().Set("Cache-Control", "no-store")

	sh, ok := srv.store.Get(token)
	if !ok || !usable(sh, time.Now().UTC()) {
		http.NotFound(w, r)
		return
	}

	switch sh.Mode {
	case ModePublic:
		http.Redirect(w, r, "/s/"+token, http.StatusSeeOther)
	case ModeSingle:
		if sh.ClaimedAt != nil {
			if deviceMatchesCookie(r, sh) {
				http.Redirect(w, r, "/s/"+token, http.StatusSeeOther)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		secret, err := generateSecret()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !srv.store.Claim(token, secret) {
			// Lost the first-claim race or the share became unusable.
			http.NotFound(w, r)
			return
		}
		setShareCookie(w, token, secret, sh.ExpiresAt)
		http.Redirect(w, r, "/s/"+token, http.StatusSeeOther)
	default:
		http.NotFound(w, r)
	}
}

// handleMedia serves the frozen sub-playlist and whitelisted chapter segments.
// Single shares require a prior claim from the bound device; public shares do
// not. Only files frozen into the share's segment list are reachable.
func (srv *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	file := r.PathValue("file")

	sh, ok := srv.store.Get(token)
	if !ok || !usable(sh, time.Now().UTC()) {
		http.NotFound(w, r)
		return
	}
	if sh.Mode == ModeSingle {
		if sh.ClaimedAt == nil || sh.DeviceHash == "" || !deviceMatchesCookie(r, sh) {
			http.NotFound(w, r)
			return
		}
	}

	if file == playlistFile {
		h := w.Header()
		h.Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
		h.Set("Pragma", "no-cache")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Type", mpegURLType)
		_, _ = w.Write([]byte(sh.Playlist))
		return
	}

	if !segmentAllowed(sh.Segments, file) {
		http.NotFound(w, r)
		return
	}
	srv.hls.ServeScopedSegment(w, r, sh.Show, file)
}

// locateChapter identifies the requested boundary by its original index and
// confirms it matches the supplied name/start (±1s), then derives the chapter's
// end from the next boundary in start order (or 0 for the last chapter, meaning
// "to the end of the video"). Returns the authoritative boundary and rawEnd.
func locateChapter(boundaries []labels.Boundary, index int, name string, start float64) (labels.Boundary, float64, bool) {
	if index < 0 || index >= len(boundaries) {
		return labels.Boundary{}, 0, false
	}
	chosen := boundaries[index]
	if chosen.Name != name || strings.TrimSpace(chosen.Name) == "" || math.Abs(chosen.Start-start) > 1.0 {
		return labels.Boundary{}, 0, false
	}

	type ordered struct {
		origIndex int
		start     float64
	}
	sorted := make([]ordered, 0, len(boundaries))
	for i, b := range boundaries {
		if strings.TrimSpace(b.Name) == "" {
			continue
		}
		sorted = append(sorted, ordered{origIndex: i, start: b.Start})
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })

	pos := -1
	for i, o := range sorted {
		if o.origIndex == index {
			pos = i
			break
		}
	}
	if pos < 0 {
		return labels.Boundary{}, 0, false
	}
	rawEnd := 0.0 // last chapter: BuildChapterPlaylist treats <=0 as "to the end"
	if pos+1 < len(sorted) {
		rawEnd = sorted[pos+1].start
	}
	return chosen, rawEnd, true
}

func parseExpiresAt(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, errors.New("invalid expiresAt (want RFC3339)")
	}
	tu := t.UTC()
	if !tu.After(time.Now().UTC()) {
		return nil, errors.New("expiresAt must be in the future")
	}
	return &tu, nil
}

func segmentAllowed(segments []string, file string) bool {
	for _, s := range segments {
		if s == file {
			return true
		}
	}
	return false
}

func deviceMatchesCookie(r *http.Request, sh *Share) bool {
	c, err := r.Cookie(shareCookieName)
	if err != nil {
		return false
	}
	return sh.DeviceMatches(c.Value)
}

func setShareCookie(w http.ResponseWriter, token, secret string, expiresAt *time.Time) {
	maxAge := shareCookieMaxAge
	if expiresAt != nil {
		if secs := int(time.Until(*expiresAt).Seconds()); secs < maxAge {
			maxAge = secs
		}
		if maxAge < 1 {
			maxAge = 1
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     shareCookieName,
		Value:    secret,
		Path:     "/s/" + token,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

// setShareHTMLHeaders applies the no-chrome viewer's hardening headers: no
// caching, no referrer leakage of the token, and a CSP that still allows
// hls.js to attach its blob-backed MediaSource and worker.
func setShareHTMLHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; media-src 'self' blob:; worker-src 'self' blob:; img-src 'self' data:; base-uri 'none'; form-action 'self'")
}

func validShowName(show string) bool {
	if show == "" {
		return false
	}
	return filepath.Base(show) == show && !strings.ContainsAny(show, `/\`) && !strings.Contains(show, "..")
}

func validTokenHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

const adminTimeFormat = "2006-01-02 15:04 UTC"

func adminRow(sh ShareSummary, now time.Time) adminShareRow {
	status, class, canRevoke := adminShareStatus(sh, now)
	return adminShareRow{
		TokenHash:   sh.TokenHash,
		Show:        sh.Show,
		ChapterName: sh.ChapterName,
		Mode:        string(sh.Mode),
		CreatedAt:   formatAdminTime(sh.CreatedAt),
		ClaimedAt:   formatAdminClaimedAt(sh),
		ExpiresAt:   formatAdminOptionalTime(sh.ExpiresAt, "Never"),
		Status:      status,
		StatusClass: class,
		CanRevoke:   canRevoke,
		DeleteLabel: "Delete " + sh.ChapterName,
		RevokeLabel: "Revoke " + sh.ChapterName,
	}
}

func adminShareStatus(sh ShareSummary, now time.Time) (status, class string, canRevoke bool) {
	if sh.RevokedAt != nil {
		return "Revoked", "revoked", false
	}
	if sh.ExpiresAt != nil && !now.Before(*sh.ExpiresAt) {
		return "Expired", "expired", true
	}
	return "Active", "active", true
}

func formatAdminClaimedAt(sh ShareSummary) string {
	if sh.ClaimedAt != nil {
		return formatAdminTime(*sh.ClaimedAt)
	}
	if sh.Mode == ModeSingle {
		return "Not claimed"
	}
	return "N/A"
}

func formatAdminOptionalTime(t *time.Time, empty string) string {
	if t == nil {
		return empty
	}
	return formatAdminTime(*t)
}

func formatAdminTime(t time.Time) string {
	if t.IsZero() {
		return "Unknown"
	}
	return t.UTC().Format(adminTimeFormat)
}

type interstitialData struct {
	Token       string
	ChapterName string
}

type viewerData struct {
	Token       string
	ChapterName string
	StartOffset float64
	EndOffset   float64
}

func (srv *Server) renderInterstitial(w http.ResponseWriter, token string, sh *Share) {
	_ = interstitialTemplate.Execute(w, interstitialData{Token: token, ChapterName: sh.ChapterName})
}

func (srv *Server) renderViewer(w http.ResponseWriter, token string, sh *Share) {
	_ = viewerTemplate.Execute(w, viewerData{
		Token:       token,
		ChapterName: sh.ChapterName,
		StartOffset: sh.StartOffset,
		EndOffset:   sh.EndOffset,
	})
}

var adminSharesTemplate = template.Must(template.New("admin-shares").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Share management</title>
  <link rel="stylesheet" href="/static/app.css">
  <style>
    body { background: var(--bg-deep); }
    main { width: min(1180px, calc(100% - 32px)); margin: 0 auto; padding: 32px 0 56px; }
    header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 24px; }
    h1 { margin: 0; font-size: clamp(1.8rem, 4vw, 3rem); letter-spacing: -0.04em; }
    .nav, .header-actions, .row-actions { display: flex; gap: .6rem; flex-wrap: wrap; align-items: center; }
    .nav { margin-bottom: .85rem; }
    .table-wrap { max-width: 100%; overflow-x: auto; border-radius: var(--radius); }
    table { width: 100%; min-width: 64rem; border-collapse: collapse; background: var(--surface); }
    th, td { vertical-align: middle; }
    .status-chip { display: inline-flex; align-items: center; min-height: 2rem; padding: .2rem .7rem; border-radius: var(--radius-pill); font-weight: 700; }
    .status-chip.active { background: var(--success-bg); color: var(--success-text); }
    .status-chip.expired { background: var(--warn-bg); color: var(--warn-text); }
    .status-chip.revoked { background: var(--danger-soft-bg); color: var(--danger-soft-text); border: 1px solid var(--danger-soft-border); }
    form { margin: 0; }
    .empty { padding: 2rem; color: var(--text-muted); text-align: center; }
    @media (max-width: 700px) {
      header { display: grid; }
      .header-actions { justify-content: flex-start; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <nav class="nav" aria-label="Share management navigation">
          <a class="btn btn--secondary btn--pill" href="/">← Back to library</a>
        </nav>
        <h1>Share management</h1>
        <p class="text-muted">Review and revoke chapter links without exposing share tokens or media payloads.</p>
      </div>
      <form class="header-actions" method="post" action="/logout">
        <button class="btn btn--secondary btn--pill" type="submit">Sign out</button>
      </form>
    </header>

    <section class="panel">
      {{if .Shares}}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th scope="col">Show</th>
              <th scope="col">Chapter</th>
              <th scope="col">Mode</th>
              <th scope="col">Created</th>
              <th scope="col">Claimed/device registered</th>
              <th scope="col">Expires</th>
              <th scope="col">Status</th>
              <th scope="col">Actions</th>
            </tr>
          </thead>
          <tbody>
            {{range .Shares}}
            <tr>
              <td>{{.Show}}</td>
              <td>{{.ChapterName}}</td>
              <td>{{.Mode}}</td>
              <td>{{.CreatedAt}}</td>
              <td>{{.ClaimedAt}}</td>
              <td>{{.ExpiresAt}}</td>
              <td><span class="status-chip {{.StatusClass}}">{{.Status}}</span></td>
              <td>
                <div class="row-actions">
                  {{if .CanRevoke}}
                  <form method="post" action="/admin/shares/{{.TokenHash}}/revoke">
                    <button class="btn btn--secondary" type="submit" aria-label="{{.RevokeLabel}}">Revoke</button>
                  </form>
                  {{else}}
                  <span class="text-muted">Revoked</span>
                  {{end}}
                  <form method="post" action="/admin/shares/{{.TokenHash}}/delete">
                    <button class="btn btn--danger" type="submit" aria-label="{{.DeleteLabel}}">Delete</button>
                  </form>
                </div>
              </td>
            </tr>
            {{end}}
          </tbody>
        </table>
      </div>
      {{else}}
      <p class="empty">No share links yet. Create one from a chapter in the video player.</p>
      {{end}}
    </section>
  </main>
</body>
</html>`))

var interstitialTemplate = template.Must(template.New("share-interstitial").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.ChapterName}}</title>
  <style>
    :root { color-scheme: dark; font-family: system-ui, -apple-system, Segoe UI, sans-serif; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #080d19; color: #edf2ff; }
    main { box-sizing: border-box; width: min(92vw, 26rem); padding: 2rem; border-radius: 1rem; background: #12182a; box-shadow: 0 1.5rem 4rem #02061799; text-align: center; }
    h1 { margin: 0 0 1rem; font-size: 1.5rem; }
    p { margin: 0 0 1.5rem; color: #9fb0d0; line-height: 1.5; }
    button { min-height: 3rem; width: 100%; padding: .9rem; border: 0; border-radius: .6rem; background: #38bdf8; color: #082f49; font-size: 1.05rem; font-weight: 700; cursor: pointer; }
  </style>
</head>
<body>
  <main>
    <h1>{{.ChapterName}}</h1>
    <p>This link binds to the first device that opens it. Continue on this device to watch.</p>
    <form method="post" action="/s/{{.Token}}">
      <button type="submit">Watch now</button>
    </form>
  </main>
</body>
</html>`))

var viewerTemplate = template.Must(template.New("share-viewer").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.ChapterName}}</title>
  <style>
    :root { color-scheme: dark; font-family: system-ui, -apple-system, Segoe UI, sans-serif; }
    body { margin: 0; min-height: 100vh; background: #000; color: #edf2ff; display: flex; flex-direction: column; }
    h1 { margin: 0; padding: .9rem 1rem; font-size: 1.2rem; font-weight: 700; background: #080d19; }
    .stage { flex: 1; display: grid; place-items: center; background: #000; }
    video { width: 100%; max-height: 100vh; background: #000; }
  </style>
</head>
<body>
  <h1>{{.ChapterName}}</h1>
  <div class="stage"><video id="v" controls playsinline autoplay></video></div>
  <script src="/static/hls.min.js"></script>
  <script>
    (function () {
      var token = {{.Token}};
      var startOffset = {{.StartOffset}};
      var endOffset = {{.EndOffset}};
      var video = document.getElementById('v');
      var playlist = '/s/' + token + '/playlist.m3u8';
      function clampEnd() {
        if (endOffset > startOffset && video.currentTime >= endOffset) {
          video.pause();
        }
      }
      video.addEventListener('timeupdate', clampEnd);
      if (window.Hls && Hls.isSupported()) {
        var hls = new Hls({
          startPosition: startOffset,
          xhrSetup: function (xhr) { xhr.withCredentials = true; },
          fetchSetup: function (ctx, init) { init = init || {}; init.credentials = 'include'; return new Request(ctx.url, init); }
        });
        hls.loadSource(playlist);
        hls.attachMedia(video);
        hls.on(Hls.Events.MANIFEST_PARSED, function () {
          if (startOffset > 0 && (!video.currentTime || video.currentTime < startOffset)) {
            try { video.currentTime = startOffset; } catch (e) {}
          }
          video.play().catch(function () {});
        });
      } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = playlist;
        video.addEventListener('loadedmetadata', function () {
          if (startOffset > 0) { try { video.currentTime = startOffset; } catch (e) {} }
          video.play().catch(function () {});
        });
      }
    })();
  </script>
</body>
</html>`))
