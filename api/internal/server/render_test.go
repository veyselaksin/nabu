package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/g2p"
	"github.com/veyselaksin/nabu/script"
)

// The lexicon is the synthetic fixture shared with the script package.
const lexiconFixture = "../../../script/testdata/lexicon.tsv"

// renderRulesFixture covers "z", which has IPA but no hieroglyph sign.
const renderRulesFixture = `
id: test-render
language: egy
name: Render test fixture
reference: none (test fixture)
version: "1"
graphemes:
  - { match: "ꜥ", ipa: "ʕ" }
  - { match: "n", ipa: "n" }
  - { match: "ḫ", ipa: "χ" }
  - { match: "z", ipa: "z" }
`

func newRenderServer(t *testing.T, withLexicon bool, synth Synthesizer) *httptest.Server {
	t.Helper()
	var lex *script.Lexicon
	if withLexicon {
		var err error
		if lex, err = script.LoadLexicon(lexiconFixture); err != nil {
			t.Fatal(err)
		}
	}
	r, err := script.Egyptian(lex)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := g2p.Parse([]byte(renderRulesFixture), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New([]*g2p.RuleSet{rs}, Options{Renderer: r, Synthesizer: synth, MaxTextRunes: 20})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestRender(t *testing.T) {
	ts := newRenderServer(t, true, nil)
	status, body := do(t, ts, "POST", "/v1/render", `{"text":"ꜥnḫ ḥtp"}`)
	if status != 200 {
		t.Fatalf("status %d: %v", status, body)
	}
	if body["hieroglyphs"] != "𓀀 𓎛𓏏𓊪" || body["lexicon"] != true || body["input"] != "ꜥnḫ ḥtp" {
		t.Errorf("body = %v", body)
	}
	words, _ := body["words"].([]any)
	if len(words) != 2 {
		t.Fatalf("words = %v", body["words"])
	}
	w0, _ := words[0].(map[string]any)
	w1, _ := words[1].(map[string]any)
	if w0["method"] != "lexicon" || w0["attestation"] == nil || w1["method"] != "phonetic" || w1["signs"] == nil {
		t.Errorf("words = %v", words)
	}
	sources, _ := body["sources"].([]any)
	if len(sources) != 2 {
		t.Errorf("sources = %v", body["sources"])
	}
}

func TestRenderWithoutLexicon(t *testing.T) {
	ts := newRenderServer(t, false, nil)
	status, body := do(t, ts, "POST", "/v1/render", `{"text":"ꜥnḫ"}`)
	if status != 200 || body["hieroglyphs"] != "𓂝𓈖𓐍" || body["lexicon"] != false {
		t.Errorf("status %d body %v", status, body)
	}
}

func TestRenderErrors(t *testing.T) {
	ts := newRenderServer(t, false, nil)
	tests := []struct {
		body       string
		wantStatus int
		wantCode   string
	}{
		{`{"text":"zꜣ"}`, 422, "no_sign"},
		{`{"text":"ꜥnḫ ꜥnḫ ꜥnḫ ꜥnḫ ꜥnḫ ꜥnḫ"}`, 413, "text_too_long"},
		{`{"text":"x","ruleset":"egy"}`, 400, "invalid_json"},
		{`{`, 400, "invalid_json"},
	}
	for _, tt := range tests {
		status, body := do(t, ts, "POST", "/v1/render", tt.body)
		if status != tt.wantStatus || errCode(body) != tt.wantCode {
			t.Errorf("%s: status %d code %q; want %d %q", tt.body, status, errCode(body), tt.wantStatus, tt.wantCode)
		}
	}
	_, body := do(t, ts, "POST", "/v1/render", `{"text":"ꜥnḫ zꜣ"}`)
	details, _ := body["error"].(map[string]any)["details"].(map[string]any)
	if details["grapheme"] != "z" || details["offset"] != float64(4) || details["word"] != "zꜣ" {
		t.Errorf("details = %v", details)
	}
}

func TestRenderUnavailable(t *testing.T) {
	ts := newTestServer(t, 10)
	status, body := do(t, ts, "POST", "/v1/render", `{"text":"ꜥnḫ"}`)
	if status != 503 || errCode(body) != "render_unavailable" {
		t.Errorf("status %d body %v", status, body)
	}
}

func TestSpeakHieroglyphsHeader(t *testing.T) {
	ts := newRenderServer(t, false, &fakeSynth{})
	for text, want := range map[string]string{
		"ꜥnḫ": "𓂝𓈖𓐍",
		// "z" has an IPA rule in the fixture but no sign: speech still works,
		// the header is just omitted.
		"ꜥnz": "",
	} {
		resp, err := http.Post(ts.URL+"/v1/speak", "application/json",
			strings.NewReader(`{"text":"`+text+`","ruleset":"test-render"}`))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d", text, resp.StatusCode)
		}
		got, err := url.PathUnescape(resp.Header.Get("X-Nabu-Hieroglyphs"))
		if err != nil || got != want {
			t.Errorf("%s: X-Nabu-Hieroglyphs = %q (%v), want %q", text, got, err, want)
		}
	}
	resp, _ := http.Post(ts.URL+"/v1/speak", "application/json", strings.NewReader(`{"text":"ꜥn","ruleset":"test-render"}`))
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Access-Control-Expose-Headers"), "X-Nabu-Hieroglyphs") {
		t.Error("X-Nabu-Hieroglyphs not exposed to browsers")
	}
}
