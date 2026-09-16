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
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/tenantservice"
)

const EventServiceRequestMessage = "service_request.message"

type Service struct {
	DB          *db.DB
	Jobs        jobs.Enqueuer
	SR          *tenantservice.Service
	Profile     *profile.Service
	Attachments *attachments.Service
	Ops         *operations.Service

	regLimiter *ipLimiter
}

func New(d *db.DB, j jobs.Enqueuer, sr *tenantservice.Service, prof *profile.Service, att *attachments.Service, ops *operations.Service) *Service {
	return &Service{DB: d, Jobs: j, SR: sr, Profile: prof, Attachments: att, Ops: ops, regLimiter: newIPLimiter(10, time.Minute)}
}

// SetRegistrationRateLimit: 0 = nonaktif (test).
func (s *Service) SetRegistrationRateLimit(perMinute int) {
	s.regLimiter = newIPLimiter(perMinute, time.Minute)
}

// TenantStatus: mapping status internal → tenant-facing (PRD §14; TD-P1-008). Status DB tetap kanonik.
var TenantStatus = map[workflow.Status]string{
	workflow.New: "submitted", workflow.Acknowledged: "received", workflow.Assigned: "being_assigned", workflow.InProgress: "in_progress",
	workflow.WaitingForTenant: "need_your_response", workflow.Resolved: "resolved", workflow.Closed: "closed", workflow.Cancelled: "cancelled",
}

// ---------- /tenant/me ----------

type Me struct {
	ID              uuid.UUID `json:"id"`
	TenantUserID    uuid.UUID `json:"tenant_user_id"`
	FullName        string    `json:"full_name"`
	Email           *string   `json:"email"`
	Phone           *string   `json:"phone"`
	Role            string    `json:"role"`
	AccountStatus   string    `json:"account_status"`
	OwnershipStatus *string   `json:"ownership_status"`
	Organization    struct {
		ID   uuid.UUID `json:"id"`
		Slug string    `json:"slug"`
		Name string    `json:"name"`
	} `json:"organization"`
	Property struct {
		ID          uuid.UUID               `json:"id"`
		Name        string                  `json:"name"`
		Code        string                  `json:"code"`
		Profile     profile.Profile         `json:"profile"`
		Timezone    string                  `json:"timezone"`
		Terminology map[string]profile.Term `json:"terminology"`
	} `json:"property"`
	Tenant *struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
		Code string    `json:"code"`
	} `json:"tenant"`
	Units       []AccessLoc `json:"units"`
	Areas       []AccessLoc `json:"areas"`
	PrimaryUnit *AccessLoc  `json:"primary_unit"`
	Features    struct {
		ExposeSLA            bool `json:"expose_sla"`
		ConfirmationRequired bool `json:"confirmation_required"`
		CSATEnabled          bool `json:"csat_enabled"`
		BookingApproval      bool `json:"booking_approval_required"`
		VisitorApproval      bool `json:"visitor_approval_required"`
	} `json:"features"`
	Capabilities []profile.Capability `json:"capabilities"`
	LastSeenAt   *time.Time           `json:"last_seen_at"`
}

func (s *Service) Me(ctx context.Context) (*Me, error) {
	var out *Me
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		m, err := s.meTx(ctx, tx, sc)
		if err != nil {
			return err
		}
		out = m
		return nil
	})
	return out, err
}

func (s *Service) meTx(ctx context.Context, tx pgx.Tx, sc *Scope) (*Me, error) {
	m := &Me{ID: sc.UserID, TenantUserID: sc.TenantUserID, Role: sc.Role, AccountStatus: sc.Status, OwnershipStatus: sc.OwnershipStatus, Units: sc.Units, Areas: sc.Areas, PrimaryUnit: sc.PrimaryUnit()}
	if err := tx.QueryRow(ctx, `SELECT u.full_name, u.email, u.phone, tu.last_seen_at, o.id, o.slug, o.name FROM users u JOIN tenant_users tu ON tu.user_id = u.id JOIN organizations o ON o.id = u.organization_id WHERE u.id = $1`, sc.UserID).
		Scan(&m.FullName, &m.Email, &m.Phone, &m.LastSeenAt, &m.Organization.ID, &m.Organization.Slug, &m.Organization.Name); err != nil {
		return nil, err
	}
	pc, err := s.Profile.ResolveTx(ctx, tx, sc.PropertyID)
	if err != nil {
		return nil, err
	}
	m.Property.ID = sc.PropertyID
	m.Property.Name = pc.PropertyName
	m.Property.Profile = pc.Profile
	m.Property.Terminology = pc.Terminology
	m.Capabilities = pc.Capabilities
	_ = tx.QueryRow(ctx, `SELECT l.code, p.timezone FROM properties p JOIN locations l ON l.id = p.location_id WHERE p.location_id = $1`, sc.PropertyID).Scan(&m.Property.Code, &m.Property.Timezone)
	m.Features.ExposeSLA = pc.Config.ExposeSLAToTenant
	m.Features.ConfirmationRequired = pc.Config.TenantConfirmationRequired
	m.Features.CSATEnabled = pc.Config.CSATEnabled
	m.Features.BookingApproval = pc.Config.BookingApprovalRequired
	m.Features.VisitorApproval = pc.Config.VisitorApprovalRequired
	if sc.TenantID != nil {
		var t struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
			Code string    `json:"code"`
		}
		if err := tx.QueryRow(ctx, `SELECT id, name, tenant_code FROM tenants WHERE id = $1`, *sc.TenantID).Scan(&t.ID, &t.Name, &t.Code); err == nil {
			m.Tenant = &t
		}
	}
	return m, nil
}

type UpdateMeInput struct {
	FullName *string `json:"full_name"`
	Phone    *string `json:"phone"`
}

func (s *Service) UpdateMe(ctx context.Context, in UpdateMeInput) (*Me, error) {
	var out *Me
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		if in.FullName != nil && strings.TrimSpace(*in.FullName) == "" {
			return apperr.Validation("full_name tidak boleh kosong")
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET full_name = COALESCE(NULLIF(TRIM($2),''), full_name), phone = COALESCE(NULLIF(TRIM($3),''), phone) WHERE id = $1`, sc.UserID, deref(in.FullName), deref(in.Phone)); err != nil {
			return err
		}
		if sc.OccupantID != nil {
			_, _ = tx.Exec(ctx, `UPDATE occupants SET full_name = COALESCE(NULLIF(TRIM($2),''), full_name), phone = COALESCE(NULLIF(TRIM($3),''), phone) WHERE id = $1`, *sc.OccupantID, deref(in.FullName), deref(in.Phone))
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "tenant_user", EntityID: &sc.TenantUserID, EntityLabel: "profile", After: in})
		out, err = s.meTx(ctx, tx, sc)
		return err
	})
	return out, err
}

// ---------- lokasi & kategori untuk Report an Issue ----------

func (s *Service) ReportableLocations(ctx context.Context) ([]ReportableLocation, error) {
	var out []ReportableLocation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		out, err = ReportableLocationsTx(ctx, tx, sc)
		return err
	})
	return out, err
}

func (s *Service) Categories(ctx context.Context) ([]tenantservice.Category, error) {
	var out []tenantservice.Category
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		out, err = s.SR.ListCategoriesTx(ctx, tx, tenantservice.CategoryFilter{PropertyID: &sc.PropertyID, TenantOnly: true})
		return err
	})
	if out == nil {
		out = []tenantservice.Category{}
	}
	return out, err
}

// ---------- Ticket (tenant view) ----------

type Photo struct {
	ID             uuid.UUID `json:"id"`
	AttachmentType string    `json:"attachment_type"`
	Kind           string    `json:"kind"` // problem | resolution
	URL            string    `json:"url,omitempty"`
	ThumbURL       string    `json:"thumb_url,omitempty"`
	ContentType    string    `json:"content_type"`
	Status         string    `json:"status"`
	UploadedAt     time.Time `json:"uploaded_at"`
}

type TimelineEvent struct {
	ID         uuid.UUID `json:"id"`
	Key        string    `json:"key"` // submitted | received | being_assigned | in_progress | need_your_response | resolved | closed | cancelled | reopened | message
	Title      string    `json:"title"`
	Detail     *string   `json:"detail,omitempty"`
	ActorKind  string    `json:"actor_kind"` // tenant | staff | system
	OccurredAt time.Time `json:"occurred_at"`
}

type Request struct {
	ID            uuid.UUID `json:"id"`
	RequestNumber string    `json:"request_number"`
	Title         string    `json:"title"`
	Description   *string   `json:"description"`
	CategoryCode  string    `json:"category_code"`
	CategoryName  *string   `json:"category_name"`
	CategoryIcon  *string   `json:"category_icon"`
	Status        string    `json:"status"`        // kanonik (NC §28)
	TenantStatus  string    `json:"tenant_status"` // tenant-facing (PRD §14)
	Priority      string    `json:"priority"`
	AreaScope     *string   `json:"area_scope"`
	Location      struct {
		ID       *uuid.UUID `json:"id"`
		Name     *string    `json:"name"`
		PathText *string    `json:"path_text"`
	} `json:"location"`
	PropertyName      string                  `json:"property_name"`
	AssignedTeam      *string                 `json:"assigned_team"` // hanya nama team (PRD G3 "team yang menangani bila diizinkan")
	CreatedAt         time.Time               `json:"created_at"`
	AcknowledgedAt    *time.Time              `json:"acknowledged_at"`
	ResolvedAt        *time.Time              `json:"resolved_at"`
	ClosedAt          *time.Time              `json:"closed_at"`
	CancelledAt       *time.Time              `json:"cancelled_at"`
	ConfirmedAt       *time.Time              `json:"confirmed_at"`
	DueEstimateAt     *time.Time              `json:"due_estimate_at"` // hanya bila expose_sla_to_tenant
	Resolution        *string                 `json:"resolution"`
	ReopenCount       int                     `json:"reopen_count"`
	ContactPreference *string                 `json:"contact_preference"`
	PreferredVisitAt  *time.Time              `json:"preferred_visit_at"`
	AdditionalNote    *string                 `json:"additional_note"`
	Photos            []Photo                 `json:"photos"`
	MessageCount      int                     `json:"message_count"`
	UnreadMessages    int                     `json:"unread_messages"`
	Feedback          *tenantservice.Feedback `json:"feedback"`
	Timeline          []TimelineEvent         `json:"timeline,omitempty"`
	AllowedActions    []string                `json:"allowed_actions"`
	Version           int                     `json:"version"`
}

const reqSelect = `SELECT sr.id, sr.request_number, sr.title, sr.description, sr.category_code, c.name, c.icon, sr.status, sr.priority, sr.area_scope,
	sr.location_id, l.name, sr.assignee_team_id, tm.name, sr.created_at, sr.acknowledged_at, sr.resolved_at, sr.closed_at, sr.cancelled_at, sr.confirmed_at,
	COALESCE(sr.due_estimate_at, st.resolution_due_at), sr.resolution, sr.reopen_count, sr.contact_preference, sr.preferred_visit_at, sr.additional_note, sr.version, pl.name,
	(SELECT count(*) FROM service_request_messages m WHERE m.service_request_id = sr.id),
	(SELECT count(*) FROM service_request_messages m WHERE m.service_request_id = sr.id AND m.author_kind <> 'tenant' AND m.read_by_tenant_at IS NULL),
	fb.rating, fb.comment, fb.created_at
	FROM service_requests sr
	LEFT JOIN service_request_categories c ON c.id = sr.category_id
	LEFT JOIN locations l ON l.id = sr.location_id
	LEFT JOIN locations pl ON pl.id = sr.property_id
	LEFT JOIN teams tm ON tm.id = sr.assignee_team_id
	LEFT JOIN sla_tracking st ON st.object_type = 'service_request' AND st.object_id = sr.id
	LEFT JOIN service_request_feedback fb ON fb.service_request_id = sr.id`

func scanReq(row pgx.Row) (*Request, error) {
	var r Request
	var teamID *uuid.UUID
	var fbRating *int
	var fbComment *string
	var fbAt *time.Time
	var status workflow.Status
	if err := row.Scan(&r.ID, &r.RequestNumber, &r.Title, &r.Description, &r.CategoryCode, &r.CategoryName, &r.CategoryIcon, &status, &r.Priority, &r.AreaScope,
		&r.Location.ID, &r.Location.Name, &teamID, &r.AssignedTeam, &r.CreatedAt, &r.AcknowledgedAt, &r.ResolvedAt, &r.ClosedAt, &r.CancelledAt, &r.ConfirmedAt,
		&r.DueEstimateAt, &r.Resolution, &r.ReopenCount, &r.ContactPreference, &r.PreferredVisitAt, &r.AdditionalNote, &r.Version, &r.PropertyName,
		&r.MessageCount, &r.UnreadMessages, &fbRating, &fbComment, &fbAt); err != nil {
		return nil, err
	}
	r.Status = string(status)
	r.TenantStatus = TenantStatus[status]
	if fbRating != nil {
		r.Feedback = &tenantservice.Feedback{Rating: *fbRating, Comment: fbComment, CreatedAt: *fbAt}
	}
	r.Photos = []Photo{}
	return &r, nil
}

// finalize: hapus data yang tidak boleh dilihat tenant sesuai config & hitung allowed_actions.
func (s *Service) finalize(ctx context.Context, tx pgx.Tx, sc *Scope, r *Request, pc *profile.Context, withDetail bool) error {
	if !pc.Config.ExposeSLAToTenant {
		r.DueEstimateAt = nil
	}
	if !pc.Config.CSATEnabled {
		r.Feedback = nil
	}
	if r.Location.ID != nil {
		var pt string
		_ = tx.QueryRow(ctx, `SELECT string_agg(a.name, ' · ' ORDER BY a.depth) FROM locations l JOIN locations a ON a.path @> l.path AND a.depth > 0 WHERE l.id = $1`, *r.Location.ID).Scan(&pt)
		if pt != "" {
			r.Location.PathText = &pt
		}
	}
	st := workflow.Status(r.Status)
	r.AllowedActions = []string{"view"}
	if !workflow.ServiceRequest.IsTerminal(st) {
		r.AllowedActions = append(r.AllowedActions, "message")
	}
	switch st {
	case workflow.New, workflow.Acknowledged:
		r.AllowedActions = append(r.AllowedActions, "cancel", "attach")
	case workflow.Assigned, workflow.InProgress, workflow.WaitingForTenant:
		r.AllowedActions = append(r.AllowedActions, "attach")
	case workflow.Resolved:
		r.AllowedActions = append(r.AllowedActions, "confirm", "reopen")
	case workflow.Closed:
		if r.ClosedAt != nil && time.Since(*r.ClosedAt) < 7*24*time.Hour {
			r.AllowedActions = append(r.AllowedActions, "reopen")
		}
		if pc.Config.CSATEnabled && r.Feedback == nil {
			r.AllowedActions = append(r.AllowedActions, "feedback")
		}
	}
	if withDetail {
		atts, err := attachments.ListForObjectTx(ctx, tx, operations.ObjServiceRequest, r.ID)
		if err != nil {
			return err
		}
		s.Attachments.FillURLs(ctx, atts)
		for _, a := range atts {
			kind := ""
			switch {
			case a.UploadedBy == sc.UserID:
				kind = "problem"
			case a.AttachmentType == "photo_after":
				kind = "resolution" // bukti hasil yang sengaja dibagikan ke tenant; evidence lain tetap internal (guardrail #7)
			default:
				continue
			}
			r.Photos = append(r.Photos, Photo{ID: a.ID, AttachmentType: a.AttachmentType, Kind: kind, URL: a.URL, ThumbURL: a.ThumbURL, ContentType: a.ContentType, Status: a.Status, UploadedAt: a.UploadedAt})
		}
		tl, err := s.timelineTx(ctx, tx, r.ID)
		if err != nil {
			return err
		}
		r.Timeline = tl
	}
	return nil
}

// timelineTx: tenant-facing timeline dari activities (guardrail #8: komentar/catatan internal tidak pernah masuk).
func (s *Service) timelineTx(ctx context.Context, tx pgx.Tx, srID uuid.UUID) ([]TimelineEvent, error) {
	rows, err := tx.Query(ctx, `SELECT id, action, from_value, to_value, payload, occurred_at, source FROM activities WHERE object_type = 'service_request' AND object_id = $1 AND action IN ('created','status_changed','resolved','reopened','cancelled') ORDER BY occurred_at, id`, srID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimelineEvent{}
	titles := map[string]string{"submitted": "Ticket dikirim", "received": "Diterima building management", "being_assigned": "Sedang ditugaskan", "in_progress": "Sedang dikerjakan", "need_your_response": "Menunggu respons Anda", "resolved": "Selesai ditangani", "closed": "Ticket ditutup", "cancelled": "Dibatalkan", "reopened": "Dibuka kembali"}
	for rows.Next() {
		var e TimelineEvent
		var action string
		var from, to *string
		var payload map[string]any
		var src string
		if err := rows.Scan(&e.ID, &action, &from, &to, &payload, &e.OccurredAt, &src); err != nil {
			return nil, err
		}
		e.ActorKind = "staff"
		if src == "tenant_app" {
			e.ActorKind = "tenant"
		} else if src == "system" {
			e.ActorKind = "system"
		}
		if ak, ok := payload["actor_kind"].(string); ok && ak != "" {
			e.ActorKind = ak
		}
		switch action {
		case "created":
			e.Key = "submitted"
		case "reopened":
			e.Key = "reopened"
			if r, ok := payload["reason"].(string); ok && r != "" && e.ActorKind == "tenant" {
				e.Detail = &r
			}
		case "resolved":
			continue // sudah tercakup status_changed → resolved (resolution ditampilkan di field Resolution)
		case "cancelled":
			e.Key = "cancelled"
		default: // status_changed
			if to == nil {
				continue
			}
			e.Key = TenantStatus[workflow.Status(*to)]
			if e.Key == "" {
				continue
			}
			if e.Key == "need_your_response" {
				if r, ok := payload["reason"].(string); ok && r != "" {
					e.Detail = &r // permintaan informasi tambahan ke tenant
				}
			}
		}
		e.Title = titles[e.Key]
		out = append(out, e)
	}
	return out, rows.Err()
}

type CreateRequestInput struct {
	CategoryCode      string     `json:"category_code"`
	Title             string     `json:"title"`
	Description       string     `json:"description"`
	LocationID        *uuid.UUID `json:"location_id"`
	ContactPreference *string    `json:"contact_preference"`
	PreferredVisitAt  *time.Time `json:"preferred_visit_at"`
	AdditionalNote    *string    `json:"additional_note"`
}

// CreateRequest: Report an Issue (PRD §10; AT-P1-002/003/004). Unit otomatis = primary unit bila location_id kosong.
func (s *Service) CreateRequest(ctx context.Context, in CreateRequestInput) (*Request, error) {
	var out *Request
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		in.Description = strings.TrimSpace(in.Description)
		if len(in.Description) < 10 {
			return apperr.Validation("description minimal 10 karakter").WithField("description", "terlalu pendek")
		}
		in.Title = strings.TrimSpace(in.Title)
		if in.Title == "" {
			in.Title = in.Description
			if len([]rune(in.Title)) > 80 {
				in.Title = string([]rune(in.Title)[:77]) + "…"
			}
		}
		if in.CategoryCode == "" {
			return apperr.Validation("category_code wajib").WithField("category_code", "wajib")
		}
		var locID uuid.UUID
		var scope string
		if in.LocationID != nil {
			locID = *in.LocationID
			scope, err = ResolveLocationTx(ctx, tx, sc, locID)
			if err != nil {
				return err
			}
		} else {
			pu := sc.PrimaryUnit()
			if pu == nil {
				return apperr.Validation("Anda belum memiliki unit terdaftar; pilih lokasi common area").WithField("location_id", "wajib")
			}
			locID, scope = pu.ID, "unit"
		}
		// kategori harus terlihat untuk tenant di profile property ini
		cats, err := s.SR.ListCategoriesTx(ctx, tx, tenantservice.CategoryFilter{PropertyID: &sc.PropertyID, TenantOnly: true})
		if err != nil {
			return err
		}
		okCat := false
		for _, c := range cats {
			if c.Code == in.CategoryCode {
				okCat = true
				break
			}
		}
		if !okCat {
			return apperr.Validation("category_code tidak tersedia").WithField("category_code", "tidak valid")
		}
		var fullName, email, phone *string
		_ = tx.QueryRow(ctx, `SELECT full_name, email, phone FROM users WHERE id = $1`, sc.UserID).Scan(&fullName, &email, &phone)
		id, err := s.SR.CreateTx(ctx, tx, tenantservice.CreateInput{
			PropertyID: &sc.PropertyID, CategoryCode: in.CategoryCode, Title: in.Title, Description: &in.Description,
			TenantID: sc.TenantID, OccupantID: sc.OccupantID, RequesterName: fullName, RequesterPhone: phone, RequesterEmail: email,
			LocationID: &locID, Channel: "tenant_app", TenantUserID: &sc.UserID, AreaScope: scope, AsTenant: true,
		})
		if err != nil {
			return err
		}
		if in.ContactPreference != nil || in.PreferredVisitAt != nil || in.AdditionalNote != nil {
			if _, err := tx.Exec(ctx, `UPDATE service_requests SET contact_preference = $2, preferred_visit_at = $3, additional_note = $4 WHERE id = $1`, id, in.ContactPreference, in.PreferredVisitAt, in.AdditionalNote); err != nil {
				return err
			}
		}
		out, err = s.getTx(ctx, tx, sc, id, true)
		return err
	})
	return out, err
}

func (s *Service) getTx(ctx context.Context, tx pgx.Tx, sc *Scope, id uuid.UUID, withDetail bool) (*Request, error) {
	where, args := srOwnershipWhere(sc, 2)
	r, err := scanReq(tx.QueryRow(ctx, reqSelect+` WHERE sr.id = $1 AND `+where, append([]any{id}, args...)...))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Ticket") // bukan 403: tidak membocorkan keberadaan ticket lain (AT-P1-004)
		}
		return nil, err
	}
	pc, err := s.Profile.ResolveTx(ctx, tx, sc.PropertyID)
	if err != nil {
		return nil, err
	}
	return r, s.finalize(ctx, tx, sc, r, pc, withDetail)
}

func (s *Service) GetRequest(ctx context.Context, id uuid.UUID) (*Request, error) {
	var out *Request
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, sc, id, true)
		return err
	})
	return out, err
}

type ListFilter struct {
	Open     *bool
	Statuses []string
	Q        string
}

func (s *Service) ListRequests(ctx context.Context, f ListFilter, page httpx.Page) ([]Request, *string, error) {
	var out []Request
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		pc, err := s.Profile.ResolveTx(ctx, tx, sc.PropertyID)
		if err != nil {
			return err
		}
		where, args := srOwnershipWhere(sc, 1)
		where = " WHERE " + where
		if f.Open != nil {
			if *f.Open {
				where += " AND sr.status NOT IN ('closed','cancelled')"
			} else {
				where += " AND sr.status IN ('closed','cancelled')"
			}
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND sr.status = ANY($%d)", len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (sr.request_number ILIKE $%d OR sr.title ILIKE $%d)", len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (sr.created_at, sr.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, reqSelect+where+fmt.Sprintf(" ORDER BY sr.created_at DESC, sr.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []Request
		for rows.Next() {
			r, err := scanReq(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, *r)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			if err := s.finalize(ctx, tx, sc, &items[i], pc, false); err != nil {
				return err
			}
		}
		out = items
		return nil
	})
	if out == nil {
		out = []Request{}
	}
	return out, next, err
}

type ActionInput struct {
	Reason string `json:"reason"`
}

// Act: cancel | confirm | reopen (PRD §17, WF-P1-003; guardrail #9 reopen auditable).
func (s *Service) Act(ctx context.Context, id uuid.UUID, action string, in ActionInput) (*Request, error) {
	var out *Request
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		r, err := s.getTx(ctx, tx, sc, id, false)
		if err != nil {
			return err
		}
		allowed := false
		for _, a := range r.AllowedActions {
			if a == action {
				allowed = true
				break
			}
		}
		if !allowed {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk ticket berstatus %s", action, r.TenantStatus))
		}
		var wfAction string
		reason := strings.TrimSpace(in.Reason)
		switch action {
		case "cancel":
			wfAction = workflow.ActCancel
			if reason == "" {
				reason = "Dibatalkan oleh tenant"
			}
		case "confirm":
			wfAction = workflow.ActClose
			reason = "Dikonfirmasi selesai oleh tenant"
		case "reopen":
			wfAction = workflow.ActReopen
			if reason == "" {
				return apperr.Validation("reason wajib saat membuka kembali ticket (Still Have Problem)").WithField("reason", "wajib")
			}
		default:
			return apperr.Validation("aksi tidak dikenal")
		}
		if err := s.SR.TransitionTx(ctx, tx, id, wfAction, tenantservice.TransitionInput{Reason: reason}, true); err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, sc, id, true)
		return err
	})
	return out, err
}

type FeedbackInput struct {
	Rating  int     `json:"rating"`
	Comment *string `json:"comment"`
}

// Feedback: CSAT setelah Closed (PRD §18; AT-P1-007) — sekali per ticket.
func (s *Service) Feedback(ctx context.Context, id uuid.UUID, in FeedbackInput) (*Request, error) {
	if in.Rating < 1 || in.Rating > 5 {
		return nil, apperr.Validation("rating harus 1..5").WithField("rating", "1..5")
	}
	var out *Request
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		r, err := s.getTx(ctx, tx, sc, id, false)
		if err != nil {
			return err
		}
		if r.Status != string(workflow.Closed) {
			return apperr.InvalidTransition("Feedback hanya untuk ticket yang sudah Closed")
		}
		if r.Feedback != nil {
			return apperr.Conflict("FEEDBACK_EXISTS", "Feedback sudah diberikan")
		}
		pc, _ := s.Profile.ResolveTx(ctx, tx, sc.PropertyID)
		if pc != nil && !pc.Config.CSATEnabled {
			return apperr.Forbidden("Feedback tidak diaktifkan pada property ini")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO service_request_feedback (service_request_id, organization_id, property_id, tenant_user_id, rating, comment) VALUES ($1,$2,$3,$4,$5,NULLIF(TRIM($6),''))`,
			id, sc.OrganizationID, sc.PropertyID, sc.UserID, in.Rating, deref(in.Comment)); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: id, Action: "feedback_given", Payload: map[string]any{"rating": in.Rating, "actor_kind": "tenant"}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "service_request.feedback", OrganizationID: sc.OrganizationID, PropertyID: &sc.PropertyID, ObjectType: operations.ObjServiceRequest, ObjectID: id, ObjectLabel: r.RequestNumber, ActorUserID: &sc.UserID, Payload: map[string]any{"rating": in.Rating, "domain": "tenant_relation"}})
		}
		out, err = s.getTx(ctx, tx, sc, id, true)
		return err
	})
	return out, err
}

// ---------- Messages (ServiceRequestMessage) ----------

type Message struct {
	ID            uuid.UUID   `json:"id"`
	AuthorKind    string      `json:"author_kind"`
	AuthorName    *string     `json:"author_name"`
	Body          string      `json:"body"`
	AttachmentIDs []uuid.UUID `json:"attachment_ids"`
	CreatedAt     time.Time   `json:"created_at"`
	ReadAt        *time.Time  `json:"read_at"`
}

// ListMessagesTx: thread komunikasi (dipakai tenant & staf). forTenant → tandai pesan staf terbaca oleh tenant.
func ListMessagesTx(ctx context.Context, tx pgx.Tx, srID uuid.UUID, forTenant bool) ([]Message, error) {
	if forTenant {
		_, _ = tx.Exec(ctx, `UPDATE service_request_messages SET read_by_tenant_at = now() WHERE service_request_id = $1 AND author_kind <> 'tenant' AND read_by_tenant_at IS NULL`, srID)
	} else {
		_, _ = tx.Exec(ctx, `UPDATE service_request_messages SET read_by_staff_at = now() WHERE service_request_id = $1 AND author_kind = 'tenant' AND read_by_staff_at IS NULL`, srID)
	}
	rows, err := tx.Query(ctx, `SELECT m.id, m.author_kind, CASE WHEN m.author_kind = 'staff' THEN 'Building Management' ELSE u.full_name END, m.body, m.attachment_ids, m.created_at,
		CASE WHEN m.author_kind = 'tenant' THEN m.read_by_staff_at ELSE m.read_by_tenant_at END
		FROM service_request_messages m LEFT JOIN users u ON u.id = m.author_user_id WHERE m.service_request_id = $1 ORDER BY m.created_at, m.id`, srID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.AuthorKind, &m.AuthorName, &m.Body, &m.AttachmentIDs, &m.CreatedAt, &m.ReadAt); err != nil {
			return nil, err
		}
		if m.AttachmentIDs == nil {
			m.AttachmentIDs = []uuid.UUID{}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) ListMessages(ctx context.Context, id uuid.UUID) ([]Message, error) {
	var out []Message
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := s.getTx(ctx, tx, sc, id, false); err != nil {
			return err
		}
		out, err = ListMessagesTx(ctx, tx, id, true)
		return err
	})
	return out, err
}

type MessageInput struct {
	Body          string      `json:"body"`
	AttachmentIDs []uuid.UUID `json:"attachment_ids"`
}

// PostMessageTx: kirim pesan (tenant atau staf). Event service_request.message → notifikasi pihak lawan.
func PostMessageTx(ctx context.Context, tx pgx.Tx, j jobs.Enqueuer, srID uuid.UUID, authorKind string, in MessageInput) (*Message, error) {
	p := authctx.Must(ctx)
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return nil, apperr.Validation("body wajib").WithField("body", "wajib")
	}
	if len([]rune(body)) > 4000 {
		return nil, apperr.Validation("body maksimal 4000 karakter")
	}
	var propertyID uuid.UUID
	var number string
	var tenantUser *uuid.UUID
	var status string
	if err := tx.QueryRow(ctx, `SELECT property_id, request_number, tenant_user_id, status FROM service_requests WHERE id = $1`, srID).Scan(&propertyID, &number, &tenantUser, &status); err != nil {
		return nil, apperr.NotFound("Service Request")
	}
	if workflow.ServiceRequest.IsTerminal(workflow.Status(status)) {
		return nil, apperr.Conflict("OBJECT_TERMINAL", "Ticket sudah "+workflow.Label(workflow.Status(status)))
	}
	if in.AttachmentIDs == nil {
		in.AttachmentIDs = []uuid.UUID{}
	}
	var m Message
	if err := tx.QueryRow(ctx, `INSERT INTO service_request_messages (organization_id, service_request_id, author_user_id, author_kind, body, attachment_ids) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`,
		p.OrganizationID, srID, p.UserID, authorKind, body, in.AttachmentIDs).Scan(&m.ID, &m.CreatedAt); err != nil {
		return nil, err
	}
	m.AuthorKind, m.Body, m.AttachmentIDs = authorKind, body, in.AttachmentIDs
	name := p.FullName
	if authorKind == "staff" {
		name = "Building Management"
	}
	m.AuthorName = &name
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: srID, Action: "tenant_message", Payload: map[string]any{"author_kind": authorKind, "message_id": m.ID}})
	if j != nil {
		preview := body
		if len([]rune(preview)) > 120 {
			preview = string([]rune(preview)[:117]) + "…"
		}
		payload := map[string]any{"author_kind": authorKind, "message_preview": preview, "domain": "tenant_relation"}
		if tenantUser != nil {
			payload["tenant_user_id"] = *tenantUser
		}
		_ = j.EnqueueEventTx(ctx, tx, events.Event{Type: EventServiceRequestMessage, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: operations.ObjServiceRequest, ObjectID: srID, ObjectLabel: number, ActorUserID: &p.UserID, Payload: payload})
	}
	return &m, nil
}

func (s *Service) PostMessage(ctx context.Context, id uuid.UUID, in MessageInput) (*Message, error) {
	var out *Message
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := s.getTx(ctx, tx, sc, id, false); err != nil {
			return err
		}
		// lampiran harus milik tenant & menempel pada SR ini
		for _, aid := range in.AttachmentIDs {
			var ok bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attachments WHERE id = $1 AND object_type = 'service_request' AND object_id = $2 AND uploaded_by = $3)`, aid, id, sc.UserID).Scan(&ok)
			if !ok {
				return apperr.Validation("attachment_ids tidak valid")
			}
		}
		out, err = PostMessageTx(ctx, tx, s.Jobs, id, "tenant", in)
		return err
	})
	return out, err
}

// ---------- Announcements (read) ----------

type Announcement struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Excerpt     *string    `json:"excerpt"`
	Body        string     `json:"body"`
	Importance  string     `json:"importance"`
	PublishedAt *time.Time `json:"published_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	ImageURL    string     `json:"image_url,omitempty"`
}

// Announcement: satu pengumuman (deep link Inbox → /inbox/announcements/{id}); hanya yang published & dalam scope property.
func (s *Service) Announcement(ctx context.Context, id uuid.UUID) (*Announcement, error) {
	var out *Announcement
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		var a Announcement
		var img *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id, title, excerpt, body, importance, published_at, expires_at, image_attachment_id FROM announcements
			WHERE id = $2 AND status = 'published' AND audience IN ('tenant','all') AND (property_id IS NULL OR property_id = $1)`, sc.PropertyID, id).
			Scan(&a.ID, &a.Title, &a.Excerpt, &a.Body, &a.Importance, &a.PublishedAt, &a.ExpiresAt, &img); err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Announcement")
			}
			return err
		}
		if img != nil {
			if at, err := s.Attachments.GetTx(ctx, tx, *img); err == nil {
				s.Attachments.FillURLs(ctx, []attachments.Attachment{*at})
				a.ImageURL = at.URL
			}
		}
		out = &a
		return nil
	})
	return out, err
}

func (s *Service) Announcements(ctx context.Context, page httpx.Page) ([]Announcement, *string, error) {
	var out []Announcement
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		args := []any{sc.PropertyID}
		where := ` WHERE status = 'published' AND audience IN ('tenant','all') AND (property_id IS NULL OR property_id = $1) AND (expires_at IS NULL OR expires_at > now())`
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (published_at, id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, `SELECT id, title, excerpt, body, importance, published_at, expires_at, image_attachment_id FROM announcements`+where+fmt.Sprintf(" ORDER BY published_at DESC, id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []Announcement
		var imgIDs []*uuid.UUID
		for rows.Next() {
			var a Announcement
			var img *uuid.UUID
			if err := rows.Scan(&a.ID, &a.Title, &a.Excerpt, &a.Body, &a.Importance, &a.PublishedAt, &a.ExpiresAt, &img); err != nil {
				return err
			}
			items = append(items, a)
			imgIDs = append(imgIDs, img)
		}
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.PublishedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			if imgIDs[i] != nil {
				if a, err := s.Attachments.GetTx(ctx, tx, *imgIDs[i]); err == nil {
					s.Attachments.FillURLs(ctx, []attachments.Attachment{*a})
					items[i].ImageURL = a.URL
				}
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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
