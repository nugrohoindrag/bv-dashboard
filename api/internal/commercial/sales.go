package commercial

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/profile"
)

// ============ UNIT LISTING (sales inventory) ============

type Listing struct {
	ID              uuid.UUID  `json:"id"`
	ListingCode     string     `json:"listing_code"`
	PropertyID      uuid.UUID  `json:"property_id"`
	UnitLocationID  uuid.UUID  `json:"unit_location_id"`
	UnitNumber      string     `json:"unit_number"`
	UnitName        string     `json:"unit_name"`
	FloorName       *string    `json:"floor_name"`
	OccupancyStatus string     `json:"unit_occupancy_status"`
	Title           string     `json:"title"`
	Description     *string    `json:"description"`
	AskingPrice     int64      `json:"asking_price"`
	CurrencyCode    string     `json:"currency_code"`
	PriceNegotiable bool       `json:"price_negotiable"`
	Bedrooms        *int       `json:"bedrooms"`
	Bathrooms       *int       `json:"bathrooms"`
	AreaM2          *float64   `json:"area_m2"`
	Furnishing      *string    `json:"furnishing"`
	Features        []string   `json:"features"`
	Status          string     `json:"status"`
	Documents       []Document `json:"documents"`
	PublishedAt     *time.Time `json:"published_at"`
	SoldAt          *time.Time `json:"sold_at"`
	ArchivedAt      *time.Time `json:"archived_at"`
	LeadCount       int        `json:"lead_count"`
	ActiveResID     *uuid.UUID `json:"active_reservation_id"`
	AllowedActions  []string   `json:"allowed_actions"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Version         int        `json:"version"`
}

const listingSelect = `SELECT x.id, x.listing_code, x.property_id, x.unit_location_id, u.unit_number, l.name, pl.name, u.occupancy_status, x.title, x.description, x.asking_price, x.currency_code, x.price_negotiable,
	x.bedrooms, x.bathrooms, x.area_m2, x.furnishing, x.features, x.status, x.documents, x.published_at, x.sold_at, x.archived_at,
	(SELECT count(*) FROM unit_sales_leads sl WHERE sl.listing_id = x.id AND sl.status NOT IN ('lost','cancelled')),
	(SELECT id FROM unit_sale_reservations sr WHERE sr.listing_id = x.id AND sr.status IN ('reserved','contract_signed','sold') LIMIT 1),
	x.created_at, x.updated_at, x.version
	FROM unit_listings x JOIN units u ON u.location_id = x.unit_location_id JOIN locations l ON l.id = u.location_id LEFT JOIN locations pl ON pl.id = l.parent_id`

func scanListing(row pgx.Row) (*Listing, error) {
	var x Listing
	var docs []byte
	if err := row.Scan(&x.ID, &x.ListingCode, &x.PropertyID, &x.UnitLocationID, &x.UnitNumber, &x.UnitName, &x.FloorName, &x.OccupancyStatus, &x.Title, &x.Description, &x.AskingPrice, &x.CurrencyCode, &x.PriceNegotiable,
		&x.Bedrooms, &x.Bathrooms, &x.AreaM2, &x.Furnishing, &x.Features, &x.Status, &docs, &x.PublishedAt, &x.SoldAt, &x.ArchivedAt, &x.LeadCount, &x.ActiveResID, &x.CreatedAt, &x.UpdatedAt, &x.Version); err != nil {
		return nil, err
	}
	x.CurrencyCode = strings.TrimSpace(x.CurrencyCode)
	x.Documents = []Document{}
	_ = json.Unmarshal(docs, &x.Documents)
	if x.Features == nil {
		x.Features = []string{}
	}
	switch x.Status {
	case "draft":
		x.AllowedActions = []string{"publish", "archive"}
	case "published":
		x.AllowedActions = []string{"unpublish", "archive"}
	case "reserved", "sold":
		x.AllowedActions = []string{}
	case "archived":
		x.AllowedActions = []string{"publish"}
	}
	return &x, nil
}

type ListingInput struct {
	PropertyID      *uuid.UUID  `json:"property_id"`
	UnitLocationID  *uuid.UUID  `json:"unit_location_id"`
	Title           *string     `json:"title"`
	Description     *string     `json:"description"`
	AskingPrice     *int64      `json:"asking_price"`
	PriceNegotiable *bool       `json:"price_negotiable"`
	Bedrooms        *int        `json:"bedrooms"`
	Bathrooms       *int        `json:"bathrooms"`
	AreaM2          *float64    `json:"area_m2"`
	Furnishing      *string     `json:"furnishing"`
	Features        *[]string   `json:"features"`
	Documents       *[]Document `json:"documents"`
}

var furnishings = map[string]bool{"unfurnished": true, "semi_furnished": true, "furnished": true}

type ListingFilter struct {
	PropertyID *uuid.UUID
	Statuses   []string
	Q          string
}

func (s *Service) ListListings(ctx context.Context, f ListingFilter, page httpx.Page) ([]Listing, *string, error) {
	p := authctx.Must(ctx)
	out := []Listing{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args, err := s.scopeWhere(ctx, p, "commercial.unit_listings.view", f.PropertyID, "x.property_id")
		if err != nil {
			return err
		}
		if f.PropertyID != nil {
			if err := s.requireSales(ctx, tx, *f.PropertyID); err != nil {
				return err
			}
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND x.status = ANY($%d)", len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (x.listing_code ILIKE $%d OR x.title ILIKE $%d OR u.unit_number ILIKE $%d)", len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (x.created_at, x.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, listingSelect+where+fmt.Sprintf(" ORDER BY x.created_at DESC, x.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanListing(rows)
			if err != nil {
				return err
			}
			out = append(out, *x)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	return out, next, err
}

// scopeWhere: filter property eksplisit (dengan cek izin) atau seluruh property yang diizinkan (authz server-side).
func (s *Service) scopeWhere(ctx context.Context, p *authctx.Principal, perm string, propertyID *uuid.UUID, col string) (string, []any, error) {
	where := " WHERE true"
	var args []any
	if propertyID != nil {
		if err := iam.CanOnProperty(ctx, perm, *propertyID); err != nil {
			return "", nil, err
		}
		args = append(args, *propertyID)
		where += fmt.Sprintf(" AND %s = $%d", col, len(args))
	} else if pids, all := p.PropertyIDsFor(perm); !all {
		args = append(args, pids)
		where += fmt.Sprintf(" AND %s = ANY($%d)", col, len(args))
	}
	return where, args, nil
}

func (s *Service) getListingTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Listing, error) {
	x, err := scanListing(tx.QueryRow(ctx, listingSelect+` WHERE x.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Unit listing")
		}
		return nil, err
	}
	return x, nil
}

func (s *Service) GetListing(ctx context.Context, id uuid.UUID) (*Listing, error) {
	var out *Listing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getListingTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.unit_listings.view", x.PropertyID); err != nil {
			return err
		}
		out = x
		return nil
	})
	return out, err
}

func (s *Service) CreateListing(ctx context.Context, in ListingInput) (*Listing, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil || in.UnitLocationID == nil {
		return nil, apperr.Validation("property_id dan unit_location_id wajib")
	}
	if err := iam.CanOnProperty(ctx, "commercial.unit_listings.create", *in.PropertyID); err != nil {
		return nil, err
	}
	if in.AskingPrice == nil || *in.AskingPrice <= 0 {
		return nil, apperr.Validation("asking_price wajib > 0").WithField("asking_price", "wajib")
	}
	if in.Furnishing != nil && !furnishings[*in.Furnishing] {
		return nil, apperr.Validation("furnishing harus unfurnished|semi_furnished|furnished")
	}
	var out *Listing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireSales(ctx, tx, *in.PropertyID); err != nil {
			return err
		}
		u, err := s.unitTx(ctx, tx, *in.UnitLocationID)
		if err != nil {
			return err
		}
		if u.PropertyID != *in.PropertyID {
			return apperr.Validation("Unit bukan milik property ini").WithField("unit_location_id", "bukan milik property")
		}
		if u.UnitType == "hotel_room" {
			return apperr.Validation("Kamar hotel tidak dapat dijual sebagai unit")
		}
		title := strings.TrimSpace(deref(in.Title))
		if title == "" {
			title = "Unit " + u.UnitNumber
		}
		docs := []Document{}
		if in.Documents != nil {
			if docs, err = normalizeDocs(*in.Documents); err != nil {
				return err
			}
		}
		feats := []string{}
		if in.Features != nil {
			feats = *in.Features
		}
		loc := property.PropertyTimezone(ctx, tx, *in.PropertyID)
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixUnitListing, time.Now(), loc)
		if err != nil {
			return err
		}
		area := in.AreaM2
		if area == nil {
			area = u.AreaM2
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO unit_listings (organization_id, property_id, listing_code, unit_location_id, title, description, asking_price, price_negotiable, bedrooms, bathrooms, area_m2, furnishing, features, documents, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE($8,false),$9,$10,$11,$12,$13,$14,$15,$15) RETURNING id`,
			p.OrganizationID, *in.PropertyID, code, u.LocationID, title, nilIfEmpty(in.Description), *in.AskingPrice, in.PriceNegotiable, in.Bedrooms, in.Bathrooms, area, in.Furnishing, feats, mustJSON(docs), p.UserID).Scan(&id); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("LISTING_EXISTS", "Unit "+u.UnitNumber+" sudah memiliki listing penjualan aktif")
			}
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "unit_listing", EntityID: &id, EntityLabel: code + " " + title})
		out, err = s.getListingTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateListing(ctx context.Context, id uuid.UUID, in ListingInput, ifVersion *int) (*Listing, error) {
	p := authctx.Must(ctx)
	var out *Listing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getListingTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.unit_listings.update", x.PropertyID); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != x.Version {
			return apperr.StaleVersion()
		}
		if x.Status == "sold" {
			return apperr.InvalidTransition("Listing yang sudah terjual tidak dapat diubah")
		}
		if in.Furnishing != nil && !furnishings[*in.Furnishing] {
			return apperr.Validation("furnishing harus unfurnished|semi_furnished|furnished")
		}
		if in.AskingPrice != nil && *in.AskingPrice <= 0 {
			return apperr.Validation("asking_price harus > 0")
		}
		var docs any
		if in.Documents != nil {
			d, err := normalizeDocs(*in.Documents)
			if err != nil {
				return err
			}
			docs = mustJSON(d)
		}
		var feats any
		if in.Features != nil {
			feats = *in.Features
		}
		if _, err := tx.Exec(ctx, `UPDATE unit_listings SET title = COALESCE(NULLIF(TRIM($2),''), title), description = COALESCE($3, description), asking_price = COALESCE($4, asking_price), price_negotiable = COALESCE($5, price_negotiable),
			bedrooms = COALESCE($6, bedrooms), bathrooms = COALESCE($7, bathrooms), area_m2 = COALESCE($8, area_m2), furnishing = COALESCE($9, furnishing), features = COALESCE($10, features), documents = COALESCE($11, documents), updated_by = $12 WHERE id = $1`,
			id, deref(in.Title), in.Description, in.AskingPrice, in.PriceNegotiable, in.Bedrooms, in.Bathrooms, in.AreaM2, in.Furnishing, feats, docs, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "unit_listing", EntityID: &id, EntityLabel: x.ListingCode, After: in})
		out, err = s.getListingTx(ctx, tx, id)
		return err
	})
	return out, err
}

// ListingAct: publish | unpublish | archive (unit availability/status).
func (s *Service) ListingAct(ctx context.Context, id uuid.UUID, action string) (*Listing, error) {
	p := authctx.Must(ctx)
	var out *Listing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getListingTx(ctx, tx, id)
		if err != nil {
			return err
		}
		perm := map[string]string{"publish": "commercial.unit_listings.publish", "unpublish": "commercial.unit_listings.publish", "archive": "commercial.unit_listings.archive"}[action]
		if perm == "" {
			return apperr.Validation("aksi tidak dikenal")
		}
		if err := iam.CanOnProperty(ctx, perm, x.PropertyID); err != nil {
			return err
		}
		if !has(x.AllowedActions, action) {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk listing berstatus %s", action, x.Status))
		}
		if err := s.requireSales(ctx, tx, x.PropertyID); err != nil {
			return err
		}
		var to string
		switch action {
		case "publish":
			to = "published"
			if x.OccupancyStatus == "occupied" && !p.HasOnProperty("commercial.unit_listings.archive", x.PropertyID) {
				return apperr.Conflict("UNIT_OCCUPIED", "Unit sedang ditempati; listing hanya dapat dipublikasikan oleh manajer")
			}
			if _, err := tx.Exec(ctx, `UPDATE unit_listings SET status = 'published', published_at = COALESCE(published_at, now()), archived_at = NULL, updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("LISTING_EXISTS", "Unit sudah memiliki listing aktif lain")
				}
				return err
			}
		case "unpublish":
			to = "draft"
			if _, err := tx.Exec(ctx, `UPDATE unit_listings SET status = 'draft', updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
		case "archive":
			to = "archived"
			if _, err := tx.Exec(ctx, `UPDATE unit_listings SET status = 'archived', archived_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "unit_listing", ObjectID: id, Action: audit.ActStatusChanged, From: x.Status, To: to})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "unit_listing", EntityID: &id, EntityLabel: x.ListingCode, Before: map[string]any{"status": x.Status}, After: map[string]any{"status": to}})
		if action == "publish" && s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventListingPublished, OrganizationID: p.OrganizationID, PropertyID: &x.PropertyID, ObjectType: "unit_listing", ObjectID: id, ObjectLabel: x.ListingCode + " · " + x.Title, ActorUserID: &p.UserID, Payload: map[string]any{"domain": "management"}})
		}
		out, err = s.getListingTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) setListingStatusTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, by uuid.UUID) error {
	extra := ""
	if status == "sold" {
		extra = ", sold_at = now()"
	}
	_, err := tx.Exec(ctx, `UPDATE unit_listings SET status = $2`+extra+`, updated_by = $3 WHERE id = $1`, id, status, by)
	return err
}

// ============ SALES LEAD (prospective buyer + inquiry + pipeline) ============

type Lead struct {
	ID             uuid.UUID  `json:"id"`
	LeadCode       string     `json:"lead_code"`
	PropertyID     uuid.UUID  `json:"property_id"`
	ListingID      *uuid.UUID `json:"listing_id"`
	ListingCode    *string    `json:"listing_code"`
	ListingTitle   *string    `json:"listing_title"`
	UnitNumber     *string    `json:"unit_number"`
	FullName       string     `json:"full_name"`
	Phone          *string    `json:"phone"`
	Email          *string    `json:"email"`
	Company        *string    `json:"company"`
	Source         string     `json:"source"`
	BudgetMin      *int64     `json:"budget_min"`
	BudgetMax      *int64     `json:"budget_max"`
	Inquiry        *string    `json:"inquiry"`
	Status         string     `json:"status"`
	AssignedTo     *uuid.UUID `json:"assigned_to"`
	AssignedName   *string    `json:"assigned_name"`
	NextFollowUpAt *time.Time `json:"next_follow_up_at"`
	LastActivityAt *time.Time `json:"last_activity_at"`
	LostReason     *string    `json:"lost_reason"`
	Notes          *string    `json:"notes"`
	ReservationID  *uuid.UUID `json:"reservation_id"`
	AllowedActions []string   `json:"allowed_actions"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Version        int        `json:"version"`
}

const leadSelect = `SELECT x.id, x.lead_code, x.property_id, x.listing_id, li.listing_code, li.title, u.unit_number, x.full_name, x.phone, x.email, x.company, x.source, x.budget_min, x.budget_max, x.inquiry, x.status,
	x.assigned_to, au.full_name, x.next_follow_up_at, x.last_activity_at, x.lost_reason, x.notes,
	(SELECT id FROM unit_sale_reservations sr WHERE sr.lead_id = x.id AND sr.status <> 'cancelled' ORDER BY sr.created_at DESC LIMIT 1),
	x.created_at, x.updated_at, x.version
	FROM unit_sales_leads x LEFT JOIN unit_listings li ON li.id = x.listing_id LEFT JOIN units u ON u.location_id = li.unit_location_id LEFT JOIN users au ON au.id = x.assigned_to`

func scanLead(row pgx.Row) (*Lead, error) {
	var x Lead
	if err := row.Scan(&x.ID, &x.LeadCode, &x.PropertyID, &x.ListingID, &x.ListingCode, &x.ListingTitle, &x.UnitNumber, &x.FullName, &x.Phone, &x.Email, &x.Company, &x.Source, &x.BudgetMin, &x.BudgetMax, &x.Inquiry, &x.Status,
		&x.AssignedTo, &x.AssignedName, &x.NextFollowUpAt, &x.LastActivityAt, &x.LostReason, &x.Notes, &x.ReservationID, &x.CreatedAt, &x.UpdatedAt, &x.Version); err != nil {
		return nil, err
	}
	x.AllowedActions = leadActions(x.Status)
	return &x, nil
}

// leadActions: pipeline New → Contacted → Qualified → Reserved (via Unit Reservation) → Sold; Lost/Cancelled; reopen dari lost/cancelled.
func leadActions(status string) []string {
	switch status {
	case "new":
		return []string{"contact", "qualify", "lose", "cancel"}
	case "contacted":
		return []string{"qualify", "lose", "cancel"}
	case "qualified":
		return []string{"reserve", "lose", "cancel"}
	case "reserved", "sold":
		return []string{}
	case "lost", "cancelled":
		return []string{"reopen"}
	}
	return []string{}
}

var leadSources = map[string]bool{"walk_in": true, "phone": true, "email": true, "website": true, "referral": true, "agent": true, "other": true}

type LeadInput struct {
	PropertyID     *uuid.UUID `json:"property_id"`
	ListingID      *uuid.UUID `json:"listing_id"`
	FullName       *string    `json:"full_name"`
	Phone          *string    `json:"phone"`
	Email          *string    `json:"email"`
	Company        *string    `json:"company"`
	Source         *string    `json:"source"`
	BudgetMin      *int64     `json:"budget_min"`
	BudgetMax      *int64     `json:"budget_max"`
	Inquiry        *string    `json:"inquiry"`
	AssignedTo     *uuid.UUID `json:"assigned_to"`
	NextFollowUpAt *time.Time `json:"next_follow_up_at"`
	Notes          *string    `json:"notes"`
}

type LeadFilter struct {
	PropertyID *uuid.UUID
	ListingID  *uuid.UUID
	Statuses   []string
	AssignedTo *uuid.UUID
	Q          string
}

func (s *Service) ListLeads(ctx context.Context, f LeadFilter, page httpx.Page) ([]Lead, *string, error) {
	p := authctx.Must(ctx)
	out := []Lead{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args, err := s.scopeWhere(ctx, p, "commercial.sales_leads.view", f.PropertyID, "x.property_id")
		if err != nil {
			return err
		}
		if f.PropertyID != nil {
			if err := s.requireSales(ctx, tx, *f.PropertyID); err != nil {
				return err
			}
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND x.status = ANY($%d)", len(args))
		}
		if f.ListingID != nil {
			args = append(args, *f.ListingID)
			where += fmt.Sprintf(" AND x.listing_id = $%d", len(args))
		}
		if f.AssignedTo != nil {
			args = append(args, *f.AssignedTo)
			where += fmt.Sprintf(" AND x.assigned_to = $%d", len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (x.lead_code ILIKE $%d OR x.full_name ILIKE $%d OR x.phone ILIKE $%d OR x.email ILIKE $%d)", len(args), len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (x.created_at, x.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, leadSelect+where+fmt.Sprintf(" ORDER BY x.created_at DESC, x.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanLead(rows)
			if err != nil {
				return err
			}
			out = append(out, *x)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	return out, next, err
}

func (s *Service) getLeadTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Lead, error) {
	x, err := scanLead(tx.QueryRow(ctx, leadSelect+` WHERE x.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Sales lead")
		}
		return nil, err
	}
	return x, nil
}

func (s *Service) GetLead(ctx context.Context, id uuid.UUID) (*Lead, error) {
	var out *Lead
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getLeadTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.sales_leads.view", x.PropertyID); err != nil {
			return err
		}
		out = x
		return nil
	})
	return out, err
}

func (s *Service) CreateLead(ctx context.Context, in LeadInput) (*Lead, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil {
		return nil, apperr.Validation("property_id wajib")
	}
	if err := iam.CanOnProperty(ctx, "commercial.sales_leads.create", *in.PropertyID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(deref(in.FullName))
	if name == "" {
		return nil, apperr.Validation("full_name wajib").WithField("full_name", "wajib")
	}
	src := deref(in.Source)
	if src == "" {
		src = "walk_in"
	}
	if !leadSources[src] {
		return nil, apperr.Validation("source tidak valid")
	}
	var out *Lead
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireSales(ctx, tx, *in.PropertyID); err != nil {
			return err
		}
		if in.ListingID != nil {
			li, err := s.getListingTx(ctx, tx, *in.ListingID)
			if err != nil {
				return err
			}
			if li.PropertyID != *in.PropertyID {
				return apperr.Validation("listing_id bukan milik property ini")
			}
		}
		loc := property.PropertyTimezone(ctx, tx, *in.PropertyID)
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixSalesLead, time.Now(), loc)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO unit_sales_leads (organization_id, property_id, lead_code, listing_id, full_name, phone, email, company, source, budget_min, budget_max, inquiry, assigned_to, next_follow_up_at, notes, last_activity_at, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,now(),$16,$16) RETURNING id`,
			p.OrganizationID, *in.PropertyID, code, in.ListingID, name, nilIfEmpty(in.Phone), nilIfEmpty(in.Email), nilIfEmpty(in.Company), src, in.BudgetMin, in.BudgetMax, nilIfEmpty(in.Inquiry), in.AssignedTo, in.NextFollowUpAt, nilIfEmpty(in.Notes), p.UserID).Scan(&id); err != nil {
			return err
		}
		if inq := nilIfEmpty(in.Inquiry); inq != nil {
			_ = s.addActivityTx(ctx, tx, p, id, "note", "Inquiry: "+*inq, nil)
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "unit_sales_lead", EntityID: &id, EntityLabel: code + " " + name})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventLeadCreated, OrganizationID: p.OrganizationID, PropertyID: in.PropertyID, ObjectType: "unit_sales_lead", ObjectID: id, ObjectLabel: code + " · " + name, ActorUserID: &p.UserID, Payload: map[string]any{"domain": "management", "assignee_user_id": uuidStr(in.AssignedTo)}})
		}
		out, err = s.getLeadTx(ctx, tx, id)
		return err
	})
	return out, err
}

func uuidStr(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func (s *Service) UpdateLead(ctx context.Context, id uuid.UUID, in LeadInput, ifVersion *int) (*Lead, error) {
	p := authctx.Must(ctx)
	var out *Lead
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getLeadTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.sales_leads.update", x.PropertyID); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != x.Version {
			return apperr.StaleVersion()
		}
		if in.Source != nil && !leadSources[*in.Source] {
			return apperr.Validation("source tidak valid")
		}
		if in.ListingID != nil {
			li, err := s.getListingTx(ctx, tx, *in.ListingID)
			if err != nil {
				return err
			}
			if li.PropertyID != x.PropertyID {
				return apperr.Validation("listing_id bukan milik property ini")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE unit_sales_leads SET listing_id = COALESCE($2, listing_id), full_name = COALESCE(NULLIF(TRIM($3),''), full_name), phone = COALESCE($4, phone), email = COALESCE($5, email), company = COALESCE($6, company),
			source = COALESCE($7, source), budget_min = COALESCE($8, budget_min), budget_max = COALESCE($9, budget_max), inquiry = COALESCE($10, inquiry), assigned_to = COALESCE($11, assigned_to), next_follow_up_at = COALESCE($12, next_follow_up_at), notes = COALESCE($13, notes), updated_by = $14 WHERE id = $1`,
			id, in.ListingID, deref(in.FullName), in.Phone, in.Email, in.Company, in.Source, in.BudgetMin, in.BudgetMax, in.Inquiry, in.AssignedTo, in.NextFollowUpAt, in.Notes, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "unit_sales_lead", EntityID: &id, EntityLabel: x.LeadCode, After: in})
		out, err = s.getLeadTx(ctx, tx, id)
		return err
	})
	return out, err
}

type LeadActionInput struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

// LeadAct: contact | qualify | lose | cancel | reopen (reserve = CreateSaleReservation).
func (s *Service) LeadAct(ctx context.Context, id uuid.UUID, action string, in LeadActionInput) (*Lead, error) {
	p := authctx.Must(ctx)
	var out *Lead
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getLeadTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.sales_leads.update", x.PropertyID); err != nil {
			return err
		}
		if action == "reserve" {
			return apperr.Validation("Gunakan POST /unit-sales/reservations untuk mereservasi unit bagi lead ini")
		}
		if !has(x.AllowedActions, action) {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk lead berstatus %s", action, x.Status))
		}
		to := map[string]string{"contact": "contacted", "qualify": "qualified", "lose": "lost", "cancel": "cancelled", "reopen": "new"}[action]
		if to == "" {
			return apperr.Validation("aksi tidak dikenal")
		}
		reason := strings.TrimSpace(in.Reason)
		if (action == "lose" || action == "cancel") && reason == "" {
			return apperr.Validation("reason wajib").WithField("reason", "wajib")
		}
		var lost any
		if action == "lose" || action == "cancel" {
			lost = reason
		}
		if _, err := tx.Exec(ctx, `UPDATE unit_sales_leads SET status = $2, lost_reason = CASE WHEN $2 IN ('lost','cancelled') THEN $3::text WHEN $2 = 'new' THEN NULL ELSE lost_reason END, last_activity_at = now(), updated_by = $4 WHERE id = $1`, id, to, lost, p.UserID); err != nil {
			return err
		}
		summary := fmt.Sprintf("Status %s → %s", x.Status, to)
		if reason != "" {
			summary += " · " + reason
		}
		if n := strings.TrimSpace(in.Note); n != "" {
			summary += " · " + n
		}
		_ = s.addActivityTx(ctx, tx, p, id, "status_change", summary, nil)
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "unit_sales_lead", ObjectID: id, Action: audit.ActStatusChanged, From: x.Status, To: to, Payload: map[string]any{"reason": reason}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "unit_sales_lead", EntityID: &id, EntityLabel: x.LeadCode, Before: map[string]any{"status": x.Status}, After: map[string]any{"status": to, "reason": reason}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventLeadStatusChanged, OrganizationID: p.OrganizationID, PropertyID: &x.PropertyID, ObjectType: "unit_sales_lead", ObjectID: id, ObjectLabel: x.LeadCode + " · " + x.FullName, ActorUserID: &p.UserID, Payload: map[string]any{"from": x.Status, "to": to, "reason": reason, "domain": "management", "assignee_user_id": uuidStr(x.AssignedTo)}})
		}
		out, err = s.getLeadTx(ctx, tx, id)
		return err
	})
	return out, err
}

// ---------- Sales activity / history ----------

type Activity struct {
	ID           uuid.UUID `json:"id"`
	LeadID       uuid.UUID `json:"lead_id"`
	ActivityType string    `json:"activity_type"`
	Summary      string    `json:"summary"`
	OccurredAt   time.Time `json:"occurred_at"`
	CreatedBy    *uuid.UUID `json:"created_by"`
	CreatedName  *string   `json:"created_by_name"`
	CreatedAt    time.Time `json:"created_at"`
}

type ActivityInput struct {
	ActivityType string     `json:"activity_type"`
	Summary      string     `json:"summary"`
	OccurredAt   *time.Time `json:"occurred_at"`
	NextFollowUp *time.Time `json:"next_follow_up_at"`
}

var activityTypes = map[string]bool{"note": true, "call": true, "meeting": true, "site_visit": true, "email": true, "whatsapp": true, "document": true}

func (s *Service) addActivityTx(ctx context.Context, tx pgx.Tx, p *authctx.Principal, leadID uuid.UUID, typ, summary string, at *time.Time) error {
	occurred := time.Now()
	if at != nil {
		occurred = *at
	}
	_, err := tx.Exec(ctx, `INSERT INTO unit_sales_lead_activities (organization_id, lead_id, activity_type, summary, occurred_at, created_by) VALUES ($1,$2,$3,$4,$5,$6)`, p.OrganizationID, leadID, typ, summary, occurred, p.UserID)
	return err
}

func (s *Service) ListActivities(ctx context.Context, leadID uuid.UUID) ([]Activity, error) {
	out := []Activity{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getLeadTx(ctx, tx, leadID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.sales_leads.view", x.PropertyID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT a.id, a.lead_id, a.activity_type, a.summary, a.occurred_at, a.created_by, u.full_name, a.created_at FROM unit_sales_lead_activities a LEFT JOIN users u ON u.id = a.created_by WHERE a.lead_id = $1 ORDER BY a.occurred_at DESC, a.created_at DESC`, leadID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Activity
			if err := rows.Scan(&a.ID, &a.LeadID, &a.ActivityType, &a.Summary, &a.OccurredAt, &a.CreatedBy, &a.CreatedName, &a.CreatedAt); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) AddActivity(ctx context.Context, leadID uuid.UUID, in ActivityInput) (*Lead, error) {
	p := authctx.Must(ctx)
	if !activityTypes[in.ActivityType] {
		return nil, apperr.Validation("activity_type harus note|call|meeting|site_visit|email|whatsapp|document")
	}
	in.Summary = strings.TrimSpace(in.Summary)
	if in.Summary == "" {
		return nil, apperr.Validation("summary wajib").WithField("summary", "wajib")
	}
	var out *Lead
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getLeadTx(ctx, tx, leadID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.sales_leads.update", x.PropertyID); err != nil {
			return err
		}
		if err := s.addActivityTx(ctx, tx, p, leadID, in.ActivityType, in.Summary, in.OccurredAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE unit_sales_leads SET last_activity_at = now(), next_follow_up_at = COALESCE($2, next_follow_up_at), status = CASE WHEN status = 'new' AND $3 IN ('call','meeting','site_visit','email','whatsapp') THEN 'contacted' ELSE status END, updated_by = $4 WHERE id = $1`,
			leadID, in.NextFollowUp, in.ActivityType, p.UserID); err != nil {
			return err
		}
		out, err = s.getLeadTx(ctx, tx, leadID)
		return err
	})
	return out, err
}

// ============ UNIT SALE RESERVATION (WF-P1-008 Unit Reservation → Sales Status → Handover) ============

type SaleReservation struct {
	ID                uuid.UUID  `json:"id"`
	ReservationNumber string     `json:"reservation_number"`
	PropertyID        uuid.UUID  `json:"property_id"`
	ListingID         uuid.UUID  `json:"listing_id"`
	ListingCode       string     `json:"listing_code"`
	ListingTitle      string     `json:"listing_title"`
	UnitLocationID    uuid.UUID  `json:"unit_location_id"`
	UnitNumber        string     `json:"unit_number"`
	LeadID            uuid.UUID  `json:"lead_id"`
	LeadCode          string     `json:"lead_code"`
	BuyerName         string     `json:"buyer_name"`
	BuyerPhone        *string    `json:"buyer_phone"`
	BuyerEmail        *string    `json:"buyer_email"`
	AgreedPrice       int64      `json:"agreed_price"`
	BookingFee        int64      `json:"booking_fee"`
	CurrencyCode      string     `json:"currency_code"`
	ReservedUntil     *time.Time `json:"reserved_until"`
	Status            string     `json:"status"`
	ContractReference *string    `json:"contract_reference"`
	Documents         []Document `json:"documents"`
	Notes             *string    `json:"notes"`
	InvoiceID         *uuid.UUID `json:"invoice_id"`
	TenantID          *uuid.UUID `json:"tenant_id"`
	OccupantID        *uuid.UUID `json:"occupant_id"`
	TenantUserID      *uuid.UUID `json:"tenant_user_id"`
	ContractSignedAt  *time.Time `json:"contract_signed_at"`
	SoldAt            *time.Time `json:"sold_at"`
	HandedOverAt      *time.Time `json:"handed_over_at"`
	CancelledAt       *time.Time `json:"cancelled_at"`
	CancelReason      *string    `json:"cancel_reason"`
	AllowedActions    []string   `json:"allowed_actions"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	Version           int        `json:"version"`
}

const saleResSelect = `SELECT x.id, x.reservation_number, x.property_id, x.listing_id, li.listing_code, li.title, x.unit_location_id, u.unit_number, x.lead_id, ld.lead_code, ld.full_name, ld.phone, ld.email,
	x.agreed_price, x.booking_fee, x.currency_code, x.reserved_until, x.status, x.contract_reference, x.documents, x.notes, x.invoice_id, x.tenant_id, x.occupant_id, x.tenant_user_id,
	x.contract_signed_at, x.sold_at, x.handed_over_at, x.cancelled_at, x.cancel_reason, x.created_at, x.updated_at, x.version
	FROM unit_sale_reservations x JOIN unit_listings li ON li.id = x.listing_id JOIN units u ON u.location_id = x.unit_location_id JOIN unit_sales_leads ld ON ld.id = x.lead_id`

func scanSaleRes(row pgx.Row) (*SaleReservation, error) {
	var x SaleReservation
	var docs []byte
	if err := row.Scan(&x.ID, &x.ReservationNumber, &x.PropertyID, &x.ListingID, &x.ListingCode, &x.ListingTitle, &x.UnitLocationID, &x.UnitNumber, &x.LeadID, &x.LeadCode, &x.BuyerName, &x.BuyerPhone, &x.BuyerEmail,
		&x.AgreedPrice, &x.BookingFee, &x.CurrencyCode, &x.ReservedUntil, &x.Status, &x.ContractReference, &docs, &x.Notes, &x.InvoiceID, &x.TenantID, &x.OccupantID, &x.TenantUserID,
		&x.ContractSignedAt, &x.SoldAt, &x.HandedOverAt, &x.CancelledAt, &x.CancelReason, &x.CreatedAt, &x.UpdatedAt, &x.Version); err != nil {
		return nil, err
	}
	x.CurrencyCode = strings.TrimSpace(x.CurrencyCode)
	x.Documents = []Document{}
	_ = json.Unmarshal(docs, &x.Documents)
	switch x.Status {
	case "reserved":
		x.AllowedActions = []string{"sign_contract", "complete", "cancel"}
	case "contract_signed":
		x.AllowedActions = []string{"complete", "cancel"}
	case "sold":
		x.AllowedActions = []string{"handover"}
	default:
		x.AllowedActions = []string{}
	}
	return &x, nil
}

type SaleReservationInput struct {
	LeadID            *uuid.UUID  `json:"lead_id"`
	ListingID         *uuid.UUID  `json:"listing_id"`
	AgreedPrice       *int64      `json:"agreed_price"`
	BookingFee        *int64      `json:"booking_fee"`
	ReservedUntil     *string     `json:"reserved_until"` // YYYY-MM-DD
	ContractReference *string     `json:"contract_reference"`
	Documents         *[]Document `json:"documents"`
	Notes             *string     `json:"notes"`
	IssueInvoice      *bool       `json:"issue_invoice"` // booking fee invoice (Billing aktif) — default true bila booking_fee > 0
}

type SaleResFilter struct {
	PropertyID *uuid.UUID
	Statuses   []string
	ListingID  *uuid.UUID
	LeadID     *uuid.UUID
	Q          string
}

func (s *Service) ListSaleReservations(ctx context.Context, f SaleResFilter, page httpx.Page) ([]SaleReservation, *string, error) {
	p := authctx.Must(ctx)
	out := []SaleReservation{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args, err := s.scopeWhere(ctx, p, "commercial.sale_reservations.view", f.PropertyID, "x.property_id")
		if err != nil {
			return err
		}
		if f.PropertyID != nil {
			if err := s.requireSales(ctx, tx, *f.PropertyID); err != nil {
				return err
			}
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND x.status = ANY($%d)", len(args))
		}
		if f.ListingID != nil {
			args = append(args, *f.ListingID)
			where += fmt.Sprintf(" AND x.listing_id = $%d", len(args))
		}
		if f.LeadID != nil {
			args = append(args, *f.LeadID)
			where += fmt.Sprintf(" AND x.lead_id = $%d", len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (x.reservation_number ILIKE $%d OR ld.full_name ILIKE $%d OR u.unit_number ILIKE $%d)", len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (x.created_at, x.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, saleResSelect+where+fmt.Sprintf(" ORDER BY x.created_at DESC, x.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanSaleRes(rows)
			if err != nil {
				return err
			}
			out = append(out, *x)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	return out, next, err
}

func (s *Service) getSaleResTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*SaleReservation, error) {
	x, err := scanSaleRes(tx.QueryRow(ctx, saleResSelect+` WHERE x.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Unit sale reservation")
		}
		return nil, err
	}
	return x, nil
}

func (s *Service) GetSaleReservation(ctx context.Context, id uuid.UUID) (*SaleReservation, error) {
	var out *SaleReservation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getSaleResTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.sale_reservations.view", x.PropertyID); err != nil {
			return err
		}
		out = x
		return nil
	})
	return out, err
}

// CreateSaleReservation: lead qualified/contacted/new + listing published → reservasi; listing → reserved; unit → reserved; lead → reserved.
func (s *Service) CreateSaleReservation(ctx context.Context, in SaleReservationInput) (*SaleReservation, error) {
	p := authctx.Must(ctx)
	if in.LeadID == nil || in.ListingID == nil {
		return nil, apperr.Validation("lead_id dan listing_id wajib")
	}
	var out *SaleReservation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		li, err := s.getListingTx(ctx, tx, *in.ListingID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.sale_reservations.create", li.PropertyID); err != nil {
			return err
		}
		if err := s.requireSales(ctx, tx, li.PropertyID); err != nil {
			return err
		}
		ld, err := s.getLeadTx(ctx, tx, *in.LeadID)
		if err != nil {
			return err
		}
		if ld.PropertyID != li.PropertyID {
			return apperr.Validation("Lead dan listing harus pada property yang sama")
		}
		if ld.Status == "reserved" || ld.Status == "sold" {
			return apperr.InvalidTransition("Lead sudah memiliki reservasi/penjualan aktif")
		}
		if ld.Status == "lost" || ld.Status == "cancelled" {
			return apperr.InvalidTransition("Lead berstatus " + ld.Status + "; buka kembali (reopen) terlebih dahulu")
		}
		if li.Status != "published" {
			return apperr.Conflict("UNIT_UNAVAILABLE", "Listing berstatus "+li.Status+"; hanya listing published yang dapat direservasi")
		}
		price := li.AskingPrice
		if in.AgreedPrice != nil {
			if *in.AgreedPrice <= 0 {
				return apperr.Validation("agreed_price harus > 0")
			}
			price = *in.AgreedPrice
		}
		fee := int64(0)
		if in.BookingFee != nil {
			if *in.BookingFee < 0 {
				return apperr.Validation("booking_fee tidak boleh negatif")
			}
			fee = *in.BookingFee
		}
		var until *time.Time
		if v := nilIfEmpty(in.ReservedUntil); v != nil {
			t, err := parseDate(*v, "reserved_until")
			if err != nil {
				return err
			}
			until = &t
		}
		docs := []Document{}
		if in.Documents != nil {
			if docs, err = normalizeDocs(*in.Documents); err != nil {
				return err
			}
		}
		loc := property.PropertyTimezone(ctx, tx, li.PropertyID)
		num, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixSaleReservation, time.Now(), loc)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO unit_sale_reservations (organization_id, property_id, reservation_number, listing_id, unit_location_id, lead_id, agreed_price, booking_fee, currency_code, reserved_until, contract_reference, documents, notes, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14) RETURNING id`,
			p.OrganizationID, li.PropertyID, num, li.ID, li.UnitLocationID, ld.ID, price, fee, li.CurrencyCode, until, nilIfEmpty(in.ContractReference), mustJSON(docs), nilIfEmpty(in.Notes), p.UserID).Scan(&id); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("UNIT_UNAVAILABLE", "Unit "+li.UnitNumber+" sudah direservasi")
			}
			return err
		}
		if err := s.setListingStatusTx(ctx, tx, li.ID, "reserved", p.UserID); err != nil {
			return err
		}
		if err := s.setUnitOccupancyTx(ctx, tx, li.UnitLocationID, "reserved", nil, true); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE unit_sales_leads SET status = 'reserved', listing_id = $2, last_activity_at = now(), updated_by = $3 WHERE id = $1`, ld.ID, li.ID, p.UserID); err != nil {
			return err
		}
		_ = s.addActivityTx(ctx, tx, p, ld.ID, "reservation", fmt.Sprintf("Reservasi unit %s (%s) · %s", li.UnitNumber, num, formatIDR(price)), nil)
		// Billing linkage: invoice booking fee (opsional)
		issue := in.IssueInvoice == nil || *in.IssueInvoice
		if issue && fee > 0 && s.Billing != nil && p.HasOnProperty("billing.invoices.create", li.PropertyID) {
			pc, _ := s.Profile.ResolveTx(ctx, tx, li.PropertyID)
			if pc != nil && pc.Has(profile.CapBilling) {
				due := time.Now().Add(7 * 24 * time.Hour)
				if until != nil {
					due = *until
				}
				items := []billing.Item{{Description: fmt.Sprintf("Booking fee unit %s · %s", li.UnitNumber, ld.FullName), Quantity: 1, UnitPrice: fee, Amount: fee}}
				desc := "Reservasi penjualan " + num + " · " + ld.FullName
				invID, err := s.Billing.CreateTx(ctx, tx, billing.InvoiceInput{PropertyID: &li.PropertyID, UnitLocationID: &li.UnitLocationID, InvoiceType: strPtr("deposit"), Description: &desc, DueAt: &due, Items: &items, IssueNow: true}, "unit_sale", &id)
				if err != nil {
					return err
				}
				_, _ = tx.Exec(ctx, `UPDATE unit_sale_reservations SET invoice_id = $2 WHERE id = $1`, id, invID)
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "unit_sale_reservation", EntityID: &id, EntityLabel: num + " " + li.UnitNumber, After: map[string]any{"lead": ld.LeadCode, "price": price}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventSaleReservationCreated, OrganizationID: p.OrganizationID, PropertyID: &li.PropertyID, ObjectType: "unit_sale_reservation", ObjectID: id, ObjectLabel: num + " · Unit " + li.UnitNumber + " · " + ld.FullName, ActorUserID: &p.UserID, Payload: map[string]any{"domain": "management"}})
		}
		out, err = s.getSaleResTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateSaleReservation(ctx context.Context, id uuid.UUID, in SaleReservationInput, ifVersion *int) (*SaleReservation, error) {
	p := authctx.Must(ctx)
	var out *SaleReservation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getSaleResTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.sale_reservations.update", x.PropertyID); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != x.Version {
			return apperr.StaleVersion()
		}
		if x.Status == "cancelled" || x.Status == "handed_over" {
			return apperr.InvalidTransition("Reservasi berstatus " + x.Status + " tidak dapat diubah")
		}
		if in.AgreedPrice != nil && *in.AgreedPrice <= 0 {
			return apperr.Validation("agreed_price harus > 0")
		}
		var until any
		if v := nilIfEmpty(in.ReservedUntil); v != nil {
			t, err := parseDate(*v, "reserved_until")
			if err != nil {
				return err
			}
			until = t
		}
		var docs any
		if in.Documents != nil {
			d, err := normalizeDocs(*in.Documents)
			if err != nil {
				return err
			}
			docs = mustJSON(d)
		}
		if _, err := tx.Exec(ctx, `UPDATE unit_sale_reservations SET agreed_price = COALESCE($2, agreed_price), booking_fee = COALESCE($3, booking_fee), reserved_until = COALESCE($4, reserved_until), contract_reference = COALESCE($5, contract_reference), documents = COALESCE($6, documents), notes = COALESCE($7, notes), updated_by = $8 WHERE id = $1`,
			id, in.AgreedPrice, in.BookingFee, until, in.ContractReference, docs, in.Notes, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "unit_sale_reservation", EntityID: &id, EntityLabel: x.ReservationNumber, After: in})
		out, err = s.getSaleResTx(ctx, tx, id)
		return err
	})
	return out, err
}

type SaleActionInput struct {
	Reason            string  `json:"reason"`
	ContractReference *string `json:"contract_reference"`
	// handover: Tenant Onboarding pemilik (Tenant/Occupant bersama) + akun Tenant App opsional
	OwnerName     *string `json:"owner_name"`
	OwnerPhone    *string `json:"owner_phone"`
	OwnerEmail    *string `json:"owner_email"`
	Company       *string `json:"company"`
	TenantType    *string `json:"tenant_type"` // individual | company
	CreateAccount bool    `json:"create_tenant_account"`
	Password      string  `json:"password"`
	HandoverDate  *string `json:"handover_date"` // YYYY-MM-DD (default hari ini)
}

type SaleActionResult struct {
	Reservation *SaleReservation  `json:"reservation"`
	Onboarding  *OnboardingResult `json:"onboarding,omitempty"`
}

// SaleAct: sign_contract | complete (Sold) | handover | cancel — sales status per WF-P1-008.
func (s *Service) SaleAct(ctx context.Context, id uuid.UUID, action string, in SaleActionInput) (*SaleActionResult, error) {
	p := authctx.Must(ctx)
	res := &SaleActionResult{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getSaleResTx(ctx, tx, id)
		if err != nil {
			return err
		}
		perm := map[string]string{"sign_contract": "commercial.sale_reservations.update", "complete": "commercial.sale_reservations.complete", "handover": "commercial.sale_reservations.complete", "cancel": "commercial.sale_reservations.cancel"}[action]
		if perm == "" {
			return apperr.Validation("aksi tidak dikenal")
		}
		if err := iam.CanOnProperty(ctx, perm, x.PropertyID); err != nil {
			return err
		}
		if !has(x.AllowedActions, action) {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk reservasi berstatus %s", action, x.Status))
		}
		var to, ev string
		switch action {
		case "sign_contract":
			to = "contract_signed"
			if _, err := tx.Exec(ctx, `UPDATE unit_sale_reservations SET status = 'contract_signed', contract_signed_at = now(), contract_reference = COALESCE($2, contract_reference), updated_by = $3 WHERE id = $1`, id, nilIfEmpty(in.ContractReference), p.UserID); err != nil {
				return err
			}
			_ = s.addActivityTx(ctx, tx, p, x.LeadID, "document", "Kontrak ditandatangani "+deref(nilIfEmpty(in.ContractReference)), nil)
		case "complete":
			to, ev = "sold", EventSaleReservationSold
			if _, err := tx.Exec(ctx, `UPDATE unit_sale_reservations SET status = 'sold', sold_at = now(), contract_reference = COALESCE($2, contract_reference), updated_by = $3 WHERE id = $1`, id, nilIfEmpty(in.ContractReference), p.UserID); err != nil {
				return err
			}
			if err := s.setListingStatusTx(ctx, tx, x.ListingID, "sold", p.UserID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE unit_sales_leads SET status = 'sold', last_activity_at = now(), updated_by = $2 WHERE id = $1`, x.LeadID, p.UserID); err != nil {
				return err
			}
			_ = s.addActivityTx(ctx, tx, p, x.LeadID, "status_change", fmt.Sprintf("Unit %s terjual (%s) · %s", x.UnitNumber, x.ReservationNumber, formatIDR(x.AgreedPrice)), nil)
		case "handover":
			to, ev = "handed_over", EventSaleReservationHandedOver
			at := time.Now()
			if v := nilIfEmpty(in.HandoverDate); v != nil {
				if at, err = parseDate(*v, "handover_date"); err != nil {
					return err
				}
			}
			name := deref(nilIfEmpty(in.OwnerName))
			if name == "" {
				name = x.BuyerName
			}
			phone, email := deref(nilIfEmpty(in.OwnerPhone)), deref(nilIfEmpty(in.OwnerEmail))
			if phone == "" {
				phone = deref(x.BuyerPhone)
			}
			if email == "" {
				email = deref(x.BuyerEmail)
			}
			// Handover = pemilik menjadi Tenant/Occupant unit (model bersama) — bukan entitas "buyer" terpisah
			ob, err := s.onboardTx(ctx, tx, x.PropertyID, x.UnitLocationID, OnboardingInput{FullName: name, Phone: phone, Email: email, Company: deref(in.Company), TenantType: deref(in.TenantType), OwnershipStatus: "owner", MovedInAt: dateOnly(at),
				CreateAccount: in.CreateAccount, Password: in.Password, Source: "sale_handover", Notes: "Pemilik unit · " + x.ReservationNumber}, nil)
			if err != nil {
				return err
			}
			res.Onboarding = ob
			if _, err := tx.Exec(ctx, `UPDATE unit_sale_reservations SET status = 'handed_over', handed_over_at = $2, tenant_id = $3, occupant_id = $4, tenant_user_id = $5, updated_by = $6 WHERE id = $1`, id, at, ob.TenantID, ob.OccupantID, ob.TenantUserID, p.UserID); err != nil {
				return err
			}
			_ = s.addActivityTx(ctx, tx, p, x.LeadID, "status_change", fmt.Sprintf("Serah terima unit %s · pemilik %s (%s)", x.UnitNumber, name, ob.TenantCode), &at)
		case "cancel":
			to, ev = "cancelled", EventSaleReservationCancelled
			reason := strings.TrimSpace(in.Reason)
			if reason == "" {
				return apperr.Validation("reason wajib").WithField("reason", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE unit_sale_reservations SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2, updated_by = $3 WHERE id = $1`, id, reason, p.UserID); err != nil {
				return err
			}
			// listing kembali tersedia, unit kembali vacant (tenant tetap bila sebelumnya ada), lead → qualified
			if err := s.setListingStatusTx(ctx, tx, x.ListingID, "published", p.UserID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE units SET occupancy_status = CASE WHEN tenant_id IS NULL THEN 'vacant' ELSE 'occupied' END WHERE location_id = $1`, x.UnitLocationID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE unit_sales_leads SET status = 'qualified', last_activity_at = now(), updated_by = $2 WHERE id = $1 AND status = 'reserved'`, x.LeadID, p.UserID); err != nil {
				return err
			}
			_ = s.addActivityTx(ctx, tx, p, x.LeadID, "status_change", "Reservasi "+x.ReservationNumber+" dibatalkan · "+reason, nil)
			if x.InvoiceID != nil {
				_, _ = tx.Exec(ctx, `UPDATE invoices SET status = 'cancelled', updated_by = $2 WHERE id = $1 AND status IN ('draft','issued')`, *x.InvoiceID, p.UserID)
			}
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "unit_sale_reservation", ObjectID: id, Action: audit.ActStatusChanged, From: x.Status, To: to, Payload: map[string]any{"action": action, "reason": in.Reason}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "unit_sale_reservation", EntityID: &id, EntityLabel: x.ReservationNumber, Before: map[string]any{"status": x.Status}, After: map[string]any{"status": to, "reason": in.Reason}})
		if ev != "" && s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: ev, OrganizationID: p.OrganizationID, PropertyID: &x.PropertyID, ObjectType: "unit_sale_reservation", ObjectID: id, ObjectLabel: x.ReservationNumber + " · Unit " + x.UnitNumber + " · " + x.BuyerName, ActorUserID: &p.UserID, Payload: map[string]any{"from": x.Status, "to": to, "reason": in.Reason, "domain": "management"}})
		}
		res.Reservation, err = s.getSaleResTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ---------- Pipeline summary (Unit Sales Management overview) ----------

type SalesSummary struct {
	Listings     map[string]int `json:"listings"`      // by status
	Leads        map[string]int `json:"leads"`         // by status
	Reservations map[string]int `json:"reservations"`  // by status
	SoldValue    int64          `json:"sold_value"`    // total agreed_price sold/handed_over
	FollowUpsDue int            `json:"follow_ups_due"` // next_follow_up_at <= now, lead terbuka
}

func (s *Service) SalesSummaryView(ctx context.Context, propertyID uuid.UUID) (*SalesSummary, error) {
	if err := iam.CanOnProperty(ctx, "commercial.sales_leads.view", propertyID); err != nil {
		return nil, err
	}
	out := &SalesSummary{Listings: map[string]int{}, Leads: map[string]int{}, Reservations: map[string]int{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireSales(ctx, tx, propertyID); err != nil {
			return err
		}
		if err := countBy(ctx, tx, `SELECT status, count(*) FROM unit_listings WHERE property_id = $1 GROUP BY status`, propertyID, out.Listings); err != nil {
			return err
		}
		if err := countBy(ctx, tx, `SELECT status, count(*) FROM unit_sales_leads WHERE property_id = $1 GROUP BY status`, propertyID, out.Leads); err != nil {
			return err
		}
		if err := countBy(ctx, tx, `SELECT status, count(*) FROM unit_sale_reservations WHERE property_id = $1 GROUP BY status`, propertyID, out.Reservations); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(agreed_price),0), (SELECT count(*) FROM unit_sales_leads WHERE property_id = $1 AND status IN ('new','contacted','qualified') AND next_follow_up_at <= now()) FROM unit_sale_reservations WHERE property_id = $1 AND status IN ('sold','handed_over')`, propertyID).Scan(&out.SoldValue, &out.FollowUpsDue); err != nil {
			return err
		}
		return nil
	})
	return out, err
}

func countBy(ctx context.Context, tx pgx.Tx, q string, arg any, into map[string]int) error {
	rows, err := tx.Query(ctx, q, arg)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		into[k] = n
	}
	return rows.Err()
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func formatIDR(v int64) string {
	s := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return "Rp " + b.String()
}
