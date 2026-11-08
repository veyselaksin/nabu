// Package server implements the Nabu HTTP API.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/veyselaksin/nabu/g2p"
)

const maxBodyBytes = 64 << 10

// Options configures a Server.
type Options struct {
	Logger         *slog.Logger
	MaxTextRunes   int
	RequestTimeout time.Duration
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
	r.Use(middleware.Timeout(s.opts.RequestTimeout))

	r.Get("/healthz", s.healthz)
	r.Route("/v1", func(r chi.Router) {
		r.Get("/rulesets", s.listRulesets)
		r.Post("/g2p", s.g2p)
		r.Post("/translate", s.notImplemented("translation is not implemented yet"))
		r.Post("/speak", s.notImplemented("speech synthesis is not implemented yet"))
	})
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
	if req.Ruleset == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ruleset is required", nil)
		return
	}
	rs, ok := s.rulesets[req.Ruleset]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown_ruleset",
			fmt.Sprintf("unknown ruleset %q; see GET /v1/rulesets", req.Ruleset), nil)
		return
	}
	if n := utf8.RuneCountInString(req.Text); n > s.opts.MaxTextRunes {
		writeError(w, http.StatusRequestEntityTooLarge, "text_too_long",
			fmt.Sprintf("text has %d characters; the limit is %d", n, s.opts.MaxTextRunes), nil)
		return
	}

	ipa, err := rs.ToIPA(req.Text)
	var unknown *g2p.UnknownGraphemeError
	var badUTF8 *g2p.InvalidUTF8Error
	switch {
	case errors.As(err, &unknown):
		writeError(w, http.StatusUnprocessableEntity, "unknown_grapheme", unknown.Error(), map[string]any{
			"grapheme": unknown.Grapheme,
			"offset":   unknown.Offset,
		})
		return
	case errors.As(err, &badUTF8):
		writeError(w, http.StatusBadRequest, "invalid_utf8", "text is not valid UTF-8", nil)
		return
	case err != nil:
		s.log.ErrorContext(r.Context(), "g2p failed", "err", err, "ruleset", rs.ID)
		writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
		return
	}
	writeJSON(w, http.StatusOK, g2pResponse{IPA: ipa, Ruleset: rs.ID, Reference: rs.Reference})
}

func (s *Server) notImplemented(msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotImplemented, "not_implemented", msg, nil)
	}
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
