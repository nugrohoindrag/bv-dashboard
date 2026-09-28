package app

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/booking"
	"github.com/buildingvision/api/internal/bvrooms"
	"github.com/buildingvision/api/internal/commercial"
	"github.com/buildingvision/api/internal/demo"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/exports"
	"github.com/buildingvision/api/internal/finance"
	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/inventory"
	"github.com/buildingvision/api/internal/metering"
	"github.com/buildingvision/api/internal/notification"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/overview"
	"github.com/buildingvision/api/internal/parcels"
	"github.com/buildingvision/api/internal/reports"
	"github.com/buildingvision/api/internal/search"
	"github.com/buildingvision/api/internal/security"
	bvsync "github.com/buildingvision/api/internal/sync"
	"github.com/buildingvision/api/internal/tenantapp"
	"github.com/buildingvision/api/internal/tenantrelation"
	"github.com/buildingvision/api/internal/tenantservice"
	"github.com/buildingvision/api/internal/vendor"
	"github.com/buildingvision/api/internal/visitor"
	"github.com/buildingvision/api/internal/waassist"
	"github.com/buildingvision/api/internal/workforce"
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
	// PRD P2 v2.1 P2-EQH-03: health diperbarui setelah perbaikan / inspeksi (Roadmap §17 contoh 3 "Equipment Health Updated")
	a.Operations.RegisterHook("work_order:*", assetHealthHook{svc: assetSvc})
	a.Operations.RegisterHook("task:inspection", assetHealthHook{svc: assetSvc})
	a.Engineering = engSvc
	tsSvc := tenantservice.New(a.DB, a.Jobs, a.Operations)
	a.Security = secSvc
	a.Housekeeping = hkSvc
	a.TenantService = tsSvc
	a.Notification = &notification.Service{DB: a.DB, Jobs: a.Jobs, PublicURL: a.Cfg.PublicURL,
		Channels: map[string]notification.ChannelAdapter{"email": notification.EmailChannel{Mailer: a.Mailer}}}
	a.Search = &search.Service{DB: a.DB}
	a.Overview = &overview.Service{DB: a.DB, Ops: a.Operations, CacheTTL: a.Cfg.OverviewCacheTTL}
	a.Workforce = workforce.New(a.DB, a.Jobs)
	a.Overview.Workforce = a.Workforce
	// lampiran object domain P2 (dokumen equipment) — validasi akses via registry operations
	a.Operations.RegisterObjectAccess("asset_document", assetSvc.DocumentAccess)
	a.Sync = &bvsync.Service{DB: a.DB, Jobs: a.Jobs, Ops: a.Operations, Security: secSvc, Workforce: a.Workforce, Housekeeping: hkSvc, Attachments: a.Attachments, Storage: a.Storage, ClockSkew: 10 * time.Minute}
	a.Exports = &exports.Service{DB: a.DB, Jobs: a.Jobs, Storage: a.Storage, IAM: a.IAM, Ops: a.Operations, Assets: assetSvc, SR: tsSvc, Property: a.Property, DownloadTTL: 24 * time.Hour}
	// P1: Mobile Tenant (consumer) & Tenant Relation (dashboard) — shared domain: tenantservice + profile + attachments
	a.TenantApp = tenantapp.New(a.DB, a.Jobs, tsSvc, a.Profile, a.Attachments, a.Operations)
	a.TenantRelation = tenantrelation.New(a.DB, a.Jobs, a.IAM)
	a.TenantRelation.Attachments = a.Attachments
	// PRD P3 v2.1 P3-ACC-08: Tenant Admin membuat akun anggota lewat inti pembuatan akun Tenant Relation
	a.TenantApp.Accounts = memberAccounts{tr: a.TenantRelation}
	a.TenantApp.OnUserChanged = a.IAM.InvalidateUser
	a.Operations.RegisterObjectAccess("tenant_feedback", tenantapp.FeedbackAccess)
	// PRD P3 v2.1: Package, kendaraan tenant (foto/STNK), WhatsApp manual
	a.Parcels = parcels.New(a.DB, a.Jobs, a.Attachments)
	a.Operations.RegisterObjectAccess("package", parcels.Access)
	a.Operations.RegisterObjectAccess("vehicle", security.VehicleAccess)
	tenantAppURL := a.Cfg.TenantAppURL
	a.WAAssist = &waassist.Service{DB: a.DB, Profile: a.Profile, TenantAppURL: tenantAppURL, PublicURL: a.Cfg.PublicURL}
	a.Booking = booking.New(a.DB, a.Jobs, a.Profile)
	a.Visitor = visitor.New(a.DB, a.Jobs, a.Profile)
	a.Billing = billing.New(a.DB, a.Jobs, a.Profile, a.Cfg.PublicURL)
	// PRD P4 v2.1: meter & pemakaian (billing rule meter_usage), tautan dokumen PDF bertanda tangan, lampiran bukti bayar/biaya
	a.Metering = metering.New(a.DB, a.Jobs, a.Attachments)
	a.Billing.Metering = a.Metering
	a.Sync.Metering = a.Metering
	a.Billing.DocumentSecret = documentSecret(a.IAM.Signer)
	a.Operations.RegisterObjectAccess("meter", a.Metering.Access("meter"))
	a.Operations.RegisterObjectAccess("meter_reading", a.Metering.Access("meter_reading"))
	a.Operations.RegisterObjectAccess("payment", billing.PaymentAccess)
	a.Finance = finance.New(a.DB, a.Jobs, a.Attachments)
	a.Finance.AllowInsecureWebhooks = a.Cfg.Env != "production"
	a.Operations.RegisterObjectAccess("cost_entry", finance.CostAccess)
	a.Vendor = vendor.New(a.DB, a.Jobs)
	a.Exports.Vendors = a.Vendor // export vendors (PRD P0 v2 §18)
	a.Inventory = inventory.New(a.DB, a.Jobs)
	a.Sync.Inventory = a.Inventory                                                                             // PRD P2 v2.1 P2-CNS-02: use_consumable offline
	a.Hotel = hotel.New(a.DB, a.Jobs, a.Profile, a.Property, hkSvc, a.Billing, a.TenantRelation, a.Operations) // profile Hotel
	a.Commercial = commercial.New(a.DB, a.Jobs, a.Profile, a.Property, a.Billing, a.TenantRelation)            // profile Apartment: Unit Sales & Rental
	a.Reports = reports.New(a.DB)
	a.BVRooms = bvrooms.New(a.DB, a.Jobs, a.Storage, a.Hotel, a.Profile, a.IAM.Signer, a.Log, bvrooms.Config{Env: a.Cfg.Env, PublicURL: a.Cfg.PublicURL, RefreshTTL: a.Cfg.RefreshTokenTTL, VAPIDPublicKey: a.Cfg.VAPIDPublicKey, OTPStaticCode: a.Cfg.OTPStaticCode, OTPExposeCode: a.Cfg.OTPExposeCode, AuthMethod: a.Cfg.BVRoomsAuthMethod, DefaultPIN: a.Cfg.BVRoomsDefaultPIN, MaxImageBytes: a.Cfg.MaxImageBytes})
	a.Demo = demo.New(demo.Deps{DB: a.DB, Storage: a.Storage, Log: a.Log, Env: a.Cfg.Env, IAM: a.IAM, Signer: a.IAM.Signer, PublicURL: a.Cfg.PublicURL, DemoEnabled: a.Cfg.DemoEnabled})
	if a.Cfg.VAPIDPublicKey != "" && a.Cfg.VAPIDPrivateKey != "" {
		a.BVRooms.Pusher = bvrooms.VAPIDPusher{PublicKey: a.Cfg.VAPIDPublicKey, PrivateKey: a.Cfg.VAPIDPrivateKey, Subscriber: a.Cfg.VAPIDSubject}
		// PRD P3 v2.1 P3-PSH-01: Web Push Tenant PWA memakai kunci VAPID yang sama
		a.Notification.WebPush = notification.VAPIDSender{PublicKey: a.Cfg.VAPIDPublicKey, PrivateKey: a.Cfg.VAPIDPrivateKey, Subscriber: a.Cfg.VAPIDSubject}
		a.Notification.VAPIDPublicKey = a.Cfg.VAPIDPublicKey
	}
	a.Notification.FCMEnabled = a.Cfg.FCMProjectID != "" && a.Cfg.FCMServiceAccountJSON != ""
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
		extFn{"billing", func(a *App, r chi.Router) {
			(&billing.Handler{Svc: a.Billing, IAM: a.IAM, Att: a.Attachments}).Mount(r)
		}},
		extFn{"metering", func(a *App, r chi.Router) { (&metering.Handler{Svc: a.Metering, IAM: a.IAM}).Mount(r) }},
		extFn{"finance", func(a *App, r chi.Router) { (&finance.Handler{Svc: a.Finance, IAM: a.IAM}).Mount(r) }},
		extFn{"vendor", func(a *App, r chi.Router) { (&vendor.Handler{Svc: a.Vendor, IAM: a.IAM}).Mount(r) }},
		extFn{"inventory", func(a *App, r chi.Router) { (&inventory.Handler{Svc: a.Inventory, IAM: a.IAM}).Mount(r) }},
		extFn{"hotel", func(a *App, r chi.Router) { (&hotel.Handler{Svc: a.Hotel, IAM: a.IAM}).Mount(r) }},
		extFn{"commercial", func(a *App, r chi.Router) { (&commercial.Handler{Svc: a.Commercial, IAM: a.IAM}).Mount(r) }},
		extFn{"reports", func(a *App, r chi.Router) { (&reports.Handler{Svc: a.Reports, IAM: a.IAM}).Mount(r) }},
		extFn{"bvrooms", func(a *App, r chi.Router) { (&bvrooms.Handler{Svc: a.BVRooms, IAM: a.IAM, DB: a.DB}).MountAdmin(r) }},
		extFn{"workforce", func(a *App, r chi.Router) { (&workforce.Handler{Svc: a.Workforce, IAM: a.IAM}).Mount(r) }},
		extFn{"parcels", func(a *App, r chi.Router) { (&parcels.Handler{Svc: a.Parcels, IAM: a.IAM}).Mount(r) }},
		extFn{"waassist", func(a *App, r chi.Router) { (&waassist.Handler{Svc: a.WAAssist, IAM: a.IAM}).Mount(r) }},
	}
}

// memberAccounts: adapter tenantapp.AccountCreator → tenantrelation (tanpa import siklik).
type memberAccounts struct{ tr *tenantrelation.Service }

func (m memberAccounts) CreateMemberTx(ctx context.Context, tx pgx.Tx, in tenantapp.MemberAccount) (uuid.UUID, string, error) {
	return m.tr.CreateMemberTx(ctx, tx, tenantrelation.MemberInput{PropertyID: in.PropertyID, TenantID: in.TenantID, FullName: in.FullName, Email: in.Email, Phone: in.Phone, UnitIDs: in.UnitIDs})
}

type extFn struct {
	name  string
	mount func(a *App, r chi.Router)
}

func (e extFn) Name() string               { return e.name }
func (e extFn) Mount(a *App, r chi.Router) { e.mount(a, r) }

// assetHealthHook: WO/inspeksi ber-asset selesai/ditutup → hitung ulang equipment health (tanpa impor siklik asset↔operations).
type assetHealthHook struct{ svc *asset.Service }

func (assetHealthHook) BeforeComplete(context.Context, pgx.Tx, *operations.WorkItem, operations.TransitionInput) error {
	return nil
}

func (h assetHealthHook) AfterTransition(ctx context.Context, tx pgx.Tx, item *operations.WorkItem, action string, from, to workflow.Status) error {
	if item.Asset.ID == nil {
		return nil
	}
	switch action {
	case workflow.ActComplete, workflow.ActClose, workflow.ActReopen, workflow.ActCancel, workflow.ActStart:
		_, err := h.svc.RecomputeHealthTx(ctx, tx, *item.Asset.ID)
		return err
	}
	return nil
}

// documentSecret: kunci HMAC tautan dokumen PDF publik (PRD P4 v2.1 P4-TNT-03) diturunkan dari kunci penandatangan JWT —
// tetap selama kunci JWT tetap (tautan tidak berlaku lagi bila kunci dirotasi).
func documentSecret(signer *iam.TokenSigner) []byte {
	if signer == nil {
		return nil
	}
	priv, _ := signer.Keys()
	if len(priv) == 0 {
		return nil
	}
	h := sha256.New()
	h.Write([]byte("bv-document-link-v1:"))
	h.Write(priv.Seed())
	return h.Sum(nil)
}
