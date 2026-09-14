package app

import (
	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/security"
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
	a.Security = secSvc
	a.Housekeeping = hkSvc
	opsH := &operations.Handler{Svc: a.Operations, IAM: a.IAM}
	return []Extension{
		extFn{"asset", func(a *App, r chi.Router) { (&asset.Handler{Svc: assetSvc, IAM: a.IAM}).Mount(r) }},
		extFn{"engineering", func(a *App, r chi.Router) { (&engineering.Handler{Svc: engSvc, IAM: a.IAM}).Mount(r) }},
		extFn{"security", func(a *App, r chi.Router) { (&security.Handler{Svc: secSvc, IAM: a.IAM, Ops: opsH}).Mount(r) }},
		extFn{"housekeeping", func(a *App, r chi.Router) { (&housekeeping.Handler{Svc: hkSvc, IAM: a.IAM, Ops: opsH}).Mount(r) }},
	}
}

type extFn struct {
	name  string
	mount func(a *App, r chi.Router)
}

func (e extFn) Name() string               { return e.name }
func (e extFn) Mount(a *App, r chi.Router) { e.mount(a, r) }
