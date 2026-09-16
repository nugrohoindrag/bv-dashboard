package growth

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/mailer"
)

// ---------- Plans (§33): katalog sederhana; pembayaran online di-hold (keputusan 16 Sep 2026) ----------

type Plan struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Tagline     string   `json:"tagline"`
	PriceLabel  string   `json:"price_label"`
	Period      string   `json:"period"`
	Profiles    []string `json:"profiles"`
	StaffLimit  string   `json:"staff_limit"`
	Properties  string   `json:"properties"`
	Includes    []string `json:"includes"`
	Trial       bool     `json:"trial_available"`
	Highlighted bool     `json:"highlighted"`
	CTA         string   `json:"cta"` // start_trial | contact_sales
}

var Plans = []Plan{
	{Code: "starter", Name: "Starter", Tagline: "For a single property getting organized.", PriceLabel: "Rp 1.500.000", Period: "per property / month", Profiles: []string{"hotel", "apartment", "office"}, StaffLimit: "Up to 25 staff users", Properties: "1 property",
		Includes: []string{"Housekeeping, Engineering, Security", "Work Orders, Tasks, Findings", "Service Requests and Tenant Relation", "Staff App with offline mode", "Tenant App", "Email support"}, Trial: true, CTA: "start_trial"},
	{Code: "growth", Name: "Growth", Tagline: "For teams running several buildings.", PriceLabel: "Rp 3.500.000", Period: "per property / month", Profiles: []string{"hotel", "apartment", "office"}, StaffLimit: "Up to 100 staff users", Properties: "Up to 5 properties",
		Includes: []string{"Everything in Starter", "Preventive Maintenance and Asset Management", "Facility Booking and Visitor Management", "Billing with manual verification", "Vendor and Inventory", "Reports and CSV export", "Priority support"}, Trial: true, Highlighted: true, CTA: "start_trial"},
	{Code: "enterprise", Name: "Enterprise", Tagline: "For portfolios with custom needs.", PriceLabel: "Custom", Period: "annual agreement", Profiles: []string{"hotel", "apartment", "office"}, StaffLimit: "Unlimited staff users", Properties: "Unlimited properties",
		Includes: []string{"Everything in Growth", "Hotel Booking Management and Reception", "Apartment Unit Sales and Rental", "SSO and audit exports", "Dedicated onboarding and training", "SLA-backed support"}, Trial: false, CTA: "contact_sales"},
}

func planByCode(code string) *Plan {
	for i := range Plans {
		if Plans[i].Code == code {
			return &Plans[i]
		}
	}
	return nil
}

// ---------- Trial status (§31) ----------

type TrialInfo struct {
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"started_at"`
	EndsAt      *time.Time `json:"ends_at"`
	ConvertedAt *time.Time `json:"converted_at"`
	CancelledAt *time.Time `json:"cancelled_at"`
	DaysLeft    int        `json:"days_left"`
	PlanCode    *string    `json:"plan_code"`
	IsTrialOrg  bool       `json:"is_trial_org"` // pernah trial (signup website)
	Locked      bool       `json:"locked"`       // trial_expired / cancelled: perubahan data diblokir (§31)
}

func trialInfoTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (*TrialInfo, error) {
	var t TrialInfo
	if err := tx.QueryRow(ctx, `SELECT trial_status, trial_started_at, trial_ends_at, trial_converted_at, trial_cancelled_at, plan_code FROM organizations WHERE id = $1`, orgID).
		Scan(&t.Status, &t.StartedAt, &t.EndsAt, &t.ConvertedAt, &t.CancelledAt, &t.PlanCode); err != nil {
		return nil, err
	}
	t.IsTrialOrg = t.Status != TrialNone
	if t.EndsAt != nil && (t.Status == TrialActive || t.Status == TrialEndingSoon) {
		left := time.Until(*t.EndsAt).Hours() / 24
		if left > 0 {
			t.DaysLeft = int(left) + 1
		}
	}
	t.Locked = t.Status == TrialExpired || t.Status == TrialCancelled
	return &t, nil
}

func (s *Service) Trial(ctx context.Context) (*TrialInfo, error) {
	p := authctx.Must(ctx)
	var out *TrialInfo
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = trialInfoTx(ctx, tx, p.OrganizationID)
		return err
	})
	return out, err
}

// Convert: "Choose a plan" (§31 Converted, §32). Tanpa pembayaran online (di-hold): status converted + plan_code, sales dihubungi.
func (s *Service) Convert(ctx context.Context, planCode string) (*TrialInfo, error) {
	p := authctx.Must(ctx)
	planCode = strings.ToLower(strings.TrimSpace(planCode))
	plan := planByCode(planCode)
	if plan == nil {
		return nil, apperr.Validation("Unknown plan").WithField("plan_code", "invalid")
	}
	var out *TrialInfo
	var orgName, email, name string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur, err := trialInfoTx(ctx, tx, p.OrganizationID)
		if err != nil {
			return err
		}
		if cur.Status == TrialConverted {
			return apperr.Conflict("ALREADY_CONVERTED", "This workspace is already on a plan")
		}
		if _, err := tx.Exec(ctx, `UPDATE organizations SET trial_status = 'converted', trial_converted_at = now(), plan_code = $2, updated_by = $3 WHERE id = $1`, p.OrganizationID, planCode, p.UserID); err != nil {
			return err
		}
		_ = tx.QueryRow(ctx, `SELECT o.name, coalesce(u.email,''), u.full_name FROM organizations o JOIN users u ON u.id = $2 WHERE o.id = $1`, p.OrganizationID, p.UserID).Scan(&orgName, &email, &name)
		if err := audit.Log(ctx, tx, audit.AuditEntry{Action: AuditTrialConverted, EntityType: "organization", EntityID: &p.OrganizationID, EntityLabel: orgName,
			Before: map[string]any{"trial_status": cur.Status}, After: map[string]any{"trial_status": TrialConverted, "plan_code": planCode}}); err != nil {
			return err
		}
		out, err = trialInfoTx(ctx, tx, p.OrganizationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.invalidateTrial(p.OrganizationID)
	if email != "" {
		go func() {
			bg := context.WithoutCancel(ctx)
			_ = s.Mailer.Send(bg, withTo(planChosenEmail(name, orgName, plan.Name), email))
			if s.Cfg.SalesEmail != "" {
				m := planChosenEmail(name, orgName, plan.Name)
				m.Subject = "Plan chosen: " + orgName + " (" + plan.Name + ")"
				m.To = []string{s.Cfg.SalesEmail}
				_ = s.Mailer.Send(bg, m)
			}
		}()
	}
	s.track(ctx, "subscription_started", &p.OrganizationID, &p.UserID, nil, nil, map[string]any{"plan": planCode})
	return out, nil
}

// Cancel: pengguna mengakhiri trial (§31 Cancelled). Data tidak dihapus; perubahan diblokir.
func (s *Service) Cancel(ctx context.Context, reason string) (*TrialInfo, error) {
	p := authctx.Must(ctx)
	var out *TrialInfo
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur, err := trialInfoTx(ctx, tx, p.OrganizationID)
		if err != nil {
			return err
		}
		if cur.Status == TrialNone || cur.Status == TrialConverted {
			return apperr.Conflict("NOT_ON_TRIAL", "This workspace is not on a trial")
		}
		if _, err := tx.Exec(ctx, `UPDATE organizations SET trial_status = 'cancelled', trial_cancelled_at = now(), updated_by = $2 WHERE id = $1`, p.OrganizationID, p.UserID); err != nil {
			return err
		}
		if err := audit.Log(ctx, tx, audit.AuditEntry{Action: AuditTrialCancelled, EntityType: "organization", EntityID: &p.OrganizationID,
			Before: map[string]any{"trial_status": cur.Status}, After: map[string]any{"trial_status": TrialCancelled, "reason": reason}}); err != nil {
			return err
		}
		out, err = trialInfoTx(ctx, tx, p.OrganizationID)
		return err
	})
	if err == nil {
		s.invalidateTrial(p.OrganizationID)
	}
	return out, err
}

// ---------- Trial guard: workspace expired/cancelled hanya boleh membaca (§31 "Trial expiration is handled") ----------

type trialCacheEntry struct {
	locked bool
	at     time.Time
}

var trialCache sync.Map // orgID → trialCacheEntry

func (s *Service) invalidateTrial(orgID uuid.UUID) { trialCache.Delete(orgID) }

func (s *Service) isLocked(ctx context.Context, orgID uuid.UUID) bool {
	if v, ok := trialCache.Load(orgID); ok {
		e := v.(trialCacheEntry)
		if time.Since(e.at) < 60*time.Second {
			return e.locked
		}
	}
	var status string
	if err := s.DB.Pool.QueryRow(ctx, `SELECT trial_status FROM organizations WHERE id = $1`, orgID).Scan(&status); err != nil {
		return false
	}
	locked := status == TrialExpired || status == TrialCancelled
	trialCache.Store(orgID, trialCacheEntry{locked: locked, at: time.Now()})
	return locked
}

// Guard: middleware untuk group protected. Mutasi ditolak (402 TRIAL_LOCKED) bila trial berakhir; GET tetap boleh
// agar pengguna bisa melihat data dan memilih plan. Endpoint /trial/* dan /auth/* dikecualikan.
func (s *Service) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		path := r.URL.Path
		if strings.Contains(path, "/trial") || strings.Contains(path, "/auth/") || strings.Contains(path, "/me/") {
			next.ServeHTTP(w, r)
			return
		}
		p, ok := authctx.From(r.Context())
		if ok && !p.IsSystem && s.isLocked(r.Context(), p.OrganizationID) {
			httpx.WriteError(w, r, apperr.New(402, "TRIAL_LOCKED", "Trial has ended", "Your trial has ended. Choose a plan to continue making changes."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- Sweep (worker; §31–§32): ending soon, expired, pengingat setup ----------

// Sweep memproses seluruh organization trial. Mengembalikan jumlah organization yang berubah/diberi notifikasi.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	type row struct {
		ID       uuid.UUID
		Name     string
		Status   string
		Started  *time.Time
		Ends     *time.Time
		Settings map[string]any
	}
	rows, err := s.DB.Pool.Query(ctx, `SELECT id, name, trial_status, trial_started_at, trial_ends_at, settings FROM organizations WHERE is_active AND trial_status IN ('trial','trial_ending_soon')`)
	if err != nil {
		return 0, err
	}
	var orgs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.Name, &r.Status, &r.Started, &r.Ends, &r.Settings); err != nil {
			rows.Close()
			return 0, err
		}
		orgs = append(orgs, r)
	}
	rows.Close()
	now := time.Now()
	soon := time.Duration(s.Cfg.TrialEndingSoonDays) * 24 * time.Hour
	n := 0
	app := strings.TrimRight(s.Cfg.PublicURL, "/")
	for _, o := range orgs {
		if o.Ends == nil {
			continue
		}
		switch {
		case !o.Ends.After(now):
			err = s.DB.WithOrgTx(ctx, o.ID, func(ctx context.Context, tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `UPDATE organizations SET trial_status = 'trial_expired' WHERE id = $1`, o.ID); err != nil {
					return err
				}
				_ = audit.LogAs(ctx, tx, o.ID, nil, "", "worker", audit.AuditEntry{Action: AuditTrialExpired, EntityType: "organization", EntityID: &o.ID, EntityLabel: o.Name, Before: map[string]any{"trial_status": o.Status}, After: map[string]any{"trial_status": TrialExpired}})
				return s.notifyAdmins(ctx, tx, o.ID, "trial_expired", "Your trial has expired", "Your data is safe. Choose a plan to keep working in BuildingVision.", "critical", "/settings/plan",
					func(name, email string) { _ = s.Mailer.Send(ctx, withTo(trialExpiredEmail(name, o.Name, app), email)) })
			})
		case o.Status == TrialActive && o.Ends.Sub(now) <= soon:
			err = s.DB.WithOrgTx(ctx, o.ID, func(ctx context.Context, tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `UPDATE organizations SET trial_status = 'trial_ending_soon' WHERE id = $1`, o.ID); err != nil {
					return err
				}
				return s.notifyAdmins(ctx, tx, o.ID, "trial_ending_soon", "Your trial is ending soon", "Your trial ends on "+o.Ends.Format("2 January 2006")+". Choose a plan to keep your workspace.", "warning", "/settings/plan",
					func(name, email string) {
						_ = s.Mailer.Send(ctx, withTo(trialEndingSoonEmail(name, o.Name, app, *o.Ends), email))
					})
			})
		case o.Status == TrialActive && o.Started != nil && now.Sub(*o.Started) >= 48*time.Hour && !settingFlag(o.Settings, "onboarding", "reminder_sent_at"):
			// pengingat "Complete your setup" sekali, 2 hari setelah mulai, bila belum ada property
			err = s.DB.WithOrgTx(ctx, o.ID, func(ctx context.Context, tx pgx.Tx) error {
				var props int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM properties WHERE organization_id = $1`, o.ID).Scan(&props); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE organizations SET settings = jsonb_set(coalesce(settings,'{}'::jsonb), '{onboarding,reminder_sent_at}', to_jsonb(now()), true) WHERE id = $1`, o.ID); err != nil {
					return err
				}
				if props > 0 {
					return nil
				}
				return s.notifyAdmins(ctx, tx, o.ID, "trial_setup_reminder", "Complete your setup", "Create your first property to start using BuildingVision.", "info", "/onboarding",
					func(name, email string) { _ = s.Mailer.Send(ctx, withTo(setupReminderEmail(name, o.Name, app), email)) })
			})
		default:
			continue
		}
		if err != nil {
			if s.Log != nil {
				s.Log.Error("trial sweep", "org", o.ID, "err", err)
			}
			continue
		}
		s.invalidateTrial(o.ID)
		n++
	}
	return n, nil
}

func settingFlag(settings map[string]any, keys ...string) bool {
	var cur any = settings
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		cur, ok = m[k]
		if !ok || cur == nil {
			return false
		}
	}
	return true
}

func withTo(m mailer.Message, email string) mailer.Message {
	m.To = []string{email}
	return m
}

// notifyAdmins: notifikasi in-app ke seluruh Organization Admin + callback email per admin.
func (s *Service) notifyAdmins(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, typ, title, body, severity, deepLink string, mail func(name, email string)) error {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT u.id, u.full_name, coalesce(u.email,'') FROM users u
		JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
		WHERE u.organization_id = $1 AND u.is_active AND u.deleted_at IS NULL AND r.code = 'organization_admin'`, orgID)
	if err != nil {
		return err
	}
	type adm struct {
		id          uuid.UUID
		name, email string
	}
	var admins []adm
	for rows.Next() {
		var a adm
		if err := rows.Scan(&a.id, &a.name, &a.email); err != nil {
			rows.Close()
			return err
		}
		admins = append(admins, a)
	}
	rows.Close()
	for _, a := range admins {
		if _, err := tx.Exec(ctx, `INSERT INTO notifications (organization_id, user_id, type, title, body, object_type, object_id, deep_link, severity) VALUES ($1,$2,$3,$4,$5,'organization',$1,$6,$7)`,
			orgID, a.id, typ, title, body, deepLink, severity); err != nil {
			return err
		}
		if mail != nil && a.email != "" {
			mail(a.name, a.email)
		}
	}
	return nil
}
