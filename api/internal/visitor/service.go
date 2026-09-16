// Package visitor: Visitor Management (PRD P1 v1.3 §22, WF-P1-005, AT-P1-011) — registrasi oleh tenant/staf, VisitorPass (QR),
// verifikasi Security (entry/exit). Bagian dari modul Security (NC §15 Visitors); approval opsional (OD-P1-008).
package visitor

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
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantscope"
)

const (
	EventVisitorRegistered = "visitor.registered"
	EventVisitorApproved   = "visitor.approved"
	EventVisitorDenied     = "visitor.denied"
	EventVisitorCheckedIn  = "visitor.checked_in"
	EventVisitorCheckedOut = "visitor.checked_out"
	EventVisitorCancelled  = "visitor.cancelled"
)

type Service struct {
	DB      *db.DB
	Jobs    jobs.Enqueuer
	Profile *profile.Service
}

func New(d *db.DB, j jobs.Enqueuer, prof *profile.Service) *Service {
	return &Service{DB: d, Jobs: j, Profile: prof}
}

type Pass struct {
	ID         uuid.UUID  `json:"id"`
	PassCode   string     `json:"pass_code"`
	QRPayload  string     `json:"qr_payload"`
	ValidFrom  time.Time  `json:"valid_from"`
	ValidUntil time.Time  `json:"valid_until"`
	Status     string     `json:"status"`
	UsedAt     *time.Time `json:"used_at"`
}

type Visitor struct {
	ID                 uuid.UUID  `json:"id"`
	VisitorNumber      string     `json:"visitor_number"`
	PropertyID         uuid.UUID  `json:"property_id"`
	TenantUserID       *uuid.UUID `json:"tenant_user_id"`
	TenantID           *uuid.UUID `json:"tenant_id"`
	TenantName         *string    `json:"tenant_name"`
	HostName           *string    `json:"host_name"`
	HostUnitLocationID *uuid.UUID `json:"host_unit_location_id"`
	HostUnitLabel      *string    `json:"host_unit_label"`
	VisitorName        string     `json:"visitor_name"`
	VisitorPhone       *string    `json:"visitor_phone"`
	VisitorCompany     *string    `json:"visitor_company"`
	IDNumberMasked     *string    `json:"id_number_masked"`
	Purpose            *string    `json:"purpose"`
	VehiclePlate       *string    `json:"vehicle_plate"`
	Headcount          int        `json:"headcount"`
	ExpectedAt         time.Time  `json:"expected_at"`
	ExpectedUntil      *time.Time `json:"expected_until"`
	Status             string     `json:"status"`
	Channel            string     `json:"channel"`
	ApprovedAt         *time.Time `json:"approved_at"`
	DeniedReason       *string    `json:"denied_reason"`
	VerifiedByName     *string    `json:"verified_by_name"`
	CheckedInAt        *time.Time `json:"checked_in_at"`
	CheckedOutAt       *time.Time `json:"checked_out_at"`
	CheckinNote        *string    `json:"checkin_note"`
	CancelledAt        *time.Time `json:"cancelled_at"`
	CreatedAt          time.Time  `json:"created_at"`
	Pass               *Pass      `json:"pass,omitempty"`
	AllowedActions     []string   `json:"allowed_actions"`
	Version            int        `json:"version"`
}

const visSelect = `SELECT v.id, v.visitor_number, v.property_id, v.tenant_user_id, v.tenant_id, t.name, v.host_name, v.host_unit_location_id, COALESCE('Unit ' || u.unit_number, l.name),
	v.visitor_name, v.visitor_phone, v.visitor_company, v.id_number_masked, v.purpose, v.vehicle_plate, v.headcount, v.expected_at, v.expected_until, v.status, v.channel,
	v.approved_at, v.denied_reason, vb.full_name, v.checked_in_at, v.checked_out_at, v.checkin_note, v.cancelled_at, v.created_at, v.version
	FROM visitors v LEFT JOIN tenants t ON t.id = v.tenant_id LEFT JOIN locations l ON l.id = v.host_unit_location_id LEFT JOIN units u ON u.location_id = l.id LEFT JOIN users vb ON vb.id = v.verified_by`

func scanVisitor(row pgx.Row) (*Visitor, error) {
	var v Visitor
	if err := row.Scan(&v.ID, &v.VisitorNumber, &v.PropertyID, &v.TenantUserID, &v.TenantID, &v.TenantName, &v.HostName, &v.HostUnitLocationID, &v.HostUnitLabel,
		&v.VisitorName, &v.VisitorPhone, &v.VisitorCompany, &v.IDNumberMasked, &v.Purpose, &v.VehiclePlate, &v.Headcount, &v.ExpectedAt, &v.ExpectedUntil, &v.Status, &v.Channel,
		&v.ApprovedAt, &v.DeniedReason, &v.VerifiedByName, &v.CheckedInAt, &v.CheckedOutAt, &v.CheckinNote, &v.CancelledAt, &v.CreatedAt, &v.Version); err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Service) loadPass(ctx context.Context, tx pgx.Tx, v *Visitor) {
	var p Pass
	if err := tx.QueryRow(ctx, `SELECT id, pass_code, valid_from, valid_until, status, used_at FROM visitor_passes WHERE visitor_id = $1 ORDER BY issued_at DESC LIMIT 1`, v.ID).Scan(&p.ID, &p.PassCode, &p.ValidFrom, &p.ValidUntil, &p.Status, &p.UsedAt); err == nil {
		p.QRPayload = "bv:visitor:" + p.PassCode
		v.Pass = &p
	}
}

func (s *Service) staffActions(ctx context.Context, v *Visitor) {
	p := authctx.Must(ctx)
	can := func(perm string) bool { return p.HasOnProperty(perm, v.PropertyID) }
	v.AllowedActions = []string{"view"}
	switch v.Status {
	case "pending_approval":
		if can("security.visitors.manage") {
			v.AllowedActions = append(v.AllowedActions, "approve", "deny")
		}
	case "registered":
		if can("security.visitors.verify") {
			v.AllowedActions = append(v.AllowedActions, "check_in")
		}
		if can("security.visitors.manage") || can("security.visitors.update") {
			v.AllowedActions = append(v.AllowedActions, "cancel")
		}
	case "checked_in":
		if can("security.visitors.verify") {
			v.AllowedActions = append(v.AllowedActions, "check_out")
		}
	}
}

func tenantActions(v *Visitor) {
	v.AllowedActions = []string{"view"}
	if v.Status == "pending_approval" || v.Status == "registered" {
		v.AllowedActions = append(v.AllowedActions, "cancel")
	}
}

type CreateInput struct {
	PropertyID         *uuid.UUID `json:"property_id"`
	TenantID           *uuid.UUID `json:"tenant_id"`
	HostName           *string    `json:"host_name"`
	HostUnitLocationID *uuid.UUID `json:"host_unit_location_id"`
	VisitorName        string     `json:"visitor_name"`
	VisitorPhone       *string    `json:"visitor_phone"`
	VisitorCompany     *string    `json:"visitor_company"`
	IDNumber           *string    `json:"id_number"` // disimpan hanya 4 digit terakhir
	Purpose            *string    `json:"purpose"`
	VehiclePlate       *string    `json:"vehicle_plate"`
	Headcount          *int       `json:"headcount"`
	ExpectedAt         time.Time  `json:"expected_at"`
	ExpectedUntil      *time.Time `json:"expected_until"`
}

func maskID(v *string) *string {
	if v == nil {
		return nil
	}
	d := strings.TrimSpace(*v)
	if len(d) <= 4 {
		return v
	}
	m := strings.Repeat("•", len(d)-4) + d[len(d)-4:]
	return &m
}

// createTx: registrasi tamu (tenant → host otomatis; staf → wajib property/host). Status ikut OD-P1-008.
func (s *Service) createTx(ctx context.Context, tx pgx.Tx, in CreateInput, sc *tenantscope.Scope) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	in.VisitorName = strings.TrimSpace(in.VisitorName)
	if in.VisitorName == "" {
		return uuid.Nil, apperr.Validation("visitor_name wajib").WithField("visitor_name", "wajib")
	}
	if in.ExpectedAt.IsZero() {
		return uuid.Nil, apperr.Validation("expected_at wajib").WithField("expected_at", "wajib")
	}
	if in.ExpectedAt.Before(time.Now().Add(-2 * time.Hour)) {
		return uuid.Nil, apperr.Validation("expected_at sudah lewat")
	}
	if in.ExpectedUntil != nil && !in.ExpectedUntil.After(in.ExpectedAt) {
		return uuid.Nil, apperr.Validation("expected_until harus setelah expected_at")
	}
	head := 1
	if in.Headcount != nil {
		head = *in.Headcount
	}
	if head < 1 || head > 100 {
		return uuid.Nil, apperr.Validation("headcount 1..100")
	}
	var propertyID uuid.UUID
	channel := "staff"
	var tenantUser, tenantID, hostUnit *uuid.UUID
	hostName := in.HostName
	if sc != nil {
		channel = "tenant_app"
		propertyID = sc.PropertyID
		tenantUser, tenantID = &sc.UserID, sc.TenantID
		var fullName string
		_ = tx.QueryRow(ctx, `SELECT full_name FROM users WHERE id = $1`, sc.UserID).Scan(&fullName)
		hostName = &fullName
		if in.HostUnitLocationID != nil {
			if !sc.HasUnit(*in.HostUnitLocationID) {
				return uuid.Nil, apperr.New(403, "LOCATION_NOT_AUTHORIZED", "Location not authorized", "Unit tidak berada dalam scope akses Anda")
			}
			hostUnit = in.HostUnitLocationID
		} else if pu := sc.PrimaryUnit(); pu != nil {
			hostUnit = &pu.ID
		}
	} else {
		if in.PropertyID == nil {
			return uuid.Nil, apperr.Validation("property_id wajib")
		}
		propertyID = *in.PropertyID
		tenantID = in.TenantID
		hostUnit = in.HostUnitLocationID
		if hostUnit != nil {
			pid, err := property.ResolvePropertyOfLocation(ctx, tx, *hostUnit)
			if err != nil || pid != propertyID {
				return uuid.Nil, apperr.Validation("host_unit_location_id tidak berada di property ini")
			}
		}
	}
	if err := s.Profile.RequireCapabilityTx(ctx, tx, propertyID, profile.CapVisitorManagement); err != nil {
		return uuid.Nil, err
	}
	pc, err := s.Profile.ResolveTx(ctx, tx, propertyID)
	if err != nil {
		return uuid.Nil, err
	}
	status := "registered"
	if pc.Config.VisitorApprovalRequired && sc != nil {
		status = "pending_approval"
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixVisitor, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO visitors (organization_id, property_id, visitor_number, tenant_user_id, tenant_id, host_name, host_unit_location_id, visitor_name, visitor_phone, visitor_company, id_number_masked, purpose, vehicle_plate, headcount, expected_at, expected_until, status, channel, created_by, updated_by, approved_at, approved_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$19, CASE WHEN $17::text = 'registered' THEN now() END, CASE WHEN $17::text = 'registered' AND $18::text = 'staff' THEN $19::uuid END) RETURNING id`,
		p.OrganizationID, propertyID, number, tenantUser, tenantID, hostName, hostUnit, in.VisitorName, in.VisitorPhone, in.VisitorCompany, maskID(in.IDNumber), in.Purpose, in.VehiclePlate, head, in.ExpectedAt, in.ExpectedUntil, status, channel, p.UserID).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if status == "registered" {
		if err := s.issuePassTx(ctx, tx, id, in.ExpectedAt, in.ExpectedUntil); err != nil {
			return uuid.Nil, err
		}
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "visitor", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "status": status, "channel": channel}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "visitor", EntityID: &id, EntityLabel: number + " " + in.VisitorName})
	if s.Jobs != nil {
		payload := map[string]any{"status": status, "visitor": in.VisitorName, "domain": "security", "channel": channel}
		if tenantUser != nil {
			payload["tenant_user_id"] = *tenantUser
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventVisitorRegistered, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "visitor", ObjectID: id, ObjectLabel: number + " · " + in.VisitorName, ActorUserID: &p.UserID, Payload: payload})
	}
	return id, nil
}

// issuePassTx: pass berlaku dari 2 jam sebelum expected_at sampai expected_until (default +12 jam).
func (s *Service) issuePassTx(ctx context.Context, tx pgx.Tx, visitorID uuid.UUID, from time.Time, until *time.Time) error {
	p := authctx.Must(ctx)
	code, err := ids.NewQRCode()
	if err != nil {
		return err
	}
	vu := from.Add(12 * time.Hour)
	if until != nil {
		vu = until.Add(2 * time.Hour)
	}
	_, _ = tx.Exec(ctx, `UPDATE visitor_passes SET status = 'revoked', revoked_at = now() WHERE visitor_id = $1 AND status = 'active'`, visitorID)
	_, err = tx.Exec(ctx, `INSERT INTO visitor_passes (organization_id, visitor_id, pass_code, valid_from, valid_until) VALUES ($1,$2,$3,$4,$5)`, p.OrganizationID, visitorID, code, from.Add(-2*time.Hour), vu)
	return err
}

func (s *Service) getTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Visitor, error) {
	v, err := scanVisitor(tx.QueryRow(ctx, visSelect+` WHERE v.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Visitor")
		}
		return nil, err
	}
	s.loadPass(ctx, tx, v)
	return v, nil
}

// ---- staff API ----

type Filter struct {
	PropertyID *uuid.UUID
	Statuses   []string
	Date       *time.Time
	TenantID   *uuid.UUID
	Q          string
}

func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]Visitor, *string, error) {
	p := authctx.Must(ctx)
	var out []Visitor
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if err := iam.CanOnProperty(ctx, "security.visitors.view", *f.PropertyID); err != nil {
				return err
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND v.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("security.visitors.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND v.property_id = ANY($%d)", len(args))
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND v.status = ANY($%d)", len(args))
		}
		if f.TenantID != nil {
			args = append(args, *f.TenantID)
			where += fmt.Sprintf(" AND v.tenant_id = $%d", len(args))
		}
		if f.Date != nil {
			args = append(args, *f.Date, f.Date.Add(24*time.Hour))
			where += fmt.Sprintf(" AND v.expected_at >= $%d AND v.expected_at < $%d", len(args)-1, len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (v.visitor_number ILIKE $%d OR v.visitor_name ILIKE $%d OR v.host_name ILIKE $%d OR v.vehicle_plate ILIKE $%d OR v.visitor_company ILIKE $%d)", len(args), len(args), len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (v.expected_at, v.id) > ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, visSelect+where+fmt.Sprintf(" ORDER BY v.expected_at, v.id LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []Visitor
		for rows.Next() {
			v, err := scanVisitor(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, *v)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.ExpectedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			s.loadPass(ctx, tx, &items[i])
			s.staffActions(ctx, &items[i])
		}
		out = items
		return nil
	})
	if out == nil {
		out = []Visitor{}
	}
	return out, next, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Visitor, error) {
	var out *Visitor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "security.visitors.view", v.PropertyID); err != nil {
			return err
		}
		s.staffActions(ctx, v)
		out = v
		return nil
	})
	return out, err
}

// Resolve: Security memindai QR / mengetik kode pass → visitor (AT-P1-011 "Visitor registration tersedia untuk Security").
func (s *Service) Resolve(ctx context.Context, code string) (*Visitor, error) {
	code = strings.TrimPrefix(strings.TrimSpace(code), "bv:visitor:")
	if code == "" {
		return nil, apperr.Validation("code wajib")
	}
	var out *Visitor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var vid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT visitor_id FROM visitor_passes WHERE pass_code = $1`, code).Scan(&vid); err != nil {
			return apperr.NotFound("Visitor pass")
		}
		v, err := s.getTx(ctx, tx, vid)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "security.visitors.view", v.PropertyID); err != nil {
			return err
		}
		s.staffActions(ctx, v)
		out = v
		return nil
	})
	return out, err
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*Visitor, error) {
	var out *Visitor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if in.PropertyID == nil {
			return apperr.Validation("property_id wajib")
		}
		if err := iam.CanOnProperty(ctx, "security.visitors.create", *in.PropertyID); err != nil {
			return err
		}
		id, err := s.createTx(ctx, tx, in, nil)
		if err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.staffActions(ctx, out)
		return nil
	})
	return out, err
}

type ActionInput struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

// Act: approve | deny | check_in | check_out | cancel (Security / Tenant Relation).
func (s *Service) Act(ctx context.Context, id uuid.UUID, action string, in ActionInput) (*Visitor, error) {
	p := authctx.Must(ctx)
	var out *Visitor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "security.visitors.view", v.PropertyID); err != nil {
			return err
		}
		s.staffActions(ctx, v)
		if !has(v.AllowedActions, action) {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk visitor berstatus %s", action, v.Status))
		}
		if err := s.applyTx(ctx, tx, v, action, in, "staff", p.UserID); err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.staffActions(ctx, out)
		return nil
	})
	return out, err
}

func (s *Service) applyTx(ctx context.Context, tx pgx.Tx, v *Visitor, action string, in ActionInput, actorKind string, actor uuid.UUID) error {
	p := authctx.Must(ctx)
	reason := strings.TrimSpace(in.Reason)
	var to, ev, sets string
	args := []any{v.ID, actor}
	switch action {
	case "approve":
		to, ev, sets = "registered", EventVisitorApproved, "approved_at = now(), approved_by = $2"
		if err := s.issuePassTx(ctx, tx, v.ID, v.ExpectedAt, v.ExpectedUntil); err != nil {
			return err
		}
	case "deny":
		if reason == "" {
			return apperr.Validation("reason wajib").WithField("reason", "wajib")
		}
		args = append(args, reason)
		to, ev, sets = "denied", EventVisitorDenied, "denied_reason = $3"
	case "check_in":
		args = append(args, strings.TrimSpace(in.Note))
		to, ev, sets = "checked_in", EventVisitorCheckedIn, "checked_in_at = now(), verified_by = $2, checkin_note = NULLIF($3,'')"
		_, _ = tx.Exec(ctx, `UPDATE visitor_passes SET status = 'used', used_at = now() WHERE visitor_id = $1 AND status = 'active'`, v.ID)
	case "check_out":
		to, ev, sets = "checked_out", EventVisitorCheckedOut, "checked_out_at = now()"
	case "cancel":
		to, ev, sets = "cancelled", EventVisitorCancelled, "cancelled_at = now()"
		_, _ = tx.Exec(ctx, `UPDATE visitor_passes SET status = 'revoked', revoked_at = now() WHERE visitor_id = $1 AND status = 'active'`, v.ID)
	default:
		return apperr.Validation("aksi tidak dikenal")
	}
	if _, err := tx.Exec(ctx, `UPDATE visitors SET status = '`+to+`', updated_by = $2, `+sets+` WHERE id = $1`, args...); err != nil {
		return err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "visitor", ObjectID: v.ID, Action: audit.ActStatusChanged, From: v.Status, To: to, Payload: map[string]any{"action": action, "reason": reason, "actor_kind": actorKind}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "visitor", EntityID: &v.ID, EntityLabel: v.VisitorNumber, Before: map[string]any{"status": v.Status}, After: map[string]any{"status": to, "reason": reason}})
	if s.Jobs != nil {
		payload := map[string]any{"from": v.Status, "to": to, "reason": reason, "visitor": v.VisitorName, "domain": "security", "actor_kind": actorKind}
		if v.TenantUserID != nil {
			payload["tenant_user_id"] = *v.TenantUserID
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: ev, OrganizationID: p.OrganizationID, PropertyID: &v.PropertyID, ObjectType: "visitor", ObjectID: v.ID, ObjectLabel: v.VisitorNumber + " · " + v.VisitorName, ActorUserID: &p.UserID, Payload: payload})
	}
	return nil
}

// ---- tenant API ----

func tenantOwnership(sc *tenantscope.Scope, argIdx int) (string, []any) {
	if sc.Role == "tenant_admin" && sc.TenantID != nil {
		return fmt.Sprintf("(v.tenant_user_id = $%d OR v.tenant_id = $%d)", argIdx, argIdx+1), []any{sc.UserID, *sc.TenantID}
	}
	return fmt.Sprintf("v.tenant_user_id = $%d", argIdx), []any{sc.UserID}
}

func (s *Service) TenantCreate(ctx context.Context, in CreateInput) (*Visitor, error) {
	var out *Visitor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		in.PropertyID, in.TenantID, in.HostName = nil, nil, nil
		id, err := s.createTx(ctx, tx, in, sc)
		if err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		tenantActions(out)
		return nil
	})
	return out, err
}

func (s *Service) TenantList(ctx context.Context, upcoming *bool, page httpx.Page) ([]Visitor, *string, error) {
	var out []Visitor
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		where, args := tenantOwnership(sc, 1)
		where = " WHERE " + where
		order, cmp := " ORDER BY v.expected_at DESC, v.id DESC", "<"
		if upcoming != nil && *upcoming {
			where += " AND v.status IN ('pending_approval','registered','checked_in') AND COALESCE(v.expected_until, v.expected_at + interval '12 hours') >= now()"
			order, cmp = " ORDER BY v.expected_at, v.id", ">"
		} else if upcoming != nil {
			where += " AND NOT (v.status IN ('pending_approval','registered','checked_in') AND COALESCE(v.expected_until, v.expected_at + interval '12 hours') >= now())"
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (v.expected_at, v.id) %s ($%d::timestamptz, $%d)", cmp, len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, visSelect+where+order+fmt.Sprintf(" LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []Visitor
		for rows.Next() {
			v, err := scanVisitor(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, *v)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.ExpectedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			s.loadPass(ctx, tx, &items[i])
			tenantActions(&items[i])
		}
		out = items
		return nil
	})
	if out == nil {
		out = []Visitor{}
	}
	return out, next, err
}

func (s *Service) tenantGetTx(ctx context.Context, tx pgx.Tx, sc *tenantscope.Scope, id uuid.UUID) (*Visitor, error) {
	where, args := tenantOwnership(sc, 2)
	v, err := scanVisitor(tx.QueryRow(ctx, visSelect+` WHERE v.id = $1 AND `+where, append([]any{id}, args...)...))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Visitor")
		}
		return nil, err
	}
	s.loadPass(ctx, tx, v)
	tenantActions(v)
	return v, nil
}

func (s *Service) TenantGet(ctx context.Context, id uuid.UUID) (*Visitor, error) {
	var out *Visitor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		out, err = s.tenantGetTx(ctx, tx, sc, id)
		return err
	})
	return out, err
}

func (s *Service) TenantCancel(ctx context.Context, id uuid.UUID) (*Visitor, error) {
	var out *Visitor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		v, err := s.tenantGetTx(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		if !has(v.AllowedActions, "cancel") {
			return apperr.InvalidTransition("Visitor tidak dapat dibatalkan")
		}
		if err := s.applyTx(ctx, tx, v, "cancel", ActionInput{Reason: "Dibatalkan oleh tenant"}, "tenant", sc.UserID); err != nil {
			return err
		}
		out, err = s.tenantGetTx(ctx, tx, sc, id)
		return err
	})
	return out, err
}

// ExpireSweep: worker — tamu yang tidak datang sampai batas → expired (pass ikut expired).
func (s *Service) ExpireSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	err := s.DB.WithOrgTx(authctx.With(ctx, authctx.System(orgID)), orgID, func(ctx context.Context, tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `UPDATE visitors SET status = 'expired' WHERE status IN ('pending_approval','registered') AND COALESCE(expected_until, expected_at + interval '12 hours') < now() - interval '2 hours'`)
		if err != nil {
			return err
		}
		n = int(ct.RowsAffected())
		_, _ = tx.Exec(ctx, `UPDATE visitor_passes SET status = 'expired' WHERE status = 'active' AND valid_until < now()`)
		return nil
	})
	return n, err
}

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
