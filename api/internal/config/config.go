// Package config reads the API configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Config is the runtime configuration of nabu-api.
type Config struct {
	// Addr is the listen address. Env: NABU_ADDR (default ":8080").
	Addr string
	// RulesDir, if set, loads *.yaml rule files from this directory instead
	// of the rule files embedded in the binary. Env: NABU_RULES_DIR.
	RulesDir string
	// LogLevel is debug, info, warn or error. Env: NABU_LOG_LEVEL (default info).
	LogLevel slog.Level
	// LogFormat is json or text. Env: NABU_LOG_FORMAT (default json).
	LogFormat string
	// MaxTextRunes caps the length of text accepted by /v1/g2p.
	// Env: NABU_MAX_TEXT_RUNES (default 2000).
	MaxTextRunes int
	// RequestTimeout bounds handler run time. Env: NABU_REQUEST_TIMEOUT (default 10s).
	RequestTimeout time.Duration
	// TTSModel is an ONNX model exported by nabu-tts-export. If empty, speech
	// synthesis is disabled and /v1/speak returns 503. Env: NABU_TTS_MODEL.
	TTSModel string
	// ORTLibrary is the ONNX Runtime shared library to load (default: the
	// platform's library name on the loader's search path). Env: NABU_ORT_LIB.
	ORTLibrary string
	// TTSThreads limits ONNX Runtime threads per request (0 = ORT default).
	// Env: NABU_TTS_THREADS.
	TTSThreads int
}

// FromEnv builds a Config from getenv (normally os.Getenv).
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		Addr:           ":8080",
		RulesDir:       strings.TrimSpace(getenv("NABU_RULES_DIR")),
		TTSModel:       strings.TrimSpace(getenv("NABU_TTS_MODEL")),
		ORTLibrary:     strings.TrimSpace(getenv("NABU_ORT_LIB")),
		LogLevel:       slog.LevelInfo,
		LogFormat:      "json",
		MaxTextRunes:   2000,
		RequestTimeout: 10 * time.Second,
	}
	if v := getenv("NABU_ADDR"); v != "" {
		c.Addr = v
	}
	if v := getenv("NABU_LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return c, fmt.Errorf("NABU_LOG_LEVEL: %w", err)
		}
	}
	if v := getenv("NABU_LOG_FORMAT"); v != "" {
		if v != "json" && v != "text" {
			return c, fmt.Errorf("NABU_LOG_FORMAT: must be json or text, got %q", v)
		}
		c.LogFormat = v
	}
	if v := getenv("NABU_MAX_TEXT_RUNES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return c, fmt.Errorf("NABU_MAX_TEXT_RUNES: must be a positive integer, got %q", v)
		}
		c.MaxTextRunes = n
	}
	if v := getenv("NABU_REQUEST_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return c, fmt.Errorf("NABU_REQUEST_TIMEOUT: must be a positive duration such as 10s, got %q", v)
		}
		c.RequestTimeout = d
	}
	if v := getenv("NABU_TTS_THREADS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return c, fmt.Errorf("NABU_TTS_THREADS: must be a non-negative integer, got %q", v)
		}
		c.TTSThreads = n
	}
	return c, nil
}
