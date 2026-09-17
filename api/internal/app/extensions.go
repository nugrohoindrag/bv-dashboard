package app

import (
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/booking"
	"github.com/buildingvision/api/internal/bvrooms"
	"github.com/buildingvision/api/internal/commercial"
	"github.com/buildingvision/api/internal/demo"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/exports"
	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/inventory"
	"github.com/buildingvision/api/internal/notification"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/overview"
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
	a.Overview = &overview.Service{DB: a.DB, Ops: a.Operations, CacheTTL: a.Cfg.OverviewCacheTTL}
	a.Sync = &bvsync.Service{DB: a.DB, Jobs: a.Jobs, Ops: a.Operations, Security: secSvc, Attachments: a.Attachments, Storage: a.Storage, ClockSkew: 10 * time.Minute}
	a.Exports = &exports.Service{DB: a.DB, Jobs: a.Jobs, Storage: a.Storage, IAM: a.IAM, Ops: a.Operations, Assets: assetSvc, SR: tsSvc, DownloadTTL: 24 * time.Hour}
	// P1: Mobile Tenant (consumer) & Tenant Relation (dashboard) — shared domain: tenantservice + profile + attachments
	a.TenantApp = tenantapp.New(a.DB, a.Jobs, tsSvc, a.Profile, a.Attachments, a.Operations)
	a.TenantRelation = tenantrelation.New(a.DB, a.Jobs, a.IAM)
	a.Booking = booking.New(a.DB, a.Jobs, a.Profile)
	a.Visitor = visitor.New(a.DB, a.Jobs, a.Profile)
	a.Billing = billing.New(a.DB, a.Jobs, a.Profile, a.Cfg.PublicURL)
	a.Vendor = vendor.New(a.DB, a.Jobs)
	a.Inventory = inventory.New(a.DB, a.Jobs)
	a.Hotel = hotel.New(a.DB, a.Jobs, a.Profile, a.Property, hkSvc, a.Billing, a.TenantRelation, a.Operations) // profile Hotel
	a.Commercial = commercial.New(a.DB, a.Jobs, a.Profile, a.Property, a.Billing, a.TenantRelation)            // profile Apartment: Unit Sales & Rental
	a.Reports = reports.New(a.DB)
	a.BVRooms = bvrooms.New(a.DB, a.Jobs, a.Storage, a.Hotel, a.Profile, a.IAM.Signer, a.Log, bvrooms.Config{Env: a.Cfg.Env, PublicURL: a.Cfg.PublicURL, RefreshTTL: a.Cfg.RefreshTokenTTL, VAPIDPublicKey: a.Cfg.VAPIDPublicKey, OTPStaticCode: a.Cfg.OTPStaticCode, OTPExposeCode: a.Cfg.OTPExposeCode, AuthMethod: a.Cfg.BVRoomsAuthMethod, DefaultPIN: a.Cfg.BVRoomsDefaultPIN})
	a.Demo = demo.New(demo.Deps{DB: a.DB, Storage: a.Storage, Log: a.Log, Env: a.Cfg.Env, IAM: a.IAM, Signer: a.IAM.Signer, PublicURL: a.Cfg.PublicURL, DemoEnabled: a.Cfg.DemoEnabled})
	if a.Cfg.VAPIDPublicKey != "" && a.Cfg.VAPIDPrivateKey != "" {
		a.BVRooms.Pusher = bvrooms.VAPIDPusher{PublicKey: a.Cfg.VAPIDPublicKey, PrivateKey: a.Cfg.VAPIDPrivateKey, Subscriber: a.Cfg.VAPIDSubject}
	}
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
		extFn{"overview", func(a *App, r chi.Router) { (&overview.Handler{Svc: a.Overview, IAM: a.IAM, Eng: engSvc}).Mount(r) }},
		extFn{"sync", func(a *App, r chi.Router) { (&bvsync.Handler{Svc: a.Sync, IAM: a.IAM}).Mount(r) }},
		extFn{"tenantapp", func(a *App, r chi.Router) { (&tenantapp.Handler{Svc: a.TenantApp, IAM: a.IAM}).Mount(r) }},
		extFn{"tenantrelation", func(a *App, r chi.Router) { (&tenantrelation.Handler{Svc: a.TenantRelation, IAM: a.IAM}).Mount(r) }},
		extFn{"booking", func(a *App, r chi.Router) { (&booking.Handler{Svc: a.Booking, IAM: a.IAM}).Mount(r) }},
		extFn{"visitor", func(a *App, r chi.Router) { (&visitor.Handler{Svc: a.Visitor, IAM: a.IAM}).Mount(r) }},
		extFn{"billing", func(a *App, r chi.Router) { (&billing.Handler{Svc: a.Billing, IAM: a.IAM}).Mount(r) }},
		extFn{"vendor", func(a *App, r chi.Router) { (&vendor.Handler{Svc: a.Vendor, IAM: a.IAM}).Mount(r) }},
		extFn{"inventory", func(a *App, r chi.Router) { (&inventory.Handler{Svc: a.Inventory, IAM: a.IAM}).Mount(r) }},
		extFn{"hotel", func(a *App, r chi.Router) { (&hotel.Handler{Svc: a.Hotel, IAM: a.IAM}).Mount(r) }},
		extFn{"commercial", func(a *App, r chi.Router) { (&commercial.Handler{Svc: a.Commercial, IAM: a.IAM}).Mount(r) }},
		extFn{"reports", func(a *App, r chi.Router) { (&reports.Handler{Svc: a.Reports, IAM: a.IAM}).Mount(r) }},
		extFn{"bvrooms", func(a *App, r chi.Router) { (&bvrooms.Handler{Svc: a.BVRooms, IAM: a.IAM, DB: a.DB}).MountAdmin(r) }},
	}
}

type extFn struct {
	name  string
	mount func(a *App, r chi.Router)
}

func (e extFn) Name() string               { return e.name }
func (e extFn) Mount(a *App, r chi.Router) { e.mount(a, r) }
