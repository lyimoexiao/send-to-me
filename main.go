package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sendtome/internal/config"
	"sendtome/internal/server"
	"sendtome/internal/storage"
	"sendtome/internal/store"
)

//go:embed all:web/dist
var webFiles embed.FS

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		logger.Error("create data directory failed", "error", err)
		os.Exit(1)
	}

	metadata, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.Error("open metadata store failed", "error", err)
		os.Exit(1)
	}
	defer metadata.Close()

	payloads, err := storage.New(cfg.Storage)
	if err != nil {
		logger.Error("initialize storage failed", "error", err)
		os.Exit(1)
	}

	assets, err := fs.Sub(webFiles, "web/dist")
	if err != nil {
		logger.Error("load frontend assets failed", "error", err)
		os.Exit(1)
	}

	app := server.New(cfg, metadata, payloads, logger, assets)
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go runMaintenance(rootCtx, metadata, payloads, cfg.CleanupGrace, logger)

	serverErrors := make(chan error, 1)
	go func() {
		storageDetails := []any{"addr", cfg.Addr, "storage", cfg.Storage.Driver}
		if cfg.Storage.Driver == "s3" {
			storageDetails = append(storageDetails, "s3_provider", cfg.Storage.S3Provider)
		}
		if cfg.PublicBaseURL != "" {
			storageDetails = append(storageDetails, "public_base_url", cfg.PublicBaseURL)
		}
		logger.Info("send-to-me listening", storageDetails...)
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-rootCtx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
			_ = httpServer.Close()
		}
		select {
		case <-serverErrors:
		case <-shutdownCtx.Done():
		}
	}
}

func runMaintenance(ctx context.Context, metadata *store.Store, payloads storage.Storage, grace time.Duration, logger *slog.Logger) {
	purge := func() {
		timeout := 10 * time.Minute
		purgeCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cutoff := time.Now().Add(-grace).Unix()
		candidates, err := metadata.ExpiredBefore(purgeCtx, cutoff)
		if err != nil {
			if ctx.Err() == nil {
				logger.Warn("list expired shares failed", "error", err)
			}
			return
		}
		removed := 0
		for _, candidate := range candidates {
			// Drop metadata first: the transaction returns only payloads that
			// no live share references any more, so a deduplicated object
			// keeps serving its remaining shares.
			keys, err := metadata.DeleteShare(purgeCtx, candidate.ShareID)
			if err != nil {
				logger.Warn("delete expired share failed", "share_id", candidate.ShareID, "error", err)
				continue
			}
			for _, key := range keys {
				if err := payloads.Delete(purgeCtx, key); err != nil {
					logger.Warn("delete expired payload failed", "share_id", candidate.ShareID, "key", key, "error", err)
				}
			}
			removed++
		}
		if removed > 0 {
			logger.Info("removed expired shares", "count", removed)
		}
	}

	purge()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}
