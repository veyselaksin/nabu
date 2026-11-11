package config

import (
	"log/slog"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestFromEnvDefaults(t *testing.T) {
	c, err := FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.LogLevel != slog.LevelInfo || c.LogFormat != "json" ||
		c.MaxTextRunes != 2000 || c.RequestTimeout != 10*time.Second || c.RulesDir != "" ||
		c.TTSModel != "" || c.ORTLibrary != "" || c.TTSThreads != 0 || c.Lexicon != "" ||
		c.LLMURL != "" || c.TLACorpus != "" || c.TranslateTimeout != 3*time.Minute {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	c, err := FromEnv(env(map[string]string{
		"NABU_ADDR": "127.0.0.1:9000", "NABU_RULES_DIR": "/rules", "NABU_LOG_LEVEL": "debug",
		"NABU_LOG_FORMAT": "text", "NABU_MAX_TEXT_RUNES": "10", "NABU_REQUEST_TIMEOUT": "2s",
		"NABU_TTS_MODEL": "/m/model.onnx", "NABU_ORT_LIB": "/lib/libonnxruntime.so.1.31.0", "NABU_TTS_THREADS": "2",
		"NABU_LEXICON": "/data/lexicon-egy.tsv", "NABU_LLM_URL": "http://127.0.0.1:8090/v1",
		"NABU_LLM_MODEL": "qwen", "NABU_LLM_API_KEY": "k", "NABU_TLA_CORPUS": "/data/train.jsonl",
		"NABU_TRANSLATE_TIMEOUT": "90s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Addr: "127.0.0.1:9000", RulesDir: "/rules", LogLevel: slog.LevelDebug,
		LogFormat: "text", MaxTextRunes: 10, RequestTimeout: 2 * time.Second,
		TTSModel: "/m/model.onnx", ORTLibrary: "/lib/libonnxruntime.so.1.31.0", TTSThreads: 2,
		Lexicon: "/data/lexicon-egy.tsv", LLMURL: "http://127.0.0.1:8090/v1", LLMModel: "qwen", LLMAPIKey: "k",
		TLACorpus: "/data/train.jsonl", TranslateTimeout: 90 * time.Second}
	if c != want {
		t.Errorf("got %+v, want %+v", c, want)
	}
}

func TestFromEnvErrors(t *testing.T) {
	for _, kv := range [][2]string{
		{"NABU_LOG_LEVEL", "loud"},
		{"NABU_LOG_FORMAT", "xml"},
		{"NABU_MAX_TEXT_RUNES", "0"},
		{"NABU_MAX_TEXT_RUNES", "many"},
		{"NABU_REQUEST_TIMEOUT", "soon"},
		{"NABU_TTS_THREADS", "-1"},
		{"NABU_TRANSLATE_TIMEOUT", "0s"},
		{"NABU_LLM_URL", "http://127.0.0.1:8090/v1"}, // without NABU_TLA_CORPUS
	} {
		if _, err := FromEnv(env(map[string]string{kv[0]: kv[1]})); err == nil {
			t.Errorf("%s=%q: expected error", kv[0], kv[1])
		}
	}
}
