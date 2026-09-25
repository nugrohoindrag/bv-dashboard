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

const EventAnnouncementPublished = "announcement.published"

// Announcement: komunikasi Tenant Relation → tenant/staf (PRD §3.5 Communication; NC §71 Tenant Relation › Communication).
type Announcement struct {
	ID                uuid.UUID  `json:"id"`
	PropertyID        *uuid.UUID `json:"property_id"`
	PropertyName      *string    `json:"property_name"`
	Title             string     `json:"title"`
	Excerpt           *string    `json:"excerpt"`
	Body              string     `json:"body"`
	Audience          string     `json:"audience"`
	Importance        string     `json:"importance"`
	ImageAttachmentID *uuid.UUID `json:"image_attachment_id"`
	Status            string     `json:"status"`
	PublishedAt       *time.Time `json:"published_at"`
	ExpiresAt         *time.Time `json:"expires_at"`
	CreatedAt         time.Time  `json:"created_at"`
	CreatedByName     *string    `json:"created_by_name"`
	Version           int        `json:"version"`
	AllowedActions    []string   `json:"allowed_actions"`
}

const annSelect = `SELECT a.id, a.property_id, pl.name, a.title, a.excerpt, a.body, a.audience, a.importance, a.image_attachment_id, a.status, a.published_at, a.expires_at, a.created_at, u.full_name, a.version
	FROM announcements a LEFT JOIN locations pl ON pl.id = a.property_id LEFT JOIN users u ON u.id = a.created_by`

func scanAnn(row pgx.Row) (*Announcement, error) {
	var a Announcement
	if err := row.Scan(&a.ID, &a.PropertyID, &a.PropertyName, &a.Title, &a.Excerpt, &a.Body, &a.Audience, &a.Importance, &a.ImageAttachmentID, &a.Status, &a.PublishedAt, &a.ExpiresAt, &a.CreatedAt, &a.CreatedByName, &a.Version); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Service) annFinalize(ctx context.Context, a *Announcement) {
	p := authctx.Must(ctx)
	a.AllowedActions = []string{"view"}
	can := func(perm string) bool {
		if a.PropertyID != nil {
			return p.HasOnProperty(perm, *a.PropertyID)
		}
		return p.Has(perm)
	}
	if a.Status != "archived" && can("tenant_relation.announcements.update") {
		a.AllowedActions = append(a.AllowedActions, "update")
	}
	if can("tenant_relation.announcements.publish") {
		switch a.Status {
		case "draft":
			a.AllowedActions = append(a.AllowedActions, "publish", "archive")
		case "published":
			a.AllowedActions = append(a.AllowedActions, "archive")
		}
	}
}

type AnnouncementInput struct {
	PropertyID        *uuid.UUID `json:"property_id"`
	Title             *string    `json:"title"`
	Excerpt           *string    `json:"excerpt"`
	Body              *string    `json:"body"`
	Audience          *string    `json:"audience"`
	Importance        *string    `json:"importance"`
	ImageAttachmentID *uuid.UUID `json:"image_attachment_id"`
	ExpiresAt         *time.Time `json:"expires_at"`
}

func (s *Service) ListAnnouncements(ctx context.Context, propertyID *uuid.UUID, status []string, page httpx.Page) ([]Announcement, *string, error) {
	p := authctx.Must(ctx)
	var out []Announcement
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if propertyID != nil {
			if err := iam.CanOnProperty(ctx, "tenant_relation.announcements.view", *propertyID); err != nil {
				return err
			}
			args = append(args, *propertyID)
			where += fmt.Sprintf(" AND (a.property_id IS NULL OR a.property_id = $%d)", len(args))
		} else if pids, all := p.PropertyIDsFor("tenant_relation.announcements.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND (a.property_id IS NULL OR a.property_id = ANY($%d))", len(args))
		}
		if len(status) > 0 {
			args = append(args, status)
			where += fmt.Sprintf(" AND a.status = ANY($%d)", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (a.created_at, a.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, annSelect+where+fmt.Sprintf(" ORDER BY a.created_at DESC, a.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []Announcement
		for rows.Next() {
			a, err := scanAnn(rows)
			if err != nil {
				return err
			}
			items = append(items, *a)
		}
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			s.annFinalize(ctx, &items[i])
		}
		out = items
		return rows.Err()
	})
	if out == nil {
		out = []Announcement{}
	}
	return out, next, err
}

func (s *Service) annGetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Announcement, error) {
	a, err := scanAnn(tx.QueryRow(ctx, annSelect+` WHERE a.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Announcement")
		}
		return nil, err
	}
	if a.PropertyID != nil {
		if err := iam.CanOnProperty(ctx, "tenant_relation.announcements.view", *a.PropertyID); err != nil {
			return nil, err
		}
	}
	s.annFinalize(ctx, a)
	return a, nil
}

func (s *Service) GetAnnouncement(ctx context.Context, id uuid.UUID) (*Announcement, error) {
	var out *Announcement
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.annGetTx(ctx, tx, id)
		return err
	})
	return out, err
}

// staffPropertyScope: property tempat staf punya grant apa pun; all=true bila ada grant level organization.
func staffPropertyScope(p *authctx.Principal) (ids []uuid.UUID, all bool) {
	for _, g := range p.Grants {
		if g.PropertyID == nil {
			return nil, true
		}
		ids = append(ids, *g.PropertyID)
	}
	return ids, false
}

// staffAnnWhere: published, audience staff|all, belum kedaluwarsa, property global atau dalam scope staf.
func staffAnnWhere(p *authctx.Principal) (string, []any, error) {
	if p.IsTenant {
		return "", nil, apperr.Forbidden("Pengumuman staf hanya untuk akun staf")
	}
	where := ` WHERE a.status = 'published' AND a.audience IN ('staff','all') AND (a.expires_at IS NULL OR a.expires_at > now())`
	var args []any
	if pids, all := staffPropertyScope(p); !all {
		args = append(args, pids)
		where += fmt.Sprintf(" AND (a.property_id IS NULL OR a.property_id = ANY($%d))", len(args))
	}
	return where, args, nil
}

// StaffAnnouncements: menu News di Staff App — baca saja, tanpa permission Tenant Relation.
func (s *Service) StaffAnnouncements(ctx context.Context, page httpx.Page) ([]Announcement, *string, error) {
	p := authctx.Must(ctx)
	where, args, err := staffAnnWhere(p)
	if err != nil {
		return nil, nil, err
	}
	var out []Announcement
	var next *string
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (a.created_at, a.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, annSelect+where+fmt.Sprintf(" ORDER BY a.created_at DESC, a.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanAnn(rows)
			if err != nil {
				return err
			}
			a.AllowedActions = []string{"view"}
			out = append(out, *a)
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
		out = []Announcement{}
	}
	return out, next, err
}

func (s *Service) StaffAnnouncement(ctx context.Context, id uuid.UUID) (*Announcement, error) {
	p := authctx.Must(ctx)
	where, args, err := staffAnnWhere(p)
	if err != nil {
		return nil, err
	}
	args = append(args, id)
	var out *Announcement
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := scanAnn(tx.QueryRow(ctx, annSelect+where+fmt.Sprintf(" AND a.id = $%d", len(args)), args...))
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Announcement")
			}
			return err
		}
		a.AllowedActions = []string{"view"}
		out = a
		return nil
	})
	return out, err
}

func (s *Service) CreateAnnouncement(ctx context.Context, in AnnouncementInput) (*Announcement, error) {
	p := authctx.Must(ctx)
	if in.PropertyID != nil {
		if err := iam.CanOnProperty(ctx, "tenant_relation.announcements.create", *in.PropertyID); err != nil {
			return nil, err
		}
	} else if !p.Has("platform.organizations.update") {
		return nil, apperr.Forbidden("Announcement lintas property memerlukan platform.organizations.update")
	}
	title := strings.TrimSpace(deref(in.Title))
	body := strings.TrimSpace(deref(in.Body))
	if title == "" || body == "" {
		return nil, apperr.Validation("title dan body wajib")
	}
	aud, imp := deref(in.Audience), deref(in.Importance)
	if aud == "" {
		aud = "tenant"
	}
	if imp == "" {
		imp = "normal"
	}
	if aud != "tenant" && aud != "staff" && aud != "all" {
		return nil, apperr.Validation("audience harus tenant|staff|all")
	}
	if imp != "normal" && imp != "important" {
		return nil, apperr.Validation("importance harus normal|important")
	}
	var out *Announcement
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO announcements (organization_id, property_id, title, excerpt, body, audience, importance, image_attachment_id, expires_at, created_by, updated_by) VALUES ($1,$2,$3,NULLIF(TRIM($4),''),$5,$6,$7,$8,$9,$10,$10) RETURNING id`,
			p.OrganizationID, in.PropertyID, title, deref(in.Excerpt), body, aud, imp, in.ImageAttachmentID, in.ExpiresAt, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "announcement", EntityID: &id, EntityLabel: title})
		var err error
		out, err = s.annGetTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateAnnouncement(ctx context.Context, id uuid.UUID, in AnnouncementInput, ifVersion *int) (*Announcement, error) {
	p := authctx.Must(ctx)
	var out *Announcement
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := s.annGetTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.PropertyID != nil {
			if err := iam.CanOnProperty(ctx, "tenant_relation.announcements.update", *a.PropertyID); err != nil {
				return err
			}
		}
		if ifVersion != nil && *ifVersion != a.Version {
			return apperr.StaleVersion()
		}
		if a.Status == "archived" {
			return apperr.Conflict("OBJECT_TERMINAL", "Announcement sudah diarsipkan")
		}
		if _, err := tx.Exec(ctx, `UPDATE announcements SET title = COALESCE(NULLIF(TRIM($2),''), title), excerpt = COALESCE($3, excerpt), body = COALESCE(NULLIF(TRIM($4),''), body), audience = COALESCE($5, audience), importance = COALESCE($6, importance), image_attachment_id = COALESCE($7, image_attachment_id), expires_at = COALESCE($8, expires_at), updated_by = $9 WHERE id = $1`,
			id, deref(in.Title), in.Excerpt, deref(in.Body), in.Audience, in.Importance, in.ImageAttachmentID, in.ExpiresAt, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "announcement", EntityID: &id, EntityLabel: a.Title, After: in})
		out, err = s.annGetTx(ctx, tx, id)
		return err
	})
	return out, err
}

// TransitionAnnouncement: publish | archive. Publish → event announcement.published → notifikasi tenant property (resolver tenant_property_users).
func (s *Service) TransitionAnnouncement(ctx context.Context, id uuid.UUID, action string) (*Announcement, error) {
	p := authctx.Must(ctx)
	var out *Announcement
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := s.annGetTx(ctx, tx, id)
		if err != nil {
			return err
		}
		allowed := false
		for _, x := range a.AllowedActions {
			if x == action {
				allowed = true
			}
		}
		if !allowed {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk announcement berstatus %s", action, a.Status))
		}
		switch action {
		case "publish":
			if _, err := tx.Exec(ctx, `UPDATE announcements SET status = 'published', published_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
			if s.Jobs != nil && (a.Audience == "tenant" || a.Audience == "all") {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventAnnouncementPublished, OrganizationID: p.OrganizationID, PropertyID: a.PropertyID, ObjectType: "announcement", ObjectID: id, ObjectLabel: a.Title, ActorUserID: &p.UserID, Payload: map[string]any{"importance": a.Importance}})
			}
		case "archive":
			if _, err := tx.Exec(ctx, `UPDATE announcements SET status = 'archived', updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "announcement", EntityID: &id, EntityLabel: a.Title, Before: map[string]any{"status": a.Status}, After: map[string]any{"action": action}})
		out, err = s.annGetTx(ctx, tx, id)
		return err
	})
	return out, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
