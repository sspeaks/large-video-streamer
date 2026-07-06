package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

const (
	sessionCookieName = "vid_sess"
	sessionDuration   = 12 * time.Hour
)

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Sign in</title>
  <style>
    :root { color-scheme: dark; font-family: system-ui, -apple-system, Segoe UI, sans-serif; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #0f172a; color: #e5e7eb; }
    main { width: min(92vw, 24rem); padding: 2rem; border-radius: 1rem; background: #111827; box-shadow: 0 1.5rem 4rem #02061799; }
    h1 { margin: 0 0 1.5rem; font-size: 1.6rem; }
    label { display: block; margin: 1rem 0 .4rem; color: #cbd5e1; }
    input { box-sizing: border-box; width: 100%; padding: .8rem; border: 1px solid #334155; border-radius: .55rem; background: #020617; color: #f8fafc; }
    button { width: 100%; margin-top: 1.4rem; padding: .85rem; border: 0; border-radius: .55rem; background: #38bdf8; color: #082f49; font-weight: 700; cursor: pointer; }
    .error { padding: .75rem; border-radius: .55rem; background: #7f1d1d; color: #fecaca; }
  </style>
</head>
<body>
  <main>
    <h1>Sign in</h1>
    {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
    <form method="post" action="/login">
      <label for="user">Username</label>
      <input id="user" name="user" type="text" autocomplete="username" required autofocus>
      <label for="pass">Password</label>
      <input id="pass" name="pass" type="password" autocomplete="current-password" required>
      <button type="submit">Continue</button>
    </form>
  </main>
</body>
</html>`))

type tokenPayload struct {
	User   string `json:"u"`
	Expiry int64  `json:"e"`
}

// Authenticator owns login/logout routes and auth gates for pages and media.
type Authenticator struct {
	cfg    config.Config
	secret []byte
}

// New returns an authenticator configured from application settings.
func New(cfg config.Config) *Authenticator {
	return &Authenticator{cfg: cfg, secret: cfg.CookieSecret}
}

// RegisterRoutes wires authentication endpoints into mux.
func (a *Authenticator) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", a.handleLoginGet)
	mux.HandleFunc("POST /login", a.handleLoginPost)
	mux.HandleFunc("POST /logout", a.handleLogoutPost)
}

// RequirePage protects HTML pages.
func (a *Authenticator) RequirePage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.NoAuth {
			next.ServeHTTP(w, r)
			return
		}
		if !a.isAuthenticated(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireMedia protects media assets.
func (a *Authenticator) RequireMedia(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.NoAuth {
			next.ServeHTTP(w, r)
			return
		}
		if !a.isAuthenticated(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Authenticator) newToken(user string) (string, error) {
	payload, err := json.Marshal(tokenPayload{
		User:   user,
		Expiry: time.Now().Add(sessionDuration).Unix(),
	})
	if err != nil {
		return "", err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := a.sign(encodedPayload)

	return encodedPayload + "." + signature, nil
}

func (a *Authenticator) verifyToken(tok string) (user string, ok bool) {
	encodedPayload, encodedSignature, found := strings.Cut(tok, ".")
	if !found || encodedPayload == "" || encodedSignature == "" {
		return "", false
	}

	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil {
		return "", false
	}
	expectedSignature, err := base64.RawURLEncoding.DecodeString(a.sign(encodedPayload))
	if err != nil {
		return "", false
	}
	if !hmac.Equal(signature, expectedSignature) {
		return "", false
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return "", false
	}

	var payload tokenPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return "", false
	}
	if time.Now().Unix() > payload.Expiry {
		return "", false
	}

	return payload.User, true
}

func (a *Authenticator) sign(payload string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *Authenticator) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	_, ok := a.verifyToken(cookie.Value)
	return ok
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (a *Authenticator) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.isAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	renderLogin(w, http.StatusOK, "")
}

func (a *Authenticator) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if err := r.ParseForm(); err != nil {
		renderLogin(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	userMatch := subtle.ConstantTimeCompare([]byte(r.FormValue("user")), []byte(a.cfg.LoginUser))
	passMatch := subtle.ConstantTimeCompare([]byte(r.FormValue("pass")), []byte(a.cfg.LoginPass))
	if userMatch == 1 && passMatch == 1 {
		token, err := a.newToken(a.cfg.LoginUser)
		if err != nil {
			http.Error(w, "failed to create session", http.StatusInternalServerError)
			return
		}
		setSessionCookie(w, token)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	renderLogin(w, http.StatusUnauthorized, "Invalid username or password")
}

func (a *Authenticator) handleLogoutPost(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func renderLogin(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := loginTemplate.Execute(w, struct{ Error string }{Error: message}); err != nil {
		http.Error(w, "failed to render login", http.StatusInternalServerError)
	}
}
