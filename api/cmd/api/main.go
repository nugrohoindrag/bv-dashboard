// api: HTTP server BuildingVision (TAD §5.1).
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

	"github.com/buildingvision/api/internal/app"
	"github.com/buildingvision/api/internal/platform/config"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/metrics"
	"github.com/buildingvision/api/internal/platform/storage"
)

// version diisi saat build (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	lvl := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		lvl = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer d.Close()

	enq, err := jobs.NewInsertOnlyClient(d.Pool)
	if err != nil {
		log.Error("river", "err", err)
		os.Exit(1)
	}

	var store storage.Storage
	if cfg.S3Endpoint == "memory" {
		store = storage.NewMemory(cfg.PublicURL)
	} else {
		s3, err := storage.NewS3(ctx, cfg)
		if err != nil {
			log.Error("storage", "err", err)
			os.Exit(1)
		}
		if cfg.IsLocal() {
			if err := s3.EnsureBucket(ctx); err != nil {
				log.Warn("ensure bucket", "err", err)
			}
		}
		store = s3
	}

	a, err := app.New(app.Options{Cfg: cfg, Log: log, DB: d, Jobs: enq, Storage: store})
	if err != nil {
		log.Error("app", "err", err)
		os.Exit(1)
	}
	a.Use(app.DefaultExtensions(a)...)
	metrics.Serve(ctx, cfg.MetricsAddr, log)
	go metrics.CollectPool(ctx, d.Pool, 15*time.Second)
	handler := a.BuildRouter()

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		log.Info("api listening", "version", version, "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http", "err", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info("api stopped")
}
