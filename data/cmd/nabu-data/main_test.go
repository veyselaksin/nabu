package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/data/source"
	"github.com/veyselaksin/nabu/script"
)

func runCmd(t *testing.T, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := run(context.Background(), args, func(k string) string { return env[k] }, &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestUsage(t *testing.T) {
	tests := []struct {
		args       []string
		wantStatus int
		wantStderr string
	}{
		{nil, 2, "usage: nabu-data"},
		{[]string{"help"}, 0, "usage: nabu-data"},
		{[]string{"bogus"}, 2, `unknown command "bogus"`},
		{[]string{"fetch"}, 2, "at least one source"},
		{[]string{"fetch", "nope"}, 2, `unknown source "nope"`},
		{[]string{"fetch", "oracc"}, 1, "not verified"},
		{[]string{"build-lexicon", "--out", "script/signs/x.tsv"}, 2, "must not be written into script/"},
		{[]string{"build-signs", "-h"}, 0, "-unikemet"},
	}
	for _, tt := range tests {
		status, _, stderr := runCmd(t, map[string]string{"NABU_DATA_DIR": t.TempDir()}, tt.args...)
		if status != tt.wantStatus || !strings.Contains(stderr, tt.wantStderr) {
			t.Errorf("%q: status %d stderr %q; want %d and %q", tt.args, status, stderr, tt.wantStatus, tt.wantStderr)
		}
	}
}

func TestSources(t *testing.T) {
	status, out, _ := runCmd(t, map[string]string{"NABU_DATA_DIR": "/d"}, "sources")
	if status != 0 || !strings.Contains(out, "CC-BY-SA-4.0") || !strings.Contains(out, "UNVERIFIED") ||
		!strings.Contains(out, filepath.Join("/d", "tla")) {
		t.Errorf("status %d, output:\n%s", status, out)
	}
}

func TestBuildNeedsDownloads(t *testing.T) {
	status, _, stderr := runCmd(t, map[string]string{"NABU_DATA_DIR": t.TempDir()}, "build-signs", "--out", filepath.Join(t.TempDir(), "s.tsv"))
	if status != 1 || !strings.Contains(stderr, "run: nabu-data fetch unikemet") {
		t.Errorf("status %d stderr %q", status, stderr)
	}
}

// TestCommittedSignListIsReproducible rebuilds the shipped sign list from the
// pinned upstream files, when they have been downloaded (nabu-data fetch
// unikemet tla), and checks it matches the committed file byte for byte.
func TestCommittedSignListIsReproducible(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	env := map[string]string{"NABU_DATA_DIR": filepath.Join(root, "data", "corpora")}
	for _, key := range []string{"unikemet", "tla"} {
		src, _ := source.Get(key)
		if err := source.VerifyFile(filepath.Join(source.Dir(func(k string) string { return env[k] }, key), src.Files[0].Name), src.Files[0].SHA256); err != nil {
			t.Skipf("upstream data not downloaded (%v); run: go run ./data/cmd/nabu-data fetch unikemet tla", err)
		}
	}
	out := filepath.Join(t.TempDir(), "signs.tsv")
	if status, _, stderr := runCmd(t, env, "build-signs", "--out", out); status != 0 {
		t.Fatalf("build-signs: %s", stderr)
	}
	got, _ := os.ReadFile(out)
	committed := filepath.Join(root, "script", "signs", "egy-unikemet-uniliterals.tsv")
	want, err := os.ReadFile(committed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from a fresh build; regenerate it with `make signs`", committed)
	}

	lex := filepath.Join(t.TempDir(), "lexicon.tsv")
	if status, _, stderr := runCmd(t, env, "build-lexicon", "--out", lex); status != 0 {
		t.Fatalf("build-lexicon: %s", stderr)
	}
	lx, err := script.LoadLexicon(lex)
	if err != nil {
		t.Fatal(err)
	}
	if lx.Len() < 1000 || lx.Source.License != "CC-BY-SA-4.0" || lx.Source.Attribution == "" {
		t.Errorf("lexicon: %d forms, source %+v", lx.Len(), lx.Source)
	}
	if _, err := script.Egyptian(lx); err != nil {
		t.Errorf("renderer rejects the built lexicon: %v", err)
	}
}
