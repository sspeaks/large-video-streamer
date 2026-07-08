package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config contains all runtime settings consumed by the application.
type Config struct {
	ListenAddr       string // default 127.0.0.1:8080 ; env LISTEN_ADDR
	VideoDir         string // source folder of .mkv files (read-only) ; env VIDEO_DIR
	HLSDir           string // writable dir for generated HLS output ; env HLS_DIR (default StateDir/hls)
	LoginUser        string // env LOGIN_USER or file at LOGIN_USER_FILE
	LoginPass        string // env LOGIN_PASS or file at LOGIN_PASS_FILE
	CookieSecret     []byte // base64 from COOKIE_SECRET or file at COOKIE_SECRET_FILE ; must decode >=32 bytes
	NoAuth           bool   // env VIDSTREAMER_DEV_NOAUTH=1/true disables auth for local development
	SegmentOnStart   bool   // env VIDSTREAMER_SEGMENT_ON_START=1/true segments videos at startup
	StateDir         string // writable dir for server state (cookie-secret, shares.json, app.db) ; STATE_DIRECTORY env else parent of HLSDir
	DBPath           string // SQLite database path ; env DB_PATH (default StateDir/app.db)
	UseFlatFileState bool   // env VIDSTREAMER_FLAT_FILE_STATE=1/true uses legacy flat-file stores instead of SQLite
}

// Load reads environment configuration, applies defaults, and validates required settings.
func Load() (Config, error) {
	noAuth := truthyEnv("VIDSTREAMER_DEV_NOAUTH")
	segmentOnStart := truthyEnv("VIDSTREAMER_SEGMENT_ON_START")
	useFlatFileState := truthyEnv("VIDSTREAMER_FLAT_FILE_STATE")
	var loginUser, loginPass string
	var cookieSecret []byte
	var validationErrs []error

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = "127.0.0.1:8080"
	}

	videoDir := os.Getenv("VIDEO_DIR")
	if videoDir == "" {
		validationErrs = append(validationErrs, errors.New("VIDEO_DIR is required"))
	}

	hlsDir := os.Getenv("HLS_DIR")
	if hlsDir == "" {
		stateDir := os.Getenv("STATE_DIRECTORY")
		if stateDir == "" {
			stateDir = "state"
		}
		hlsDir = filepath.Join(stateDir, "hls")
	}

	// Resolve the writable state directory unconditionally (independent of the
	// auth branch) so it is populated even in NoAuth mode. This is the same
	// directory that holds persisted server state such as cookie-secret,
	// shares.json, and app.db:
	// STATE_DIRECTORY when set by systemd, otherwise the parent of hlsDir.
	stateDir := os.Getenv("STATE_DIRECTORY")
	if stateDir == "" {
		stateDir = filepath.Dir(hlsDir)
	}
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(stateDir, "app.db")
	}

	if noAuth {
		cookieSecret = make([]byte, 32)
		if _, err := rand.Read(cookieSecret); err != nil {
			validationErrs = append(validationErrs, fmt.Errorf("generate dev cookie secret: %w", err))
		}
	} else {
		var err error
		loginUser, err = readSecret("LOGIN_USER", "LOGIN_USER_FILE")
		if err != nil {
			return Config{}, err
		}
		loginPass, err = readSecret("LOGIN_PASS", "LOGIN_PASS_FILE")
		if err != nil {
			return Config{}, err
		}

		if loginUser == "" {
			validationErrs = append(validationErrs, errors.New("LOGIN_USER or LOGIN_USER_FILE is required"))
		}
		if loginPass == "" {
			validationErrs = append(validationErrs, errors.New("LOGIN_PASS or LOGIN_PASS_FILE is required"))
		}

		// Only resolve/persist the cookie secret when the login credentials are
		// present; if creds are missing the config is already invalid and we must
		// not create side effects (a persisted secret file).
		if loginUser != "" && loginPass != "" {
			secret, err := resolveCookieSecret(stateDir)
			if err != nil {
				validationErrs = append(validationErrs, err)
			} else {
				cookieSecret = secret
			}
		}
	}

	if err := errors.Join(validationErrs...); err != nil {
		return Config{}, err
	}

	return Config{
		ListenAddr:       listenAddr,
		VideoDir:         videoDir,
		HLSDir:           hlsDir,
		LoginUser:        loginUser,
		LoginPass:        loginPass,
		CookieSecret:     cookieSecret,
		NoAuth:           noAuth,
		SegmentOnStart:   segmentOnStart,
		StateDir:         stateDir,
		DBPath:           dbPath,
		UseFlatFileState: useFlatFileState,
	}, nil
}

// resolveCookieSecret returns the HMAC signing key for session cookies.
//
// If COOKIE_SECRET or COOKIE_SECRET_FILE is provided, it is used (and must be
// base64-encoded and decode to at least 32 bytes). Otherwise a random 32-byte
// secret is generated once and persisted to <stateDir>/cookie-secret so it is
// stable across restarts. stateDir is the caller-resolved state directory
// (STATE_DIRECTORY when set by systemd, otherwise the parent of hlsDir). This
// lets operators manage only the username/password: the cookie secret is
// internal server state, not a credential.
func resolveCookieSecret(stateDir string) ([]byte, error) {
	if os.Getenv("COOKIE_SECRET") != "" || os.Getenv("COOKIE_SECRET_FILE") != "" {
		encoded, err := readSecret("COOKIE_SECRET", "COOKIE_SECRET_FILE")
		if err != nil {
			return nil, err
		}
		secret, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("COOKIE_SECRET must be base64 encoded: %w", err)
		}
		if len(secret) < 32 {
			return nil, fmt.Errorf("COOKIE_SECRET must decode to at least 32 bytes, got %d", len(secret))
		}
		return secret, nil
	}

	dir := stateDir
	path := filepath.Join(dir, "cookie-secret")

	if data, err := os.ReadFile(path); err == nil {
		if secret, derr := base64.StdEncoding.DecodeString(strings.TrimRight(string(data), "\r\n")); derr == nil && len(secret) >= 32 {
			return secret, nil
		}
		// Corrupt/short persisted secret: fall through and regenerate.
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate cookie secret: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state dir %q for cookie secret: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".cookie-secret-*")
	if err != nil {
		return nil, fmt.Errorf("persist cookie secret: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(base64.StdEncoding.EncodeToString(secret)); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return nil, fmt.Errorf("persist cookie secret: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return nil, fmt.Errorf("persist cookie secret: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return nil, fmt.Errorf("persist cookie secret: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return nil, fmt.Errorf("persist cookie secret: %w", err)
	}
	return secret, nil
}

func truthyEnv(name string) bool {
	value := os.Getenv(name)
	return value == "1" || strings.EqualFold(value, "true")
}

func readSecret(envName, fileEnvName string) (string, error) {
	if path := os.Getenv(fileEnvName); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s path %q: %w", fileEnvName, path, err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	return os.Getenv(envName), nil
}
