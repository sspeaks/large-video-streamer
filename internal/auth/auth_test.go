package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

func testAuthenticator() *Authenticator {
	return New(config.Config{
		LoginUser:    "alice",
		LoginPass:    "correct horse battery staple",
		CookieSecret: []byte("0123456789abcdef0123456789abcdef"),
	})
}

func signedTestToken(t *testing.T, a *Authenticator, user string, expiry int64) string {
	t.Helper()

	payload, err := json.Marshal(tokenPayload{User: user, Expiry: expiry})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encodedPayload + "." + signature
}

func TestTokenRoundTripVerifiesUser(t *testing.T) {
	a := testAuthenticator()

	token, err := a.newToken("alice")
	if err != nil {
		t.Fatalf("newToken returned error: %v", err)
	}

	user, ok := a.verifyToken(token)
	if !ok {
		t.Fatal("verifyToken rejected token from newToken")
	}
	if user != "alice" {
		t.Fatalf("verifyToken user = %q, want %q", user, "alice")
	}
}

func TestVerifyTokenRejectsExpiredToken(t *testing.T) {
	a := testAuthenticator()
	token := signedTestToken(t, a, "alice", time.Now().Add(-time.Minute).Unix())

	if user, ok := a.verifyToken(token); ok {
		t.Fatalf("verifyToken accepted expired token for %q", user)
	}
}

func TestVerifyTokenRejectsTamperedPayloadAndSignature(t *testing.T) {
	a := testAuthenticator()
	token := signedTestToken(t, a, "alice", time.Now().Add(time.Hour).Unix())

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("token parts = %d, want 2", len(parts))
	}

	tamperedPayload, err := json.Marshal(tokenPayload{User: "mallory", Expiry: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("marshal tampered payload: %v", err)
	}
	tamperedPayloadToken := base64.RawURLEncoding.EncodeToString(tamperedPayload) + "." + parts[1]
	if user, ok := a.verifyToken(tamperedPayloadToken); ok {
		t.Fatalf("verifyToken accepted tampered payload for %q", user)
	}

	tamperedSignatureToken := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte("bad signature"))
	if user, ok := a.verifyToken(tamperedSignatureToken); ok {
		t.Fatalf("verifyToken accepted tampered signature for %q", user)
	}
}

func TestRequireMediaReturnsUnauthorizedWithoutCookie(t *testing.T) {
	a := testAuthenticator()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	a.RequireMedia(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/media/seg.ts", nil))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestRequireMediaCallsNextWithValidCookie(t *testing.T) {
	a := testAuthenticator()
	token, err := a.newToken("alice")
	if err != nil {
		t.Fatalf("newToken returned error: %v", err)
	}
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/media/seg.ts", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	rr := httptest.NewRecorder()
	a.RequireMedia(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !nextCalled {
		t.Fatal("next handler was not called")
	}
}

func TestRequirePageRedirectsWithoutCookie(t *testing.T) {
	a := testAuthenticator()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	a.RequirePage(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if location := rr.Header().Get("Location"); location != "/login" {
		t.Fatalf("Location = %q, want %q", location, "/login")
	}
}

func TestNoAuthBypassesPageAndMediaGatesWithoutCookie(t *testing.T) {
	a := New(config.Config{NoAuth: true})
	for name, gate := range map[string]func(http.Handler) http.Handler{
		"page":  a.RequirePage,
		"media": a.RequireMedia,
	} {
		t.Run(name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			rr := httptest.NewRecorder()
			gate(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
			}
			if !nextCalled {
				t.Fatal("next handler was not called")
			}
		})
	}
}

func TestLoginSuccessSetsCookieAndRedirects(t *testing.T) {
	a := testAuthenticator()
	mux := http.NewServeMux()
	a.RegisterRoutes(mux)
	form := url.Values{"user": {"alice"}, "pass": {"correct horse battery staple"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusSeeOther)
	}
	if location := rr.Header().Get("Location"); location != "/" {
		t.Fatalf("Location = %q, want %q", location, "/")
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName {
		t.Fatalf("cookie name = %q, want %q", cookie.Name, sessionCookieName)
	}
	if cookie.Value == "" {
		t.Fatal("session cookie value is empty")
	}
	if !cookie.HttpOnly {
		t.Fatal("session cookie is not HttpOnly")
	}
	if !cookie.Secure {
		t.Fatal("session cookie is not Secure")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want %v", cookie.SameSite, http.SameSiteLaxMode)
	}
	if cookie.MaxAge != int((12 * time.Hour).Seconds()) {
		t.Fatalf("MaxAge = %d, want %d", cookie.MaxAge, int((12 * time.Hour).Seconds()))
	}
	if user, ok := a.verifyToken(cookie.Value); !ok || user != "alice" {
		t.Fatalf("cookie token verifies as (%q, %v), want (alice, true)", user, ok)
	}
}

func TestLoginFailureReturnsUnauthorized(t *testing.T) {
	a := testAuthenticator()
	mux := http.NewServeMux()
	a.RegisterRoutes(mux)
	form := url.Values{"user": {"alice"}, "pass": {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rr.Body.String(), "Invalid username or password") {
		t.Fatal("login failure response did not include error message")
	}
}
