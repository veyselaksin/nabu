package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/veyselaksin/nabu/g2p"
	"github.com/veyselaksin/nabu/script"
	"github.com/veyselaksin/nabu/translate"
)

type fakeTranslator struct {
	res *translate.Result
	err error
	// wait blocks until the request context ends, to test the timeout.
	wait bool
}

func (f *fakeTranslator) Translate(ctx context.Context, text string) (*translate.Result, error) {
	if f.wait {
		<-ctx.Done()
		return nil, &translate.LLMError{Msg: "request failed", Err: ctx.Err()}
	}
	if f.err != nil {
		return nil, f.err
	}
	r := *f.res
	r.Input = text
	return &r, nil
}

func (f *fakeTranslator) Model() string { return "fake-model" }

type fakePing struct{ err error }

func (p fakePing) Ping(context.Context) error { return p.err }

func newTranslateServer(t *testing.T, tr Translator, ping Pinger, timeout time.Duration) *httptest.Server {
	t.Helper()
	rs, err := g2p.Parse([]byte(fixture), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New([]*g2p.RuleSet{rs}, Options{
		Translator: tr, TranslatorPing: ping, TranslateTimeout: timeout, MaxTranslateRunes: 20,
		RequestTimeout: time.Millisecond, // must not apply to /v1/translate
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestTranslate(t *testing.T) {
	ok := &fakeTranslator{res: &translate.Result{
		Transliteration: "ꜥnḫ", Words: []translate.Word{{Form: "ꜥnḫ", Attested: true, Count: 3}},
		Confidence: 1, Keywords: []string{"Leben"}, Examples: []translate.Hit{}, Model: "fake-model",
		Sources: []script.Source{translate.TLASource()},
	}}
	tests := []struct {
		name   string
		tr     *fakeTranslator
		body   string
		status int
		code   string
	}{
		{"ok", ok, `{"text":"life"}`, 200, ""},
		{"ok with lang", ok, `{"text":"life","lang":"egy"}`, 200, ""},
		{"other language", ok, `{"text":"life","lang":"sux"}`, 400, "unsupported_language"},
		{"too long", ok, `{"text":"` + strings.Repeat("a", 21) + `"}`, 413, "text_too_long"},
		{"bad json", ok, `{"text":1}`, 400, "invalid_json"},
		{"empty", &fakeTranslator{err: translate.ErrEmptyInput}, `{"text":" "}`, 400, "invalid_request"},
		{"bad utf8", &fakeTranslator{err: translate.ErrInvalidUTF8}, `{"text":"x"}`, 400, "invalid_utf8"},
		{"model output", &fakeTranslator{err: translate.ErrModelOutput}, `{"text":"x"}`, 502, "bad_model_output"},
		{"llm down", &fakeTranslator{err: &translate.LLMError{Msg: "not reachable"}}, `{"text":"x"}`, 502, "llm_unavailable"},
		{"other error", &fakeTranslator{err: errors.New("boom")}, `{"text":"x"}`, 500, "internal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTranslateServer(t, tc.tr, nil, time.Minute)
			status, body := do(t, ts, "POST", "/v1/translate", tc.body)
			if status != tc.status || errCode(body) != tc.code {
				t.Fatalf("got %d %q, want %d %q: %v", status, errCode(body), tc.status, tc.code, body)
			}
			if status == 200 {
				if body["transliteration"] != "ꜥnḫ" || body["input"] != "life" || body["confidence"] != 1.0 {
					t.Errorf("body = %v", body)
				}
				src := body["sources"].([]any)[0].(map[string]any)
				if src["license"] != "CC-BY-SA-4.0" {
					t.Errorf("sources = %v", body["sources"])
				}
			}
		})
	}
}

func TestTranslateTimeout(t *testing.T) {
	ts := newTranslateServer(t, &fakeTranslator{wait: true}, nil, 50*time.Millisecond)
	status, body := do(t, ts, "POST", "/v1/translate", `{"text":"x"}`)
	if status != http.StatusGatewayTimeout || errCode(body) != "timeout" {
		t.Fatalf("got %d %v", status, body)
	}
}

func TestTranslator(t *testing.T) {
	tr := &fakeTranslator{}
	for _, tc := range []struct {
		name      string
		ping      Pinger
		available bool
	}{
		{"no ping", nil, true},
		{"reachable", fakePing{}, true},
		{"unreachable", fakePing{&translate.LLMError{Msg: "not reachable"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTranslateServer(t, tr, tc.ping, time.Minute)
			status, body := do(t, ts, "GET", "/v1/translator", "")
			if status != 200 || body["available"] != tc.available || body["model"] != "fake-model" {
				t.Fatalf("got %d %v", status, body)
			}
			if msg, _ := body["message"].(string); !tc.available && !strings.Contains(msg, "make run-llm") {
				t.Errorf("message = %q", msg)
			}
		})
	}
}
