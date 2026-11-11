// Package server implements the Nabu HTTP API.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/veyselaksin/nabu/g2p"
	"github.com/veyselaksin/nabu/script"
	"github.com/veyselaksin/nabu/translate"
	"github.com/veyselaksin/nabu/tts"
)

const maxBodyBytes = 64 << 10

// Synthesizer turns IPA into audio. *tts.Model implements it.
type Synthesizer interface {
	Synthesize(ctx context.Context, ipa string, p tts.Params) (*tts.Audio, error)
	Info() tts.Info
}

// Translator translates English into transliteration. *translate.Translator
// implements it.
type Translator interface {
	Translate(ctx context.Context, text string) (*translate.Result, error)
	Model() string
}

// Pinger reports whether the translator's language model server is reachable.
// *translate.Client implements it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Options configures a Server.
type Options struct {
	Logger         *slog.Logger
	MaxTextRunes   int
	RequestTimeout time.Duration
	// Synthesizer enables POST /v1/speak. If nil, /v1/speak returns 503.
	Synthesizer Synthesizer
	// Renderer enables POST /v1/render and the X-Nabu-Hieroglyphs header of
	// /v1/speak. If nil, /v1/render returns 503.
	Renderer *script.Renderer
	// Translator enables POST /v1/translate. If nil, it returns 503.
	Translator Translator
	// TranslatorPing, if set, lets GET /v1/translator report whether the
	// language model server is reachable.
	TranslatorPing Pinger
	// TranslateTimeout bounds POST /v1/translate, which runs a language model
	// (default 3m). RequestTimeout bounds every other route.
	TranslateTimeout time.Duration
	// MaxTranslateRunes caps the English text accepted by /v1/translate
	// (default 500).
	MaxTranslateRunes int
}

// Server serves the Nabu API over a fixed set of rule sets.
type Server struct {
	log      *slog.Logger
	opts     Options
	rulesets map[string]*g2p.RuleSet
	summary  []rulesetInfo // sorted by id
}

// New returns a Server for the given rule sets; IDs must be unique.
func New(rulesets []*g2p.RuleSet, opts Options) (*Server, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.MaxTextRunes <= 0 {
		opts.MaxTextRunes = 2000
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 10 * time.Second
	}
	if opts.TranslateTimeout <= 0 {
		opts.TranslateTimeout = 3 * time.Minute
	}
	if opts.MaxTranslateRunes <= 0 {
		opts.MaxTranslateRunes = 500
	}
	s := &Server{log: opts.Logger, opts: opts, rulesets: make(map[string]*g2p.RuleSet, len(rulesets))}
	for _, rs := range rulesets {
		if _, dup := s.rulesets[rs.ID]; dup {
			return nil, fmt.Errorf("duplicate rule set id %q", rs.ID)
		}
		s.rulesets[rs.ID] = rs
		s.summary = append(s.summary, rulesetInfo{
			ID: rs.ID, Language: rs.Language, Name: rs.Name, Reference: rs.Reference, Version: rs.Version,
		})
	}
	sort.Slice(s.summary, func(i, j int) bool { return s.summary[i].ID < s.summary[j].ID })
	return s, nil
}

// Handler returns the HTTP handler with all routes and middleware.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(s.logRequests)
	r.Use(middleware.Recoverer)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(s.opts.RequestTimeout))
		r.Get("/healthz", s.healthz)
		r.Route("/v1", func(r chi.Router) {
			r.Get("/rulesets", s.listRulesets)
			r.Post("/g2p", s.g2p)
			r.Post("/render", s.render)
			r.Post("/speak", s.speak)
			r.Get("/voices", s.voices)
			r.Get("/translator", s.translator)
		})
	})
	// Translation runs a language model, which on CPU takes far longer than
	// anything else, so it has its own timeout.
	r.With(middleware.Timeout(s.opts.TranslateTimeout)).Post("/v1/translate", s.translate)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	})
	return r
}

type rulesetInfo struct {
	ID        string `json:"id"`
	Language  string `json:"language"`
	Name      string `json:"name"`
	Reference string `json:"reference"`
	Version   string `json:"version"`
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listRulesets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"rulesets": s.summary})
}

type g2pRequest struct {
	Text    string `json:"text"`
	Ruleset string `json:"ruleset"`
}

type g2pResponse struct {
	IPA       string `json:"ipa"`
	Ruleset   string `json:"ruleset"`
	Reference string `json:"reference"`
}

func (s *Server) g2p(w http.ResponseWriter, r *http.Request) {
	var req g2pRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rs, ipa, ok := s.toIPA(w, r, req.Text, req.Ruleset)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, g2pResponse{IPA: ipa, Ruleset: rs.ID, Reference: rs.Reference})
}

// toIPA validates the ruleset and text and runs G2P, writing an error
// response and returning ok=false on failure.
func (s *Server) toIPA(w http.ResponseWriter, r *http.Request, text, ruleset string) (rs *g2p.RuleSet, ipa string, ok bool) {
	if ruleset == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ruleset is required", nil)
		return nil, "", false
	}
	rs, found := s.rulesets[ruleset]
	if !found {
		writeError(w, http.StatusBadRequest, "unknown_ruleset",
			fmt.Sprintf("unknown ruleset %q; see GET /v1/rulesets", ruleset), nil)
		return nil, "", false
	}
	if !s.checkLength(w, text) {
		return nil, "", false
	}

	ipa, err := rs.ToIPA(text)
	var unknown *g2p.UnknownGraphemeError
	var badUTF8 *g2p.InvalidUTF8Error
	switch {
	case errors.As(err, &unknown):
		writeError(w, http.StatusUnprocessableEntity, "unknown_grapheme", unknown.Error(), map[string]any{
			"grapheme": unknown.Grapheme,
			"offset":   unknown.Offset,
		})
		return nil, "", false
	case errors.As(err, &badUTF8):
		writeError(w, http.StatusBadRequest, "invalid_utf8", "text is not valid UTF-8", nil)
		return nil, "", false
	case err != nil:
		s.log.ErrorContext(r.Context(), "g2p failed", "err", err, "ruleset", rs.ID)
		writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
		return nil, "", false
	}
	return rs, ipa, true
}

type renderRequest struct {
	Text string `json:"text"`
}

type renderResponse struct {
	*script.Rendering
	// Lexicon reports whether an attested-spelling lexicon is loaded. Without
	// one, every word is spelled phonetically.
	Lexicon bool `json:"lexicon"`
}

const renderUnavailable = "hieroglyph rendering is not configured on this server"

func (s *Server) render(w http.ResponseWriter, r *http.Request) {
	if s.opts.Renderer == nil {
		writeError(w, http.StatusServiceUnavailable, "render_unavailable", renderUnavailable, nil)
		return
	}
	var req renderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !s.checkLength(w, req.Text) {
		return
	}
	res, err := s.opts.Renderer.Render(req.Text)
	var noSign *script.NoSignError
	var badUTF8 *script.InvalidUTF8Error
	switch {
	case errors.As(err, &noSign):
		writeError(w, http.StatusUnprocessableEntity, "no_sign", noSign.Error(), map[string]any{
			"grapheme": noSign.Grapheme,
			"offset":   noSign.Offset,
			"word":     noSign.Word,
		})
		return
	case errors.As(err, &badUTF8):
		writeError(w, http.StatusBadRequest, "invalid_utf8", "text is not valid UTF-8", nil)
		return
	case err != nil:
		s.log.ErrorContext(r.Context(), "render failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
		return
	}
	writeJSON(w, http.StatusOK, renderResponse{Rendering: res, Lexicon: s.opts.Renderer.HasLexicon()})
}

// checkLength writes a 413 response and returns false if text is too long.
func (s *Server) checkLength(w http.ResponseWriter, text string) bool {
	if n := utf8.RuneCountInString(text); n > s.opts.MaxTextRunes {
		writeError(w, http.StatusRequestEntityTooLarge, "text_too_long",
			fmt.Sprintf("text has %d characters; the limit is %d", n, s.opts.MaxTextRunes), nil)
		return false
	}
	return true
}

type speakRequest struct {
	Text    string   `json:"text"`
	Ruleset string   `json:"ruleset"`
	Speaker *int     `json:"speaker"`
	Speed   *float64 `json:"speed"`
}

// Response headers of POST /v1/speak. Values that may contain non-ASCII text
// are percent-encoded UTF-8 (decode with decodeURIComponent).
const (
	headerIPA            = "X-Nabu-IPA"
	headerRuleset        = "X-Nabu-Ruleset"
	headerRulesetVersion = "X-Nabu-Ruleset-Version"
	headerReference      = "X-Nabu-Reference"
	headerModel          = "X-Nabu-Model"
	headerSpeaker        = "X-Nabu-Speaker"
	headerHieroglyphs    = "X-Nabu-Hieroglyphs"
)

var exposedHeaders = headerIPA + ", " + headerRuleset + ", " + headerRulesetVersion + ", " +
	headerReference + ", " + headerModel + ", " + headerSpeaker + ", " + headerHieroglyphs

const ttsUnavailable = "speech synthesis is not configured on this server: set NABU_TTS_MODEL " +
	"to a model exported with nabu-tts-export (see tts/train/README.md)"

func (s *Server) speak(w http.ResponseWriter, r *http.Request) {
	if s.opts.Synthesizer == nil {
		writeError(w, http.StatusServiceUnavailable, "tts_unavailable", ttsUnavailable, nil)
		return
	}
	var req speakRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	params := tts.Params{}
	if req.Speaker != nil {
		params.Speaker = *req.Speaker
	}
	if req.Speed != nil {
		if *req.Speed == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request",
				fmt.Sprintf("speed must be between %g and %g", tts.MinSpeed, tts.MaxSpeed), nil)
			return
		}
		params.Speed = *req.Speed
	}
	rs, ipa, ok := s.toIPA(w, r, req.Text, req.Ruleset)
	if !ok {
		return
	}

	audio, err := s.opts.Synthesizer.Synthesize(r.Context(), ipa, params)
	var unknown *tts.UnknownSymbolError
	switch {
	case errors.As(err, &unknown):
		writeError(w, http.StatusUnprocessableEntity, "unknown_ipa_symbol",
			fmt.Sprintf("the voice model cannot pronounce IPA symbol %q (U+%04X) produced by ruleset %s",
				string(unknown.Symbol), unknown.Symbol, rs.ID),
			map[string]any{"symbol": string(unknown.Symbol), "offset": unknown.Offset, "ipa": ipa})
		return
	case errors.Is(err, tts.ErrEmptyInput):
		writeError(w, http.StatusUnprocessableEntity, "nothing_to_speak",
			"the text converts to empty IPA; there is nothing to speak", map[string]any{"ipa": ipa})
		return
	case errors.Is(err, tts.ErrInvalidParams):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	case errors.Is(err, tts.ErrTooLong):
		writeError(w, http.StatusRequestEntityTooLarge, "text_too_long", err.Error(), nil)
		return
	case err != nil && r.Context().Err() != nil:
		writeError(w, http.StatusGatewayTimeout, "timeout", "speech synthesis took too long", nil)
		return
	case err != nil:
		s.log.ErrorContext(r.Context(), "tts failed", "err", err, "ruleset", rs.ID)
		writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
		return
	}

	var buf bytes.Buffer
	if err := tts.EncodeWAV(&buf, audio.Samples, audio.SampleRate); err != nil {
		s.log.ErrorContext(r.Context(), "wav encoding failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "audio/wav")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set(headerIPA, url.PathEscape(ipa))
	h.Set(headerRuleset, rs.ID)
	h.Set(headerRulesetVersion, url.PathEscape(rs.Version))
	h.Set(headerReference, url.PathEscape(rs.Reference))
	h.Set(headerModel, url.PathEscape(s.opts.Synthesizer.Info().Name))
	h.Set(headerSpeaker, strconv.Itoa(params.Speaker))
	// Best effort: speech does not depend on the script, so a word without a
	// sign only omits the header. Use POST /v1/render for per-word details.
	if s.opts.Renderer != nil {
		if res, err := s.opts.Renderer.Render(req.Text); err == nil && res.Hieroglyphs != "" {
			h.Set(headerHieroglyphs, url.PathEscape(res.Hieroglyphs))
		}
	}
	h.Set("Access-Control-Expose-Headers", exposedHeaders)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

type translateRequest struct {
	Text string `json:"text"`
	// Lang is the target language; only "egy" (the default) is supported.
	Lang string `json:"lang"`
}

const translateUnavailable = "translation is not configured on this server: set NABU_LLM_URL to an " +
	"OpenAI-compatible server and NABU_TLA_CORPUS to the TLA corpus (see translate/README.md)"

func (s *Server) translate(w http.ResponseWriter, r *http.Request) {
	if s.opts.Translator == nil {
		writeError(w, http.StatusServiceUnavailable, "translate_unavailable", translateUnavailable, nil)
		return
	}
	var req translateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Lang != "" && req.Lang != "egy" {
		writeError(w, http.StatusBadRequest, "unsupported_language",
			fmt.Sprintf("translation into %q is not supported; supported: egy", req.Lang), nil)
		return
	}
	if n := utf8.RuneCountInString(req.Text); n > s.opts.MaxTranslateRunes {
		writeError(w, http.StatusRequestEntityTooLarge, "text_too_long",
			fmt.Sprintf("text has %d characters; the limit for translation is %d", n, s.opts.MaxTranslateRunes), nil)
		return
	}

	res, err := s.opts.Translator.Translate(r.Context(), req.Text)
	var llmErr *translate.LLMError
	switch {
	case errors.Is(err, translate.ErrEmptyInput):
		writeError(w, http.StatusBadRequest, "invalid_request", "text is required", nil)
		return
	case errors.Is(err, translate.ErrInvalidUTF8):
		writeError(w, http.StatusBadRequest, "invalid_utf8", "text is not valid UTF-8", nil)
		return
	case err != nil && r.Context().Err() != nil:
		writeError(w, http.StatusGatewayTimeout, "timeout",
			"the language model took too long (NABU_TRANSLATE_TIMEOUT)", nil)
		return
	case errors.Is(err, translate.ErrModelOutput):
		s.log.WarnContext(r.Context(), "unusable model output", "err", err)
		writeError(w, http.StatusBadGateway, "bad_model_output", err.Error(), nil)
		return
	case errors.As(err, &llmErr):
		s.log.ErrorContext(r.Context(), "language model request failed", "err", err)
		writeError(w, http.StatusBadGateway, "llm_unavailable", llmErr.Error(), nil)
		return
	case err != nil:
		s.log.ErrorContext(r.Context(), "translate failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type translatorResponse struct {
	Available bool   `json:"available"`
	Message   string `json:"message,omitempty"`
	Model     string `json:"model,omitempty"`
}

// translator reports whether POST /v1/translate can work right now: it is
// configured and, when it can be checked, its model server answers.
func (s *Server) translator(w http.ResponseWriter, r *http.Request) {
	if s.opts.Translator == nil {
		writeJSON(w, http.StatusOK, translatorResponse{Message: translateUnavailable})
		return
	}
	model := s.opts.Translator.Model()
	if s.opts.TranslatorPing != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.opts.TranslatorPing.Ping(ctx); err != nil {
			writeJSON(w, http.StatusOK, translatorResponse{Model: model,
				Message: "the language model server is not responding (" + err.Error() + "); start it, e.g. make run-llm"})
			return
		}
	}
	writeJSON(w, http.StatusOK, translatorResponse{Available: true, Model: model})
}

type voicesResponse struct {
	Available bool        `json:"available"`
	Message   string      `json:"message,omitempty"`
	Model     *voiceModel `json:"model,omitempty"`
}

type voiceModel struct {
	Name       string   `json:"name"`
	SampleRate int      `json:"sample_rate"`
	Languages  []string `json:"languages"`
	Speakers   []string `json:"speakers"`
	MinSpeed   float64  `json:"min_speed"`
	MaxSpeed   float64  `json:"max_speed"`
}

// voices reports whether /v1/speak is available and which speakers it has.
// Speaker labels are pseudonymous (hashed Common Voice ids).
func (s *Server) voices(w http.ResponseWriter, _ *http.Request) {
	if s.opts.Synthesizer == nil {
		writeJSON(w, http.StatusOK, voicesResponse{Available: false, Message: ttsUnavailable})
		return
	}
	info := s.opts.Synthesizer.Info()
	writeJSON(w, http.StatusOK, voicesResponse{Available: true, Model: &voiceModel{
		Name: info.Name, SampleRate: info.SampleRate, Languages: info.Languages, Speakers: info.Speakers,
		MinSpeed: tts.MinSpeed, MaxSpeed: tts.MaxSpeed,
	}})
}

// decodeJSON decodes a single JSON object from the body into dst, writing a
// 4xx response and returning false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("body must contain a single JSON object")
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
				fmt.Sprintf("request body exceeds %d bytes", maxBodyBytes), nil)
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body: "+err.Error(), nil)
		return false
	}
	return true
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, msg string, details map[string]any) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: msg, Details: details}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// logRequests logs one structured line per request.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			level := slog.LevelInfo
			if ww.Status() >= 500 && ww.Status() != http.StatusNotImplemented {
				level = slog.LevelError
			}
			s.log.LogAttrs(r.Context(), level, "request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", middleware.GetReqID(r.Context())),
			)
		}()
		next.ServeHTTP(ww, r)
	})
}
