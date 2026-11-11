package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const corpus = "../../../data/corpora/tla/train.jsonl"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		code int
		want string
	}{
		{"help", []string{"-h"}, nil, 0, "usage:"},
		{"no llm", []string{"hello"}, nil, 2, "NABU_LLM_URL"},
		{"no text", []string{}, map[string]string{"NABU_LLM_URL": "http://x"}, 2, "usage:"},
		{"bad flag", []string{"--nope"}, nil, 2, "flag provided but not defined"},
		{"missing corpus", []string{"--tla", "nope.jsonl", "hello"}, map[string]string{"NABU_LLM_URL": "http://x"}, 2, "make data-fetch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), tc.args, env(tc.env), &stdout, &stderr)
			if code != tc.code || !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("code %d, stderr %q; want %d and %q", code, stderr.String(), tc.code, tc.want)
			}
		})
	}
}

// The tests below need the real corpus (make data-fetch).
func needCorpus(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(corpus); err != nil {
		t.Skip("TLA corpus not downloaded (make data-fetch)")
	}
}

func TestSearch(t *testing.T) {
	needCorpus(t)
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"--tla", corpus, "--search", "König", "--examples", "3"}, env(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	if n := strings.Count(stdout.String(), "\n"); n != 6 {
		t.Errorf("want 3 hits (6 lines), got:\n%s", stdout.String())
	}
}

func TestTranslateWithFakeModel(t *testing.T) {
	needCorpus(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reply := `{\"transliteration\": \"nsw.t zzzq\", \"notes\": \"n\"}`
		if strings.Contains(string(body), "keywords") {
			reply = `{\"keywords\": [\"König\"]}`
		}
		_, _ = io.WriteString(w, `{"choices": [{"message": {"content": "`+reply+`"}}]}`)
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--tla", corpus, "--llm-url", srv.URL, "the", "king"}, env(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	if stdout.String() != "nsw.t zzzq\n" {
		t.Errorf("stdout = %q", stdout.String())
	}
	for _, want := range []string{"confidence 0.50", "not attested: zzzq", "model notes: n", "CC BY-SA 4.0"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr.String())
		}
	}
}
