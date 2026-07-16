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
	t.Setenv("STATE_DIRECTORY", "")
	t.Setenv("DB_PATH", "")
	t.Setenv("VIDSTREAMER_FLAT_FILE_STATE", "")

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
	if cfg.DBPath != "/state/app.db" {
		t.Fatalf("DBPath = %q, want /state/app.db", cfg.DBPath)
	}
}

func TestLoadRejectsMissingRequiredFields(t *testing.T) {
	for _, name := range []string{"LISTEN_ADDR", "VIDEO_DIR", "HLS_DIR", "LOGIN_USER", "LOGIN_USER_FILE", "LOGIN_PASS", "LOGIN_PASS_FILE", "COOKIE_SECRET", "COOKIE_SECRET_FILE", "STATE_DIRECTORY", "DB_PATH", "VIDSTREAMER_FLAT_FILE_STATE"} {
		t.Setenv(name, "")
	}
	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing required fields error")
	}
}

func TestLoadDevNoAuthDoesNotRequireCredentials(t *testing.T) {
	for _, name := range []string{"LOGIN_USER", "LOGIN_USER_FILE", "LOGIN_PASS", "LOGIN_PASS_FILE", "COOKIE_SECRET", "COOKIE_SECRET_FILE", "VIDSTREAMER_FLAT_FILE_STATE"} {
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
	for _, name := range []string{"LOGIN_USER", "LOGIN_USER_FILE", "LOGIN_PASS", "LOGIN_PASS_FILE", "COOKIE_SECRET", "COOKIE_SECRET_FILE", "VIDSTREAMER_DEV_NOAUTH", "VIDSTREAMER_FLAT_FILE_STATE"} {
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
	for _, name := range []string{"COOKIE_SECRET", "COOKIE_SECRET_FILE", "VIDSTREAMER_DEV_NOAUTH", "STATE_DIRECTORY", "DB_PATH", "VIDSTREAMER_FLAT_FILE_STATE"} {
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

func TestLoadStateDirFromEnv(t *testing.T) {
	for _, name := range []string{"COOKIE_SECRET", "COOKIE_SECRET_FILE", "VIDSTREAMER_DEV_NOAUTH", "HLS_DIR", "DB_PATH", "VIDSTREAMER_FLAT_FILE_STATE"} {
		t.Setenv(name, "")
	}
	stateDir := t.TempDir()
	t.Setenv("STATE_DIRECTORY", stateDir)
	t.Setenv("VIDEO_DIR", "/videos")
	t.Setenv("LOGIN_USER", "alice")
	t.Setenv("LOGIN_PASS", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StateDir != stateDir {
		t.Fatalf("StateDir = %q, want %q", cfg.StateDir, stateDir)
	}
	if cfg.DBPath != filepath.Join(stateDir, "app.db") {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, filepath.Join(stateDir, "app.db"))
	}
}

func TestLoadStateDirDerivedFromHLSDir(t *testing.T) {
	for _, name := range []string{"COOKIE_SECRET", "COOKIE_SECRET_FILE", "VIDSTREAMER_DEV_NOAUTH", "STATE_DIRECTORY", "DB_PATH", "VIDSTREAMER_FLAT_FILE_STATE"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	t.Setenv("VIDEO_DIR", "/videos")
	t.Setenv("HLS_DIR", filepath.Join(dir, "hls"))
	t.Setenv("LOGIN_USER", "alice")
	t.Setenv("LOGIN_PASS", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StateDir != dir {
		t.Fatalf("StateDir = %q, want %q (parent of hlsDir)", cfg.StateDir, dir)
	}
	if cfg.DBPath != filepath.Join(dir, "app.db") {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, filepath.Join(dir, "app.db"))
	}
}

func TestLoadStateDirPopulatedInNoAuth(t *testing.T) {
	for _, name := range []string{"COOKIE_SECRET", "COOKIE_SECRET_FILE", "HLS_DIR", "DB_PATH", "VIDSTREAMER_FLAT_FILE_STATE"} {
		t.Setenv(name, "")
	}
	stateDir := t.TempDir()
	t.Setenv("STATE_DIRECTORY", stateDir)
	t.Setenv("VIDEO_DIR", "/videos")
	t.Setenv("VIDSTREAMER_DEV_NOAUTH", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.NoAuth {
		t.Fatal("NoAuth = false, want true")
	}
	if cfg.StateDir != stateDir {
		t.Fatalf("StateDir = %q, want %q even in NoAuth mode", cfg.StateDir, stateDir)
	}
}

func TestLoadDBPathOverride(t *testing.T) {
	for _, name := range []string{"COOKIE_SECRET", "COOKIE_SECRET_FILE", "HLS_DIR", "VIDSTREAMER_FLAT_FILE_STATE"} {
		t.Setenv(name, "")
	}
	stateDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "custom.db")
	t.Setenv("STATE_DIRECTORY", stateDir)
	t.Setenv("DB_PATH", dbPath)
	t.Setenv("VIDEO_DIR", "/videos")
	t.Setenv("VIDSTREAMER_DEV_NOAUTH", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StateDir != stateDir {
		t.Fatalf("StateDir = %q, want %q", cfg.StateDir, stateDir)
	}
	if cfg.DBPath != dbPath {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, dbPath)
	}
}

func TestLoadFlatFileStateFlag(t *testing.T) {
	for _, name := range []string{"COOKIE_SECRET", "COOKIE_SECRET_FILE", "HLS_DIR", "STATE_DIRECTORY", "DB_PATH"} {
		t.Setenv(name, "")
	}
	t.Setenv("VIDEO_DIR", "/videos")
	t.Setenv("VIDSTREAMER_DEV_NOAUTH", "1")
	t.Setenv("VIDSTREAMER_FLAT_FILE_STATE", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.UseFlatFileState {
		t.Fatal("UseFlatFileState = false, want true")
	}
}

// TestRelativeDirectoryPathsNormalizedToAbsolute verifies that relative
// VideoDir, StateDir, and HLSDir inputs are resolved to absolute paths by
// Load(). Paths are constructed from placeholders; no real media paths or
// personal data are embedded.
func TestRelativeDirectoryPathsNormalizedToAbsolute(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		videoDir     string
		stateDirEnv  string // empty = do not set STATE_DIRECTORY
		hlsDirEnv    string // empty = do not set HLS_DIR
		wantVideoDir string
		wantStateDir string
		wantHLSDir   string
	}{
		{
			name:         "relative VideoDir normalized",
			videoDir:     "rel-video",
			hlsDirEnv:    filepath.Join(cwd, "abs-hls"),
			wantVideoDir: filepath.Join(cwd, "rel-video"),
			wantHLSDir:   filepath.Join(cwd, "abs-hls"),
			wantStateDir: cwd, // parent of abs-hls has no sub-path component
		},
		{
			name:         "relative HLSDir normalized",
			videoDir:     filepath.Join(cwd, "abs-video"),
			hlsDirEnv:    "rel-hls",
			wantVideoDir: filepath.Join(cwd, "abs-video"),
			wantHLSDir:   filepath.Join(cwd, "rel-hls"),
			wantStateDir: cwd, // filepath.Dir("rel-hls") == "." -> abs == cwd
		},
		{
			name:         "explicit relative StateDir normalized",
			videoDir:     filepath.Join(cwd, "abs-video"),
			stateDirEnv:  "rel-state",
			wantVideoDir: filepath.Join(cwd, "abs-video"),
			wantStateDir: filepath.Join(cwd, "rel-state"),
			wantHLSDir:   filepath.Join(cwd, "rel-state", "hls"), // derived from normalized StateDir
		},
		{
			name:         "all relative inputs normalized",
			videoDir:     "rel-video",
			hlsDirEnv:    "rel-hls",
			wantVideoDir: filepath.Join(cwd, "rel-video"),
			wantHLSDir:   filepath.Join(cwd, "rel-hls"),
			wantStateDir: cwd, // filepath.Dir("rel-hls") == "." -> abs == cwd
		},
		{
			name:         "default derived dirs become absolute",
			videoDir:     filepath.Join(cwd, "abs-video"),
			// no HLS_DIR, no STATE_DIRECTORY: defaults to "state" / "state/hls"
			wantVideoDir: filepath.Join(cwd, "abs-video"),
			wantStateDir: filepath.Join(cwd, "state"),
			wantHLSDir:   filepath.Join(cwd, "state", "hls"),
		},
		{
			name:         "absolute inputs unchanged",
			videoDir:     filepath.Join(cwd, "abs-video"),
			stateDirEnv:  filepath.Join(cwd, "abs-state"),
			hlsDirEnv:    filepath.Join(cwd, "abs-hls"),
			wantVideoDir: filepath.Join(cwd, "abs-video"),
			wantStateDir: filepath.Join(cwd, "abs-state"),
			wantHLSDir:   filepath.Join(cwd, "abs-hls"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{
				"HLS_DIR", "STATE_DIRECTORY", "DB_PATH",
				"COOKIE_SECRET", "COOKIE_SECRET_FILE",
				"LOGIN_USER", "LOGIN_USER_FILE", "LOGIN_PASS", "LOGIN_PASS_FILE",
				"LISTEN_ADDR", "VIDSTREAMER_FLAT_FILE_STATE",
			} {
				t.Setenv(name, "")
			}
			t.Setenv("VIDEO_DIR", tc.videoDir)
			t.Setenv("VIDSTREAMER_DEV_NOAUTH", "1")
			if tc.hlsDirEnv != "" {
				t.Setenv("HLS_DIR", tc.hlsDirEnv)
			}
			if tc.stateDirEnv != "" {
				t.Setenv("STATE_DIRECTORY", tc.stateDirEnv)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.VideoDir != tc.wantVideoDir {
				t.Errorf("VideoDir = %q, want %q", cfg.VideoDir, tc.wantVideoDir)
			}
			if cfg.HLSDir != tc.wantHLSDir {
				t.Errorf("HLSDir = %q, want %q", cfg.HLSDir, tc.wantHLSDir)
			}
			if cfg.StateDir != tc.wantStateDir {
				t.Errorf("StateDir = %q, want %q", cfg.StateDir, tc.wantStateDir)
			}

			// Invariant: all directory paths must be absolute regardless of input form.
			for _, p := range []string{cfg.VideoDir, cfg.HLSDir, cfg.StateDir} {
				if !filepath.IsAbs(p) {
					t.Errorf("path %q is not absolute", p)
				}
			}
		})
	}
}
