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

	"github.com/buildingvision/api/internal/appdownload"
	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/booking"
	"github.com/buildingvision/api/internal/bvrooms"
	"github.com/buildingvision/api/internal/commercial"
	"github.com/buildingvision/api/internal/demo"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/exports"
	"github.com/buildingvision/api/internal/growth"
	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/inventory"
	"github.com/buildingvision/api/internal/notification"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/overview"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/config"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/mailer"
	"github.com/buildingvision/api/internal/platform/metrics"
	"github.com/buildingvision/api/internal/platform/storage"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/reports"
	"github.com/buildingvision/api/internal/search"
	"github.com/buildingvision/api/internal/security"
	bvsync "github.com/buildingvision/api/internal/sync"
	"github.com/buildingvision/api/internal/tenantapp"
	"github.com/buildingvision/api/internal/tenantrelation"
	"github.com/buildingvision/api/internal/tenantservice"
	"github.com/buildingvision/api/internal/vendor"
	"github.com/buildingvision/api/internal/visitor"
)

type App struct {
	Cfg     config.Config
	Log     *slog.Logger
	DB      *db.DB
	Jobs    jobs.Enqueuer
	Storage storage.Storage

	IAM         *iam.Service
	Profile     *profile.Service // Profile Engine (P1.0) — dipakai modul lain untuk enforcement capability
	Property    *property.Service
	Attachments *attachments.Service
	Operations  *operations.Service

	// modul domain (diisi DefaultExtensions)
	Asset          *asset.Service
	Engineering    *engineering.Service
	Security       *security.Service
	Housekeeping   *housekeeping.Service
	TenantService  *tenantservice.Service
	Notification   *notification.Service
	Search         *search.Service
	Exports        *exports.Service
	Overview       *overview.Service
	Sync           *bvsync.Service
	TenantApp      *tenantapp.Service      // P1 Mobile Tenant API
	TenantRelation *tenantrelation.Service // P1 Tenant Relation (dashboard)
	Booking        *booking.Service        // P1 Facility Booking
	Visitor        *visitor.Service        // P1 Visitor Management
	Billing        *billing.Service        // P1 Billing & Payment
	Vendor         *vendor.Service         // P1 Vendor Management
	Inventory      *inventory.Service      // P1 Inventory / Spare Parts
	Hotel          *hotel.Service          // P1 Hotel Booking Management (profile hotel)
	Commercial     *commercial.Service     // P1 Apartment Unit Sales & Rental (profile apartment)
	Reports        *reports.Service        // P1 Advanced Reports (PRD §26)
	AppDownload    *appdownload.Service    // Website PRD: tautan unduhan aplikasi (admin_internal + public)
	Growth         *growth.Service         // Website PRD: signup, trial, onboarding, Book a Demo, funnel events
	BVRooms        *bvrooms.Service        // BVRooms customer booking channel (white-label per org; Requirements v0.2)
	Demo           *demo.Service           // Demo Seed Database (Admin Internal → Demo Data; bvctl demo)
	Mailer         mailer.Mailer

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
	Mailer  mailer.Mailer // nil = SMTP dari config (atau log-only bila BV_SMTP_HOST kosong)
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
	a.Profile = profile.New(opts.DB, opts.Jobs)
	a.Property = &property.Service{DB: opts.DB, Jobs: opts.Jobs}
	a.Attachments = &attachments.Service{DB: opts.DB, Storage: opts.Storage, Jobs: opts.Jobs, UploadTTL: opts.Cfg.PresignUploadTTL, DownloadTTL: opts.Cfg.PresignDownloadTTL, MaxBytes: opts.Cfg.MaxUploadBytes, MaxImageBytes: opts.Cfg.MaxImageBytes}
	a.Operations = operations.NewService(opts.DB, opts.Jobs, a.Attachments)
	a.Operations.RegisterHook("work_order:*", operations.FindingHook(a.Operations))
	a.Operations.RegisterHook("task:*", operations.FindingHook(a.Operations))
	a.Mailer = opts.Mailer
	if a.Mailer == nil {
		if opts.Cfg.SMTPHost != "" {
			a.Mailer = mailer.SMTP{Host: opts.Cfg.SMTPHost, Port: opts.Cfg.SMTPPort, User: opts.Cfg.SMTPUser, Password: opts.Cfg.SMTPPassword, From: opts.Cfg.SMTPFrom, StartTLS: opts.Cfg.SMTPStartTLS}
		} else {
			a.Mailer = mailer.LogMailer{Log: log}
		}
	}
	a.AppDownload = appdownload.New(opts.DB)
	a.Growth = growth.New(opts.DB, opts.Jobs, a.IAM, a.Mailer, log, growth.Config{
		Env: opts.Cfg.Env, PublicURL: opts.Cfg.PublicURL, WebsiteURL: opts.Cfg.WebsiteURL, TrialDays: opts.Cfg.TrialDays, TrialEndingSoonDays: opts.Cfg.TrialEndingSoonDays,
		SignupTokenTTL: opts.Cfg.SignupTokenTTL, SalesEmail: opts.Cfg.SalesEmail, EventsEnabled: opts.Cfg.GrowthEventsEnabled,
	})
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
	r.Use(metrics.Middleware)
	r.Use(httpx.SecurityHeaders)
	r.Use(httpx.CORS(a.Cfg.CORSOrigins))
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/health", a.health)
	r.Get("/ready", a.ready)

	iamH := &iam.Handler{Svc: a.IAM, CookieDomain: a.Cfg.CookieDomain, CookieSecure: a.Cfg.CookieSecure}
	propH := &property.Handler{Svc: a.Property, IAM: a.IAM}
	profH := &profile.Handler{Svc: a.Profile, IAM: a.IAM}
	attH := &attachments.Handler{Svc: a.Attachments, IAM: a.IAM}
	opsH := &operations.Handler{Svc: a.Operations, IAM: a.IAM}
	adH := &appdownload.Handler{Svc: a.AppDownload, IAM: a.IAM}
	growthH := &growth.Handler{Svc: a.Growth, IAM: a.IAM, CookieDomain: a.Cfg.CookieDomain, CookieSecure: a.Cfg.CookieSecure}

	if a.TenantService != nil {
		tenantservice.NewPublicHandler(a.TenantService, nil).Mount(r)
	}
	r.Route("/api/v1", func(r chi.Router) {
		iamH.MountPublic(r)
		if a.TenantApp != nil {
			(&tenantapp.Handler{Svc: a.TenantApp, IAM: a.IAM}).MountPublic(r) // registrasi tenant (tanpa token)
		}
		if a.Billing != nil {
			(&billing.Handler{Svc: a.Billing, IAM: a.IAM}).MountPublic(r) // callback payment gateway (signature per provider)
		}
		adH.MountPublic(r) // GET /public/app-downloads — hanya tautan aktif (Website PRD §22)
		if a.BVRooms != nil {
			(&bvrooms.Handler{Svc: a.BVRooms, IAM: a.IAM, DB: a.DB}).MountPublic(r) // katalog publik, OTP, endpoint customer (token bvrooms_customer)
		}
		growthH.MountPublic(r) // signup/verify/demo/events/plans (Website PRD §25, §33–§34, §40)
		r.Group(func(r chi.Router) {
			r.Use(a.IAM.Authenticate)
			r.Use(a.Growth.Guard) // trial expired/cancelled: mutasi diblokir (Website PRD §31)
			r.Use(httpx.Idempotency(a.DB))
			iamH.MountProtected(r)
			adH.MountAdmin(r) // /admin/app-downloads — role admin_internal (Website PRD §18–§19, §43)
			if a.Demo != nil {
				(&demo.Handler{Svc: a.Demo, IAM: a.IAM}).Mount(r) // /admin/demo — Demo Data (admin_internal)
			}
			growthH.Mount(r) // /onboarding, /trial, /growth/events
			propH.Mount(r)
			profH.Mount(r)
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
