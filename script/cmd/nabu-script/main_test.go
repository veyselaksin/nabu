package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

const fixture = "../../testdata/lexicon.tsv"

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		stdin      string
		wantStatus int
		golden     string // compared against stdout
		wantStderr string // substring
	}{
		{name: "plain", args: []string{"ꜥnḫ", "ḏd.ꞽn =f"}, golden: "plain.golden", wantStderr: "not an attested spelling"},
		{name: "json", args: []string{"--json", "ꜥnḫ"}, golden: "json.golden"},
		{name: "lexicon", args: []string{"--lexicon", fixture, "--json", "ꜥnḫ ḥtp"}, golden: "lexicon.golden"},
		{name: "stdin", stdin: "ꜥnḫ\nḥtp\n", golden: "stdin.golden"},
		{name: "no sign", args: []string{"zꜣ", "ḥtp"}, wantStatus: 1, golden: "nosign.golden", wantStderr: `no sign spells "z"`},
		{name: "bad lexicon path", args: []string{"--lexicon", "does-not-exist.tsv", "x"}, wantStatus: 2, wantStderr: "does-not-exist.tsv"},
		{name: "help", args: []string{"-h"}, wantStatus: 0, wantStderr: "usage: nabu-script"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := run(tt.args, strings.NewReader(tt.stdin), &stdout, &stderr)
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d (stderr: %s)", status, tt.wantStatus, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want substring %q", stderr.String(), tt.wantStderr)
			}
			if tt.golden != "" {
				compareGolden(t, filepath.Join("testdata", tt.golden), stdout.Bytes())
			}
		})
	}
}

func compareGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s mismatch\n got: %q\nwant: %q", path, got, want)
	}
}
