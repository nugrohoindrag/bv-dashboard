package property

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
)

// ---------- Tenant (PRD §16.1) ----------

type TenantUnit struct {
	LocationID uuid.UUID `json:"location_id"`
	UnitNumber string    `json:"unit_number"`
	PathText   string    `json:"path_text"`
}

type Tenant struct {
	ID           uuid.UUID    `json:"id"`
	PropertyID   uuid.UUID    `json:"property_id"`
	TenantCode   string       `json:"tenant_code"`
	Name         string       `json:"name"`
	TenantType   string       `json:"tenant_type"`
	ContactName  *string      `json:"contact_name"`
	ContactPhone *string      `json:"contact_phone"`
	ContactEmail *string      `json:"contact_email"`
	Status       string       `json:"status"`
	Notes        *string      `json:"notes"`
	Units        []TenantUnit `json:"units"`
	OpenRequests int          `json:"open_requests"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	Version      int          `json:"version"`
}

type TenantInput struct {
	PropertyID   *uuid.UUID   `json:"property_id"`
	Name         *string      `json:"name"`
	TenantType   *string      `json:"tenant_type"`
	ContactName  *string      `json:"contact_name"`
	ContactPhone *string      `json:"contact_phone"`
	ContactEmail *string      `json:"contact_email"`
	Status       *string      `json:"status"`
	Notes        *string      `json:"notes"`
	UnitIDs      *[]uuid.UUID `json:"unit_ids"`
}

func (s *Service) CreateTenant(ctx context.Context, in TenantInput) (*Tenant, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil || in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("property_id dan name wajib")
	}
	if err := requirePropertyPerm(ctx, "property.tenants.create", *in.PropertyID); err != nil {
		return nil, err
	}
	tt := "company"
	if in.TenantType != nil {
		tt = *in.TenantType
	}
	if tt != "company" && tt != "individual" {
		return nil, apperr.Validation("tenant_type harus company|individual")
	}
	var out *Tenant
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		code, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixTenant)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO tenants (organization_id, property_id, tenant_code, name, tenant_type, contact_name, contact_phone, contact_email, notes, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10) RETURNING id`,
			p.OrganizationID, *in.PropertyID, code, strings.TrimSpace(*in.Name), tt, in.ContactName, in.ContactPhone, in.ContactEmail, in.Notes, p.UserID).Scan(&id); err != nil {
			return err
		}
		if in.UnitIDs != nil {
			if err := s.setTenantUnits(ctx, tx, id, *in.PropertyID, *in.UnitIDs); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "tenant", EntityID: &id, EntityLabel: code, After: in})
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "tenant", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"name": *in.Name}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.TenantCreated, OrganizationID: p.OrganizationID, PropertyID: in.PropertyID, ObjectType: "tenant", ObjectID: id, ObjectLabel: code, ActorUserID: &p.UserID})
		}
		out, err = s.getTenantTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) setTenantUnits(ctx context.Context, tx pgx.Tx, tenantID, propertyID uuid.UUID, unitIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE units SET tenant_id = NULL, occupancy_status = 'vacant' WHERE tenant_id = $1 AND NOT (location_id = ANY($2::uuid[]))`, tenantID, unitIDs); err != nil {
		return err
	}
	for _, uid := range unitIDs {
		tag, err := tx.Exec(ctx, `UPDATE units u SET tenant_id = $1, occupancy_status = 'occupied' FROM locations l WHERE l.id = u.location_id AND u.location_id = $2 AND l.property_id = $3`, tenantID, uid, propertyID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.Validation("unit tidak ditemukan di property ini: " + uid.String())
		}
	}
	return nil
}

func (s *Service) UpdateTenant(ctx context.Context, id uuid.UUID, in TenantInput, ifVersion *int) (*Tenant, error) {
	p := authctx.Must(ctx)
	var out *Tenant
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getTenantTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := requirePropertyPerm(ctx, "property.tenants.update", before.PropertyID); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		if in.Status != nil && *in.Status != "active" && *in.Status != "inactive" && *in.Status != "moved_out" {
			return apperr.Validation("status tidak valid")
		}
		if _, err := tx.Exec(ctx, `UPDATE tenants SET name = COALESCE(NULLIF($2,''), name), tenant_type = COALESCE(NULLIF($3,''), tenant_type), contact_name = COALESCE($4, contact_name),
			contact_phone = COALESCE($5, contact_phone), contact_email = COALESCE($6, contact_email), status = COALESCE(NULLIF($7,''), status), notes = COALESCE($8, notes), updated_by = $9 WHERE id = $1`,
			id, deref(in.Name), deref(in.TenantType), in.ContactName, in.ContactPhone, in.ContactEmail, deref(in.Status), in.Notes, p.UserID); err != nil {
			return err
		}
		if in.UnitIDs != nil {
			if err := s.setTenantUnits(ctx, tx, id, before.PropertyID, *in.UnitIDs); err != nil {
				return err
			}
		}
		if in.Status != nil && *in.Status == "moved_out" {
			_, _ = tx.Exec(ctx, `UPDATE units SET tenant_id = NULL, occupancy_status = 'vacant' WHERE tenant_id = $1`, id)
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "tenant", EntityID: &id, EntityLabel: before.TenantCode, Before: before, After: in})
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "tenant", ObjectID: id, Action: audit.ActUpdated})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.TenantUpdated, OrganizationID: p.OrganizationID, PropertyID: &before.PropertyID, ObjectType: "tenant", ObjectID: id, ObjectLabel: before.TenantCode, ActorUserID: &p.UserID})
		}
		out, err = s.getTenantTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) getTenantTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Tenant, error) {
	var t Tenant
	err := tx.QueryRow(ctx, `SELECT t.id, t.property_id, t.tenant_code, t.name, t.tenant_type, t.contact_name, t.contact_phone, t.contact_email, t.status, t.notes, t.created_at, t.updated_at, t.version,
		(SELECT count(*) FROM service_requests sr WHERE sr.tenant_id = t.id AND sr.status NOT IN ('closed','cancelled'))
		FROM tenants t WHERE t.id = $1 AND t.deleted_at IS NULL`, id).
		Scan(&t.ID, &t.PropertyID, &t.TenantCode, &t.Name, &t.TenantType, &t.ContactName, &t.ContactPhone, &t.ContactEmail, &t.Status, &t.Notes, &t.CreatedAt, &t.UpdatedAt, &t.Version, &t.OpenRequests)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Tenant")
		}
		return nil, err
	}
	t.Units = []TenantUnit{}
	rows, err := tx.Query(ctx, `SELECT u.location_id, u.unit_number FROM units u WHERE u.tenant_id = $1 ORDER BY u.unit_number`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tu TenantUnit
		if err := rows.Scan(&tu.LocationID, &tu.UnitNumber); err != nil {
			return nil, err
		}
		t.Units = append(t.Units, tu)
	}
	for i := range t.Units {
		t.Units[i].PathText = LocationPathText(ctx, tx, t.Units[i].LocationID)
	}
	return &t, rows.Err()
}

func (s *Service) GetTenant(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	var out *Tenant
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.getTenantTx(ctx, tx, id)
		if err != nil {
			return err
		}
		return requirePropertyPerm(ctx, "property.tenants.view", out.PropertyID)
	})
	return out, err
}

type TenantFilter struct {
	PropertyID *uuid.UUID
	Status     string
	Q          string
}

func (s *Service) ListTenants(ctx context.Context, f TenantFilter, page httpx.Page) ([]Tenant, *string, error) {
	p := authctx.Must(ctx)
	var out []Tenant
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		where := "WHERE t.deleted_at IS NULL"
		if f.PropertyID != nil {
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND t.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("property.tenants.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND t.property_id = ANY($%d::uuid[])", len(args))
		}
		if f.Status != "" {
			args = append(args, f.Status)
			where += fmt.Sprintf(" AND t.status = $%d", len(args))
		}
		if f.Q != "" {
			args = append(args, "%"+f.Q+"%")
			where += fmt.Sprintf(" AND (t.name ILIKE $%d OR t.tenant_code ILIKE $%d OR t.contact_name ILIKE $%d)", len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (t.name, t.id) > ($%d, $%d)", len(args)-1, len(args))
		}
		args = append(args, page.Limit+1)
		rows, err := tx.Query(ctx, `SELECT t.id FROM tenants t `+where+fmt.Sprintf(` ORDER BY t.name, t.id LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		var idList []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			idList = append(idList, id)
		}
		rows.Close()
		for _, id := range idList {
			t, err := s.getTenantTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *t)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			out = out[:page.Limit]
			c := httpx.EncodeCursor(last.Name, last.ID)
			next = &c
		}
		return nil
	})
	return out, next, err
}

// ---------- Occupant ----------

type Occupant struct {
	ID               uuid.UUID   `json:"id"`
	TenantID         *uuid.UUID  `json:"tenant_id"`
	FullName         string      `json:"full_name"`
	Phone            *string     `json:"phone"`
	Email            *string     `json:"email"`
	IsPrimaryContact bool        `json:"is_primary_contact"`
	Status           string      `json:"status"`
	UnitIDs          []uuid.UUID `json:"unit_ids"`
	Version          int         `json:"version"`
}

type OccupantInput struct {
	TenantID         *uuid.UUID   `json:"tenant_id"`
	FullName         *string      `json:"full_name"`
	Phone            *string      `json:"phone"`
	Email            *string      `json:"email"`
	IsPrimaryContact *bool        `json:"is_primary_contact"`
	Status           *string      `json:"status"`
	UnitIDs          *[]uuid.UUID `json:"unit_ids"`
}

func (s *Service) CreateOccupant(ctx context.Context, in OccupantInput) (*Occupant, error) {
	p := authctx.Must(ctx)
	if in.FullName == nil || strings.TrimSpace(*in.FullName) == "" {
		return nil, apperr.Validation("full_name wajib")
	}
	if !p.Has("property.occupants.create") {
		return nil, apperr.Forbidden("")
	}
	var out *Occupant
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO occupants (organization_id, tenant_id, full_name, phone, email, is_primary_contact, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,COALESCE($6,false),$7,$7) RETURNING id`,
			p.OrganizationID, in.TenantID, strings.TrimSpace(*in.FullName), in.Phone, in.Email, in.IsPrimaryContact, p.UserID).Scan(&id); err != nil {
			return err
		}
		if in.UnitIDs != nil {
			if err := setOccupantUnits(ctx, tx, id, *in.UnitIDs); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "occupant", EntityID: &id, EntityLabel: *in.FullName, After: in})
		var err error
		out, err = getOccupantTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateOccupant(ctx context.Context, id uuid.UUID, in OccupantInput) (*Occupant, error) {
	p := authctx.Must(ctx)
	if !p.Has("property.occupants.update") {
		return nil, apperr.Forbidden("")
	}
	var out *Occupant
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE occupants SET full_name = COALESCE(NULLIF($2,''), full_name), phone = COALESCE($3, phone), email = COALESCE($4, email), is_primary_contact = COALESCE($5, is_primary_contact), status = COALESCE(NULLIF($6,''), status), tenant_id = COALESCE($7, tenant_id), updated_by = $8 WHERE id = $1 AND deleted_at IS NULL`,
			id, deref(in.FullName), in.Phone, in.Email, in.IsPrimaryContact, deref(in.Status), in.TenantID, p.UserID); err != nil {
			return err
		}
		if in.UnitIDs != nil {
			if err := setOccupantUnits(ctx, tx, id, *in.UnitIDs); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "occupant", EntityID: &id, After: in})
		var err error
		out, err = getOccupantTx(ctx, tx, id)
		return err
	})
	return out, err
}

func setOccupantUnits(ctx context.Context, tx pgx.Tx, occID uuid.UUID, unitIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE unit_occupants SET moved_out_at = CURRENT_DATE WHERE occupant_id = $1 AND moved_out_at IS NULL AND NOT (unit_location_id = ANY($2::uuid[]))`, occID, unitIDs); err != nil {
		return err
	}
	for _, u := range unitIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO unit_occupants (unit_location_id, occupant_id, moved_in_at) VALUES ($1,$2,CURRENT_DATE) ON CONFLICT (unit_location_id, occupant_id) DO UPDATE SET moved_out_at = NULL`, u, occID); err != nil {
			return err
		}
	}
	return nil
}

func getOccupantTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Occupant, error) {
	var o Occupant
	if err := tx.QueryRow(ctx, `SELECT o.id, o.tenant_id, o.full_name, o.phone, o.email, o.is_primary_contact, o.status, o.version,
		COALESCE((SELECT array_agg(unit_location_id) FROM unit_occupants WHERE occupant_id = o.id AND moved_out_at IS NULL), '{}')
		FROM occupants o WHERE o.id = $1 AND o.deleted_at IS NULL`, id).
		Scan(&o.ID, &o.TenantID, &o.FullName, &o.Phone, &o.Email, &o.IsPrimaryContact, &o.Status, &o.Version, &o.UnitIDs); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Occupant")
		}
		return nil, err
	}
	if o.UnitIDs == nil {
		o.UnitIDs = []uuid.UUID{}
	}
	return &o, nil
}

func (s *Service) ListOccupants(ctx context.Context, tenantID *uuid.UUID, unitID *uuid.UUID, q string) ([]Occupant, error) {
	var out []Occupant
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT o.id FROM occupants o WHERE o.deleted_at IS NULL
			AND ($1::uuid IS NULL OR o.tenant_id = $1)
			AND ($2::uuid IS NULL OR EXISTS (SELECT 1 FROM unit_occupants uo WHERE uo.occupant_id = o.id AND uo.unit_location_id = $2 AND uo.moved_out_at IS NULL))
			AND ($3 = '' OR o.full_name ILIKE '%' || $3 || '%') ORDER BY o.full_name LIMIT 500`, tenantID, unitID, q)
		if err != nil {
			return err
		}
		var idList []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			idList = append(idList, id)
		}
		rows.Close()
		for _, id := range idList {
			o, err := getOccupantTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *o)
		}
		return nil
	})
	return out, err
}
