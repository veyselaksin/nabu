package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistry(t *testing.T) {
	for _, s := range All() {
		if s.Key == "" || s.Name == "" || s.Homepage == "" {
			t.Errorf("%+v: key, name and homepage are required", s)
		}
		if !s.Verified() && len(s.Files) > 0 {
			t.Errorf("%s: an unverified source must not list downloadable files", s.Key)
		}
		if s.Verified() && (s.LicenseURL == "" || s.Attribution == "") {
			t.Errorf("%s: a verified source needs a license URL and an attribution line", s.Key)
		}
		for _, f := range s.Files {
			if len(f.SHA256) != 64 || !strings.HasPrefix(f.URL, "https://") || f.Name == "" || strings.ContainsAny(f.Name, `/\`) {
				t.Errorf("%s: bad file %+v", s.Key, f)
			}
		}
	}
	if _, ok := Get("tla"); !ok {
		t.Error("tla not registered")
	}
	if _, ok := Get("nope"); ok {
		t.Error("Get(nope) found something")
	}
}

func TestDir(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if got := Dir(getenv, "tla"); got != filepath.Join("data", "corpora", "tla") {
		t.Errorf("default Dir = %q", got)
	}
	env["NABU_DATA_DIR"] = "/x"
	if got := Dir(getenv, "tla"); got != filepath.Join("/x", "tla") {
		t.Errorf("Dir = %q", got)
	}
}

func TestFetch(t *testing.T) {
	body := "hello"
	sum := sha256.Sum256([]byte(body))
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("User-Agent") != "nabu-data" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	yes := true
	src := Source{Key: "t", License: "MIT", ShareAlike: &yes, Files: []File{{Name: "f.txt", URL: srv.URL, SHA256: hex.EncodeToString(sum[:])}}}
	dir := t.TempDir()
	paths, err := Fetch(context.Background(), srv.Client(), src, dir, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(paths[0]); string(b) != body {
		t.Errorf("file = %q", b)
	}
	// A second fetch keeps the verified file.
	if _, err := Fetch(context.Background(), srv.Client(), src, dir, io.Discard); err != nil || hits != 1 {
		t.Errorf("refetch: err %v, hits %d (want 1)", err, hits)
	}

	// A checksum mismatch is rejected and leaves no file behind.
	src.Files[0].SHA256 = strings.Repeat("0", 64)
	src.Files[0].Name = "g.txt"
	_, err = Fetch(context.Background(), srv.Client(), src, dir, io.Discard)
	var ce *ChecksumError
	if !errors.As(err, &ce) {
		t.Errorf("err = %v, want *ChecksumError", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries after a failed download, want 1", len(entries))
	}

	// Unverified sources are refused before any request.
	if _, err := Fetch(context.Background(), srv.Client(), Source{Key: "u"}, dir, io.Discard); !errors.Is(err, ErrUnverified) {
		t.Errorf("err = %v, want ErrUnverified", err)
	}
}
