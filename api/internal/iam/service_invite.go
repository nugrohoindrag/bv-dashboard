package iam

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/mailer"
)

// ---------- Undangan user (PRD P0 v2 §26.1 "Invite Users") ----------
//
// Admin membuat user tanpa password (atau mengirim ulang undangan); user menerima email berisi tautan
// sekali pakai (TTL 72 jam) untuk menetapkan password sendiri. Token disimpan sebagai hash (password_reset_tokens).

const inviteTTL = 72 * time.Hour

type InviteResult struct {
	UserID    uuid.UUID `json:"user_id"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
	// InviteURL hanya dikembalikan di lingkungan non-produksi (tanpa SMTP) untuk uji manual.
	InviteURL *string `json:"invite_url,omitempty"`
}

// Invite membuat token undangan baru (token lama dibatalkan) dan mengirim email.
func (s *Service) Invite(ctx context.Context, userID uuid.UUID) (*InviteResult, error) {
	p := authctx.Must(ctx)
	raw, hash, err := NewRefreshToken()
	if err != nil {
		return nil, err
	}
	var out InviteResult
	var fullName, orgName string
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var email *string
		var active bool
		if err := tx.QueryRow(ctx, `SELECT u.email, u.full_name, u.is_active, o.name FROM users u JOIN organizations o ON o.id = u.organization_id WHERE u.id = $1 AND u.deleted_at IS NULL`, userID).
			Scan(&email, &fullName, &active, &orgName); err != nil {
			return apperr.NotFound("User")
		}
		if email == nil || *email == "" {
			return apperr.Validation("User tanpa email tidak dapat diundang")
		}
		if !active {
			return apperr.Conflict("USER_INACTIVE", "User nonaktif; aktifkan terlebih dahulu")
		}
		if _, err := tx.Exec(ctx, `UPDATE password_reset_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
			return err
		}
		out = InviteResult{UserID: userID, Email: *email, ExpiresAt: time.Now().Add(inviteTTL).UTC()}
		if _, err := tx.Exec(ctx, `INSERT INTO password_reset_tokens (token_hash, organization_id, user_id, expires_at) VALUES ($1,$2,$3,$4)`,
			hash, p.OrganizationID, userID, out.ExpiresAt); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditInvite, EntityType: "user", EntityID: &userID, EntityLabel: *email, After: map[string]any{"expires_at": out.ExpiresAt}})
	})
	if err != nil {
		return nil, err
	}
	link := strings.TrimRight(s.PublicURL, "/") + "/accept-invite?token=" + raw
	if s.Mailer != nil {
		msg := mailer.Message{To: []string{out.Email}, Subject: "Undangan BuildingVision — " + orgName,
			Text: "Halo " + fullName + ",\n\nAnda diundang bergabung ke " + orgName + " di BuildingVision.\nTetapkan password Anda melalui tautan berikut (berlaku 72 jam):\n\n" + link + "\n\nAbaikan email ini bila Anda tidak merasa diundang.\n\n— BuildingVision"}
		go func() { _ = s.Mailer.Send(context.WithoutCancel(ctx), msg) }()
	}
	if s.ExposeInviteLink {
		out.InviteURL = &link
	}
	return &out, nil
}

// AcceptInvite: publik — tetapkan password dari token undangan (sekali pakai). Tidak membuat sesi; user lalu login.
func (s *Service) AcceptInvite(ctx context.Context, rawToken, newPassword, ip, ua string) error {
	if strings.TrimSpace(rawToken) == "" {
		return apperr.Validation("token wajib")
	}
	if len(newPassword) < 8 {
		return apperr.Validation("password minimal 8 karakter").WithField("password", "min 8")
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var userID, orgID uuid.UUID
		var expires time.Time
		var used *time.Time
		if err := tx.QueryRow(ctx, `SELECT user_id, organization_id, expires_at, used_at FROM password_reset_tokens WHERE token_hash = $1`, HashToken(rawToken)).
			Scan(&userID, &orgID, &expires, &used); err != nil {
			return apperr.New(400, "INVITE_INVALID", "Invalid invite", "Tautan undangan tidak valid")
		}
		if used != nil || expires.Before(time.Now()) {
			return apperr.New(400, "INVITE_EXPIRED", "Invite expired", "Tautan undangan sudah dipakai atau kedaluwarsa; minta undangan baru ke admin")
		}
		if err := orgStatusError(ctx, tx, orgID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.organization_id', $1, true)`, orgID.String()); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, email_verified_at = COALESCE(email_verified_at, now()), failed_login_count = 0, locked_until = NULL
			WHERE id = $1 AND is_active AND deleted_at IS NULL`, userID, hash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.New(400, "INVITE_INVALID", "Invalid invite", "Akun tidak aktif")
		}
		if _, err := tx.Exec(ctx, `UPDATE password_reset_tokens SET used_at = now() WHERE token_hash = $1`, HashToken(rawToken)); err != nil {
			return err
		}
		return audit.LogAs(ctx, tx, orgID, &userID, ip, ua, audit.AuditEntry{Action: audit.AuditPasswordReset, EntityType: "user", EntityID: &userID, After: map[string]any{"via": "invite"}})
	})
}
