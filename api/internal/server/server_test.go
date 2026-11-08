package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/veyselaksin/nabu/g2p"
	"github.com/veyselaksin/nabu/g2p/rules"
)

// fixture is a synthetic rule set (not linguistic data) so API tests do not
// depend on the placeholder values in the shipped rule files.
const fixture = `
id: test-api
language: egy
name: API test fixture
reference: none (test fixture)
version: "1"
graphemes:
  - { match: "ꜥ", ipa: "<c>" }
  - { match: "n", ipa: "n" }
  - { match: "ḫ", ipa: "<x>" }
`

func newTestServer(t *testing.T, maxRunes int) *httptest.Server {
	t.Helper()
	rs, err := g2p.Parse([]byte(fixture), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New([]*g2p.RuleSet{rs}, Options{MaxTextRunes: maxRunes})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func do(t *testing.T, ts *httptest.Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("%s %s: Content-Type = %q", method, path, ct)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: decoding body: %v", method, path, err)
	}
	return resp.StatusCode, out
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestEndpoints(t *testing.T) {
	ts := newTestServer(t, 10)
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string            // error code, if an error is expected
		wantFields map[string]string // top-level string fields on success
	}{
		{"healthz", "GET", "/healthz", "", 200, "", map[string]string{"status": "ok"}},
		{"g2p ok", "POST", "/v1/g2p", `{"text":"ꜥnḫ","ruleset":"test-api"}`, 200, "",
			map[string]string{"ipa": "<c>n<x>", "ruleset": "test-api", "reference": "none (test fixture)"}},
		{"g2p decomposed input", "POST", "/v1/g2p", `{"text":"ꜥnḫ","ruleset":"test-api"}`, 200, "",
			map[string]string{"ipa": "<c>n<x>"}},
		{"g2p unknown grapheme", "POST", "/v1/g2p", `{"text":"ꜥnz","ruleset":"test-api"}`, 422, "unknown_grapheme", nil},
		{"g2p unknown ruleset", "POST", "/v1/g2p", `{"text":"n","ruleset":"nope"}`, 400, "unknown_ruleset", nil},
		{"g2p missing ruleset", "POST", "/v1/g2p", `{"text":"n"}`, 400, "invalid_request", nil},
		{"g2p bad json", "POST", "/v1/g2p", `{"text":`, 400, "invalid_json", nil},
		{"g2p unknown field", "POST", "/v1/g2p", `{"text":"n","ruleset":"test-api","lang":"x"}`, 400, "invalid_json", nil},
		{"g2p trailing data", "POST", "/v1/g2p", `{"text":"n","ruleset":"test-api"} {}`, 400, "invalid_json", nil},
		{"g2p text too long", "POST", "/v1/g2p", `{"text":"nnnnnnnnnnn","ruleset":"test-api"}`, 413, "text_too_long", nil},
		{"g2p body too large", "POST", "/v1/g2p", `{"text":"` + strings.Repeat("n", 70<<10) + `","ruleset":"test-api"}`, 413, "body_too_large", nil},
		{"g2p wrong method", "GET", "/v1/g2p", "", 405, "method_not_allowed", nil},
		{"translate stub", "POST", "/v1/translate", `{"text":"hello","lang":"egy"}`, 501, "not_implemented", nil},
		{"speak stub", "POST", "/v1/speak", `{"ipa":"x"}`, 501, "not_implemented", nil},
		{"not found", "GET", "/v2/nothing", "", 404, "not_found", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := do(t, ts, tt.method, tt.path, tt.body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %v", status, tt.wantStatus, body)
			}
			if got := errCode(body); got != tt.wantCode {
				t.Errorf("error code = %q, want %q; body = %v", got, tt.wantCode, body)
			}
			for k, want := range tt.wantFields {
				if got, _ := body[k].(string); got != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
		})
	}
}

func TestUnknownGraphemeDetails(t *testing.T) {
	ts := newTestServer(t, 100)
	_, body := do(t, ts, "POST", "/v1/g2p", `{"text":"ꜥnz","ruleset":"test-api"}`)
	details := body["error"].(map[string]any)["details"].(map[string]any)
	if details["grapheme"] != "z" || details["offset"] != float64(2) {
		t.Errorf("details = %v, want grapheme z at offset 2", details)
	}
}

func TestListRulesets(t *testing.T) {
	ts := newTestServer(t, 100)
	status, body := do(t, ts, "GET", "/v1/rulesets", "")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	list, _ := body["rulesets"].([]any)
	if len(list) != 1 {
		t.Fatalf("rulesets = %v, want 1 entry", body["rulesets"])
	}
	got := list[0].(map[string]any)
	for k, want := range map[string]string{"id": "test-api", "language": "egy", "name": "API test fixture", "version": "1"} {
		if got[k] != want {
			t.Errorf("%s = %v, want %q", k, got[k], want)
		}
	}
}

func TestEmbeddedRulesetsLoad(t *testing.T) {
	sets, err := g2p.LoadFS(rules.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(sets, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsDuplicateIDs(t *testing.T) {
	rs, err := g2p.Parse([]byte(fixture), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New([]*g2p.RuleSet{rs, rs}, Options{}); err == nil {
		t.Fatal("expected duplicate id error")
	}
}
