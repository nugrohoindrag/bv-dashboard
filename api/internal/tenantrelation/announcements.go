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

const (
	EventAnnouncementPublished = "announcement.published"
	// EventAnnouncementBroadcast: broadcast darurat/operasional ke tenant (PRD P3 v2.1 P3-BRC-01) — rule severity tinggi.
	EventAnnouncementBroadcast = "announcement.broadcast"
)

// Announcement: komunikasi Tenant Relation → tenant/staf (PRD §3.5 Communication; NC §71 Tenant Relation › Communication).
// PRD P3 v2.1 §5.10: kategori (announcement | news | alert), target building/tower/floor/unit/tenant, jadwal publish,
// kedaluwarsa, gambar, pelacakan baca & konfirmasi (requires_ack).
type Announcement struct {
	ID                uuid.UUID     `json:"id"`
	PropertyID        *uuid.UUID    `json:"property_id"`
	PropertyName      *string       `json:"property_name"`
	Title             string        `json:"title"`
	Excerpt           *string       `json:"excerpt"`
	Body              string        `json:"body"`
	Audience          string        `json:"audience"`
	Importance        string        `json:"importance"`
	Category          string        `json:"category"`
	Severity          string        `json:"severity"`
	ImageAttachmentID *uuid.UUID    `json:"image_attachment_id"`
	Status            string        `json:"status"`
	PublishAt         *time.Time    `json:"publish_at"`
	PublishedAt       *time.Time    `json:"published_at"`
	ExpiresAt         *time.Time    `json:"expires_at"`
	RequiresAck       bool          `json:"requires_ack"`
	TargetLocationIDs []uuid.UUID   `json:"target_location_ids"`
	TargetTenantIDs   []uuid.UUID   `json:"target_tenant_ids"`
	Targets           []TargetLabel `json:"targets"`
	RecipientsCount   *int          `json:"recipients_count"`
	ReadCount         int           `json:"read_count"`
	AckCount          int           `json:"ack_count"`
	CreatedAt         time.Time     `json:"created_at"`
	CreatedByName     *string       `json:"created_by_name"`
	Version           int           `json:"version"`
	AllowedActions    []string      `json:"allowed_actions"`
}

type TargetLabel struct {
	Kind  string    `json:"kind"` // location | tenant
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label"`
	Type  string    `json:"type,omitempty"` // tipe lokasi
}

const annSelect = `SELECT a.id, a.property_id, pl.name, a.title, a.excerpt, a.body, a.audience, a.importance, a.category, a.severity, a.image_attachment_id, a.status, a.publish_at, a.published_at, a.expires_at,
	a.requires_ack, a.target_location_ids, a.target_tenant_ids, a.recipients_count,
	(SELECT count(*) FROM announcement_reads r WHERE r.announcement_id = a.id), (SELECT count(*) FROM announcement_reads r WHERE r.announcement_id = a.id AND r.acknowledged_at IS NOT NULL),
	a.created_at, u.full_name, a.version
	FROM announcements a LEFT JOIN locations pl ON pl.id = a.property_id LEFT JOIN users u ON u.id = a.created_by`

func scanAnn(row pgx.Row) (*Announcement, error) {
	var a Announcement
	if err := row.Scan(&a.ID, &a.PropertyID, &a.PropertyName, &a.Title, &a.Excerpt, &a.Body, &a.Audience, &a.Importance, &a.Category, &a.Severity, &a.ImageAttachmentID, &a.Status, &a.PublishAt, &a.PublishedAt, &a.ExpiresAt,
		&a.RequiresAck, &a.TargetLocationIDs, &a.TargetTenantIDs, &a.RecipientsCount, &a.ReadCount, &a.AckCount, &a.CreatedAt, &a.CreatedByName, &a.Version); err != nil {
		return nil, err
	}
	if a.TargetLocationIDs == nil {
		a.TargetLocationIDs = []uuid.UUID{}
	}
	if a.TargetTenantIDs == nil {
		a.TargetTenantIDs = []uuid.UUID{}
	}
	a.Targets = []TargetLabel{}
	return &a, nil
}

func (s *Service) loadTargets(ctx context.Context, tx pgx.Tx, a *Announcement) {
	if len(a.TargetLocationIDs) > 0 {
		rows, err := tx.Query(ctx, `SELECT l.id, l.location_type, COALESCE((SELECT string_agg(x.name, ' · ' ORDER BY x.depth) FROM locations x WHERE x.path @> l.path AND x.depth > 0), l.name)
			FROM locations l WHERE l.id = ANY($1)`, a.TargetLocationIDs)
		if err == nil {
			for rows.Next() {
				var t TargetLabel
				if rows.Scan(&t.ID, &t.Type, &t.Label) == nil {
					t.Kind = "location"
					a.Targets = append(a.Targets, t)
				}
			}
			rows.Close()
		}
	}
	if len(a.TargetTenantIDs) > 0 {
		rows, err := tx.Query(ctx, `SELECT id, name FROM tenants WHERE id = ANY($1)`, a.TargetTenantIDs)
		if err == nil {
			for rows.Next() {
				var t TargetLabel
				if rows.Scan(&t.ID, &t.Label) == nil {
					t.Kind = "tenant"
					a.Targets = append(a.Targets, t)
				}
			}
			rows.Close()
		}
	}
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
	if (a.Status == "draft" || a.Status == "scheduled" || a.Status == "published") && can("tenant_relation.announcements.update") {
		a.AllowedActions = append(a.AllowedActions, "update")
	}
	if can("tenant_relation.announcements.publish") {
		switch a.Status {
		case "draft":
			a.AllowedActions = append(a.AllowedActions, "publish", "schedule", "archive")
		case "scheduled":
			a.AllowedActions = append(a.AllowedActions, "publish", "unschedule", "archive")
		case "published":
			a.AllowedActions = append(a.AllowedActions, "archive")
		}
	}
	if a.Status == "published" && can("tenant_relation.announcements.view") {
		a.AllowedActions = append(a.AllowedActions, "view_reads")
	}
}

type AnnouncementInput struct {
	PropertyID        *uuid.UUID   `json:"property_id"`
	Title             *string      `json:"title"`
	Excerpt           *string      `json:"excerpt"`
	Body              *string      `json:"body"`
	Audience          *string      `json:"audience"`
	Importance        *string      `json:"importance"`
	Category          *string      `json:"category"`
	Severity          *string      `json:"severity"`
	ImageAttachmentID *uuid.UUID   `json:"image_attachment_id"` // UUID nol = hapus gambar
	PublishAt         *string      `json:"publish_at"`          // RFC3339; "" = kosongkan
	ExpiresAt         *string      `json:"expires_at"`          // RFC3339; "" = kosongkan
	RequiresAck       *bool        `json:"requires_ack"`
	TargetLocationIDs *[]uuid.UUID `json:"target_location_ids"`
	TargetTenantIDs   *[]uuid.UUID `json:"target_tenant_ids"`
}

var annCategories = map[string]bool{"announcement": true, "news": true, "alert": true}
var annSeverities = map[string]bool{"info": true, "warning": true, "critical": true}

// parseOptTime: nil = tidak berubah; "" = kosongkan (clear=true); lainnya RFC3339.
func parseOptTime(v *string, field string) (t *time.Time, clear bool, err error) {
	if v == nil {
		return nil, false, nil
	}
	if strings.TrimSpace(*v) == "" {
		return nil, true, nil
	}
	x, perr := time.Parse(time.RFC3339, *v)
	if perr != nil {
		return nil, false, apperr.Validation(field+" harus RFC3339").WithField(field, "format tidak valid")
	}
	return &x, false, nil
}

// validateTargets: lokasi target harus di property pengumuman (building/tower/floor/unit/area); tenant target di property yang sama.
func validateTargets(ctx context.Context, tx pgx.Tx, propertyID *uuid.UUID, locs, tenants []uuid.UUID) error {
	if (len(locs) > 0 || len(tenants) > 0) && propertyID == nil {
		return apperr.Validation("target building/unit/tenant memerlukan property_id").WithField("property_id", "wajib untuk pengumuman bertarget")
	}
	if len(locs) > 0 {
		var n int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE id = ANY($1) AND property_id = $2 AND deleted_at IS NULL AND location_type <> 'property'`, locs, *propertyID).Scan(&n)
		if n != len(uniq(locs)) {
			return apperr.Validation("target_location_ids harus lokasi di property ini").WithField("target_location_ids", "tidak valid")
		}
	}
	if len(tenants) > 0 {
		var n int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE id = ANY($1) AND property_id = $2 AND deleted_at IS NULL`, tenants, *propertyID).Scan(&n)
		if n != len(uniq(tenants)) {
			return apperr.Validation("target_tenant_ids harus tenant di property ini").WithField("target_tenant_ids", "tidak valid")
		}
	}
	return nil
}

func uniq(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) ListAnnouncements(ctx context.Context, propertyID *uuid.UUID, status []string, page httpx.Page) ([]Announcement, *string, error) {
	return s.ListAnnouncementsFiltered(ctx, propertyID, status, "", page)
}

func (s *Service) ListAnnouncementsFiltered(ctx context.Context, propertyID *uuid.UUID, status []string, category string, page httpx.Page) ([]Announcement, *string, error) {
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
		if category != "" {
			args = append(args, category)
			where += fmt.Sprintf(" AND a.category = $%d", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (a.created_at, a.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, annSelect+where+fmt.Sprintf(" ORDER BY a.created_at DESC, a.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []Announcement
		for rows.Next() {
			a, err := scanAnn(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, *a)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			s.loadTargets(ctx, tx, &items[i])
			s.annFinalize(ctx, &items[i])
		}
		out = items
		return nil
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
	s.loadTargets(ctx, tx, a)
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
	aud, imp, cat, sev := deref(in.Audience), deref(in.Importance), deref(in.Category), deref(in.Severity)
	if aud == "" {
		aud = "tenant"
	}
	if imp == "" {
		imp = "normal"
	}
	if cat == "" {
		cat = "announcement"
	}
	if sev == "" {
		sev = "info"
	}
	if aud != "tenant" && aud != "staff" && aud != "all" {
		return nil, apperr.Validation("audience harus tenant|staff|all")
	}
	if imp != "normal" && imp != "important" {
		return nil, apperr.Validation("importance harus normal|important")
	}
	if !annCategories[cat] || !annSeverities[sev] {
		return nil, apperr.Validation("category harus announcement|news|alert; severity info|warning|critical")
	}
	publishAt, _, err := parseOptTime(in.PublishAt, "publish_at")
	if err != nil {
		return nil, err
	}
	expiresAt, _, err := parseOptTime(in.ExpiresAt, "expires_at")
	if err != nil {
		return nil, err
	}
	if publishAt != nil && expiresAt != nil && !expiresAt.After(*publishAt) {
		return nil, apperr.Validation("expires_at harus setelah publish_at").WithField("expires_at", "harus setelah jadwal publish")
	}
	var locs, tens []uuid.UUID
	if in.TargetLocationIDs != nil {
		locs = uniq(*in.TargetLocationIDs)
	}
	if in.TargetTenantIDs != nil {
		tens = uniq(*in.TargetTenantIDs)
	}
	if locs == nil {
		locs = []uuid.UUID{}
	}
	if tens == nil {
		tens = []uuid.UUID{}
	}
	ack := in.RequiresAck != nil && *in.RequiresAck
	var out *Announcement
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := validateTargets(ctx, tx, in.PropertyID, locs, tens); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO announcements (organization_id, property_id, title, excerpt, body, audience, importance, category, severity, image_attachment_id, publish_at, expires_at, requires_ack, target_location_ids, target_tenant_ids, created_by, updated_by)
			VALUES ($1,$2,$3,NULLIF(TRIM($4),''),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$16) RETURNING id`,
			p.OrganizationID, in.PropertyID, title, deref(in.Excerpt), body, aud, imp, cat, sev, in.ImageAttachmentID, publishAt, expiresAt, ack, locs, tens, p.UserID).Scan(&id); err != nil {
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
		if in.Category != nil && !annCategories[*in.Category] {
			return apperr.Validation("category harus announcement|news|alert")
		}
		if in.Severity != nil && !annSeverities[*in.Severity] {
			return apperr.Validation("severity harus info|warning|critical")
		}
		publishAt, clearPublish, err := parseOptTime(in.PublishAt, "publish_at")
		if err != nil {
			return err
		}
		expiresAt, clearExpires, err := parseOptTime(in.ExpiresAt, "expires_at")
		if err != nil {
			return err
		}
		locs, tens := a.TargetLocationIDs, a.TargetTenantIDs
		if in.TargetLocationIDs != nil {
			locs = uniq(*in.TargetLocationIDs)
		}
		if in.TargetTenantIDs != nil {
			tens = uniq(*in.TargetTenantIDs)
		}
		if a.Status == "published" && (in.TargetLocationIDs != nil || in.TargetTenantIDs != nil) {
			return apperr.Conflict("ANNOUNCEMENT_PUBLISHED", "Target pengumuman yang sudah terbit tidak dapat diubah; buat pengumuman baru")
		}
		if err := validateTargets(ctx, tx, a.PropertyID, locs, tens); err != nil {
			return err
		}
		img := in.ImageAttachmentID
		clearImg := img != nil && *img == uuid.Nil
		if clearImg {
			img = nil
		}
		if _, err := tx.Exec(ctx, `UPDATE announcements SET title = COALESCE(NULLIF(TRIM($2),''), title), excerpt = COALESCE($3, excerpt), body = COALESCE(NULLIF(TRIM($4),''), body), audience = COALESCE($5, audience),
			importance = COALESCE($6, importance), category = COALESCE($7, category), severity = COALESCE($8, severity),
			image_attachment_id = CASE WHEN $9 THEN NULL ELSE COALESCE($10, image_attachment_id) END,
			publish_at = CASE WHEN $11 THEN NULL ELSE COALESCE($12, publish_at) END, expires_at = CASE WHEN $13 THEN NULL ELSE COALESCE($14, expires_at) END,
			requires_ack = COALESCE($15, requires_ack), target_location_ids = $16, target_tenant_ids = $17, updated_by = $18 WHERE id = $1`,
			id, deref(in.Title), in.Excerpt, deref(in.Body), in.Audience, in.Importance, in.Category, in.Severity, clearImg, img, clearPublish, publishAt, clearExpires, expiresAt, in.RequiresAck, locs, tens, p.UserID); err != nil {
			return err
		}
		// jadwal berubah saat status scheduled → ikut jadwal baru; dikosongkan → kembali draft
		if a.Status == "scheduled" && clearPublish {
			_, _ = tx.Exec(ctx, `UPDATE announcements SET status = 'draft' WHERE id = $1`, id)
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "announcement", EntityID: &id, EntityLabel: a.Title, After: in})
		out, err = s.annGetTx(ctx, tx, id)
		return err
	})
	return out, err
}

// publishTx: terbitkan sekarang — hitung audiens (tenant bertarget), catat recipients_count, pancarkan event.
func (s *Service) publishTx(ctx context.Context, tx pgx.Tx, a *Announcement, actor *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE announcements SET status = 'published', published_at = now(), publish_at = COALESCE(publish_at, now()), updated_by = $2 WHERE id = $1`, a.ID, actor); err != nil {
		return err
	}
	var n int
	if a.Audience == "tenant" || a.Audience == "all" {
		_ = tx.QueryRow(ctx, `SELECT count(DISTINCT tu.user_id) FROM announcements a JOIN tenant_users tu ON tu.status = 'active' AND (a.property_id IS NULL OR tu.property_id = a.property_id)
			WHERE a.id = $1 AND ((cardinality(a.target_location_ids) = 0 AND cardinality(a.target_tenant_ids) = 0)
			  OR (tu.tenant_id IS NOT NULL AND tu.tenant_id = ANY(a.target_tenant_ids))
			  OR EXISTS (SELECT 1 FROM tenant_access ta JOIN locations l ON l.id = ta.location_id JOIN locations t ON t.id = ANY(a.target_location_ids)
			             WHERE ta.tenant_user_id = tu.id AND ta.status = 'active' AND t.path @> l.path))`, a.ID).Scan(&n)
	}
	_, _ = tx.Exec(ctx, `UPDATE announcements SET recipients_count = $2 WHERE id = $1`, a.ID, n)
	if s.Jobs != nil && (a.Audience == "tenant" || a.Audience == "all") {
		p := authctx.Must(ctx)
		ev := EventAnnouncementPublished
		if a.Category == "alert" {
			ev = EventAnnouncementBroadcast
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: ev, OrganizationID: p.OrganizationID, PropertyID: a.PropertyID, ObjectType: "announcement", ObjectID: a.ID, ObjectLabel: a.Title, ActorUserID: actor,
			Payload: map[string]any{"importance": a.Importance, "category": a.Category, "severity": a.Severity, "domain": "tenant_relation"}})
	}
	return nil
}

// TransitionAnnouncement: publish | schedule | unschedule | archive. Publish → event announcement.published (alert:
// announcement.broadcast) → notifikasi tenant sasaran (resolver tenant_property_users + target).
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
			if err := s.publishTx(ctx, tx, a, &p.UserID); err != nil {
				return err
			}
		case "schedule":
			if a.PublishAt == nil || !a.PublishAt.After(time.Now()) {
				return apperr.Validation("Jadwal publish (publish_at) harus diisi dan di masa depan").WithField("publish_at", "wajib di masa depan")
			}
			if _, err := tx.Exec(ctx, `UPDATE announcements SET status = 'scheduled', updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
		case "unschedule":
			if _, err := tx.Exec(ctx, `UPDATE announcements SET status = 'draft', updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
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

// BroadcastInput: broadcast darurat/operasional ke tenant (mis. pemadaman listrik/air) — langsung terbit, in-app + push.
type BroadcastInput struct {
	PropertyID        uuid.UUID   `json:"property_id"`
	Title             string      `json:"title"`
	Body              string      `json:"body"`
	Severity          string      `json:"severity"` // info | warning | critical
	RequiresAck       bool        `json:"requires_ack"`
	ExpiresAt         *time.Time  `json:"expires_at"`
	TargetLocationIDs []uuid.UUID `json:"target_location_ids"`
	TargetTenantIDs   []uuid.UUID `json:"target_tenant_ids"`
}

func (s *Service) Broadcast(ctx context.Context, in BroadcastInput) (*Announcement, error) {
	p := authctx.Must(ctx)
	if err := iam.CanOnProperty(ctx, "tenant_relation.announcements.broadcast", in.PropertyID); err != nil {
		return nil, err
	}
	in.Title, in.Body = strings.TrimSpace(in.Title), strings.TrimSpace(in.Body)
	if in.Title == "" || in.Body == "" {
		return nil, apperr.Validation("title dan body wajib")
	}
	if in.Severity == "" {
		in.Severity = "warning"
	}
	if !annSeverities[in.Severity] {
		return nil, apperr.Validation("severity harus info|warning|critical")
	}
	locs, tens := uniq(in.TargetLocationIDs), uniq(in.TargetTenantIDs)
	var out *Announcement
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := validateTargets(ctx, tx, &in.PropertyID, locs, tens); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO announcements (organization_id, property_id, title, body, audience, importance, category, severity, expires_at, requires_ack, target_location_ids, target_tenant_ids, created_by, updated_by)
			VALUES ($1,$2,$3,$4,'tenant','important','alert',$5,$6,$7,$8,$9,$10,$10) RETURNING id`,
			p.OrganizationID, in.PropertyID, in.Title, in.Body, in.Severity, in.ExpiresAt, in.RequiresAck, locs, tens, p.UserID).Scan(&id); err != nil {
			return err
		}
		a, err := s.annGetTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.publishTx(ctx, tx, a, &p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditBroadcast, EntityType: "announcement", EntityID: &id, EntityLabel: in.Title,
			After: map[string]any{"severity": in.Severity, "targets": len(locs) + len(tens)}})
		out, err = s.annGetTx(ctx, tx, id)
		return err
	})
	return out, err
}

// AnnouncementReader: status baca penerima pengumuman (P3-ANN-05).
type AnnouncementReader struct {
	UserID         uuid.UUID  `json:"user_id"`
	FullName       string     `json:"full_name"`
	TenantName     *string    `json:"tenant_name"`
	UnitLabel      *string    `json:"unit_label"`
	ReadAt         *time.Time `json:"read_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
}

type AnnouncementReads struct {
	Recipients   int                  `json:"recipients"`
	Read         int                  `json:"read"`
	Acknowledged int                  `json:"acknowledged"`
	Items        []AnnouncementReader `json:"items"`
}

func (s *Service) AnnouncementReads(ctx context.Context, id uuid.UUID, unreadOnly bool) (*AnnouncementReads, error) {
	out := &AnnouncementReads{Items: []AnnouncementReader{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := s.annGetTx(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `WITH aud AS (
			  SELECT DISTINCT tu.user_id, tu.id AS tu_id, tu.tenant_id FROM announcements a JOIN tenant_users tu ON tu.status = 'active' AND (a.property_id IS NULL OR tu.property_id = a.property_id)
			  WHERE a.id = $1 AND a.audience IN ('tenant','all') AND ((cardinality(a.target_location_ids) = 0 AND cardinality(a.target_tenant_ids) = 0)
			    OR (tu.tenant_id IS NOT NULL AND tu.tenant_id = ANY(a.target_tenant_ids))
			    OR EXISTS (SELECT 1 FROM tenant_access ta JOIN locations l ON l.id = ta.location_id JOIN locations t ON t.id = ANY(a.target_location_ids)
			               WHERE ta.tenant_user_id = tu.id AND ta.status = 'active' AND t.path @> l.path))
			  UNION SELECT r.user_id, NULL, NULL FROM announcement_reads r WHERE r.announcement_id = $1)
			SELECT aud.user_id, u.full_name, t.name,
			  (SELECT COALESCE('Unit ' || un.unit_number, l.name) FROM tenant_access ta JOIN locations l ON l.id = ta.location_id LEFT JOIN units un ON un.location_id = l.id
			   WHERE ta.tenant_user_id = aud.tu_id AND ta.status = 'active' ORDER BY ta.is_primary DESC LIMIT 1),
			  r.read_at, r.acknowledged_at
			FROM aud JOIN users u ON u.id = aud.user_id LEFT JOIN tenants t ON t.id = aud.tenant_id
			LEFT JOIN announcement_reads r ON r.announcement_id = $1 AND r.user_id = aud.user_id
			ORDER BY r.read_at NULLS FIRST, u.full_name LIMIT 1000`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[uuid.UUID]bool{}
		for rows.Next() {
			var it AnnouncementReader
			if err := rows.Scan(&it.UserID, &it.FullName, &it.TenantName, &it.UnitLabel, &it.ReadAt, &it.AcknowledgedAt); err != nil {
				return err
			}
			if seen[it.UserID] {
				continue
			}
			seen[it.UserID] = true
			out.Recipients++
			if it.ReadAt != nil {
				out.Read++
			}
			if it.AcknowledgedAt != nil {
				out.Acknowledged++
			}
			if unreadOnly && it.ReadAt != nil {
				continue
			}
			out.Items = append(out.Items, it)
		}
		return rows.Err()
	})
	return out, err
}

// PublishScheduledSweep (worker, 1 menit): pengumuman terjadwal yang jatuh tempo → terbit (P3-ANN-03).
func (s *Service) PublishScheduledSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM announcements WHERE status = 'scheduled' AND publish_at <= now() ORDER BY publish_at LIMIT 100 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			a, err := scanAnn(tx.QueryRow(ctx, annSelect+` WHERE a.id = $1`, id))
			if err != nil {
				continue
			}
			if err := s.publishTx(ctx, tx, a, nil); err != nil {
				return err
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "announcement", EntityID: &id, EntityLabel: a.Title, Before: map[string]any{"status": "scheduled"}, After: map[string]any{"action": "publish", "by": "schedule"}})
			n++
		}
		return nil
	})
	return n, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
