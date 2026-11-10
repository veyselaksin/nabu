package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/g2p"
	"github.com/veyselaksin/nabu/tts"
)

// fakeSynth stands in for *tts.Model: it tokenizes with the real symbol
// table (so unknown-symbol handling is exercised) and returns one sample per
// token, without ONNX Runtime.
type fakeSynth struct {
	last   tts.Params
	lastIP string
}

func (f *fakeSynth) Info() tts.Info {
	return tts.Info{Name: "fake@1", SampleRate: 8000, Speakers: []string{"a", "b"}, Languages: []string{"x"}}
}

func (f *fakeSynth) Synthesize(_ context.Context, ipa string, p tts.Params) (*tts.Audio, error) {
	f.last, f.lastIP = p, ipa
	ids, err := tts.DefaultSymbols().Tokenize(ipa)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, tts.ErrEmptyInput
	}
	if p.Speaker < 0 || p.Speaker > 1 || (p.Speed != 0 && (p.Speed < tts.MinSpeed || p.Speed > tts.MaxSpeed)) {
		return nil, tts.ErrInvalidParams
	}
	return &tts.Audio{Samples: make([]float32, len(ids)), SampleRate: 8000}, nil
}

// speakFixture maps graphemes to real IPA, plus one symbol the voice lacks
// ("ʧ") and one that maps to nothing (".").
const speakFixture = `
id: test-speak
language: egy
name: Speak test fixture
reference: Fixture, A. (2020). Ünïcode Reference.
version: "2"
graphemes:
  - { match: "ꜥ", ipa: "ʕ" }
  - { match: "n", ipa: "n" }
  - { match: "ḫ", ipa: "χ" }
  - { match: "c", ipa: "ʧ" }
  - { match: ".", ipa: "" }
`

func newSpeakServer(t *testing.T, synth Synthesizer) *httptest.Server {
	t.Helper()
	rs, err := g2p.Parse([]byte(speakFixture), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New([]*g2p.RuleSet{rs}, Options{Synthesizer: synth, MaxTextRunes: 20})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func post(t *testing.T, ts *httptest.Server, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(ts.URL+"/v1/speak", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestSpeakOK(t *testing.T) {
	synth := &fakeSynth{}
	ts := newSpeakServer(t, synth)
	resp := post(t, ts, `{"text":"ꜥnḫ ꜥn","ruleset":"test-speak","speaker":1,"speed":1.5}`)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "audio/wav" {
		t.Errorf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body[:4]) != "RIFF" || string(body[8:12]) != "WAVE" {
		t.Fatalf("not a WAV file: %x", body[:12])
	}
	if sr := binary.LittleEndian.Uint32(body[24:]); sr != 8000 {
		t.Errorf("sample rate %d", sr)
	}
	if n := binary.LittleEndian.Uint32(body[40:]); n != 2*6 { // "ʕnχ ʕn" = 6 tokens
		t.Errorf("data bytes %d", n)
	}
	decode := func(h string) string {
		v, err := url.PathUnescape(resp.Header.Get(h))
		if err != nil {
			t.Fatalf("%s: %v", h, err)
		}
		return v
	}
	for h, want := range map[string]string{
		"X-Nabu-IPA":             "ʕnχ ʕn",
		"X-Nabu-Ruleset":         "test-speak",
		"X-Nabu-Ruleset-Version": "2",
		"X-Nabu-Reference":       "Fixture, A. (2020). Ünïcode Reference.",
		"X-Nabu-Model":           "fake@1",
		"X-Nabu-Speaker":         "1",
	} {
		if got := decode(h); got != want {
			t.Errorf("%s = %q, want %q", h, got, want)
		}
	}
	for _, h := range []string{"X-Nabu-IPA", "X-Nabu-Reference"} {
		for _, c := range resp.Header.Get(h) {
			if c > 0x7e || c < 0x20 {
				t.Errorf("%s contains non-ASCII/control character %q", h, c)
			}
		}
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Expose-Headers"), "X-Nabu-IPA") {
		t.Error("X-Nabu-IPA not exposed to browsers")
	}
	if synth.last.Speaker != 1 || synth.last.Speed != 1.5 || synth.lastIP != "ʕnχ ʕn" {
		t.Errorf("synth got %+v %q", synth.last, synth.lastIP)
	}
}

func TestSpeakErrors(t *testing.T) {
	ts := newSpeakServer(t, &fakeSynth{})
	for _, tt := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"unknown ipa symbol", `{"text":"nc","ruleset":"test-speak"}`, 422, "unknown_ipa_symbol"},
		{"nothing to speak", `{"text":"..","ruleset":"test-speak"}`, 422, "nothing_to_speak"},
		{"unknown grapheme", `{"text":"nz","ruleset":"test-speak"}`, 422, "unknown_grapheme"},
		{"unknown ruleset", `{"text":"n","ruleset":"nope"}`, 400, "unknown_ruleset"},
		{"bad speaker", `{"text":"n","ruleset":"test-speak","speaker":7}`, 400, "invalid_request"},
		{"bad speed", `{"text":"n","ruleset":"test-speak","speed":9}`, 400, "invalid_request"},
		{"zero speed", `{"text":"n","ruleset":"test-speak","speed":0}`, 400, "invalid_request"},
		{"unknown field", `{"text":"n","ruleset":"test-speak","voice":"x"}`, 400, "invalid_json"},
		{"too long", `{"text":"nnnnnnnnnnnnnnnnnnnnn","ruleset":"test-speak"}`, 413, "text_too_long"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := post(t, ts, tt.body)
			var body errorBody
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.status || body.Error.Code != tt.code {
				t.Errorf("got %d %s (%s), want %d %s", resp.StatusCode, body.Error.Code, body.Error.Message, tt.status, tt.code)
			}
		})
	}
	resp := post(t, ts, `{"text":"nc","ruleset":"test-speak"}`)
	var body errorBody
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Error.Details["symbol"] != "ʧ" || body.Error.Details["ipa"] != "nʧ" {
		t.Errorf("details = %v", body.Error.Details)
	}
}

func TestVoices(t *testing.T) {
	ts := newSpeakServer(t, &fakeSynth{})
	status, body := do(t, ts, "GET", "/v1/voices", "")
	if status != 200 || body["available"] != true {
		t.Fatalf("status %d body %v", status, body)
	}
	model := body["model"].(map[string]any)
	if model["name"] != "fake@1" || len(model["speakers"].([]any)) != 2 {
		t.Errorf("model = %v", model)
	}

	off := newTestServer(t, 10)
	_, body = do(t, off, "GET", "/v1/voices", "")
	if body["available"] != false || !strings.Contains(body["message"].(string), "NABU_TTS_MODEL") {
		t.Errorf("voices without model = %v", body)
	}
}
