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

	"askdocs/backend/internal/config"
	"askdocs/backend/internal/document"
	"askdocs/backend/internal/platform/aiclient"
	"askdocs/backend/internal/platform/disk"
	"askdocs/backend/internal/platform/extract"
	"askdocs/backend/internal/platform/httpapi"
	"askdocs/backend/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api exited", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	files, err := disk.NewStore(cfg.UploadDir)
	if err != nil {
		return fmt.Errorf("init file store: %w", err)
	}
	repo := postgres.NewDocumentRepository(pool)
	docs := document.NewService(repo, files)

	ingestor := document.NewIngestor(repo, files, extract.New(), aiclient.New(cfg.AIServiceURL))
	ingestPool := document.NewPool(ingestor, repo, cfg.IngestWorkers, 2*time.Second, logger)
	poolDone := make(chan struct{})
	go func() {
		ingestPool.Run(ctx)
		close(poolDone)
	}()
	logger.Info("ingestion pool started", "workers", cfg.IngestWorkers)

	srv := &http.Server{
		Addr:              ":" + cfg.APIPort,
		Handler:           httpapi.New(logger, pool, docs),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	logger.Info("api listening", "addr", srv.Addr)

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	<-poolDone // workers finish or requeue their in-flight documents
	logger.Info("shutdown complete")
	return nil
}
