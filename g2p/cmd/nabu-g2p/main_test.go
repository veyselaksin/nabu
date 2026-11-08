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

const fixture = "../../testdata/rules/test-graphemes.yaml"

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		stdin      string
		wantStatus int
		golden     string // compared against stdout
		wantStderr string // substring
	}{
		{name: "plain", args: []string{"--rules", fixture, "ꜥnḫ"}, golden: "plain.golden"},
		{name: "json", args: []string{"--rules", fixture, "--json", "ꜥnḫ", "ḥt"}, golden: "json.golden"},
		{name: "stdin", args: []string{"--rules", fixture}, stdin: "ꜥnḫ\nḥt\n", golden: "stdin.golden"},
		{name: "unknown grapheme", args: []string{"--rules", fixture, "ꜥnz", "ḥt"}, wantStatus: 1, golden: "unknown.golden", wantStderr: `unknown grapheme "z"`},
		{name: "missing rules flag", args: []string{"ꜥnḫ"}, wantStatus: 2, wantStderr: "--rules is required"},
		{name: "bad rules path", args: []string{"--rules", "does-not-exist.yaml", "x"}, wantStatus: 2, wantStderr: "does-not-exist.yaml"},
		{name: "help", args: []string{"-h"}, wantStatus: 0, wantStderr: "usage: nabu-g2p"},
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
