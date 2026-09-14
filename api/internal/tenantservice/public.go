package tenantservice

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
)

// Public SR intake (OD-001, TAD §5.6, §9.3): endpoint terpisah /public/v1, organization slug + per-property intake key,
// feature flag `public_intake` per organization, rate limit ketat, captcha (TD-008) via CaptchaVerifier.

type CaptchaVerifier interface {
	Verify(ctx context.Context, token, ip string) bool
}

type PublicHandler struct {
	Svc     *Service
	Captcha CaptchaVerifier // nil = tidak diverifikasi (pilot tanpa captcha)
	limiter *ipLimiter
}

func NewPublicHandler(svc *Service, captcha CaptchaVerifier) *PublicHandler {
	return &PublicHandler{Svc: svc, Captcha: captcha, limiter: &ipLimiter{hits: map[string][]time.Time{}}}
}

func (h *PublicHandler) Mount(r chi.Router) {
	r.Post("/public/v1/service-requests", h.create)
	r.Get("/public/v1/service-requests/{number}", h.status)
}

type publicCreate struct {
	OrganizationSlug string  `json:"organization_slug"`
	IntakeKey        string  `json:"intake_key"`
	CategoryCode     string  `json:"category_code"`
	Title            string  `json:"title"`
	Description      *string `json:"description"`
	RequesterName    string  `json:"requester_name"`
	RequesterPhone   string  `json:"requester_phone"`
	RequesterEmail   *string `json:"requester_email"`
	UnitNumber       *string `json:"unit_number"`
	CaptchaToken     string  `json:"captcha_token"`
}

func (h *PublicHandler) create(w http.ResponseWriter, r *http.Request) {
	ip := httpx.ClientIP(r)
	if !h.limiter.allow(ip, 5, time.Minute) {
		httpx.WriteError(w, r, apperr.RateLimited())
		return
	}
	var in publicCreate
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.RequesterName) == "" || strings.TrimSpace(in.RequesterPhone) == "" {
		httpx.WriteError(w, r, apperr.Validation("title, requester_name, requester_phone wajib"))
		return
	}
	if h.Captcha != nil && !h.Captcha.Verify(r.Context(), in.CaptchaToken, ip) {
		httpx.WriteError(w, r, apperr.Validation("captcha tidak valid"))
		return
	}
	// resolve org + property via intake key (tanpa RLS: lookup dengan app.auth_lookup)
	var orgID, propertyID uuid.UUID
	var enabled bool
	err := h.Svc.DB.WithAuthLookupTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT o.id, pr.location_id, COALESCE(ff.enabled, false) FROM organizations o
			JOIN properties pr ON pr.organization_id = o.id AND pr.public_intake_key = $2
			LEFT JOIN feature_flags ff ON ff.organization_id = o.id AND ff.flag = 'public_intake'
			WHERE o.slug = $1 AND o.is_active`, in.OrganizationSlug, in.IntakeKey).Scan(&orgID, &propertyID, &enabled); err != nil {
			return apperr.NotFound("Intake")
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !enabled {
		httpx.WriteError(w, r, apperr.New(403, "FEATURE_DISABLED", "Feature disabled", "Public intake tidak aktif untuk organization ini"))
		return
	}
	sys := authctx.System(orgID)
	sys.Source = authctx.SourceWeb
	sys.FullName = "Public Intake"
	ctx := authctx.With(r.Context(), sys)
	var number string
	err = h.Svc.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var tenantID, locationID *uuid.UUID
		if in.UnitNumber != nil && *in.UnitNumber != "" {
			var lid uuid.UUID
			var tid *uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT u.location_id, u.tenant_id FROM units u JOIN locations l ON l.id = u.location_id WHERE l.property_id = $1 AND u.unit_number = $2 AND l.deleted_at IS NULL`, propertyID, *in.UnitNumber).Scan(&lid, &tid); err == nil {
				locationID, tenantID = &lid, tid
			}
		}
		cat := in.CategoryCode
		if cat == "" {
			cat = "complaint"
		}
		desc := in.Description
		id, err := h.Svc.CreateTx(ctx, tx, CreateInput{PropertyID: &propertyID, CategoryCode: cat, Title: strings.TrimSpace(in.Title), Description: desc, TenantID: tenantID, LocationID: locationID,
			RequesterName: &in.RequesterName, RequesterPhone: &in.RequesterPhone, RequesterEmail: in.RequesterEmail, Channel: "public_intake"})
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT request_number FROM service_requests WHERE id = $1`, id).Scan(&number)
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"request_number": number, "message": "Permintaan Anda telah diterima. Simpan nomor ini untuk cek status."})
}

// status: cek status by nomor + telepon (tanpa data lain).
func (h *PublicHandler) status(w http.ResponseWriter, r *http.Request) {
	ip := httpx.ClientIP(r)
	if !h.limiter.allow(ip, 20, time.Minute) {
		httpx.WriteError(w, r, apperr.RateLimited())
		return
	}
	number := strings.ToUpper(chi.URLParam(r, "number"))
	phone := r.URL.Query().Get("phone")
	slug := r.URL.Query().Get("organization_slug")
	if phone == "" || slug == "" {
		httpx.WriteError(w, r, apperr.Validation("organization_slug dan phone wajib"))
		return
	}
	var orgID uuid.UUID
	if err := h.Svc.DB.WithAuthLookupTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM organizations WHERE slug = $1 AND is_active`, slug).Scan(&orgID)
	}); err != nil {
		httpx.WriteError(w, r, apperr.NotFound("Service Request"))
		return
	}
	var status string
	var updated time.Time
	err := h.Svc.DB.WithOrgTx(r.Context(), orgID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status, updated_at FROM service_requests WHERE request_number = $1 AND requester_phone = $2 AND channel = 'public_intake'`, number, phone).Scan(&status, &updated)
	})
	if err != nil {
		httpx.WriteError(w, r, apperr.NotFound("Service Request"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"request_number": number, "status": status, "updated_at": updated})
}

type ipLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *ipLimiter) allow(ip string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var keep []time.Time
	for _, t := range l.hits[ip] {
		if now.Sub(t) < window {
			keep = append(keep, t)
		}
	}
	if len(keep) >= max {
		l.hits[ip] = keep
		return false
	}
	l.hits[ip] = append(keep, now)
	if len(l.hits) > 20000 {
		l.hits = map[string][]time.Time{}
	}
	return true
}

// EnablePublicIntake: Organization Admin mengaktifkan flag + membuat intake key per property.
func (s *Service) EnablePublicIntake(ctx context.Context, propertyID uuid.UUID, enabled bool) (string, error) {
	p := authctx.Must(ctx)
	if !p.Has("platform.organizations.update") {
		return "", apperr.Forbidden("")
	}
	var key string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO feature_flags (organization_id, flag, enabled) VALUES ($1,'public_intake',$2) ON CONFLICT (organization_id, flag) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = now()`, p.OrganizationID, enabled); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(public_intake_key, '') FROM properties WHERE location_id = $1`, propertyID).Scan(&key); err != nil {
			return apperr.NotFound("Property")
		}
		if key == "" {
			key = strings.ReplaceAll(uuid.NewString(), "-", "")
			if _, err := tx.Exec(ctx, `UPDATE properties SET public_intake_key = $2 WHERE location_id = $1`, propertyID, key); err != nil {
				return err
			}
		}
		return nil
	})
	return key, err
}
