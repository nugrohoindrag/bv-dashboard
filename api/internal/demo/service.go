// Package demo: Demo Seed Database & Demo Environment (Development Instruction v1.1).
// Membentuk satu organization demo ("BuildingVision Demo", slug `demo`) dengan tiga property lengkap — Hotel, Apartment,
// Office — yang saling terhubung end-to-end (master data → user → tenant/guest → ticket → work order → evidence → resolusi →
// notifikasi → CSAT, booking, visitor, billing, inventory, vendor, hotel booking, BVRooms, unit sales & rental).
// Seed memakai service layer aplikasi (bukan SQL langsung) agar business rule, audit, dan event tetap berlaku (§35);
// timestamp historis disesuaikan setelahnya. Reset menghapus seluruh data organization demo (marker `settings.demo`),
// tidak pernah menyentuh organization lain (§32, §39).
package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/booking"
	"github.com/buildingvision/api/internal/bvrooms"
	"github.com/buildingvision/api/internal/commercial"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/inventory"
	"github.com/buildingvision/api/internal/notification"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/storage"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/search"
	"github.com/buildingvision/api/internal/security"
	"github.com/buildingvision/api/internal/seed"
	"github.com/buildingvision/api/internal/tenantapp"
	"github.com/buildingvision/api/internal/tenantrelation"
	"github.com/buildingvision/api/internal/tenantservice"
	"github.com/buildingvision/api/internal/vendor"
	"github.com/buildingvision/api/internal/visitor"
)

const (
	OrgSlug     = "demo"
	OrgName     = "BuildingVision Demo"
	SeedVersion = "1.1"
	// Password seluruh akun demo (dev/demo saja; didokumentasikan di docs/demo/BuildingVision-Demo-Guide.md).
	Password   = "Demo12345!"
	AdminEmail = "admin.demo@buildingvision.local"

	ProfileHotel     = "hotel"
	ProfileApartment = "apartment"
	ProfileOffice    = "office"
)

var Profiles = []string{ProfileHotel, ProfileApartment, ProfileOffice}

// Deps: dependensi platform (service domain dibangun ulang di New dengan enqueuer sinkron).
type Deps struct {
	DB          *db.DB
	Storage     storage.Storage
	Log         *slog.Logger
	Env         string
	IAM         *iam.Service
	Signer      *iam.TokenSigner
	PublicURL   string
	DemoEnabled bool // BV_DEMO_ENABLED: izinkan tooling demo di production (§39)
}

type Service struct {
	Deps
	mu      sync.Mutex
	running bool

	jobs        *syncEnqueuer
	subscribers []events.Subscriber

	Profile      *profile.Service
	Property     *property.Service
	Attachments  *attachments.Service
	Ops          *operations.Service
	Asset        *asset.Service
	Eng          *engineering.Service
	Sec          *security.Service
	HK           *housekeeping.Service
	TS           *tenantservice.Service
	Notification *notification.Service
	Search       *search.Service
	TenantApp    *tenantapp.Service
	TR           *tenantrelation.Service
	Booking      *booking.Service
	Visitor      *visitor.Service
	Billing      *billing.Service
	Vendor       *vendor.Service
	Inventory    *inventory.Service
	Hotel        *hotel.Service
	Commercial   *commercial.Service
	BVRooms      *bvrooms.Service
}

// New merakit service domain dengan enqueuer sinkron: event domain dikirim ke Notification & Search segera setelah
// setiap langkah seed (bukan lewat worker), sehingga inbox demo terisi deterministik (§24).
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	s := &Service{Deps: d, jobs: &syncEnqueuer{}}
	j := s.jobs
	s.Profile = profile.New(d.DB, j)
	s.Property = &property.Service{DB: d.DB, Jobs: j}
	s.Attachments = &attachments.Service{DB: d.DB, Storage: d.Storage, Jobs: j, UploadTTL: 15 * time.Minute, DownloadTTL: time.Hour, MaxBytes: 10 << 20}
	s.Ops = operations.NewService(d.DB, j, s.Attachments)
	s.Ops.RegisterHook("work_order:*", operations.FindingHook(s.Ops))
	s.Ops.RegisterHook("task:*", operations.FindingHook(s.Ops))
	s.Asset = &asset.Service{DB: d.DB, Jobs: j}
	s.Eng = engineering.New(d.DB, j, s.Ops)
	s.Sec = security.New(d.DB, j, s.Ops)
	s.HK = housekeeping.New(d.DB, j, s.Ops)
	s.TS = tenantservice.New(d.DB, j, s.Ops)
	s.Notification = &notification.Service{DB: d.DB, Jobs: j, PublicURL: d.PublicURL}
	s.Search = &search.Service{DB: d.DB}
	s.TenantApp = tenantapp.New(d.DB, j, s.TS, s.Profile, s.Attachments, s.Ops)
	s.TenantApp.SetRegistrationRateLimit(0)
	s.TR = tenantrelation.New(d.DB, j, d.IAM)
	s.Booking = booking.New(d.DB, j, s.Profile)
	s.Visitor = visitor.New(d.DB, j, s.Profile)
	s.Billing = billing.New(d.DB, j, s.Profile, d.PublicURL)
	s.Vendor = vendor.New(d.DB, j)
	s.Inventory = inventory.New(d.DB, j)
	s.Hotel = hotel.New(d.DB, j, s.Profile, s.Property, s.HK, s.Billing, s.TR, s.Ops)
	s.Commercial = commercial.New(d.DB, j, s.Profile, s.Property, s.Billing, s.TR)
	s.BVRooms = bvrooms.New(d.DB, j, d.Storage, s.Hotel, s.Profile, d.Signer, d.Log, bvrooms.Config{Env: d.Env, PublicURL: d.PublicURL})
	s.BVRooms.SetIPRateLimit(0)
	s.subscribers = []events.Subscriber{s.Notification, s.Search}
	return s
}

// ---------- enqueuer sinkron ----------

type syncEnqueuer struct {
	mu     sync.Mutex
	events []events.Event
}

func (e *syncEnqueuer) EnqueueTx(_ context.Context, _ pgx.Tx, args river.JobArgs) error {
	if de, ok := args.(jobs.DomainEventArgs); ok {
		e.mu.Lock()
		e.events = append(e.events, de.Event)
		e.mu.Unlock()
	}
	return nil
}

func (e *syncEnqueuer) EnqueueEventTx(ctx context.Context, tx pgx.Tx, ev events.Event) error {
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	return e.EnqueueTx(ctx, tx, jobs.DomainEventArgs{Event: ev})
}

// flush: kirim event yang terkumpul ke subscriber (setelah transaksi service commit).
func (s *Service) flush(ctx context.Context) {
	s.jobs.mu.Lock()
	evs := s.jobs.events
	s.jobs.events = nil
	s.jobs.mu.Unlock()
	for _, ev := range evs {
		for _, sub := range s.subscribers {
			if err := sub.Handle(context.WithoutCancel(ctx), ev); err != nil {
				s.Log.Warn("demo event subscriber", "subscriber", sub.Name(), "event", ev.Type, "err", err)
			}
		}
	}
}

// ---------- state (marker) ----------

type ProfileState struct {
	PropertyID  *uuid.UUID `json:"property_id,omitempty"`
	SeededAt    *time.Time `json:"seeded_at,omitempty"`
	RecordCount int        `json:"record_count"`
	Version     string     `json:"seed_version,omitempty"`
}

type State struct {
	Present    bool                    `json:"present"`
	OrgID      *uuid.UUID              `json:"organization_id,omitempty"`
	OrgSlug    string                  `json:"organization_slug"`
	State      string                  `json:"state"` // empty | seeding | ready | failed
	Error      string                  `json:"error,omitempty"`
	Version    string                  `json:"seed_version,omitempty"`
	LastSeeded *time.Time              `json:"last_seeded_at,omitempty"`
	LastReset  *time.Time              `json:"last_reset_at,omitempty"`
	Profiles   map[string]ProfileState `json:"profiles"`
	Running    bool                    `json:"running"`
	Enabled    bool                    `json:"enabled"` // demo tooling nonaktif di production kecuali BV_DEMO_ENABLED
}

type marker struct {
	Version  string                  `json:"version"`
	State    string                  `json:"state"`
	Error    string                  `json:"error,omitempty"`
	SeededAt *time.Time              `json:"seeded_at,omitempty"`
	ResetAt  *time.Time              `json:"reset_at,omitempty"`
	Profiles map[string]ProfileState `json:"profiles"`
}

func (s *Service) readMarker(ctx context.Context) (*uuid.UUID, *marker, error) {
	var id uuid.UUID
	var settings []byte
	err := s.DB.Pool.QueryRow(ctx, `SELECT id, settings FROM organizations WHERE slug = $1`, OrgSlug).Scan(&id, &settings)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var st struct {
		Demo *marker `json:"demo"`
	}
	_ = json.Unmarshal(settings, &st)
	if st.Demo == nil {
		st.Demo = &marker{Profiles: map[string]ProfileState{}}
	}
	if st.Demo.Profiles == nil {
		st.Demo.Profiles = map[string]ProfileState{}
	}
	return &id, st.Demo, nil
}

func (s *Service) writeMarker(ctx context.Context, orgID uuid.UUID, m *marker) error {
	b, _ := json.Marshal(m)
	_, err := s.DB.Pool.Exec(ctx, `UPDATE organizations SET settings = jsonb_set(COALESCE(settings,'{}'::jsonb), '{demo}', $2::jsonb, true) WHERE id = $1`, orgID, b)
	return err
}

// Status: ringkasan environment demo untuk dashboard Admin Internal & CLI.
func (s *Service) Status(ctx context.Context) (*State, error) {
	out := &State{OrgSlug: OrgSlug, Profiles: map[string]ProfileState{}, Enabled: s.Enabled()}
	s.mu.Lock()
	out.Running = s.running
	s.mu.Unlock()
	id, m, err := s.readMarker(ctx)
	if err != nil {
		return nil, err
	}
	if id == nil {
		out.State = "empty"
		return out, nil
	}
	out.Present, out.OrgID = true, id
	out.State, out.Error, out.Version, out.LastSeeded, out.LastReset = m.State, m.Error, m.Version, m.SeededAt, m.ResetAt
	if out.State == "" {
		out.State = "ready"
	}
	for _, p := range Profiles {
		ps := m.Profiles[p]
		if ps.PropertyID != nil {
			ps.RecordCount, _ = s.countRecords(ctx, *id, *ps.PropertyID)
		}
		out.Profiles[p] = ps
	}
	return out, nil
}

// Enabled: tooling demo aktif di local/staging/test; production butuh BV_DEMO_ENABLED=true (§39).
func (s *Service) Enabled() bool {
	return s.Env != "production" || s.DemoEnabled
}

// countRecords: jumlah record operasional utama per property (indikator dashboard).
func (s *Service) countRecords(ctx context.Context, orgID, propertyID uuid.UUID) (int, error) {
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		for _, q := range []string{
			`SELECT count(*) FROM locations WHERE property_id = $1 AND deleted_at IS NULL`,
			`SELECT count(*) FROM service_requests WHERE property_id = $1`,
			`SELECT count(*) FROM work_orders WHERE property_id = $1`,
			`SELECT count(*) FROM tasks WHERE property_id = $1`,
			`SELECT count(*) FROM tenants WHERE property_id = $1`,
			`SELECT count(*) FROM bookings WHERE property_id = $1`,
			`SELECT count(*) FROM visitors WHERE property_id = $1`,
			`SELECT count(*) FROM invoices WHERE property_id = $1`,
			`SELECT count(*) FROM assets WHERE property_id = $1`,
			`SELECT count(*) FROM hotel_reservations WHERE property_id = $1`,
			`SELECT count(*) FROM bvrooms_bookings WHERE property_id = $1`,
		} {
			var c int
			if err := tx.QueryRow(ctx, q, propertyID).Scan(&c); err != nil {
				return err
			}
			n += c
		}
		return nil
	})
	return n, err
}

// ---------- orkestrasi ----------

type SeedResult struct {
	Profiles []string      `json:"profiles"`
	Took     time.Duration `json:"took"`
	State    *State        `json:"state"`
	Log      []string      `json:"log"`
}

// Seed: seed profile yang diminta (idempotent: profile yang sudah ada dilewati; pakai Reset lalu Seed untuk reseed).
func (s *Service) Seed(ctx context.Context, profiles []string) (*SeedResult, error) {
	if !s.Enabled() {
		return nil, apperr.Forbidden("Demo tooling dinonaktifkan pada environment ini")
	}
	if len(profiles) == 0 {
		profiles = Profiles
	}
	for _, p := range profiles {
		if p != ProfileHotel && p != ProfileApartment && p != ProfileOffice {
			return nil, apperr.Validation("profile harus hotel|apartment|office")
		}
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil, apperr.Conflict("DEMO_BUSY", "Operasi demo lain sedang berjalan")
	}
	s.running = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running = false; s.mu.Unlock() }()

	start := time.Now()
	res := &SeedResult{Profiles: profiles}
	logf := func(format string, a ...any) {
		msg := fmt.Sprintf(format, a...)
		res.Log = append(res.Log, msg)
		s.Log.Info("demo seed", "msg", msg)
	}
	env, err := s.ensureOrg(ctx, logf)
	if err != nil {
		return nil, err
	}
	_, m, err := s.readMarker(ctx)
	if err != nil {
		return nil, err
	}
	m.State, m.Error, m.Version = "seeding", "", SeedVersion
	_ = s.writeMarker(ctx, env.OrgID, m)
	fail := func(err error) (*SeedResult, error) {
		m.State, m.Error = "failed", err.Error()
		_ = s.writeMarker(ctx, env.OrgID, m)
		return nil, fmt.Errorf("demo seed: %w", err)
	}
	for _, p := range profiles {
		if ps, ok := m.Profiles[p]; ok && ps.PropertyID != nil {
			logf("%s: sudah di-seed (%s) — dilewati (reset untuk reseed)", p, ps.SeededAt.Format(time.RFC3339))
			continue
		}
		var propID uuid.UUID
		var err error
		switch p {
		case ProfileHotel:
			propID, err = s.seedHotel(ctx, env, logf)
		case ProfileApartment:
			propID, err = s.seedApartment(ctx, env, logf)
		case ProfileOffice:
			propID, err = s.seedOffice(ctx, env, logf)
		}
		if err != nil {
			return fail(fmt.Errorf("%s: %w", p, err))
		}
		now := time.Now().UTC()
		m.Profiles[p] = ProfileState{PropertyID: &propID, SeededAt: &now, Version: SeedVersion}
		_ = s.writeMarker(ctx, env.OrgID, m)
	}
	// SLA/overdue sweep agar flag SLA Risk/Overdue terhitung dari due date historis (§11)
	if _, err := s.Ops.OverdueSweep(ctx, env.OrgID); err != nil {
		logf("overdue sweep: %v", err)
	}
	if _, err := s.Ops.SLASweep(ctx, env.OrgID); err != nil {
		logf("sla sweep: %v", err)
	}
	_ = s.Billing.Sweep(ctx, env.OrgID)
	if _, err := s.BVRooms.Sweep(ctx, env.OrgID); err != nil {
		logf("bvrooms sweep: %v", err)
	}
	s.flush(ctx)
	now := time.Now().UTC()
	m.State, m.SeededAt, m.Error = "ready", &now, ""
	if err := s.writeMarker(ctx, env.OrgID, m); err != nil {
		return nil, err
	}
	res.Took = time.Since(start)
	res.State, _ = s.Status(ctx)
	logf("selesai dalam %s", res.Took.Round(time.Millisecond))
	return res, nil
}

// Env: referensi organization demo & akun bersama (dibagikan ke seeder per profile).
type Env struct {
	OrgID   uuid.UUID
	AdminID uuid.UUID
	// user bersama lintas property (email → id)
	Users   map[string]uuid.UUID
	Vendors map[string]uuid.UUID
	Items   map[string]uuid.UUID // inventory item master (org-level)
	// checklist template org-level (kode → id)
	Templates map[string]uuid.UUID
	sysCtx    context.Context
}

// ensureOrg: organization demo + role sistem + admin + user bersama + vendor + item master (idempotent).
func (s *Service) ensureOrg(ctx context.Context, logf func(string, ...any)) (*Env, error) {
	orgID, created, err := seed.EnsureOrganization(ctx, s.DB, OrgName, OrgSlug)
	if err != nil {
		return nil, err
	}
	env := &Env{OrgID: orgID, Users: map[string]uuid.UUID{}, Vendors: map[string]uuid.UUID{}, Items: map[string]uuid.UUID{}, Templates: map[string]uuid.UUID{}}
	if created {
		err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
			adminID, err := seed.SeedOrganization(ctx, tx, s.IAM, orgID, AdminEmail, Password)
			if err != nil {
				return err
			}
			env.AdminID = adminID
			_, err = tx.Exec(ctx, `UPDATE organizations SET settings = jsonb_set(COALESCE(settings,'{}'::jsonb), '{demo}', $2::jsonb, true), name = $3 WHERE id = $1`, orgID, mustJSON(marker{Version: SeedVersion, State: "seeding", Profiles: map[string]ProfileState{}}), OrgName)
			return err
		})
		if err != nil {
			return nil, err
		}
		logf("organization demo dibuat: %s (%s) · admin %s", OrgName, OrgSlug, AdminEmail)
	} else {
		if err := s.scanOne(ctx, orgID, `SELECT id FROM users WHERE organization_id = $1 AND lower(email) = $2 AND deleted_at IS NULL`, []any{orgID, AdminEmail}, &env.AdminID); err != nil {
			return nil, fmt.Errorf("admin demo tidak ditemukan: %w", err)
		}
	}
	env.sysCtx = authctx.With(ctx, &authctx.Principal{UserID: env.AdminID, OrganizationID: orgID, IsSystem: true, FullName: "Demo Seed", Source: authctx.SourceSystem})
	if err := s.seedCommon(ctx, env, logf); err != nil {
		return nil, err
	}
	return env, nil
}

// ---------- principal helper ----------

// as: context dengan principal user staf/tenant (permission nyata dari role) — audit tercatat atas nama user tersebut.
func (s *Service) as(ctx context.Context, env *Env, userID uuid.UUID) context.Context {
	p, err := s.IAM.LoadPrincipal(ctx, userID, env.OrgID)
	if err != nil {
		s.Log.Warn("demo principal", "user", userID, "err", err)
		return env.sysCtx
	}
	p.Source = authctx.SourceWeb
	p.FullName = strings.TrimSpace(p.FullName)
	return authctx.With(ctx, p)
}

func (s *Service) asEmail(ctx context.Context, env *Env, email string) context.Context {
	if id, ok := env.Users[email]; ok {
		return s.as(ctx, env, id)
	}
	var id uuid.UUID
	if err := s.scanOne(ctx, env.OrgID, `SELECT id FROM users WHERE organization_id = $1 AND lower(email) = $2 AND deleted_at IS NULL`, []any{env.OrgID, strings.ToLower(email)}, &id); err != nil {
		s.Log.Warn("demo user not found", "email", email)
		return env.sysCtx
	}
	env.Users[email] = id
	return s.as(ctx, env, id)
}

// asCustomer: principal customer BVRooms.
func (s *Service) asCustomer(ctx context.Context, env *Env, customerID uuid.UUID, name string) context.Context {
	return authctx.With(ctx, &authctx.Principal{UserID: customerID, OrganizationID: env.OrgID, SessionID: uuid.Nil, FullName: name, Source: authctx.SourceBVRooms, IP: "127.0.0.1", UserAgent: "demo-seed"})
}

// scanOne: satu baris query org-scoped (RLS membutuhkan app.organization_id).
func (s *Service) scanOne(ctx context.Context, orgID uuid.UUID, sql string, args []any, dst ...any) error {
	return s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sql, args...).Scan(dst...)
	})
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func ptr[T any](v T) *T { return &v }

func date(t time.Time) string { return t.Format("2006-01-02") }

// jkt: hari ini 00:00 zona Jakarta (UTC-clock untuk kolom date).
func today() time.Time {
	loc, _ := time.LoadLocation("Asia/Jakarta")
	n := time.Now().In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func at(t time.Time, hour, min int) time.Time {
	loc, _ := time.LoadLocation("Asia/Jakarta")
	return time.Date(t.Year(), t.Month(), t.Day(), hour, min, 0, 0, loc)
}
