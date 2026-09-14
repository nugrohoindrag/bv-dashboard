package iam

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam/catalog"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
)

// ---------- Users ----------

type RoleAssignment struct {
	RoleID     uuid.UUID  `json:"role_id"`
	RoleCode   string     `json:"role_code,omitempty"`
	RoleName   string     `json:"role_name,omitempty"`
	PropertyID *uuid.UUID `json:"property_id"`
}

type TeamMembership struct {
	TeamID   uuid.UUID `json:"team_id"`
	TeamName string    `json:"team_name,omitempty"`
	Domain   string    `json:"domain,omitempty"`
	IsLead   bool      `json:"is_lead"`
}

type User struct {
	ID          uuid.UUID        `json:"id"`
	UserCode    string           `json:"user_code"`
	Email       *string          `json:"email"`
	Username    *string          `json:"username"`
	FullName    string           `json:"full_name"`
	Phone       *string          `json:"phone"`
	IsActive    bool             `json:"is_active"`
	Locale      string           `json:"preferred_locale"`
	LastLoginAt *time.Time       `json:"last_login_at"`
	Roles       []RoleAssignment `json:"roles"`
	Teams       []TeamMembership `json:"teams"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Version     int              `json:"version"`
}

type CreateUserInput struct {
	Email    *string          `json:"email"`
	Username *string          `json:"username"`
	FullName string           `json:"full_name"`
	Phone    *string          `json:"phone"`
	Password string           `json:"password"`
	Roles    []RoleAssignment `json:"roles"`
	TeamIDs  []uuid.UUID      `json:"team_ids"`
}

type UpdateUserInput struct {
	FullName *string           `json:"full_name"`
	Phone    *string           `json:"phone"`
	Email    *string           `json:"email"`
	Username *string           `json:"username"`
	IsActive *bool             `json:"is_active"`
	Locale   *string           `json:"preferred_locale"`
	Roles    *[]RoleAssignment `json:"roles"`
	TeamIDs  *[]uuid.UUID      `json:"team_ids"`
}

func (s *Service) CreateUser(ctx context.Context, in CreateUserInput) (*User, error) {
	p := authctx.Must(ctx)
	if strings.TrimSpace(in.FullName) == "" {
		return nil, apperr.Validation("full_name wajib").WithField("full_name", "wajib")
	}
	if (in.Email == nil || *in.Email == "") && (in.Username == nil || *in.Username == "") {
		return nil, apperr.Validation("email atau username wajib")
	}
	if len(in.Password) < 8 {
		return nil, apperr.Validation("password minimal 8 karakter").WithField("password", "min 8")
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	var out *User
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		code, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixUser)
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO users (organization_id, user_code, email, username, full_name, phone, password_hash, created_by, updated_by)
			VALUES ($1,$2,NULLIF(lower($3),''),NULLIF(lower($4),''),$5,$6,$7,$8,$8) RETURNING id`,
			p.OrganizationID, code, deref(in.Email), deref(in.Username), strings.TrimSpace(in.FullName), in.Phone, hash, p.UserID).Scan(&id)
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("DUPLICATE_USER", "Email/username sudah digunakan")
			}
			return err
		}
		if err := s.replaceUserRoles(ctx, tx, id, in.Roles); err != nil {
			return err
		}
		if err := s.replaceUserTeams(ctx, tx, id, in.TeamIDs); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "user", EntityID: &id, EntityLabel: code, After: map[string]any{"full_name": in.FullName, "roles": in.Roles}})
		out, err = s.getUserTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateUser(ctx context.Context, id uuid.UUID, in UpdateUserInput, ifVersion *int) (*User, error) {
	p := authctx.Must(ctx)
	var out *User
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getUserTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		_, err = tx.Exec(ctx, `
			UPDATE users SET full_name = COALESCE($2, full_name), phone = COALESCE($3, phone),
			  email = COALESCE(NULLIF(lower($4),''), email), username = COALESCE(NULLIF(lower($5),''), username),
			  is_active = COALESCE($6, is_active), preferred_locale = COALESCE($7, preferred_locale), updated_by = $8
			WHERE id = $1`, id, in.FullName, in.Phone, deref(in.Email), deref(in.Username), in.IsActive, in.Locale, p.UserID)
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("DUPLICATE_USER", "Email/username sudah digunakan")
			}
			return err
		}
		roleChanged := false
		if in.Roles != nil {
			if err := s.replaceUserRoles(ctx, tx, id, *in.Roles); err != nil {
				return err
			}
			roleChanged = true
		}
		if in.TeamIDs != nil {
			if err := s.replaceUserTeams(ctx, tx, id, *in.TeamIDs); err != nil {
				return err
			}
			roleChanged = true
		}
		if in.IsActive != nil && !*in.IsActive {
			_, _ = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id)
			roleChanged = true
		}
		if roleChanged {
			_, _ = tx.Exec(ctx, `UPDATE users SET permission_version = permission_version + 1 WHERE id = $1`, id)
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditRoleChange, EntityType: "user", EntityID: &id, EntityLabel: before.UserCode, Before: before.Roles, After: in.Roles})
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "user", EntityID: &id, EntityLabel: before.UserCode, Before: before, After: in})
		out, err = s.getUserTx(ctx, tx, id)
		return err
	})
	if err == nil {
		s.InvalidateUser(id)
	}
	return out, err
}

func (s *Service) ResetPassword(ctx context.Context, id uuid.UUID, newPassword string) error {
	if len(newPassword) < 8 {
		return apperr.Validation("password minimal 8 karakter")
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	p := authctx.Must(ctx)
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, failed_login_count = 0, locked_until = NULL, updated_by = $3 WHERE id = $1 AND deleted_at IS NULL`, id, hash, p.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("User")
		}
		_, _ = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id)
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditPasswordReset, EntityType: "user", EntityID: &id})
	})
	if err == nil {
		s.InvalidateUser(id)
	}
	return err
}

func (s *Service) ChangeOwnPassword(ctx context.Context, current, newPassword string) error {
	p := authctx.Must(ctx)
	if len(newPassword) < 8 {
		return apperr.Validation("password minimal 8 karakter")
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var hash string
		if err := tx.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, p.UserID).Scan(&hash); err != nil {
			return err
		}
		if !VerifyPassword(hash, current) {
			return apperr.Validation("password saat ini salah").WithField("current_password", "salah")
		}
		nh, err := HashPassword(newPassword)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, p.UserID, nh)
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditPasswordReset, EntityType: "user", EntityID: &p.UserID, EntityLabel: p.FullName})
	})
}

func (s *Service) replaceUserRoles(ctx context.Context, tx pgx.Tx, userID uuid.UUID, roles []RoleAssignment) error {
	p := authctx.Must(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, r := range roles {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE id = $1 AND deleted_at IS NULL)`, r.RoleID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return apperr.Validation("role tidak ditemukan: " + r.RoleID.String())
		}
		if r.PropertyID != nil {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM properties WHERE location_id = $1)`, *r.PropertyID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return apperr.Validation("property tidak ditemukan: " + r.PropertyID.String())
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id, property_id, granted_by) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, userID, r.RoleID, r.PropertyID, p.UserID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) replaceUserTeams(ctx context.Context, tx pgx.Tx, userID uuid.UUID, teamIDs []uuid.UUID) error {
	// pertahankan is_lead untuk team yang tetap
	if _, err := tx.Exec(ctx, `DELETE FROM team_members WHERE user_id = $1 AND NOT (team_id = ANY($2::uuid[]))`, userID, teamIDs); err != nil {
		return err
	}
	for _, t := range teamIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, t, userID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	var out *User
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.getUserTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) getUserTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*User, error) {
	var u User
	err := tx.QueryRow(ctx, `SELECT id, user_code, email, username, full_name, phone, is_active, preferred_locale, last_login_at, created_at, updated_at, version
		FROM users WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&u.ID, &u.UserCode, &u.Email, &u.Username, &u.FullName, &u.Phone, &u.IsActive, &u.Locale, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt, &u.Version)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("User")
		}
		return nil, err
	}
	u.Roles = []RoleAssignment{}
	rows, err := tx.Query(ctx, `SELECT ur.role_id, r.code, r.name, ur.property_id FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = $1 ORDER BY r.name`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ra RoleAssignment
		if err := rows.Scan(&ra.RoleID, &ra.RoleCode, &ra.RoleName, &ra.PropertyID); err != nil {
			rows.Close()
			return nil, err
		}
		u.Roles = append(u.Roles, ra)
	}
	rows.Close()
	u.Teams = []TeamMembership{}
	trows, err := tx.Query(ctx, `SELECT tm.team_id, t.name, t.domain, tm.is_lead FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE tm.user_id = $1 ORDER BY t.name`, id)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var tm TeamMembership
		if err := trows.Scan(&tm.TeamID, &tm.TeamName, &tm.Domain, &tm.IsLead); err != nil {
			return nil, err
		}
		u.Teams = append(u.Teams, tm)
	}
	return &u, trows.Err()
}

type UserFilter struct {
	Q          string
	IsActive   *bool
	RoleCode   string
	TeamID     *uuid.UUID
	PropertyID *uuid.UUID
}

func (s *Service) ListUsers(ctx context.Context, f UserFilter, page httpx.Page) ([]User, *string, error) {
	var out []User
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		where := "WHERE u.deleted_at IS NULL"
		if f.Q != "" {
			args = append(args, "%"+f.Q+"%")
			where += " AND (u.full_name ILIKE $1 OR u.email ILIKE $1 OR u.username ILIKE $1 OR u.user_code ILIKE $1)"
		}
		if f.IsActive != nil {
			args = append(args, *f.IsActive)
			where += " AND u.is_active = $" + itoa(len(args))
		}
		if f.RoleCode != "" {
			args = append(args, f.RoleCode)
			where += " AND EXISTS (SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = u.id AND r.code = $" + itoa(len(args)) + ")"
		}
		if f.TeamID != nil {
			args = append(args, *f.TeamID)
			where += " AND EXISTS (SELECT 1 FROM team_members tm WHERE tm.user_id = u.id AND tm.team_id = $" + itoa(len(args)) + ")"
		}
		if f.PropertyID != nil {
			args = append(args, *f.PropertyID)
			where += " AND EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = u.id AND (ur.property_id IS NULL OR ur.property_id = $" + itoa(len(args)) + "))"
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += " AND (u.full_name, u.id) > ($" + itoa(len(args)-1) + ", $" + itoa(len(args)) + ")"
		}
		args = append(args, page.Limit+1)
		rows, err := tx.Query(ctx, `SELECT u.id FROM users u `+where+` ORDER BY u.full_name, u.id LIMIT $`+itoa(len(args)), args...)
		if err != nil {
			return err
		}
		var idsList []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			idsList = append(idsList, id)
		}
		rows.Close()
		for _, id := range idsList {
			u, err := s.getUserTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *u)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			out = out[:page.Limit]
			c := httpx.EncodeCursor(last.FullName, last.ID)
			next = &c
		}
		return nil
	})
	return out, next, err
}

// ---------- Roles ----------

type Role struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	IsSystem    bool      `json:"is_system"`
	Domain      *string   `json:"domain"`
	Permissions []string  `json:"permissions"`
	UserCount   int       `json:"user_count"`
	Version     int       `json:"version"`
}

type RoleInput struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Domain      *string  `json:"domain"`
	Permissions []string `json:"permissions"`
}

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	var out []Role
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT r.id, r.code, r.name, r.is_system, r.domain, r.version,
			       COALESCE((SELECT array_agg(permission_code ORDER BY permission_code) FROM role_permissions WHERE role_id = r.id), '{}'),
			       (SELECT count(DISTINCT user_id) FROM user_roles WHERE role_id = r.id)
			FROM roles r WHERE r.deleted_at IS NULL ORDER BY r.is_system DESC, r.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r Role
			if err := rows.Scan(&r.ID, &r.Code, &r.Name, &r.IsSystem, &r.Domain, &r.Version, &r.Permissions, &r.UserCount); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateRole(ctx context.Context, in RoleInput) (*Role, error) {
	p := authctx.Must(ctx)
	in.Code = strings.ToLower(strings.TrimSpace(in.Code))
	if in.Code == "" || in.Name == "" {
		return nil, apperr.Validation("code dan name wajib")
	}
	perms := s.Catalog.Expand(in.Permissions)
	var out *Role
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `INSERT INTO roles (organization_id, code, name, domain, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$5) RETURNING id`,
			p.OrganizationID, in.Code, in.Name, in.Domain, p.UserID).Scan(&id)
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("DUPLICATE_ROLE", "Kode role sudah ada")
			}
			return err
		}
		for _, pc := range perms {
			if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission_code) VALUES ($1,$2)`, id, pc); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "role", EntityID: &id, EntityLabel: in.Code, After: in})
		out = &Role{ID: id, Code: in.Code, Name: in.Name, Domain: in.Domain, Permissions: perms, Version: 1}
		return nil
	})
	return out, err
}

func (s *Service) UpdateRole(ctx context.Context, id uuid.UUID, in RoleInput) (*Role, error) {
	p := authctx.Must(ctx)
	perms := s.Catalog.Expand(in.Permissions)
	var out *Role
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var isSystem bool
		var code string
		if err := tx.QueryRow(ctx, `SELECT is_system, code FROM roles WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&isSystem, &code); err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Role")
			}
			return err
		}
		if code == "organization_admin" {
			return apperr.Forbidden("Role Organization Admin tidak dapat diubah")
		}
		if _, err := tx.Exec(ctx, `UPDATE roles SET name = COALESCE(NULLIF($2,''), name), domain = COALESCE($3, domain), updated_by = $4 WHERE id = $1`, id, in.Name, in.Domain, p.UserID); err != nil {
			return err
		}
		if in.Permissions != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, id); err != nil {
				return err
			}
			for _, pc := range perms {
				if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission_code) VALUES ($1,$2)`, id, pc); err != nil {
					return err
				}
			}
			// paksa refresh token untuk semua user dengan role ini
			if _, err := tx.Exec(ctx, `UPDATE users SET permission_version = permission_version + 1 WHERE id IN (SELECT user_id FROM user_roles WHERE role_id = $1)`, id); err != nil {
				return err
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditPermissionChange, EntityType: "role", EntityID: &id, EntityLabel: code, After: perms})
		}
		out = &Role{ID: id, Code: code, Name: in.Name, Domain: in.Domain, Permissions: perms, IsSystem: isSystem}
		return nil
	})
	if err == nil {
		s.invalidateAll()
	}
	return out, err
}

func (s *Service) DeleteRole(ctx context.Context, id uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var isSystem bool
		var code string
		if err := tx.QueryRow(ctx, `SELECT is_system, code FROM roles WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&isSystem, &code); err != nil {
			return apperr.NotFound("Role")
		}
		if isSystem {
			return apperr.Forbidden("Role sistem tidak dapat dihapus")
		}
		var n int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM user_roles WHERE role_id = $1`, id).Scan(&n)
		if n > 0 {
			return apperr.Conflict("ROLE_IN_USE", "Role masih dipakai oleh user")
		}
		_, err := tx.Exec(ctx, `UPDATE roles SET deleted_at = now() WHERE id = $1`, id)
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "role", EntityID: &id, EntityLabel: code})
	})
}

// ---------- Teams ----------

type TeamMember struct {
	UserID   uuid.UUID `json:"user_id"`
	FullName string    `json:"full_name"`
	IsLead   bool      `json:"is_lead"`
}

type Team struct {
	ID         uuid.UUID    `json:"id"`
	PropertyID *uuid.UUID   `json:"property_id"`
	Name       string       `json:"name"`
	Domain     string       `json:"domain"`
	IsActive   bool         `json:"is_active"`
	Members    []TeamMember `json:"members"`
	Version    int          `json:"version"`
}

type TeamInput struct {
	PropertyID *uuid.UUID    `json:"property_id"`
	Name       string        `json:"name"`
	Domain     string        `json:"domain"`
	IsActive   *bool         `json:"is_active"`
	Members    *[]TeamMember `json:"members"`
}

var validDomains = map[string]bool{"engineering": true, "security": true, "housekeeping": true, "management": true}

func (s *Service) CreateTeam(ctx context.Context, in TeamInput) (*Team, error) {
	p := authctx.Must(ctx)
	if in.Name == "" || !validDomains[in.Domain] {
		return nil, apperr.Validation("name wajib; domain harus engineering|security|housekeeping|management")
	}
	var out *Team
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO teams (organization_id, property_id, name, domain, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$5) RETURNING id`,
			p.OrganizationID, in.PropertyID, in.Name, in.Domain, p.UserID).Scan(&id); err != nil {
			return err
		}
		if in.Members != nil {
			if err := s.replaceTeamMembers(ctx, tx, id, *in.Members); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "team", EntityID: &id, EntityLabel: in.Name, After: in})
		var err error
		out, err = s.getTeamTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateTeam(ctx context.Context, id uuid.UUID, in TeamInput) (*Team, error) {
	p := authctx.Must(ctx)
	var out *Team
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getTeamTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if in.Domain != "" && !validDomains[in.Domain] {
			return apperr.Validation("domain tidak valid")
		}
		if _, err := tx.Exec(ctx, `UPDATE teams SET name = COALESCE(NULLIF($2,''), name), domain = COALESCE(NULLIF($3,''), domain), property_id = COALESCE($4, property_id), is_active = COALESCE($5, is_active), updated_by = $6 WHERE id = $1`,
			id, in.Name, in.Domain, in.PropertyID, in.IsActive, p.UserID); err != nil {
			return err
		}
		if in.Members != nil {
			if err := s.replaceTeamMembers(ctx, tx, id, *in.Members); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "team", EntityID: &id, EntityLabel: before.Name, Before: before, After: in})
		out, err = s.getTeamTx(ctx, tx, id)
		return err
	})
	if err == nil {
		s.invalidateAll()
	}
	return out, err
}

func (s *Service) replaceTeamMembers(ctx context.Context, tx pgx.Tx, teamID uuid.UUID, members []TeamMember) error {
	if _, err := tx.Exec(ctx, `DELETE FROM team_members WHERE team_id = $1`, teamID); err != nil {
		return err
	}
	for _, m := range members {
		if _, err := tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id, is_lead) VALUES ($1,$2,$3) ON CONFLICT (team_id, user_id) DO UPDATE SET is_lead = EXCLUDED.is_lead`, teamID, m.UserID, m.IsLead); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) getTeamTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Team, error) {
	var t Team
	if err := tx.QueryRow(ctx, `SELECT id, property_id, name, domain, is_active, version FROM teams WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&t.ID, &t.PropertyID, &t.Name, &t.Domain, &t.IsActive, &t.Version); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Team")
		}
		return nil, err
	}
	t.Members = []TeamMember{}
	rows, err := tx.Query(ctx, `SELECT tm.user_id, u.full_name, tm.is_lead FROM team_members tm JOIN users u ON u.id = tm.user_id WHERE tm.team_id = $1 ORDER BY tm.is_lead DESC, u.full_name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m TeamMember
		if err := rows.Scan(&m.UserID, &m.FullName, &m.IsLead); err != nil {
			return nil, err
		}
		t.Members = append(t.Members, m)
	}
	return &t, rows.Err()
}

func (s *Service) GetTeam(ctx context.Context, id uuid.UUID) (*Team, error) {
	var out *Team
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.getTeamTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) ListTeams(ctx context.Context, propertyID *uuid.UUID, domain string) ([]Team, error) {
	var out []Team
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM teams WHERE deleted_at IS NULL AND ($1::uuid IS NULL OR property_id IS NULL OR property_id = $1) AND ($2 = '' OR domain = $2) ORDER BY name`, propertyID, domain)
		if err != nil {
			return err
		}
		var idsList []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			idsList = append(idsList, id)
		}
		rows.Close()
		for _, id := range idsList {
			t, err := s.getTeamTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *t)
		}
		return nil
	})
	return out, err
}

func (s *Service) DeleteTeam(ctx context.Context, id uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE teams SET deleted_at = now(), is_active = false WHERE id = $1 AND deleted_at IS NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Team")
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "team", EntityID: &id})
	})
}

// ---------- Device tokens (push) ----------

type DeviceInput struct {
	Platform   string `json:"platform"`
	Token      string `json:"token"`
	DeviceID   string `json:"device_id"`
	AppVersion string `json:"app_version"`
}

func (s *Service) RegisterDevice(ctx context.Context, in DeviceInput) error {
	p := authctx.Must(ctx)
	if in.Token == "" || (in.Platform != "android" && in.Platform != "ios" && in.Platform != "web") {
		return apperr.Validation("platform (android|ios|web) dan token wajib")
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO device_tokens (organization_id, user_id, platform, token, device_id, app_version)
			VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''))
			ON CONFLICT (token) DO UPDATE SET user_id = EXCLUDED.user_id, organization_id = EXCLUDED.organization_id, device_id = EXCLUDED.device_id, app_version = EXCLUDED.app_version, last_seen_at = now()`,
			p.OrganizationID, p.UserID, in.Platform, in.Token, in.DeviceID, in.AppVersion)
		return err
	})
}

func (s *Service) UnregisterDevice(ctx context.Context, token string) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM device_tokens WHERE user_id = $1 AND token = $2`, p.UserID, token)
		return err
	})
}

// ---------- Seed role template per organization (TAD §5.7) ----------

// SeedSystemRoles membuat/menyinkronkan role is_system per organization dari katalog.
func (s *Service) SeedSystemRoles(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	for _, rt := range s.Catalog.Roles {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO roles (organization_id, code, name, is_system, domain) VALUES ($1,$2,$3,true,$4)
			ON CONFLICT (organization_id, code) DO UPDATE SET name = EXCLUDED.name, domain = EXCLUDED.domain
			RETURNING id`, orgID, rt.Code, rt.Label, rt.Domain).Scan(&id)
		if err != nil {
			return err
		}
		for _, pc := range rt.Permissions {
			if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission_code) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, pc); err != nil {
				return err
			}
		}
	}
	return nil
}

// SeedPermissionCatalog menulis katalog global (tanpa RLS).
func SeedPermissionCatalog(ctx context.Context, q db.Querier) error {
	c := catalog.MustLoad()
	for _, p := range c.Permissions {
		if _, err := q.Exec(ctx, `INSERT INTO permissions (code, module, object, action) VALUES ($1,$2,$3,$4) ON CONFLICT (code) DO NOTHING`, p.Code, p.Module, p.Object, p.Action); err != nil {
			return err
		}
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
