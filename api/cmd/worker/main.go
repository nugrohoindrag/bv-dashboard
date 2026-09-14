// worker: River job consumer + periodic scheduler (TAD §5.1, §5.11).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/buildingvision/api/internal/app"
	"github.com/buildingvision/api/internal/notification"
	"github.com/buildingvision/api/internal/platform/config"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/storage"
	"github.com/buildingvision/api/internal/worker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := db.Open(ctx, cfg.WorkerDatabaseURL)
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
		store = s3
	}
	a, err := app.New(app.Options{Cfg: cfg, Log: log, DB: d, Jobs: enq, Storage: store})
	if err != nil {
		log.Error("app", "err", err)
		os.Exit(1)
	}
	a.Use(app.DefaultExtensions(a)...)
	if cfg.FCMProjectID != "" && cfg.FCMServiceAccountJSON != "" {
		if p, err := notification.NewFCM(ctx, cfg.FCMProjectID, cfg.FCMServiceAccountJSON); err != nil {
			log.Warn("fcm disabled", "err", err)
		} else {
			a.Notification.Pusher = p
		}
	}
	w, err := worker.New(a, log)
	if err != nil {
		log.Error("worker", "err", err)
		os.Exit(1)
	}
	if err := w.Start(ctx); err != nil {
		log.Error("worker start", "err", err)
		os.Exit(1)
	}
	log.Info("worker started", "env", cfg.Env)
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = w.Stop(sctx)
	log.Info("worker stopped")
}
