package iam

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam/catalog"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
)

// Authenticate: Bearer token → principal di context (TAD §5.6). Endpoint tanpa token ditolak 401.
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			httpx.WriteError(w, r, apperr.Unauthorized("Authorization bearer token diperlukan"))
			return
		}
		claims, err := s.Signer.Verify(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			httpx.WriteError(w, r, apperr.Unauthorized("token tidak valid atau kedaluwarsa"))
			return
		}
		p, err := s.PrincipalFromClaims(r.Context(), claims)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		p.RequestID = httpx.RequestID(r.Context())
		httpx.SetLogIdentity(r.Context(), p.OrganizationID.String(), p.UserID.String())
		p.IP = httpx.ClientIP(r)
		p.UserAgent = r.UserAgent()
		p.Source = authctx.SourceWeb
		switch claims.Src {
		case "mobile":
			p.Source = authctx.SourceMobile
		case authctx.ClientTenantApp:
			p.Source = authctx.SourceTenantApp
		}
		next.ServeHTTP(w, r.WithContext(authctx.With(r.Context(), p)))
	})
}

// passwordChangeAllowed: endpoint yang tetap terbuka saat akun wajib ganti password.
var passwordChangeAllowed = map[string]bool{
	"GET /me":           true,
	"POST /me/password": true,
	"GET /tenant/me":    true,
	"GET /push/config":  true,
}

// PasswordChangeGuard: akun tenant dengan password sementara (must_change_password) hanya boleh memanggil
// endpoint ganti password & profil dasar sampai password diganti — 403 PASSWORD_CHANGE_REQUIRED (P3-ACC-03).
// Ditegakkan server, bukan hanya aplikasi. Endpoint /auth/* (refresh, logout) berada di luar grup ini.
func (s *Service) PasswordChangeGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := authctx.From(r.Context())
		if ok && p.IsTenant && p.MustChangePassword {
			path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1"), "/")
			if !passwordChangeAllowed[r.Method+" "+path] && !strings.HasPrefix(path, "/auth/") {
				httpx.WriteError(w, r, apperr.New(http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED", "Password change required", "Ganti password sementara Anda terlebih dahulu"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Require: permission di level organization (cepat, dari cached permission set). Scope property
// diperiksa di service via authz helper (TAD §5.7).
func (s *Service) Require(perm string) func(http.Handler) http.Handler {
	if !s.Catalog.Exists(perm) {
		panic("iam.Require: permission tidak ada di katalog: " + perm)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := authctx.From(r.Context())
			if !ok {
				httpx.WriteError(w, r, apperr.Unauthorized(""))
				return
			}
			if !p.Has(perm) {
				s.logDenied(r.Context(), perm)
				httpx.WriteError(w, r, apperr.Forbidden("Memerlukan permission "+perm))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAny: salah satu permission cukup.
func (s *Service) RequireAny(perms ...string) func(http.Handler) http.Handler {
	for _, p := range perms {
		if !s.Catalog.Exists(p) {
			panic("iam.RequireAny: permission tidak ada di katalog: " + p)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := authctx.From(r.Context())
			if !ok {
				httpx.WriteError(w, r, apperr.Unauthorized(""))
				return
			}
			for _, perm := range perms {
				if p.Has(perm) {
					next.ServeHTTP(w, r)
					return
				}
			}
			s.logDenied(r.Context(), strings.Join(perms, "|"))
			httpx.WriteError(w, r, apperr.Forbidden(""))
		})
	}
}

// RequireInternalAdmin: hanya role admin_internal pada organization internal BuildingVision (Website PRD §18–§19).
// Permission saja tidak cukup (organization_admin memiliki "*"), sehingga role + flag organization dicek eksplisit.
func (s *Service) RequireInternalAdmin(perm string) func(http.Handler) http.Handler {
	if !s.Catalog.Exists(perm) {
		panic("iam.RequireInternalAdmin: permission tidak ada di katalog: " + perm)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := authctx.From(r.Context())
			if !ok {
				httpx.WriteError(w, r, apperr.Unauthorized(""))
				return
			}
			if !p.IsInternalAdmin || !p.Has(perm) {
				s.logDenied(r.Context(), catalog.RoleAdminInternal+":"+perm)
				httpx.WriteError(w, r, apperr.Forbidden("Memerlukan role "+catalog.RoleAdminInternal))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePlatformAdmin: Platform Admin (PRD P0 v2 §8.2) — role platform_admin pada organization internal +
// permission. Dipakai untuk operasi lintas organization (registry organization).
func (s *Service) RequirePlatformAdmin(perm string) func(http.Handler) http.Handler {
	if !s.Catalog.Exists(perm) {
		panic("iam.RequirePlatformAdmin: permission tidak ada di katalog: " + perm)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := authctx.From(r.Context())
			if !ok {
				httpx.WriteError(w, r, apperr.Unauthorized(""))
				return
			}
			if !p.IsPlatformAdmin || !p.Has(perm) {
				s.logDenied(r.Context(), catalog.RolePlatformAdmin+":"+perm)
				httpx.WriteError(w, r, apperr.Forbidden("Memerlukan role "+catalog.RolePlatformAdmin))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Service) logDenied(ctx context.Context, perm string) {
	if _, ok := authctx.From(ctx); !ok {
		return
	}
	_ = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditAccessDenied, EntityType: "permission", EntityLabel: perm})
	})
}

// CanOnProperty: helper service-layer (TAD §5.7) — object harus berada di property tempat user punya perm.
func CanOnProperty(ctx context.Context, perm string, propertyID uuid.UUID) error {
	p, ok := authctx.From(ctx)
	if !ok {
		return apperr.Unauthorized("")
	}
	if !p.HasOnProperty(perm, propertyID) {
		return apperr.Forbidden("Tidak memiliki " + perm + " pada property ini")
	}
	return nil
}
