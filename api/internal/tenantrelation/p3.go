package tenantrelation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
)

// ---------- P3-ACC-05: reset password tenant oleh staf (dikirim lewat tombol WhatsApp, email di-hold D-P3-08) ----------

type ResetPasswordResult struct {
	TenantUser        *TenantUser `json:"tenant_user"`
	TemporaryPassword string      `json:"temporary_password"`
}

func newTemporaryPassword() (string, error) {
	raw, _, err := iam.NewRefreshToken()
	if err != nil {
		return "", err
	}
	return "Bv" + raw[:8] + "1!", nil
}

// ResetPassword: password sementara (ditampilkan sekali ke staf), wajib ganti saat login berikutnya (P3-ACC-03), sesi dicabut.
func (s *Service) ResetPassword(ctx context.Context, id uuid.UUID) (*ResetPasswordResult, error) {
	p := authctx.Must(ctx)
	var out *ResetPasswordResult
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "tenant_relation.tenant_users.reset_password", t.PropertyID); err != nil {
			return err
		}
		pw, err := newTemporaryPassword()
		if err != nil {
			return err
		}
		hash, err := iam.HashPassword(pw)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, must_change_password = true, failed_login_count = 0, locked_until = NULL, updated_by = $3 WHERE id = $1`, t.UserID, hash, p.UserID); err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, t.UserID)
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditPasswordReset, EntityType: "tenant_user", EntityID: &id, EntityLabel: t.FullName, After: map[string]any{"by": "tenant_relation", "must_change_password": true}})
		t2, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		out = &ResetPasswordResult{TenantUser: t2, TemporaryPassword: pw}
		return nil
	})
	if err == nil {
		s.IAM.InvalidateUser(out.TenantUser.UserID)
	}
	return out, err
}

// ---------- P3-ACC-08: akun anggota yang dibuat Tenant Admin (dipanggil tenantapp lewat AccountCreator) ----------

type MemberInput struct {
	PropertyID uuid.UUID
	TenantID   uuid.UUID
	FullName   string
	Email      string
	Phone      string
	UnitIDs    []uuid.UUID
}

// CreateMemberTx: tanpa permission staf (sudah divalidasi tenantapp: pemanggil tenant_admin aktif, unit milik tenant-nya).
// Akun langsung aktif dengan password sementara + wajib ganti password; tercatat sumber `tenant_admin`.
func (s *Service) CreateMemberTx(ctx context.Context, tx pgx.Tx, in MemberInput) (uuid.UUID, string, error) {
	tid := in.TenantID
	res, err := s.createAccountTx(ctx, tx, CreateInput{PropertyID: in.PropertyID, TenantID: &tid, FullName: in.FullName, Email: in.Email, Phone: in.Phone, Role: "tenant_user", OwnershipStatus: "employee", UnitIDs: in.UnitIDs, Source: "tenant_admin"})
	if err != nil {
		return uuid.Nil, "", err
	}
	return res.TenantUser.ID, res.TemporaryPassword, nil
}

// ---------- P3-FDB-02..03: feedback umum tenant → Tenant Relation ----------

type GeneralFeedback struct {
	ID             uuid.UUID  `json:"id"`
	FeedbackNumber string     `json:"feedback_number"`
	PropertyID     uuid.UUID  `json:"property_id"`
	PropertyName   string     `json:"property_name"`
	Category       string     `json:"category"`
	Subject        *string    `json:"subject"`
	Body           string     `json:"body"`
	IsAnonymous    bool       `json:"is_anonymous"`
	SenderName     *string    `json:"sender_name"` // disembunyikan bila anonim
	TenantName     *string    `json:"tenant_name"` // disembunyikan bila anonim
	UnitLabel      *string    `json:"unit_label"`  // disembunyikan bila anonim
	Status         string     `json:"status"`
	Response       *string    `json:"response"`
	RespondedBy    *string    `json:"responded_by_name"`
	RespondedAt    *time.Time `json:"responded_at"`
	PhotoCount     int        `json:"photo_count"`
	CreatedAt      time.Time  `json:"created_at"`
	AllowedActions []string   `json:"allowed_actions"`
	Version        int        `json:"version"`
}

const gfSelect = `SELECT f.id, f.feedback_number, f.property_id, pl.name, f.category, f.subject, f.body, f.is_anonymous, u.full_name, t.name, COALESCE('Unit ' || un.unit_number, ul.name),
	f.status, f.response, rb.full_name, f.responded_at,
	(SELECT count(*) FROM attachments at WHERE at.object_type = 'tenant_feedback' AND at.object_id = f.id AND at.deleted_at IS NULL), f.created_at, f.version
	FROM tenant_feedback f JOIN locations pl ON pl.id = f.property_id JOIN users u ON u.id = f.tenant_user_id LEFT JOIN tenants t ON t.id = f.tenant_id
	LEFT JOIN locations ul ON ul.id = f.unit_location_id LEFT JOIN units un ON un.location_id = f.unit_location_id LEFT JOIN users rb ON rb.id = f.responded_by`

func scanGF(row pgx.Row) (*GeneralFeedback, error) {
	var f GeneralFeedback
	if err := row.Scan(&f.ID, &f.FeedbackNumber, &f.PropertyID, &f.PropertyName, &f.Category, &f.Subject, &f.Body, &f.IsAnonymous, &f.SenderName, &f.TenantName, &f.UnitLabel,
		&f.Status, &f.Response, &f.RespondedBy, &f.RespondedAt, &f.PhotoCount, &f.CreatedAt, &f.Version); err != nil {
		return nil, err
	}
	if f.IsAnonymous {
		// P3-FDB-02: anonim → identitas pengirim tidak ditampilkan ke staf
		f.SenderName, f.TenantName, f.UnitLabel = nil, nil, nil
	}
	return &f, nil
}

func (s *Service) gfActions(ctx context.Context, f *GeneralFeedback) {
	p := authctx.Must(ctx)
	f.AllowedActions = []string{"view"}
	if !p.HasOnProperty("tenant_relation.feedback.respond", f.PropertyID) {
		return
	}
	switch f.Status {
	case "new":
		f.AllowedActions = append(f.AllowedActions, "review", "respond", "close")
	case "in_review":
		f.AllowedActions = append(f.AllowedActions, "respond", "close")
	case "responded":
		f.AllowedActions = append(f.AllowedActions, "respond", "close")
	}
}

func (s *Service) ListGeneralFeedback(ctx context.Context, propertyID *uuid.UUID, statuses []string, category string, page httpx.Page) ([]GeneralFeedback, *string, error) {
	p := authctx.Must(ctx)
	var out []GeneralFeedback
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if propertyID != nil {
			if err := iam.CanOnProperty(ctx, "tenant_relation.feedback.view", *propertyID); err != nil {
				return err
			}
			args = append(args, *propertyID)
			where += fmt.Sprintf(" AND f.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("tenant_relation.feedback.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND f.property_id = ANY($%d)", len(args))
		}
		if len(statuses) > 0 {
			args = append(args, statuses)
			where += fmt.Sprintf(" AND f.status = ANY($%d)", len(args))
		}
		if category != "" {
			args = append(args, category)
			where += fmt.Sprintf(" AND f.category = $%d", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (f.created_at, f.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, gfSelect+where+fmt.Sprintf(" ORDER BY f.created_at DESC, f.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanGF(rows)
			if err != nil {
				return err
			}
			s.gfActions(ctx, f)
			out = append(out, *f)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []GeneralFeedback{}
	}
	return out, next, err
}

func (s *Service) gfGetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*GeneralFeedback, error) {
	f, err := scanGF(tx.QueryRow(ctx, gfSelect+` WHERE f.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Feedback")
		}
		return nil, err
	}
	if err := iam.CanOnProperty(ctx, "tenant_relation.feedback.view", f.PropertyID); err != nil {
		return nil, err
	}
	s.gfActions(ctx, f)
	return f, nil
}

func (s *Service) GetGeneralFeedback(ctx context.Context, id uuid.UUID) (*GeneralFeedback, error) {
	var out *GeneralFeedback
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.gfGetTx(ctx, tx, id)
		return err
	})
	return out, err
}

type FeedbackActionInput struct {
	Response string `json:"response"`
}

// ActGeneralFeedback: review | respond (tanggapan dikirim ke tenant sebagai notifikasi) | close.
func (s *Service) ActGeneralFeedback(ctx context.Context, id uuid.UUID, action string, in FeedbackActionInput) (*GeneralFeedback, error) {
	p := authctx.Must(ctx)
	var out *GeneralFeedback
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.gfGetTx(ctx, tx, id)
		if err != nil {
			return err
		}
		ok := false
		for _, a := range f.AllowedActions {
			ok = ok || a == action
		}
		if !ok {
			return apperr.InvalidTransition("Aksi " + action + " tidak tersedia untuk feedback berstatus " + f.Status)
		}
		resp := strings.TrimSpace(in.Response)
		switch action {
		case "review":
			_, err = tx.Exec(ctx, `UPDATE tenant_feedback SET status = 'in_review', updated_by = $2 WHERE id = $1`, id, p.UserID)
		case "respond":
			if resp == "" {
				return apperr.Validation("response wajib").WithField("response", "wajib")
			}
			_, err = tx.Exec(ctx, `UPDATE tenant_feedback SET status = 'responded', response = $2, responded_by = $3, responded_at = now(), updated_by = $3 WHERE id = $1`, id, resp, p.UserID)
			if err == nil && s.Jobs != nil {
				preview := resp
				if len([]rune(preview)) > 140 {
					preview = string([]rune(preview)[:137]) + "…"
				}
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "tenant_feedback.responded", OrganizationID: p.OrganizationID, PropertyID: &f.PropertyID, ObjectType: "tenant_feedback", ObjectID: id, ObjectLabel: f.FeedbackNumber, ActorUserID: &p.UserID,
					Payload: map[string]any{"response_preview": preview, "domain": "tenant_relation"}})
			}
		case "close":
			_, err = tx.Exec(ctx, `UPDATE tenant_feedback SET status = 'closed', updated_by = $2 WHERE id = $1`, id, p.UserID)
		}
		if err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "tenant_feedback", EntityID: &id, EntityLabel: f.FeedbackNumber, Before: map[string]any{"status": f.Status}, After: map[string]any{"action": action}})
		out, err = s.gfGetTx(ctx, tx, id)
		return err
	})
	return out, err
}
