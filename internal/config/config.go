package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config contains all runtime settings consumed by the application.
type Config struct {
	ListenAddr   string // default 127.0.0.1:8080 ; env LISTEN_ADDR
	VideoDir     string // source folder of .mkv files (read-only) ; env VIDEO_DIR
	HLSDir       string // writable dir for generated HLS output ; env HLS_DIR (default StateDir/hls)
	LoginUser    string // env LOGIN_USER or file at LOGIN_USER_FILE
	LoginPass    string // env LOGIN_PASS or file at LOGIN_PASS_FILE
	CookieSecret []byte // base64 from COOKIE_SECRET or file at COOKIE_SECRET_FILE ; must decode >=32 bytes
}

// Load reads environment configuration, applies defaults, and validates required settings.
func Load() (Config, error) {
	loginUser, err := readSecret("LOGIN_USER", "LOGIN_USER_FILE")
	if err != nil {
		return Config{}, err
	}
	loginPass, err := readSecret("LOGIN_PASS", "LOGIN_PASS_FILE")
	if err != nil {
		return Config{}, err
	}
	cookieSecretEncoded, err := readSecret("COOKIE_SECRET", "COOKIE_SECRET_FILE")
	if err != nil {
		return Config{}, err
	}

	var validationErrs []error
	if loginUser == "" {
		validationErrs = append(validationErrs, errors.New("LOGIN_USER or LOGIN_USER_FILE is required"))
	}
	if loginPass == "" {
		validationErrs = append(validationErrs, errors.New("LOGIN_PASS or LOGIN_PASS_FILE is required"))
	}

	cookieSecret, err := base64.StdEncoding.DecodeString(cookieSecretEncoded)
	if err != nil {
		validationErrs = append(validationErrs, fmt.Errorf("COOKIE_SECRET must be base64 encoded: %w", err))
	} else if len(cookieSecret) < 32 {
		validationErrs = append(validationErrs, fmt.Errorf("COOKIE_SECRET must decode to at least 32 bytes, got %d", len(cookieSecret)))
	}

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

	if err := errors.Join(validationErrs...); err != nil {
		return Config{}, err
	}

	return Config{
		ListenAddr:   listenAddr,
		VideoDir:     videoDir,
		HLSDir:       hlsDir,
		LoginUser:    loginUser,
		LoginPass:    loginPass,
		CookieSecret: cookieSecret,
	}, nil
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
