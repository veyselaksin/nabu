// Command nabu-api serves the Nabu HTTP API. Configuration is read from
// environment variables; see internal/config.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/veyselaksin/nabu/api/internal/config"
	"github.com/veyselaksin/nabu/api/internal/server"
	"github.com/veyselaksin/nabu/g2p"
	"github.com/veyselaksin/nabu/g2p/rules"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nabu-api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}

	hopts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var handler slog.Handler = slog.NewJSONHandler(os.Stdout, hopts)
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, hopts)
	}
	log := slog.New(handler)
	slog.SetDefault(log)

	source := "embedded"
	var sets []*g2p.RuleSet
	if cfg.RulesDir != "" {
		source = cfg.RulesDir
		sets, err = g2p.LoadFS(os.DirFS(cfg.RulesDir))
	} else {
		sets, err = g2p.LoadFS(rules.FS)
	}
	if err != nil {
		return fmt.Errorf("loading rule sets: %w", err)
	}
	if len(sets) == 0 {
		return fmt.Errorf("no rule sets found (source: %s)", source)
	}
	for _, rs := range sets {
		log.Info("loaded rule set", "id", rs.ID, "version", rs.Version, "source", source)
	}

	srv, err := server.New(sets, server.Options{
		Logger:         log,
		MaxTextRunes:   cfg.MaxTextRunes,
		RequestTimeout: cfg.RequestTimeout,
	})
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(handler, slog.LevelWarn),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr)
		errc <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}
