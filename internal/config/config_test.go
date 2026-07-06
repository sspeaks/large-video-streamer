package config

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsSecretsFromFilesAndDefaults(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("VIDEO_DIR", "/videos")
	t.Setenv("HLS_DIR", "/state/hls")

	dir := t.TempDir()
	userFile := filepath.Join(dir, "user")
	passFile := filepath.Join(dir, "pass")
	secretFile := filepath.Join(dir, "secret")
	if err := os.WriteFile(userFile, []byte("alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passFile, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	if err := os.WriteFile(secretFile, []byte(encoded+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOGIN_USER_FILE", userFile)
	t.Setenv("LOGIN_PASS_FILE", passFile)
	t.Setenv("COOKIE_SECRET_FILE", secretFile)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.LoginUser != "alice" || cfg.LoginPass != "secret" {
		t.Fatalf("credentials not read from files: %#v", cfg)
	}
	if len(cfg.CookieSecret) != 32 {
		t.Fatalf("CookieSecret length = %d", len(cfg.CookieSecret))
	}
}

func TestLoadRejectsMissingRequiredFields(t *testing.T) {
	for _, name := range []string{"LISTEN_ADDR", "VIDEO_DIR", "HLS_DIR", "LOGIN_USER", "LOGIN_USER_FILE", "LOGIN_PASS", "LOGIN_PASS_FILE", "COOKIE_SECRET", "COOKIE_SECRET_FILE", "STATE_DIRECTORY"} {
		t.Setenv(name, "")
	}
	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing required fields error")
	}
}

func TestLoadDevNoAuthDoesNotRequireCredentials(t *testing.T) {
	for _, name := range []string{"LOGIN_USER", "LOGIN_USER_FILE", "LOGIN_PASS", "LOGIN_PASS_FILE", "COOKIE_SECRET", "COOKIE_SECRET_FILE"} {
		t.Setenv(name, "")
	}
	t.Setenv("VIDEO_DIR", "/videos")
	t.Setenv("VIDSTREAMER_DEV_NOAUTH", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.NoAuth {
		t.Fatal("NoAuth = false, want true")
	}
	if cfg.LoginUser != "" || cfg.LoginPass != "" {
		t.Fatalf("credentials = (%q, %q), want empty", cfg.LoginUser, cfg.LoginPass)
	}
	if len(cfg.CookieSecret) < 32 {
		t.Fatalf("CookieSecret length = %d, want at least 32", len(cfg.CookieSecret))
	}
}

func TestLoadWithoutDevNoAuthStillRequiresCredentials(t *testing.T) {
	for _, name := range []string{"LOGIN_USER", "LOGIN_USER_FILE", "LOGIN_PASS", "LOGIN_PASS_FILE", "COOKIE_SECRET", "COOKIE_SECRET_FILE", "VIDSTREAMER_DEV_NOAUTH"} {
		t.Setenv(name, "")
	}
	t.Setenv("VIDEO_DIR", "/videos")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want credentials required without VIDSTREAMER_DEV_NOAUTH")
	}
}

func TestLoadAutoGeneratesAndPersistsCookieSecret(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"COOKIE_SECRET", "COOKIE_SECRET_FILE", "VIDSTREAMER_DEV_NOAUTH", "STATE_DIRECTORY"} {
		t.Setenv(name, "")
	}
	t.Setenv("VIDEO_DIR", "/videos")
	t.Setenv("HLS_DIR", filepath.Join(dir, "hls")) // parent dir => secret persisted at <dir>/cookie-secret
	t.Setenv("LOGIN_USER", "alice")
	t.Setenv("LOGIN_PASS", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.CookieSecret) < 32 {
		t.Fatalf("CookieSecret length = %d, want >= 32", len(cfg.CookieSecret))
	}
	secretPath := filepath.Join(dir, "cookie-secret")
	if _, err := os.Stat(secretPath); err != nil {
		t.Fatalf("expected persisted cookie secret at %q: %v", secretPath, err)
	}

	// A second Load must return the SAME persisted secret (stable across restarts).
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}
	if !bytes.Equal(cfg.CookieSecret, cfg2.CookieSecret) {
		t.Fatal("cookie secret changed between loads; expected persisted secret to be reused")
	}
}
