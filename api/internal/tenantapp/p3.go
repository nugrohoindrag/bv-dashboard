package tenantapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

// AccountCreator: pembuatan akun anggota oleh Tenant Admin (diimplementasikan tenantrelation; diinjeksi saat wiring agar
// tidak terjadi import siklik tenantrelation → tenantapp).
type AccountCreator interface {
	CreateMemberTx(ctx context.Context, tx pgx.Tx, in MemberAccount) (uuid.UUID, string, error)
}

// MemberAccount: data akun anggota baru (P3-ACC-08).
type MemberAccount struct {
	PropertyID uuid.UUID
	TenantID   uuid.UUID
	FullName   string
	Email      string
	Phone      string
	UnitIDs    []uuid.UUID
}

// ---------- §5.2 My Unit (P3-UNT-01..04) ----------

type NamedRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Code *string   `json:"code,omitempty"`
}

type UnitPerson struct {
	Name     string  `json:"name"`
	Role     string  `json:"role"` // occupant | tenant_user | tenant_admin
	Relation *string `json:"relation,omitempty"`
	IsSelf   bool    `json:"is_self"`
}

type UnitCounts struct {
	OpenRequests      int   `json:"open_requests"`
	UnpaidInvoices    int   `json:"unpaid_invoices"`
	OutstandingAmount int64 `json:"outstanding_amount"`
	UpcomingBookings  int   `json:"upcoming_bookings"`
	UpcomingVisitors  int   `json:"upcoming_visitors"`
	PackagesWaiting   int   `json:"packages_waiting"`
	ActivePermits     int   `json:"active_parking_permits"`
}

type UnitSummary struct {
	Unit            AccessLoc    `json:"unit"`
	UnitType        *string      `json:"unit_type"`
	AreaM2          *float64     `json:"area_m2"`
	OccupancyStatus *string      `json:"occupancy_status"`
	OwnershipStatus *string      `json:"ownership_status"`
	Tenant          *NamedRef    `json:"tenant"`
	People          []UnitPerson `json:"people"`
	OtherAccess     []AccessLoc  `json:"other_access"`
	Counts          UnitCounts   `json:"counts"`
}

func (s *Service) unitSummaryTx(ctx context.Context, tx pgx.Tx, sc *Scope, u AccessLoc) (*UnitSummary, error) {
	out := &UnitSummary{Unit: u, People: []UnitPerson{}, OtherAccess: sc.Areas}
	// ownership_status tercatat per akun (unit pendaftaran = akses utama); unit tambahan tidak mewarisinya
	if u.IsPrimary {
		out.OwnershipStatus = sc.OwnershipStatus
	}
	var tenantID *uuid.UUID
	var tenantName, tenantCode *string
	_ = tx.QueryRow(ctx, `SELECT un.unit_type, un.area_m2::float8, un.occupancy_status, un.tenant_id, t.name, t.tenant_code FROM units un LEFT JOIN tenants t ON t.id = un.tenant_id WHERE un.location_id = $1`, u.ID).
		Scan(&out.UnitType, &out.AreaM2, &out.OccupancyStatus, &tenantID, &tenantName, &tenantCode)
	if tenantID != nil && tenantName != nil {
		out.Tenant = &NamedRef{ID: *tenantID, Name: *tenantName, Code: tenantCode}
	}
	// penghuni terdaftar (unit_occupants) + pemegang akun Tenant App dengan akses unit ini
	rows, err := tx.Query(ctx, `SELECT o.full_name, 'occupant', CASE WHEN o.is_primary_contact THEN 'Kontak utama' END, false FROM unit_occupants uo JOIN occupants o ON o.id = uo.occupant_id
		WHERE uo.unit_location_id = $1 AND o.deleted_at IS NULL AND o.status = 'active' AND (uo.moved_out_at IS NULL OR uo.moved_out_at >= current_date)
		UNION ALL
		SELECT us.full_name, tu.role, tu.ownership_status, tu.user_id = $2 FROM tenant_access ta JOIN tenant_users tu ON tu.id = ta.tenant_user_id JOIN users us ON us.id = tu.user_id
		WHERE ta.location_id = $1 AND ta.status = 'active' AND tu.status = 'active'`, u.ID, sc.UserID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for rows.Next() {
		var p UnitPerson
		if err := rows.Scan(&p.Name, &p.Role, &p.Relation, &p.IsSelf); err != nil {
			rows.Close()
			return nil, err
		}
		key := strings.ToLower(p.Name)
		if seen[key] && p.Role == "occupant" {
			continue
		}
		seen[key] = true
		out.People = append(out.People, p)
	}
	rows.Close()
	c := &out.Counts
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM service_requests sr WHERE sr.location_id = $1 AND sr.status NOT IN ('closed','cancelled')
		AND (sr.tenant_user_id = $2 OR ($3 = 'tenant_admin' AND $4::uuid IS NOT NULL AND sr.tenant_id = $4))`, u.ID, sc.UserID, sc.Role, sc.TenantID).Scan(&c.OpenRequests)
	_ = tx.QueryRow(ctx, `SELECT count(*), COALESCE(sum(i.total_amount - i.paid_amount - i.credited_amount),0) FROM invoices i
		WHERE i.unit_location_id = $1 AND i.status IN ('issued','partially_paid','overdue') AND (($2::uuid IS NOT NULL AND i.tenant_id = $2) OR (i.tenant_id IS NULL AND i.source NOT IN ('rental','unit_sale','hotel')))`,
		u.ID, sc.TenantID).Scan(&c.UnpaidInvoices, &c.OutstandingAmount)
	// booking per unit: pesanan seluruh pemegang akses aktif unit ini (booking tidak menyimpan unit)
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM bookings b WHERE b.starts_at > now() AND b.status IN ('pending','confirmed')
		AND b.tenant_user_id IN (SELECT tu.user_id FROM tenant_access ta JOIN tenant_users tu ON tu.id = ta.tenant_user_id
			WHERE ta.location_id = $1 AND ta.access_type = 'unit' AND ta.status = 'active' AND tu.status = 'active')`, u.ID).Scan(&c.UpcomingBookings)
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM visitors v WHERE v.host_unit_location_id = $1 AND v.expected_at > now() - interval '2 hours' AND v.status IN ('pending_approval','registered')`, u.ID).Scan(&c.UpcomingVisitors)
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM packages pk WHERE pk.status IN ('received','notified') AND (pk.unit_location_id = $1 OR pk.recipient_user_id = $2)`, u.ID, sc.UserID).Scan(&c.PackagesWaiting)
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM parking_permits pp WHERE pp.unit_location_id = $1 AND pp.status = 'approved' AND (pp.valid_until IS NULL OR pp.valid_until >= current_date)`, u.ID).Scan(&c.ActivePermits)
	return out, nil
}

// MyUnits: seluruh unit yang dapat diakses (unit utama lebih dulu) beserta ringkasannya — pengganti unit aktif di PWA (P3-UNT-04).
func (s *Service) MyUnits(ctx context.Context) ([]UnitSummary, error) {
	out := []UnitSummary{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		for _, u := range sc.Units {
			us, err := s.unitSummaryTx(ctx, tx, sc, u)
			if err != nil {
				return err
			}
			out = append(out, *us)
		}
		return nil
	})
	return out, err
}

func (s *Service) MyUnit(ctx context.Context, id uuid.UUID) (*UnitSummary, error) {
	var out *UnitSummary
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		for _, u := range sc.Units {
			if u.ID == id {
				out, err = s.unitSummaryTx(ctx, tx, sc, u)
				return err
			}
		}
		return apperr.NotFound("Unit")
	})
	return out, err
}

// ---------- §5.10 Pengumuman untuk tenant: target, kategori, baca & konfirmasi (P3-ANN-02..06) ----------

type Announcement struct {
	ID             uuid.UUID  `json:"id"`
	Title          string     `json:"title"`
	Excerpt        *string    `json:"excerpt"`
	Body           string     `json:"body"`
	Importance     string     `json:"importance"`
	Category       string     `json:"category"` // announcement | news | alert
	Severity       string     `json:"severity"`
	RequiresAck    bool       `json:"requires_ack"`
	PublishedAt    *time.Time `json:"published_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	ReadAt         *time.Time `json:"read_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
	ImageURL       string     `json:"image_url,omitempty"`
}

// tenantAnnWhere: published, audience tenant/all, property tenant (atau seluruh org), sesuai target lokasi/tenant.
// $1 property, $2 tenant_id, $3 tenant_user_id (tenant_users.id), $4 user_id (baca).
const tenantAnnWhere = ` WHERE a.status = 'published' AND a.audience IN ('tenant','all') AND (a.property_id IS NULL OR a.property_id = $1)
	AND ((cardinality(a.target_location_ids) = 0 AND cardinality(a.target_tenant_ids) = 0)
	  OR ($2::uuid IS NOT NULL AND $2 = ANY(a.target_tenant_ids))
	  OR EXISTS (SELECT 1 FROM tenant_access ta JOIN locations l ON l.id = ta.location_id JOIN locations t ON t.id = ANY(a.target_location_ids)
	             WHERE ta.tenant_user_id = $3 AND ta.status = 'active' AND t.path @> l.path))`

const tenantAnnSelect = `SELECT a.id, a.title, a.excerpt, a.body, a.importance, a.category, a.severity, a.requires_ack, a.published_at, a.expires_at, r.read_at, r.acknowledged_at, a.image_attachment_id
	FROM announcements a LEFT JOIN announcement_reads r ON r.announcement_id = a.id AND r.user_id = $4`

func (s *Service) scanTenantAnn(ctx context.Context, tx pgx.Tx, row pgx.Row) (*Announcement, error) {
	var a Announcement
	var img *uuid.UUID
	if err := row.Scan(&a.ID, &a.Title, &a.Excerpt, &a.Body, &a.Importance, &a.Category, &a.Severity, &a.RequiresAck, &a.PublishedAt, &a.ExpiresAt, &a.ReadAt, &a.AcknowledgedAt, &img); err != nil {
		return nil, err
	}
	if img != nil && s.Attachments != nil {
		if at, err := s.Attachments.GetTx(ctx, tx, *img); err == nil {
			list := []attachments.Attachment{*at}
			s.Attachments.FillURLs(ctx, list)
			a.ImageURL = list[0].URL
		}
	}
	return &a, nil
}

// Announcement: satu pengumuman (deep link Inbox → /inbox/announcements/{id}); membuka = tercatat dibaca (P3-ANN-05).
func (s *Service) Announcement(ctx context.Context, id uuid.UUID) (*Announcement, error) {
	var out *Announcement
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		args := []any{sc.PropertyID, sc.TenantID, sc.TenantUserID, sc.UserID, id}
		a, err := s.scanTenantAnn(ctx, tx, tx.QueryRow(ctx, tenantAnnSelect+tenantAnnWhere+` AND a.id = $5`, args...))
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Announcement")
			}
			return err
		}
		if a.ReadAt == nil {
			now := time.Now().UTC()
			if _, err := tx.Exec(ctx, `INSERT INTO announcement_reads (announcement_id, user_id, organization_id, read_at) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, sc.UserID, sc.OrganizationID, now); err != nil {
				return err
			}
			a.ReadAt = &now
		}
		out = a
		return nil
	})
	return out, err
}

// Announcements: kategori opsional (announcement | news | alert) — News sebagai kategori pengumuman (D-P3-06).
func (s *Service) Announcements(ctx context.Context, category string, page httpx.Page) ([]Announcement, *string, error) {
	var out []Announcement
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		args := []any{sc.PropertyID, sc.TenantID, sc.TenantUserID, sc.UserID}
		where := tenantAnnWhere + ` AND (a.expires_at IS NULL OR a.expires_at > now())`
		if category != "" {
			args = append(args, category)
			where += fmt.Sprintf(" AND a.category = $%d", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (a.published_at, a.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, tenantAnnSelect+where+fmt.Sprintf(" ORDER BY a.published_at DESC, a.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []Announcement
		for rows.Next() {
			var a Announcement
			var img *uuid.UUID
			if err := rows.Scan(&a.ID, &a.Title, &a.Excerpt, &a.Body, &a.Importance, &a.Category, &a.Severity, &a.RequiresAck, &a.PublishedAt, &a.ExpiresAt, &a.ReadAt, &a.AcknowledgedAt, &img); err != nil {
				rows.Close()
				return err
			}
			if img != nil && s.Attachments != nil {
				a.ImageURL = img.String() // diganti URL di bawah (setelah rows ditutup)
			}
			items = append(items, a)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.PublishedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			if items[i].ImageURL == "" {
				continue
			}
			imgID, _ := uuid.Parse(items[i].ImageURL)
			items[i].ImageURL = ""
			if at, err := s.Attachments.GetTx(ctx, tx, imgID); err == nil {
				list := []attachments.Attachment{*at}
				s.Attachments.FillURLs(ctx, list)
				items[i].ImageURL = list[0].URL
			}
		}
		out = items
		return nil
	})
	if out == nil {
		out = []Announcement{}
	}
	return out, next, err
}

// AcknowledgeAnnouncement: tenant menyatakan sudah membaca pengumuman penting (requires_ack).
func (s *Service) AcknowledgeAnnouncement(ctx context.Context, id uuid.UUID) (*Announcement, error) {
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		var ok bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM announcements a`+tenantAnnWhere+` AND a.id = $4)`, sc.PropertyID, sc.TenantID, sc.TenantUserID, id).Scan(&ok)
		if !ok {
			return apperr.NotFound("Announcement")
		}
		_, err = tx.Exec(ctx, `INSERT INTO announcement_reads (announcement_id, user_id, organization_id, read_at, acknowledged_at) VALUES ($1,$2,$3,now(),now())
			ON CONFLICT (announcement_id, user_id) DO UPDATE SET acknowledged_at = COALESCE(announcement_reads.acknowledged_at, now())`, id, sc.UserID, sc.OrganizationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Announcement(ctx, id)
}

// ---------- §5.12 Feedback umum (P3-FDB-02) ----------

var feedbackCategories = map[string]bool{"suggestion": true, "compliment": true, "complaint": true, "question": true, "other": true}

type FeedbackItem struct {
	ID             uuid.UUID  `json:"id"`
	FeedbackNumber string     `json:"feedback_number"`
	Category       string     `json:"category"`
	Subject        *string    `json:"subject"`
	Body           string     `json:"body"`
	IsAnonymous    bool       `json:"is_anonymous"`
	Status         string     `json:"status"`
	Response       *string    `json:"response"`
	RespondedAt    *time.Time `json:"responded_at"`
	CreatedAt      time.Time  `json:"created_at"`
	Photos         []Photo    `json:"photos"`
}

type GeneralFeedbackInput struct {
	Category    string     `json:"category"`
	Subject     string     `json:"subject"`
	Body        string     `json:"body"`
	IsAnonymous bool       `json:"is_anonymous"`
	UnitID      *uuid.UUID `json:"unit_id"`
}

const fbSelect = `SELECT id, feedback_number, category, subject, body, is_anonymous, status, response, responded_at, created_at FROM tenant_feedback`

func scanFB(row pgx.Row) (*FeedbackItem, error) {
	var f FeedbackItem
	if err := row.Scan(&f.ID, &f.FeedbackNumber, &f.Category, &f.Subject, &f.Body, &f.IsAnonymous, &f.Status, &f.Response, &f.RespondedAt, &f.CreatedAt); err != nil {
		return nil, err
	}
	f.Photos = []Photo{}
	return &f, nil
}

func (s *Service) CreateGeneralFeedback(ctx context.Context, in GeneralFeedbackInput) (*FeedbackItem, error) {
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" {
		return nil, apperr.Validation("body wajib").WithField("body", "wajib")
	}
	if len([]rune(in.Body)) > 4000 {
		return nil, apperr.Validation("body maksimal 4000 karakter")
	}
	if in.Category == "" {
		in.Category = "suggestion"
	}
	if !feedbackCategories[in.Category] {
		return nil, apperr.Validation("category harus suggestion|compliment|complaint|question|other").WithField("category", "tidak valid")
	}
	var out *FeedbackItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		unit := sc.PrimaryUnit()
		var unitID *uuid.UUID
		if in.UnitID != nil {
			if !sc.HasUnit(*in.UnitID) {
				return apperr.Validation("unit_id bukan unit Anda")
			}
			unitID = in.UnitID
		} else if unit != nil {
			unitID = &unit.ID
		}
		number, err := ids.NextYearly(ctx, tx, sc.OrganizationID, ids.PrefixFeedback, time.Now(), property.PropertyTimezone(ctx, tx, sc.PropertyID))
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO tenant_feedback (organization_id, property_id, feedback_number, tenant_user_id, tenant_id, unit_location_id, category, subject, body, is_anonymous)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF(TRIM($8),''),$9,$10) RETURNING id`, sc.OrganizationID, sc.PropertyID, number, sc.UserID, sc.TenantID, unitID, in.Category, in.Subject, in.Body, in.IsAnonymous).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "tenant_feedback", EntityID: &id, EntityLabel: number})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "tenant_feedback.submitted", OrganizationID: sc.OrganizationID, PropertyID: &sc.PropertyID, ObjectType: "tenant_feedback", ObjectID: id, ObjectLabel: number, ActorUserID: &sc.UserID,
				Payload: map[string]any{"category": in.Category, "domain": "tenant_relation"}})
		}
		out, err = scanFB(tx.QueryRow(ctx, fbSelect+` WHERE id = $1`, id))
		return err
	})
	return out, err
}

func (s *Service) ListGeneralFeedback(ctx context.Context, page httpx.Page) ([]FeedbackItem, *string, error) {
	var out []FeedbackItem
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		args := []any{sc.UserID}
		where := ` WHERE tenant_user_id = $1`
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += " AND (created_at, id) < ($2::timestamptz, $3)"
		}
		rows, err := tx.Query(ctx, fbSelect+where+fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanFB(rows)
			if err != nil {
				return err
			}
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
		out = []FeedbackItem{}
	}
	return out, next, err
}

func (s *Service) GetGeneralFeedback(ctx context.Context, id uuid.UUID) (*FeedbackItem, error) {
	var out *FeedbackItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		f, err := scanFB(tx.QueryRow(ctx, fbSelect+` WHERE id = $1 AND tenant_user_id = $2`, id, sc.UserID))
		if err != nil {
			return apperr.NotFound("Feedback")
		}
		list, _ := attachments.ListForObjectTx(ctx, tx, "tenant_feedback", id)
		if s.Attachments != nil {
			s.Attachments.FillURLs(ctx, list)
		}
		for _, a := range list {
			f.Photos = append(f.Photos, Photo{ID: a.ID, URL: a.URL, ThumbURL: a.ThumbURL})
		}
		out = f
		return nil
	})
	return out, err
}

// FeedbackAccess: akses lampiran object tenant_feedback — tenant pemilik atau staf Tenant Relation pada property.
func FeedbackAccess(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, write bool) error {
	p := authctx.Must(ctx)
	var owner, propertyID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT tenant_user_id, property_id FROM tenant_feedback WHERE id = $1`, objectID).Scan(&owner, &propertyID); err != nil {
		return apperr.NotFound("Feedback")
	}
	if p.IsTenant {
		if owner != p.UserID {
			return apperr.NotFound("Feedback")
		}
		return nil
	}
	if write || !p.HasOnProperty("tenant_relation.feedback.view", propertyID) {
		return apperr.Forbidden("")
	}
	return nil
}

// ---------- §5.13 Tenant Admin mengelola anggota tenant-nya (P3-ACC-08) ----------

type Member struct {
	TenantUserID uuid.UUID   `json:"tenant_user_id"`
	UserID       uuid.UUID   `json:"user_id"`
	FullName     string      `json:"full_name"`
	Email        *string     `json:"email"`
	Phone        *string     `json:"phone"`
	Role         string      `json:"role"`
	Status       string      `json:"status"`
	IsSelf       bool        `json:"is_self"`
	Units        []AccessLoc `json:"units"`
	LastSeenAt   *time.Time  `json:"last_seen_at"`
	CanManage    bool        `json:"can_manage"`
}

type MemberInput struct {
	FullName string      `json:"full_name"`
	Email    string      `json:"email"`
	Phone    string      `json:"phone"`
	UnitIDs  []uuid.UUID `json:"unit_ids"`
}

type MemberCreated struct {
	Member            *Member `json:"member"`
	TemporaryPassword string  `json:"temporary_password"`
}

func requireTenantAdmin(sc *Scope) error {
	if sc.Role != "tenant_admin" || sc.TenantID == nil {
		return apperr.Forbidden("Hanya Tenant Admin yang dapat mengelola anggota tenant")
	}
	return nil
}

func (s *Service) membersTx(ctx context.Context, tx pgx.Tx, sc *Scope, only *uuid.UUID) ([]Member, error) {
	rows, err := tx.Query(ctx, `SELECT tu.id, tu.user_id, u.full_name, u.email, u.phone, tu.role, tu.status, tu.last_seen_at FROM tenant_users tu JOIN users u ON u.id = tu.user_id
		WHERE tu.tenant_id = $1 AND tu.property_id = $2 AND ($3::uuid IS NULL OR tu.id = $3) ORDER BY tu.role DESC, u.full_name`, *sc.TenantID, sc.PropertyID, only)
	if err != nil {
		return nil, err
	}
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.TenantUserID, &m.UserID, &m.FullName, &m.Email, &m.Phone, &m.Role, &m.Status, &m.LastSeenAt); err != nil {
			rows.Close()
			return nil, err
		}
		m.IsSelf = m.UserID == sc.UserID
		m.CanManage = !m.IsSelf && m.Role == "tenant_user"
		m.Units = []AccessLoc{}
		out = append(out, m)
	}
	rows.Close()
	for i := range out {
		urows, err := tx.Query(ctx, `SELECT ta.id, l.id, l.location_type, l.name, l.code, un.unit_number, ta.is_primary, ta.access_type FROM tenant_access ta JOIN locations l ON l.id = ta.location_id
			LEFT JOIN units un ON un.location_id = l.id WHERE ta.tenant_user_id = $1 AND ta.status = 'active' AND ta.access_type = 'unit' ORDER BY ta.is_primary DESC, l.name`, out[i].TenantUserID)
		if err != nil {
			return nil, err
		}
		for urows.Next() {
			var a AccessLoc
			if err := urows.Scan(&a.AccessID, &a.ID, &a.LocationType, &a.Name, &a.Code, &a.UnitNumber, &a.IsPrimary, &a.AccessType); err != nil {
				urows.Close()
				return nil, err
			}
			out[i].Units = append(out[i].Units, a)
		}
		urows.Close()
	}
	if out == nil {
		out = []Member{}
	}
	return out, nil
}

func (s *Service) Members(ctx context.Context) ([]Member, error) {
	var out []Member
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		if err := requireTenantAdmin(sc); err != nil {
			return err
		}
		out, err = s.membersTx(ctx, tx, sc, nil)
		return err
	})
	return out, err
}

// validateMemberUnits: unit anggota harus subset unit milik Tenant Admin.
func validateMemberUnits(sc *Scope, unitIDs []uuid.UUID) error {
	for _, u := range unitIDs {
		if !sc.HasUnit(u) {
			return apperr.Validation("unit_ids hanya boleh unit yang Anda kelola").WithField("unit_ids", "tidak valid")
		}
	}
	return nil
}

func (s *Service) CreateMember(ctx context.Context, in MemberInput) (*MemberCreated, error) {
	if s.Accounts == nil {
		return nil, apperr.New(503, "NOT_CONFIGURED", "Not configured", "Pembuatan akun anggota belum dikonfigurasi")
	}
	var out *MemberCreated
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		if err := requireTenantAdmin(sc); err != nil {
			return err
		}
		if len(in.UnitIDs) == 0 {
			if pu := sc.PrimaryUnit(); pu != nil {
				in.UnitIDs = []uuid.UUID{pu.ID}
			}
		}
		if err := validateMemberUnits(sc, in.UnitIDs); err != nil {
			return err
		}
		tuID, pw, err := s.Accounts.CreateMemberTx(ctx, tx, MemberAccount{PropertyID: sc.PropertyID, TenantID: *sc.TenantID, FullName: in.FullName, Email: in.Email, Phone: in.Phone, UnitIDs: in.UnitIDs})
		if err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "tenant_user", EntityID: &tuID, EntityLabel: in.Email, After: map[string]any{"by": "tenant_admin", "units": len(in.UnitIDs)}})
		list, err := s.membersTx(ctx, tx, sc, &tuID)
		if err != nil || len(list) == 0 {
			return apperr.Internal(fmt.Errorf("member not found after create: %v", err))
		}
		out = &MemberCreated{Member: &list[0], TemporaryPassword: pw}
		return nil
	})
	return out, err
}

// MemberAction: deactivate | reactivate (hanya anggota tenant_user, bukan diri sendiri).
func (s *Service) MemberAction(ctx context.Context, tuID uuid.UUID, action string) (*Member, error) {
	var out *Member
	var userID uuid.UUID
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		if err := requireTenantAdmin(sc); err != nil {
			return err
		}
		list, err := s.membersTx(ctx, tx, sc, &tuID)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return apperr.NotFound("Anggota")
		}
		m := list[0]
		if !m.CanManage {
			return apperr.Forbidden("Anggota ini tidak dapat dikelola")
		}
		userID = m.UserID
		switch action {
		case "deactivate":
			if m.Status != "active" {
				return apperr.InvalidTransition("Anggota sudah tidak aktif")
			}
			_, _ = tx.Exec(ctx, `UPDATE tenant_users SET status = 'suspended', suspended_at = now(), suspended_by = $2, suspension_reason = 'Dinonaktifkan oleh Tenant Admin' WHERE id = $1`, tuID, sc.UserID)
			_, _ = tx.Exec(ctx, `UPDATE users SET is_active = false, permission_version = permission_version + 1 WHERE id = $1`, m.UserID)
			_, _ = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, m.UserID)
		case "reactivate":
			var reason *string
			_ = tx.QueryRow(ctx, `SELECT suspension_reason FROM tenant_users WHERE id = $1`, tuID).Scan(&reason)
			if m.Status != "suspended" || reason == nil || *reason != "Dinonaktifkan oleh Tenant Admin" {
				return apperr.Conflict("MEMBER_NOT_REACTIVATABLE", "Hanya anggota yang dinonaktifkan Tenant Admin yang dapat diaktifkan kembali; hubungi pengelola gedung")
			}
			_, _ = tx.Exec(ctx, `UPDATE tenant_users SET status = 'active', suspended_at = NULL, suspended_by = NULL, suspension_reason = NULL WHERE id = $1`, tuID)
			_, _ = tx.Exec(ctx, `UPDATE users SET is_active = true, permission_version = permission_version + 1 WHERE id = $1`, m.UserID)
		default:
			return apperr.NotFound("Aksi")
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "tenant_user", EntityID: &tuID, EntityLabel: m.FullName, Before: map[string]any{"status": m.Status}, After: map[string]any{"action": action, "by": "tenant_admin"}})
		list, err = s.membersTx(ctx, tx, sc, &tuID)
		if err == nil && len(list) > 0 {
			out = &list[0]
		}
		return err
	})
	if err == nil && s.OnUserChanged != nil {
		s.OnUserChanged(userID)
	}
	return out, err
}

// SetMemberUnits: atur akses unit anggota (subset unit Tenant Admin); unit di luar kelolaan admin tidak disentuh.
func (s *Service) SetMemberUnits(ctx context.Context, tuID uuid.UUID, unitIDs []uuid.UUID) (*Member, error) {
	var out *Member
	var userID uuid.UUID
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		if err := requireTenantAdmin(sc); err != nil {
			return err
		}
		if err := validateMemberUnits(sc, unitIDs); err != nil {
			return err
		}
		list, err := s.membersTx(ctx, tx, sc, &tuID)
		if err != nil {
			return err
		}
		if len(list) == 0 || !list[0].CanManage {
			return apperr.NotFound("Anggota")
		}
		userID = list[0].UserID
		admin := []uuid.UUID{}
		for _, u := range sc.Units {
			admin = append(admin, u.ID)
		}
		// cabut akses unit kelolaan admin yang tidak dipilih
		if _, err := tx.Exec(ctx, `UPDATE tenant_access SET status = 'revoked', revoked_at = now(), revoked_by = $3, is_primary = false
			WHERE tenant_user_id = $1 AND access_type = 'unit' AND status = 'active' AND location_id = ANY($2) AND NOT (location_id = ANY($4))`, tuID, admin, sc.UserID, unitIDs); err != nil {
			return err
		}
		for i, u := range unitIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO tenant_access (organization_id, tenant_user_id, property_id, location_id, access_type, is_primary, status, granted_by)
				VALUES ($1,$2,$3,$4,'unit',$5,'active',$6)
				ON CONFLICT (tenant_user_id, location_id) DO UPDATE SET status = 'active', revoked_at = NULL, revoked_by = NULL, granted_by = EXCLUDED.granted_by, granted_at = now()`,
				sc.OrganizationID, tuID, sc.PropertyID, u, i == 0, sc.UserID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "tenant_access", EntityID: &tuID, EntityLabel: list[0].FullName, After: map[string]any{"units": unitIDs, "by": "tenant_admin"}})
		list, err = s.membersTx(ctx, tx, sc, &tuID)
		if err == nil && len(list) > 0 {
			out = &list[0]
		}
		return err
	})
	if err == nil && s.OnUserChanged != nil {
		s.OnUserChanged(userID)
	}
	return out, err
}

// ---------- §5.3 P3-SRQ-05: lampiran pada pesan (tenant & staf) ----------

// MessageAttachment: file pada pesan SR dengan URL bertanda tangan (berlaku singkat).
type MessageAttachment struct {
	ID          uuid.UUID `json:"id"`
	ContentType string    `json:"content_type"`
	FileName    *string   `json:"file_name"`
	URL         string    `json:"url"`
	ThumbURL    string    `json:"thumb_url,omitempty"`
}

// FillMessageAttachments: lengkapi pesan dengan data & URL lampiran (dipakai tenant & staf).
func FillMessageAttachments(ctx context.Context, tx pgx.Tx, att *attachments.Service, msgs []Message) {
	for i := range msgs {
		msgs[i].Attachments = []MessageAttachment{}
		for _, aid := range msgs[i].AttachmentIDs {
			a, err := attachments.GetForMessageTx(ctx, tx, aid)
			if err != nil {
				continue
			}
			list := []attachments.Attachment{*a}
			if att != nil {
				att.FillURLs(ctx, list)
			}
			msgs[i].Attachments = append(msgs[i].Attachments, MessageAttachment{ID: a.ID, ContentType: a.ContentType, FileName: a.OriginalFilename, URL: list[0].URL, ThumbURL: list[0].ThumbURL})
		}
	}
}

// ValidateStaffMessageAttachments: lampiran pesan staf harus file object SR yang sama (diunggah staf mana pun).
func ValidateStaffMessageAttachments(ctx context.Context, tx pgx.Tx, srID uuid.UUID, ids []uuid.UUID) error {
	for _, aid := range ids {
		var ok bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attachments WHERE id = $1 AND object_type = 'service_request' AND object_id = $2 AND deleted_at IS NULL)`, aid, srID).Scan(&ok)
		if !ok {
			return apperr.Validation("attachment_ids tidak valid")
		}
	}
	return nil
}
