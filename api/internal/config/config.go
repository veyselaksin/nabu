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
	// Lexicon is a spelling lexicon built by `nabu-data build-lexicon`. If set,
	// /v1/render uses attested spellings for the words it contains; otherwise
	// every word is spelled phonetically. Env: NABU_LEXICON.
	Lexicon string
	// TTSThreads limits ONNX Runtime threads per request (0 = ORT default).
	// Env: NABU_TTS_THREADS.
	TTSThreads int
	// LLMURL is the root of an OpenAI-compatible chat-completions API (for
	// example llama.cpp's llama-server at http://127.0.0.1:8090/v1). If empty,
	// translation is disabled and /v1/translate returns 503. Env: NABU_LLM_URL.
	LLMURL string
	// LLMModel is the model name sent to that server. Env: NABU_LLM_MODEL.
	LLMModel string
	// LLMAPIKey is sent as a bearer token if set. Env: NABU_LLM_API_KEY.
	LLMAPIKey string
	// TLACorpus is the TLA train.jsonl downloaded by `nabu-data fetch tla`,
	// used for retrieval and the vocabulary check. Required with LLMURL.
	// Env: NABU_TLA_CORPUS.
	TLACorpus string
	// TranslateTimeout bounds POST /v1/translate. Env: NABU_TRANSLATE_TIMEOUT
	// (default 3m: a language model on CPU is slow).
	TranslateTimeout time.Duration
}

// FromEnv builds a Config from getenv (normally os.Getenv).
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		Addr:             ":8080",
		RulesDir:         strings.TrimSpace(getenv("NABU_RULES_DIR")),
		TTSModel:         strings.TrimSpace(getenv("NABU_TTS_MODEL")),
		ORTLibrary:       strings.TrimSpace(getenv("NABU_ORT_LIB")),
		Lexicon:          strings.TrimSpace(getenv("NABU_LEXICON")),
		LLMURL:           strings.TrimSpace(getenv("NABU_LLM_URL")),
		LLMModel:         strings.TrimSpace(getenv("NABU_LLM_MODEL")),
		LLMAPIKey:        strings.TrimSpace(getenv("NABU_LLM_API_KEY")),
		TLACorpus:        strings.TrimSpace(getenv("NABU_TLA_CORPUS")),
		TranslateTimeout: 3 * time.Minute,
		LogLevel:         slog.LevelInfo,
		LogFormat:        "json",
		MaxTextRunes:     2000,
		RequestTimeout:   10 * time.Second,
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
	if v := getenv("NABU_TRANSLATE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return c, fmt.Errorf("NABU_TRANSLATE_TIMEOUT: must be a positive duration such as 3m, got %q", v)
		}
		c.TranslateTimeout = d
	}
	if c.LLMURL != "" && c.TLACorpus == "" {
		return c, fmt.Errorf("NABU_TLA_CORPUS must be set when NABU_LLM_URL is set (run: make data-fetch)")
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
