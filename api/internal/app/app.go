// Package app merakit seluruh modul menjadi satu HTTP handler (dipakai cmd/api dan integration test).
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/config"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/storage"
	"github.com/buildingvision/api/internal/property"
)

type App struct {
	Cfg     config.Config
	Log     *slog.Logger
	DB      *db.DB
	Jobs    jobs.Enqueuer
	Storage storage.Storage

	IAM         *iam.Service
	Property    *property.Service
	Attachments *attachments.Service
	Operations  *operations.Service

	// modul domain (diisi DefaultExtensions)
	Asset       *asset.Service
	Engineering *engineering.Service

	// modul lanjutan didaftarkan lewat Extensions (engineering, security, housekeeping, tenantservice, notification, overview, search, sync)
	Extensions []Extension

	Router chi.Router
}

// Extension: modul yang mendaftarkan route & hook ke App.
type Extension interface {
	Name() string
	Mount(a *App, r chi.Router)
}

type Options struct {
	Cfg     config.Config
	Log     *slog.Logger
	DB      *db.DB
	Jobs    jobs.Enqueuer
	Storage storage.Storage
}

func New(opts Options) (*App, error) {
	log := opts.Log
	if log == nil {
		log = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	signer, err := iam.NewTokenSigner(opts.Cfg.JWTPrivateKeyPEM, opts.Cfg.JWTPublicKeyPEM, opts.Cfg.AccessTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("token signer: %w", err)
	}
	a := &App{Cfg: opts.Cfg, Log: log, DB: opts.DB, Jobs: opts.Jobs, Storage: opts.Storage}
	a.IAM = iam.NewService(opts.DB, signer, opts.Cfg.RefreshTokenTTL)
	a.Property = &property.Service{DB: opts.DB, Jobs: opts.Jobs}
	a.Attachments = &attachments.Service{DB: opts.DB, Storage: opts.Storage, Jobs: opts.Jobs, UploadTTL: opts.Cfg.PresignUploadTTL, DownloadTTL: opts.Cfg.PresignDownloadTTL, MaxBytes: opts.Cfg.MaxUploadBytes}
	a.Operations = operations.NewService(opts.DB, opts.Jobs, a.Attachments)
	a.Operations.RegisterHook("work_order:*", operations.FindingHook(a.Operations))
	a.Operations.RegisterHook("task:*", operations.FindingHook(a.Operations))
	return a, nil
}

// Use mendaftarkan extension (harus sebelum BuildRouter).
func (a *App) Use(ext ...Extension) { a.Extensions = append(a.Extensions, ext...) }

// BuildRouter merakit router: middleware platform, /health, /api/v1 public & protected.
func (a *App) BuildRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware)
	r.Use(middleware.RealIP)
	r.Use(httpx.RecoverMiddleware(a.Log))
	r.Use(httpx.LoggingMiddleware(a.Log))
	r.Use(httpx.SecurityHeaders)
	r.Use(httpx.CORS(a.Cfg.CORSOrigins))
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/health", a.health)
	r.Get("/ready", a.ready)

	iamH := &iam.Handler{Svc: a.IAM, CookieDomain: a.Cfg.CookieDomain, CookieSecure: a.Cfg.CookieSecure}
	propH := &property.Handler{Svc: a.Property, IAM: a.IAM}
	attH := &attachments.Handler{Svc: a.Attachments, IAM: a.IAM}
	opsH := &operations.Handler{Svc: a.Operations, IAM: a.IAM}

	r.Route("/api/v1", func(r chi.Router) {
		iamH.MountPublic(r)
		r.Group(func(r chi.Router) {
			r.Use(a.IAM.Authenticate)
			iamH.MountProtected(r)
			propH.Mount(r)
			attH.Mount(r)
			opsH.Mount(r)
			r.With(a.IAM.Require("platform.audit_logs.view")).Get("/audit-logs", a.listAuditLogs)
			for _, ext := range a.Extensions {
				ext.Mount(a, r)
			}
		})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, apperr.NotFound("Endpoint"))
	})
	a.Router = r
	return r
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC(), "min_supported_app_version": a.Cfg.MinMobileAppVersion})
}

func (a *App) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.DB.Pool.Ping(ctx); err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "db_unavailable"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (a *App) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f audit.AuditFilter
	f.EntityType = r.URL.Query().Get("entity_type")
	f.Action = r.URL.Query().Get("action")
	f.EntityID, _ = httpx.QueryUUID(r, "entity_id")
	f.ActorID, _ = httpx.QueryUUID(r, "actor_id")
	f.From, _ = httpx.QueryTime(r, "from")
	f.To, _ = httpx.QueryTime(r, "to")
	var items []audit.AuditLog
	var next *string
	err = a.DB.WithTx(r.Context(), func(ctx context.Context, tx pgxTx) error {
		var err error
		items, next, err = audit.ListAuditLogs(ctx, tx, f, page)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

var _ = authctx.Must
