package app

import (
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/exports"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/notification"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/search"
	"github.com/buildingvision/api/internal/security"
	"github.com/buildingvision/api/internal/tenantservice"
)

// DefaultExtensions: modul domain yang di-mount ke API (engineering, security, housekeeping,
// tenantservice, notification, overview, search, sync — ditambah bertahap).
func DefaultExtensions(a *App) []Extension {
	assetSvc := &asset.Service{DB: a.DB, Jobs: a.Jobs}
	asset.SetQRBaseURL(a.Cfg.QRBaseURL)
	engSvc := engineering.New(a.DB, a.Jobs, a.Operations)
	secSvc := security.New(a.DB, a.Jobs, a.Operations)
	hkSvc := housekeeping.New(a.DB, a.Jobs, a.Operations)
	a.Asset = assetSvc
	a.Engineering = engSvc
	tsSvc := tenantservice.New(a.DB, a.Jobs, a.Operations)
	a.Security = secSvc
	a.Housekeeping = hkSvc
	a.TenantService = tsSvc
	a.Notification = &notification.Service{DB: a.DB, Jobs: a.Jobs, PublicURL: a.Cfg.PublicURL}
	a.Search = &search.Service{DB: a.DB}
	a.Exports = &exports.Service{DB: a.DB, Jobs: a.Jobs, Storage: a.Storage, IAM: a.IAM, Ops: a.Operations, Assets: assetSvc, SR: tsSvc, DownloadTTL: 24 * time.Hour}
	opsH := &operations.Handler{Svc: a.Operations, IAM: a.IAM}
	return []Extension{
		extFn{"asset", func(a *App, r chi.Router) { (&asset.Handler{Svc: assetSvc, IAM: a.IAM}).Mount(r) }},
		extFn{"engineering", func(a *App, r chi.Router) { (&engineering.Handler{Svc: engSvc, IAM: a.IAM}).Mount(r) }},
		extFn{"security", func(a *App, r chi.Router) { (&security.Handler{Svc: secSvc, IAM: a.IAM, Ops: opsH}).Mount(r) }},
		extFn{"housekeeping", func(a *App, r chi.Router) { (&housekeeping.Handler{Svc: hkSvc, IAM: a.IAM, Ops: opsH}).Mount(r) }},
		extFn{"tenantservice", func(a *App, r chi.Router) { (&tenantservice.Handler{Svc: tsSvc, IAM: a.IAM, Ops: opsH}).Mount(r) }},
		extFn{"notification", func(a *App, r chi.Router) { (&notification.Handler{Svc: a.Notification, IAM: a.IAM}).Mount(r) }},
		extFn{"search", func(a *App, r chi.Router) { (&search.Handler{Svc: a.Search, IAM: a.IAM}).Mount(r) }},
		extFn{"exports", func(a *App, r chi.Router) { (&exports.Handler{Svc: a.Exports, IAM: a.IAM}).Mount(r) }},
	}
}

type extFn struct {
	name  string
	mount func(a *App, r chi.Router)
}

func (e extFn) Name() string               { return e.name }
func (e extFn) Mount(a *App, r chi.Router) { e.mount(a, r) }
