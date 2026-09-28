package iam

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/searchindex"
)

// ---------- Organization status (PRD P0 v2 §6) ----------

// orgStatusError: nil bila organization aktif; 403 ORGANIZATION_INACTIVE/SUSPENDED bila tidak.
func orgStatusError(ctx context.Context, q db.Querier, orgID uuid.UUID) *apperr.Error {
	var status string
	if err := q.QueryRow(ctx, `SELECT status FROM organizations WHERE id = $1`, orgID).Scan(&status); err != nil {
		return apperr.New(403, "ORGANIZATION_INACTIVE", "Organization inactive", "Organization tidak ditemukan atau tidak aktif")
	}
	switch status {
	case "active":
		return nil
	case "suspended":
		return apperr.New(403, "ORGANIZATION_SUSPENDED", "Organization suspended", "Organization ditangguhkan; hubungi administrator BuildingVision")
	default:
		return apperr.New(403, "ORGANIZATION_INACTIVE", "Organization inactive", "Organization tidak aktif")
	}
}

// auditLoginFailed: login gagal setelah user dikenali — dicatat di transaksi terpisah (transaksi login di-rollback).
func (s *Service) auditLoginFailed(ctx context.Context, u userRow, in LoginInput, reason string) {
	_ = s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return audit.LogAs(ctx, tx, u.OrgID, &u.ID, in.IP, in.UserAgent, audit.AuditEntry{Action: audit.AuditLoginFailed, EntityType: "user", EntityID: &u.ID, EntityLabel: u.FullName, After: map[string]any{"reason": reason, "client": in.Client}})
	})
}

// ---------- Session management (PRD P0 v2 §24.1) ----------

type Session struct {
	ID         uuid.UUID  `json:"id"`
	Client     string     `json:"client"`
	DeviceID   *string    `json:"device_id"`
	IP         *string    `json:"ip"`
	UserAgent  *string    `json:"user_agent"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Current    bool       `json:"current"`
}

// ListSessions: sesi aktif (belum dicabut & belum kedaluwarsa) milik user.
func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]Session, error) {
	p := authctx.Must(ctx)
	out := []Session{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.userExistsTx(ctx, tx, userID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, client, device_id, host(ip), user_agent, created_at, last_used_at, expires_at
			FROM sessions WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now() ORDER BY COALESCE(last_used_at, created_at) DESC`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var se Session
			if err := rows.Scan(&se.ID, &se.Client, &se.DeviceID, &se.IP, &se.UserAgent, &se.CreatedAt, &se.LastUsedAt, &se.ExpiresAt); err != nil {
				return err
			}
			se.Current = userID == p.UserID && se.ID == p.SessionID
			out = append(out, se)
		}
		return rows.Err()
	})
	return out, err
}

// RevokeSessions mencabut sesi user: sessionID != nil → satu sesi; keepCurrent → semua kecuali sesi pemanggil.
// permission_version dinaikkan agar access token yang masih berlaku di semua instance API ikut divalidasi ulang.
func (s *Service) RevokeSessions(ctx context.Context, userID uuid.UUID, sessionID *uuid.UUID, keepCurrent bool) (int, error) {
	p := authctx.Must(ctx)
	var n int64
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.userExistsTx(ctx, tx, userID); err != nil {
			return err
		}
		q := `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`
		args := []any{userID}
		switch {
		case sessionID != nil:
			q += ` AND id = $2`
			args = append(args, *sessionID)
		case keepCurrent && userID == p.UserID:
			q += ` AND id <> $2`
			args = append(args, p.SessionID)
		}
		tag, err := tx.Exec(ctx, q, args...)
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		if sessionID != nil && n == 0 {
			return apperr.NotFound("Session")
		}
		if n > 0 {
			if _, err := tx.Exec(ctx, `UPDATE users SET permission_version = permission_version + 1 WHERE id = $1`, userID); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditSessionRevoke, EntityType: "user", EntityID: &userID,
			After: map[string]any{"session_id": sessionID, "keep_current": keepCurrent, "revoked": n}})
	})
	if err == nil {
		s.InvalidateUser(userID)
	}
	return int(n), err
}

func (s *Service) userExistsTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL)`, userID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return apperr.NotFound("User")
	}
	return nil
}

// ---------- User activation (PRD P0 v2 §8.1) ----------

// SetUserActive: aktivasi/deaktivasi eksplisit dengan alasan; deaktivasi mencabut semua sesi.
func (s *Service) SetUserActive(ctx context.Context, id uuid.UUID, active bool, reason string) (*User, error) {
	p := authctx.Must(ctx)
	if id == p.UserID && !active {
		return nil, apperr.Validation("Tidak dapat menonaktifkan akun sendiri")
	}
	var out *User
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getUserTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.IsActive == active {
			out = before
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET is_active = $2, permission_version = permission_version + 1, updated_by = $3,
			failed_login_count = CASE WHEN $2 THEN 0 ELSE failed_login_count END, locked_until = CASE WHEN $2 THEN NULL ELSE locked_until END WHERE id = $1`, id, active, p.UserID); err != nil {
			return err
		}
		if !active {
			if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
				return err
			}
		}
		if err := audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "user", EntityID: &id, EntityLabel: before.UserCode,
			Before: map[string]any{"is_active": before.IsActive}, After: map[string]any{"is_active": active, "reason": reason}}); err != nil {
			return err
		}
		_ = searchindex.IndexTx(ctx, tx, p.OrganizationID, "user", id)
		out, err = s.getUserTx(ctx, tx, id)
		return err
	})
	if err == nil {
		s.InvalidateUser(id)
	}
	return out, err
}

// ---------- Privilege escalation guard (PRD P0 v2 §8.3) ----------

// checkGrantable: pemberi role harus memiliki seluruh permission role tersebut pada scope yang diberikan,
// sehingga admin property tidak dapat menaikkan hak akses (mis. memberi organization_admin).
func checkGrantable(ctx context.Context, tx pgx.Tx, roleID uuid.UUID, propertyID *uuid.UUID) error {
	p := authctx.Must(ctx)
	if p.IsSystem {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT permission_code FROM role_permissions WHERE role_id = $1`, roleID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			return err
		}
		var ok bool
		if propertyID == nil {
			_, ok = p.PropertyIDsFor(perm)
		} else {
			ok = p.HasOnProperty(perm, *propertyID)
		}
		if !ok {
			return apperr.New(403, "ROLE_NOT_GRANTABLE", "Role not grantable", "Tidak dapat memberikan role dengan permission yang tidak Anda miliki ("+perm+")")
		}
	}
	return rows.Err()
}
