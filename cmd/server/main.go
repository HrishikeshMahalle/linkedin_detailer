package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tradelab/linkedin-profile-api/internal/config"
	"github.com/tradelab/linkedin-profile-api/internal/httpapi"
	"github.com/tradelab/linkedin-profile-api/internal/linkedin"
	"github.com/tradelab/linkedin-profile-api/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration is invalid", "error", err)
		os.Exit(1)
	}

	factory := service.FetcherFactoryFunc(func(session service.Session) (service.Fetcher, error) {
		return linkedin.NewClient(linkedin.Options{
			LIAT:       session.LIAT,
			JSESSIONID: session.JSESSIONID,
			Decoration: cfg.LinkedInDecoration,
			HTTPClient: &http.Client{Timeout: cfg.UpstreamTimeout},
			Interval:   cfg.LinkedInInterval,
			Cooldown:   cfg.Cooldown,
		})
	})
	profileService := service.NewProfileService(
		factory,
		cfg.CacheTTL,
		cfg.CacheMaxEntries,
		cfg.MaxConcurrent,
	)
	handler := httpapi.NewHandler(profileService, logger, httpapi.Options{
		APIKey:         cfg.APIKey,
		RateLimitRPM:   cfg.RateLimitRPM,
		RateLimitBurst: cfg.RateLimitBurst,
		RequestTimeout: cfg.RequestTimeout,
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening", "port", cfg.Port, "environment", cfg.Environment)
		errCh <- server.ListenAndServe()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-signalCtx.Done():
		logger.Info("shutdown requested")
	case serveErr := <-errCh:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", serveErr)
			os.Exit(1)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}
