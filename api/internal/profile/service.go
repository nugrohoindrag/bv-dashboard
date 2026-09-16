package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/jobs"
)

const (
	EventProfileChanged = "property.profile_changed"
	CodeCapability      = "CAPABILITY_NOT_ENABLED"
	CodeProfileBlocked  = "PROFILE_CHANGE_BLOCKED"
)

// ChangeGuard: modul lain (hotel, commercial) mendaftarkan pemeriksaan data yang tidak kompatibel
// sebelum profile diubah (Onboarding Brief §19). Kembalikan pesan (non-kosong) bila memblokir.
type ChangeGuard func(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, from, to Profile) (string, error)

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer

	mu     sync.RWMutex
	guards []ChangeGuard
	cache  sync.Map // propertyID → cached
}

type cached struct {
	ctx    *Context
	loaded time.Time
}

func New(d *db.DB, j jobs.Enqueuer) *Service { return &Service{DB: d, Jobs: j} }

func (s *Service) RegisterChangeGuard(g ChangeGuard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guards = append(s.guards, g)
}

// Config = property_profile_configs (aturan profile per property, OD-P1-004..008).
type Config struct {
	Terminology                map[string]Term `json:"terminology"` // override
	ExposeSLAToTenant          bool            `json:"expose_sla_to_tenant"`
	TenantConfirmationRequired bool            `json:"tenant_confirmation_required"`
	CSATEnabled                bool            `json:"csat_enabled"`
	BookingApprovalRequired    bool            `json:"booking_approval_required"`
	VisitorApprovalRequired    bool            `json:"visitor_approval_required"`
	TenantSelfRegistration     bool            `json:"tenant_self_registration"`
	AutoCloseResolvedHours     int             `json:"auto_close_resolved_hours"`
	Settings                   map[string]any  `json:"settings"`
	UpdatedAt                  time.Time       `json:"updated_at"`
	Version                    int             `json:"version"`
}

// Context = Property Context (Onboarding Brief §8): profile + capability + terminologi efektif + config.
type Context struct {
	PropertyID   uuid.UUID       `json:"property_id"`
	PropertyName string          `json:"property_name"`
	Profile      Profile         `json:"profile"`
	Status       string          `json:"status"`
	Capabilities []Capability    `json:"capabilities"`
	Terminology  map[string]Term `json:"terminology"`
	Config       Config          `json:"config"`
}

func (c *Context) Has(cap Capability) bool {
	for _, x := range c.Capabilities {
		if x == cap {
			return true
		}
	}
	return false
}

// ResolveTx memuat Property Context di dalam transaksi (dipakai modul lain untuk enforcement server-side).
func (s *Service) ResolveTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) (*Context, error) {
	var out Context
	var termJSON, settingsJSON []byte
	err := tx.QueryRow(ctx, `
		SELECT l.name, p.profile, p.status,
		       COALESCE(c.terminology,'{}'::jsonb), COALESCE(c.expose_sla_to_tenant,false), COALESCE(c.tenant_confirmation_required,false),
		       COALESCE(c.csat_enabled,true), COALESCE(c.booking_approval_required,false), COALESCE(c.visitor_approval_required,false),
		       COALESCE(c.tenant_self_registration,true), COALESCE(c.auto_close_resolved_hours,72), COALESCE(c.settings,'{}'::jsonb),
		       COALESCE(c.updated_at, now()), COALESCE(c.version,0)
		FROM properties p JOIN locations l ON l.id = p.location_id
		LEFT JOIN property_profile_configs c ON c.property_id = p.location_id
		WHERE p.location_id = $1`, propertyID).
		Scan(&out.PropertyName, &out.Profile, &out.Status, &termJSON, &out.Config.ExposeSLAToTenant, &out.Config.TenantConfirmationRequired,
			&out.Config.CSATEnabled, &out.Config.BookingApprovalRequired, &out.Config.VisitorApprovalRequired,
			&out.Config.TenantSelfRegistration, &out.Config.AutoCloseResolvedHours, &settingsJSON, &out.Config.UpdatedAt, &out.Config.Version)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Property")
		}
		return nil, err
	}
	out.PropertyID = propertyID
	out.Capabilities = Capabilities(out.Profile)
	out.Config.Terminology = map[string]Term{}
	_ = json.Unmarshal(termJSON, &out.Config.Terminology)
	out.Config.Settings = map[string]any{}
	_ = json.Unmarshal(settingsJSON, &out.Config.Settings)
	out.Terminology = DefaultTerminology(out.Profile)
	for k, v := range out.Config.Terminology {
		if _, known := out.Terminology[k]; known && (v.ID != "" || v.EN != "") {
			cur := out.Terminology[k]
			if v.ID != "" {
				cur.ID = v.ID
			}
			if v.EN != "" {
				cur.EN = v.EN
			}
			out.Terminology[k] = cur
		}
	}
	return &out, nil
}

// ProfileOfTx: profile property (cache 30 dtk per proses) — dipakai jalur panas (create SR, kategori).
func (s *Service) ProfileOfTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) (Profile, error) {
	if v, ok := s.cache.Load(propertyID); ok {
		c := v.(cached)
		if time.Since(c.loaded) < 30*time.Second {
			return c.ctx.Profile, nil
		}
	}
	pc, err := s.ResolveTx(ctx, tx, propertyID)
	if err != nil {
		return "", err
	}
	s.cache.Store(propertyID, cached{ctx: pc, loaded: time.Now()})
	return pc.Profile, nil
}

// RequireCapabilityTx: enforcement server-side (AC-15/18) — 403 CAPABILITY_NOT_ENABLED bila profile tidak mengaktifkan.
func (s *Service) RequireCapabilityTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, cap Capability) error {
	p, err := s.ProfileOfTx(ctx, tx, propertyID)
	if err != nil {
		return err
	}
	if !Has(p, cap) {
		return apperr.New(403, CodeCapability, "Capability not enabled", fmt.Sprintf("Capability %s tidak tersedia pada profile %s", cap, p))
	}
	return nil
}

// Get: Property Context untuk user yang punya akses lihat property tersebut.
func (s *Service) Get(ctx context.Context, propertyID uuid.UUID) (*Context, error) {
	if err := iam.CanOnProperty(ctx, "property.properties.view", propertyID); err != nil {
		return nil, err
	}
	var out *Context
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.ResolveTx(ctx, tx, propertyID)
		return err
	})
	return out, err
}

type UpdateConfigInput struct {
	Terminology                *map[string]Term `json:"terminology"`
	ExposeSLAToTenant          *bool            `json:"expose_sla_to_tenant"`
	TenantConfirmationRequired *bool            `json:"tenant_confirmation_required"`
	CSATEnabled                *bool            `json:"csat_enabled"`
	BookingApprovalRequired    *bool            `json:"booking_approval_required"`
	VisitorApprovalRequired    *bool            `json:"visitor_approval_required"`
	TenantSelfRegistration     *bool            `json:"tenant_self_registration"`
	AutoCloseResolvedHours     *int             `json:"auto_close_resolved_hours"`
	Settings                   *map[string]any  `json:"settings"`
}

// UpdateConfig: konfigurasi profile (tidak dapat menghapus modul mandatory — tidak ada field untuk itu).
func (s *Service) UpdateConfig(ctx context.Context, propertyID uuid.UUID, in UpdateConfigInput, ifVersion *int) (*Context, error) {
	if err := iam.CanOnProperty(ctx, "property.properties.update", propertyID); err != nil {
		return nil, err
	}
	if in.AutoCloseResolvedHours != nil && (*in.AutoCloseResolvedHours < 0 || *in.AutoCloseResolvedHours > 24*30) {
		return nil, apperr.Validation("auto_close_resolved_hours harus 0..720")
	}
	if in.Terminology != nil {
		for k := range *in.Terminology {
			if _, ok := defaults[Office][k]; !ok {
				return nil, apperr.Validation("terminology key tidak dikenal: " + k)
			}
		}
	}
	p := authctx.Must(ctx)
	var out *Context
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.ResolveTx(ctx, tx, propertyID)
		if err != nil {
			return err
		}
		if ifVersion != nil && before.Config.Version != 0 && *ifVersion != before.Config.Version {
			return apperr.StaleVersion()
		}
		cfg := before.Config
		if in.Terminology != nil {
			cfg.Terminology = *in.Terminology
		}
		set := func(dst *bool, v *bool) {
			if v != nil {
				*dst = *v
			}
		}
		set(&cfg.ExposeSLAToTenant, in.ExposeSLAToTenant)
		set(&cfg.TenantConfirmationRequired, in.TenantConfirmationRequired)
		set(&cfg.CSATEnabled, in.CSATEnabled)
		set(&cfg.BookingApprovalRequired, in.BookingApprovalRequired)
		set(&cfg.VisitorApprovalRequired, in.VisitorApprovalRequired)
		set(&cfg.TenantSelfRegistration, in.TenantSelfRegistration)
		if in.AutoCloseResolvedHours != nil {
			cfg.AutoCloseResolvedHours = *in.AutoCloseResolvedHours
		}
		if in.Settings != nil {
			cfg.Settings = *in.Settings
		}
		termJSON, _ := json.Marshal(cfg.Terminology)
		settingsJSON, _ := json.Marshal(cfg.Settings)
		_, err = tx.Exec(ctx, `
			INSERT INTO property_profile_configs (property_id, organization_id, terminology, expose_sla_to_tenant, tenant_confirmation_required, csat_enabled,
			  booking_approval_required, visitor_approval_required, tenant_self_registration, auto_close_resolved_hours, settings, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (property_id) DO UPDATE SET terminology = EXCLUDED.terminology, expose_sla_to_tenant = EXCLUDED.expose_sla_to_tenant,
			  tenant_confirmation_required = EXCLUDED.tenant_confirmation_required, csat_enabled = EXCLUDED.csat_enabled,
			  booking_approval_required = EXCLUDED.booking_approval_required, visitor_approval_required = EXCLUDED.visitor_approval_required,
			  tenant_self_registration = EXCLUDED.tenant_self_registration, auto_close_resolved_hours = EXCLUDED.auto_close_resolved_hours,
			  settings = EXCLUDED.settings, updated_by = EXCLUDED.updated_by`,
			propertyID, p.OrganizationID, termJSON, cfg.ExposeSLAToTenant, cfg.TenantConfirmationRequired, cfg.CSATEnabled,
			cfg.BookingApprovalRequired, cfg.VisitorApprovalRequired, cfg.TenantSelfRegistration, cfg.AutoCloseResolvedHours, settingsJSON, p.UserID)
		if err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "property_profile_config", EntityID: &propertyID, EntityLabel: before.PropertyName, Before: before.Config, After: cfg})
		s.cache.Delete(propertyID)
		out, err = s.ResolveTx(ctx, tx, propertyID)
		return err
	})
	return out, err
}

type ChangeProfileInput struct {
	Profile string `json:"profile"`
	Reason  string `json:"reason"`
}

// ChangeProfile: aksi administratif (PS-006) — validasi guard modul; diblokir bila ada data tidak kompatibel (AC-19/20).
func (s *Service) ChangeProfile(ctx context.Context, propertyID uuid.UUID, in ChangeProfileInput) (*Context, error) {
	if err := iam.CanOnProperty(ctx, "property.properties.change_profile", propertyID); err != nil {
		return nil, err
	}
	if !Valid(in.Profile) {
		return nil, apperr.Validation("profile harus hotel|apartment|office")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return nil, apperr.Validation("reason wajib untuk perubahan profile")
	}
	p := authctx.Must(ctx)
	to := Profile(in.Profile)
	var out *Context
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.ResolveTx(ctx, tx, propertyID)
		if err != nil {
			return err
		}
		if before.Profile == to {
			return apperr.Validation("Profile sudah " + string(to))
		}
		s.mu.RLock()
		guards := append([]ChangeGuard{}, s.guards...)
		s.mu.RUnlock()
		var blockers []string
		for _, g := range guards {
			msg, err := g(ctx, tx, propertyID, before.Profile, to)
			if err != nil {
				return err
			}
			if msg != "" {
				blockers = append(blockers, msg)
			}
		}
		if len(blockers) > 0 {
			return apperr.New(409, CodeProfileBlocked, "Profile change blocked", "Perubahan profile diblokir: "+strings.Join(blockers, "; "))
		}
		if _, err := tx.Exec(ctx, `UPDATE properties SET profile = $2, profile_changed_at = now(), profile_changed_by = $3 WHERE location_id = $1`, propertyID, to, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "property_profile", EntityID: &propertyID, EntityLabel: before.PropertyName,
			Before: map[string]any{"profile": before.Profile}, After: map[string]any{"profile": to, "reason": in.Reason}})
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "location", ObjectID: propertyID, Action: audit.ActUpdated, From: string(before.Profile), To: string(to), Payload: map[string]any{"field": "profile", "reason": in.Reason}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventProfileChanged, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "property", ObjectID: propertyID, ObjectLabel: before.PropertyName, ActorUserID: &p.UserID,
				Payload: map[string]any{"from": before.Profile, "to": to}})
		}
		s.cache.Delete(propertyID)
		out, err = s.ResolveTx(ctx, tx, propertyID)
		return err
	})
	return out, err
}

// SeedCategoriesTx: kategori SR default per organization (PRD §11), profile-aware; idempotent.
func SeedCategoriesTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	for i, c := range DefaultCategories() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO service_request_categories (organization_id, code, name, default_domain, default_priority, sort_order, icon, profiles, tenant_visible)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,true)
			ON CONFLICT DO NOTHING`,
			orgID, c.Code, c.Name, c.Domain, c.Priority, 100+i, c.Icon, ProfilesToStrings(c.Profiles)); err != nil {
			return err
		}
		// lengkapi icon/profiles untuk kategori lama yang sudah ada dengan kode sama
		if _, err := tx.Exec(ctx, `UPDATE service_request_categories SET icon = COALESCE(icon,$3), profiles = CASE WHEN profiles = '{hotel,apartment,office}' THEN $4 ELSE profiles END WHERE organization_id = $1 AND code = $2 AND property_id IS NULL`,
			orgID, c.Code, c.Icon, ProfilesToStrings(c.Profiles)); err != nil {
			return err
		}
	}
	return nil
}
