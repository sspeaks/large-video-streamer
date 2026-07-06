package config

import (
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
