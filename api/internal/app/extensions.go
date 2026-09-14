package app

import (
	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/engineering"
)

// DefaultExtensions: modul domain yang di-mount ke API (engineering, security, housekeeping,
// tenantservice, notification, overview, search, sync — ditambah bertahap).
func DefaultExtensions(a *App) []Extension {
	assetSvc := &asset.Service{DB: a.DB, Jobs: a.Jobs}
	asset.SetQRBaseURL(a.Cfg.QRBaseURL)
	engSvc := engineering.New(a.DB, a.Jobs, a.Operations)
	a.Asset = assetSvc
	a.Engineering = engSvc
	return []Extension{
		extFn{"asset", func(a *App, r chi.Router) { (&asset.Handler{Svc: assetSvc, IAM: a.IAM}).Mount(r) }},
		extFn{"engineering", func(a *App, r chi.Router) { (&engineering.Handler{Svc: engSvc, IAM: a.IAM}).Mount(r) }},
	}
}

type extFn struct {
	name  string
	mount func(a *App, r chi.Router)
}

func (e extFn) Name() string               { return e.name }
func (e extFn) Mount(a *App, r chi.Router) { e.mount(a, r) }
