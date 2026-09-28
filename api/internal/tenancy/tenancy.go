// Package tenancy: registry organization untuk Platform Admin (PRD P0 v2 §6, §8.2) — membuat, melihat,
// mengubah, dan mengaktifkan/menonaktifkan organization pelanggan. Operasi lintas organization; setiap akses
// ke data org-scoped memakai WithOrgTx organization target sehingga RLS tetap berlaku.
package tenancy

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/seed"
)

type Service struct {
	DB  *db.DB
	IAM *iam.Service
}

// OrgSummary: organization + statistik ringkas untuk daftar registry.
type OrgSummary struct {
	property.Organization
	UserCount     int `json:"user_count"`
	PropertyCount int `json:"property_count"`
}

type AdminInput struct {
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type CreateInput struct {
	property.OrganizationInput
	Slug  string     `json:"slug"`
	Admin AdminInput `json:"admin"`
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,48}[a-z0-9]$`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *Service) List(ctx context.Context, q, status string) ([]OrgSummary, error) {
	var ids []uuid.UUID
	args := []any{}
	where := "WHERE 1=1"
	if q != "" {
		args = append(args, "%"+q+"%")
		where += fmt.Sprintf(" AND (name ILIKE $%d OR code ILIKE $%d OR slug ILIKE $%d OR legal_name ILIKE $%d)", len(args), len(args), len(args), len(args))
	}
	if status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	rows, err := s.DB.Pool.Query(ctx, `SELECT id FROM organizations `+where+` ORDER BY name LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]OrgSummary, 0, len(ids))
	for _, id := range ids {
		sum, err := s.summary(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *sum)
	}
	return out, nil
}

// summary: statistik dihitung di dalam konteks RLS organization target.
func (s *Service) summary(ctx context.Context, id uuid.UUID) (*OrgSummary, error) {
	var out OrgSummary
	err := s.DB.WithOrgTx(ctx, id, func(ctx context.Context, tx pgx.Tx) error {
		o, err := property.GetOrganizationTx(ctx, tx, id)
		if err != nil {
			return err
		}
		out.Organization = *o
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE deleted_at IS NULL),
			(SELECT count(*) FROM locations WHERE location_type = 'property' AND deleted_at IS NULL)`).Scan(&out.UserCount, &out.PropertyCount)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*OrgSummary, error) {
	return s.summary(ctx, id)
}

// Create: organization baru + baseline (role sistem, equipment, SLA, kategori SR, provider manual) + Organization Admin.
func (s *Service) Create(ctx context.Context, in CreateInput) (*OrgSummary, error) {
	p := authctx.Must(ctx)
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	in.Admin.Email = strings.ToLower(strings.TrimSpace(in.Admin.Email))
	if in.Admin.FullName == "" || !strings.Contains(in.Admin.Email, "@") {
		return nil, apperr.Validation("admin.full_name dan admin.email wajib").WithField("admin", "wajib")
	}
	if len(in.Admin.Password) < 8 {
		return nil, apperr.Validation("admin.password minimal 8 karakter").WithField("admin.password", "min 8")
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if slug == "" {
		slug = slugify(*in.Name)
	}
	if !slugRe.MatchString(slug) {
		return nil, apperr.Validation("slug hanya huruf kecil, angka, dan '-' (3–50 karakter)").WithField("slug", "tidak valid")
	}
	tz := "Asia/Jakarta"
	if in.Timezone != nil && *in.Timezone != "" {
		if _, err := time.LoadLocation(*in.Timezone); err != nil {
			return nil, apperr.Validation("timezone tidak valid").WithField("timezone", "tidak valid")
		}
		tz = *in.Timezone
	}
	var orgID uuid.UUID
	err := s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = $1 AND deleted_at IS NULL)`, in.Admin.Email).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return apperr.Conflict("EMAIL_TAKEN", "Email admin sudah dipakai akun lain").WithField("admin.email", "sudah dipakai")
		}
		var n int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&n)
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			code := fmt.Sprintf("ORG-%06d", n+1+attempt)
			err = tx.QueryRow(ctx, `INSERT INTO organizations (code, slug, name, timezone, signup_source, created_by) VALUES ($1,$2,$3,$4,'platform_admin',NULL) RETURNING id`,
				code, slug, strings.TrimSpace(*in.Name), tz).Scan(&orgID)
			if err == nil {
				break
			}
			if !db.IsUniqueViolation(err) {
				return err
			}
			var slugTaken bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organizations WHERE slug = $1)`, slug).Scan(&slugTaken)
			if slugTaken {
				return apperr.Conflict("SLUG_TAKEN", "Slug organization sudah dipakai").WithField("slug", "sudah dipakai")
			}
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.organization_id', $1, true)`, orgID.String()); err != nil {
			return err
		}
		sys := authctx.With(ctx, &authctx.Principal{OrganizationID: orgID, IsSystem: true, Source: authctx.SourceSystem, FullName: "Platform Admin", IP: p.IP, UserAgent: p.UserAgent})
		if _, err := property.UpdateOrganizationTx(sys, tx, orgID, in.OrganizationInput, nil); err != nil {
			return err
		}
		if err := s.IAM.SeedSystemRoles(ctx, tx, orgID); err != nil {
			return err
		}
		for _, f := range []func(context.Context, pgx.Tx, uuid.UUID) error{seed.SeedEquipment, seed.SeedSLADefaults, seed.SeedSRCategories, seed.SeedPaymentProviders} {
			if err := f(ctx, tx, orgID); err != nil {
				return err
			}
		}
		adminID, err := seed.CreateUser(ctx, tx, orgID, in.Admin.Email, strings.TrimSpace(in.Admin.FullName), in.Admin.Password, "organization_admin", nil)
		if err != nil {
			return err
		}
		entry := audit.AuditEntry{Action: audit.AuditCreate, EntityType: "organization", EntityID: &orgID, EntityLabel: slug,
			After: map[string]any{"name": *in.Name, "slug": slug, "admin_user_id": adminID, "by_organization_id": p.OrganizationID}}
		_ = audit.LogAs(ctx, tx, orgID, nil, p.IP, p.UserAgent, entry)
		if _, err := tx.Exec(ctx, `SELECT set_config('app.organization_id', $1, true)`, p.OrganizationID.String()); err != nil {
			return err
		}
		return audit.LogAs(ctx, tx, p.OrganizationID, &p.UserID, p.IP, p.UserAgent, entry)
	})
	if err != nil {
		return nil, err
	}
	return s.summary(ctx, orgID)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in property.OrganizationInput, ifVersion *int) (*OrgSummary, error) {
	p := authctx.Must(ctx)
	err := s.DB.WithOrgTx(ctx, id, func(ctx context.Context, tx pgx.Tx) error {
		// audit ditulis di organization target (konteks RLS target) dengan actor lintas-org tanpa user_id
		sys := authctx.With(ctx, &authctx.Principal{OrganizationID: id, IsSystem: true, Source: authctx.SourceSystem, FullName: "Platform Admin", IP: p.IP, UserAgent: p.UserAgent})
		_, err := property.UpdateOrganizationTx(sys, tx, id, in, ifVersion)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.summary(ctx, id)
}

func (s *Service) SetStatus(ctx context.Context, id uuid.UUID, status, reason string) (*OrgSummary, error) {
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := property.SetOrganizationStatusTx(ctx, tx, id, status, reason)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.IAM.InvalidateAll()
	return s.summary(ctx, id)
}

// ---------- HTTP ----------

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

func (h *Handler) Mount(r chi.Router) {
	pa := h.IAM.RequirePlatformAdmin
	r.With(pa("platform.org_registry.view")).Get("/platform/organizations", h.list)
	r.With(pa("platform.org_registry.create")).Post("/platform/organizations", h.create)
	r.With(pa("platform.org_registry.view")).Get("/platform/organizations/{id}", h.get)
	r.With(pa("platform.org_registry.update")).Patch("/platform/organizations/{id}", h.update)
	r.With(pa("platform.org_registry.activate")).Post("/platform/organizations/{id}/activate", h.status("active"))
	r.With(pa("platform.org_registry.activate")).Post("/platform/organizations/{id}/deactivate", h.status("inactive"))
	r.With(pa("platform.org_registry.activate")).Post("/platform/organizations/{id}/suspend", h.status("suspended"))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.List(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("status"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	o, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	o, err := h.Svc.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, o)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in property.OrganizationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	o, err := h.Svc.Update(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}

func (h *Handler) status(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &req); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		o, err := h.Svc.SetStatus(r.Context(), id, status, req.Reason)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, o)
	}
}
