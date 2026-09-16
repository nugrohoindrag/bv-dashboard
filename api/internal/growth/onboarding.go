package growth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantservice"
)

// ---------- Progressive onboarding checklist (§28) + activation (§29) ----------

type ChecklistItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	Done  bool   `json:"done"`
	Link  string `json:"link"`
}

type Onboarding struct {
	Profile         *string         `json:"profile"` // profile property pertama (konteks checklist)
	PropertyID      *uuid.UUID      `json:"property_id"`
	PropertyName    *string         `json:"property_name"`
	Items           []ChecklistItem `json:"items"`
	Completed       int             `json:"completed"`
	Total           int             `json:"total"`
	Activated       bool            `json:"activated"`
	ActivatedAt     *time.Time      `json:"activated_at"`
	SampleDataAdded *time.Time      `json:"sample_data_added_at"`
	Dismissed       bool            `json:"dismissed"`
	Trial           *TrialInfo      `json:"trial"`
}

// Checklist: dihitung dari data nyata (bukan flag tersimpan) agar selalu konsisten dengan kondisi workspace.
func (s *Service) Checklist(ctx context.Context) (*Onboarding, error) {
	p := authctx.Must(ctx)
	var out *Onboarding
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.checklistTx(ctx, tx, p.OrganizationID)
		return err
	})
	return out, err
}

func (s *Service) checklistTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (*Onboarding, error) {
	o := &Onboarding{}
	var prof, pname string
	var pid uuid.UUID
	err := tx.QueryRow(ctx, `SELECT l.id, l.name, p.profile FROM properties p JOIN locations l ON l.id = p.location_id WHERE p.organization_id = $1 AND l.deleted_at IS NULL ORDER BY l.created_at LIMIT 1`, orgID).Scan(&pid, &pname, &prof)
	hasProperty := err == nil
	if hasProperty {
		o.Profile, o.PropertyID, o.PropertyName = &prof, &pid, &pname
	} else if !db.IsNoRows(err) {
		return nil, err
	}
	count := func(q string, args ...any) bool {
		var n int
		_ = tx.QueryRow(ctx, q, args...).Scan(&n)
		return n > 0
	}
	areas := hasProperty && count(`SELECT count(*) FROM locations WHERE organization_id = $1 AND location_type <> 'property' AND deleted_at IS NULL`, orgID)
	staff := count(`SELECT count(*) FROM users u WHERE u.organization_id = $1 AND u.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM tenant_users t WHERE t.user_id = u.id)`, orgID) &&
		count(`SELECT count(*) - 1 FROM users u WHERE u.organization_id = $1 AND u.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM tenant_users t WHERE t.user_id = u.id)`, orgID)
	request := count(`SELECT count(*) FROM service_requests WHERE organization_id = $1`, orgID)
	work := count(`SELECT (SELECT count(*) FROM work_orders WHERE organization_id = $1) + (SELECT count(*) FROM tasks WHERE organization_id = $1)`, orgID)
	completed := count(`SELECT (SELECT count(*) FROM work_orders WHERE organization_id = $1 AND status IN ('completed','closed')) + (SELECT count(*) FROM tasks WHERE organization_id = $1 AND status IN ('completed','closed'))`, orgID)
	evidence := count(`SELECT count(*) FROM attachments WHERE organization_id = $1`, orgID)
	tenant := count(`SELECT count(*) FROM tenant_users WHERE organization_id = $1`, orgID)

	// terminologi per profile (§26, §28 "contextual to the selected profile")
	tenantLabel, tenantHint := "Invite a tenant", "Give a tenant access to the Tenant App so they can report issues and follow progress."
	requestLabel := "Create an operational request"
	switch prof {
	case string(profile.Hotel):
		tenantLabel, tenantHint = "Invite a guest", "Give a guest access to the Guest App to send requests during their stay."
		requestLabel = "Log a guest request"
	case string(profile.Apartment):
		tenantLabel, tenantHint = "Invite a resident", "Give a resident access to the Tenant App so they can report issues and follow progress."
		requestLabel = "Log a resident request"
	}
	o.Items = []ChecklistItem{
		{Key: "create_property", Label: "Create your property", Hint: "Choose a profile (Hotel, Apartment, or Office) and add your first property.", Done: hasProperty, Link: "/onboarding"},
		{Key: "add_areas", Label: "Add buildings and areas", Hint: "Map the places your team works in: buildings, floors, areas, and units.", Done: areas, Link: "/property/buildings"},
		{Key: "add_staff", Label: "Add your staff", Hint: "Invite supervisors and field staff so work can be assigned.", Done: staff, Link: "/settings/users"},
		{Key: "create_request", Label: requestLabel, Hint: "Record an issue the way your front line receives it.", Done: request, Link: "/operations/service-requests"},
		{Key: "create_work", Label: "Create a task or work order", Hint: "Turn the request into work that someone owns.", Done: work, Link: "/operations/work-orders"},
		{Key: "complete_work", Label: "Complete the work", Hint: "Start and complete the work order from the dashboard or the Staff App.", Done: completed, Link: "/operations/work-orders"},
		{Key: "add_evidence", Label: "Add photo evidence", Hint: "Attach a photo so everyone can see the result.", Done: evidence, Link: "/operations/work-orders"},
		{Key: "invite_tenant", Label: tenantLabel, Hint: tenantHint, Done: tenant, Link: "/tenant-relation/tenant-users"},
	}
	o.Total = len(o.Items)
	for _, it := range o.Items {
		if it.Done {
			o.Completed++
		}
	}
	// aktivasi (§29): property + request + work + completed + evidence
	o.Activated = hasProperty && request && work && completed && evidence
	var settings map[string]any
	_ = tx.QueryRow(ctx, `SELECT settings FROM organizations WHERE id = $1`, orgID).Scan(&settings)
	if ob, ok := settings["onboarding"].(map[string]any); ok {
		if v, ok := ob["dismissed"].(bool); ok {
			o.Dismissed = v
		}
		if v, ok := ob["sample_data_added_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				o.SampleDataAdded = &t
			} else if t, err := time.Parse(time.RFC3339, v); err == nil {
				o.SampleDataAdded = &t
			}
		}
		if v, ok := ob["activated_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				o.ActivatedAt = &t
			}
		}
	}
	if o.Activated && o.ActivatedAt == nil {
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `UPDATE organizations SET settings = jsonb_set(coalesce(settings,'{}'::jsonb), '{onboarding,activated_at}', to_jsonb($2::timestamptz), true) WHERE id = $1`, orgID, now); err == nil {
			o.ActivatedAt = &now
			if p, ok := authctx.From(ctx); ok {
				s.track(ctx, "trial_activated", &orgID, &p.UserID, nil, nil, nil)
			}
		}
	}
	t, err := trialInfoTx(ctx, tx, orgID)
	if err == nil {
		o.Trial = t
	}
	return o, nil
}

// Dismiss: sembunyikan panel checklist di Overview (bisa dibuka lagi dari Settings).
func (s *Service) Dismiss(ctx context.Context, dismissed bool) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE organizations SET settings = jsonb_set(coalesce(settings,'{}'::jsonb), '{onboarding,dismissed}', to_jsonb($2::boolean), true) WHERE id = $1`, p.OrganizationID, dismissed)
		return err
	})
}

// ---------- Sample data (§30): "Use Sample Property" ----------

// SampleData menambahkan struktur dan pekerjaan contoh ke property yang dipilih. Semua nama diawali "[Sample]" agar jelas
// bahwa ini data contoh. Tidak membuat user/kredensial. Sekali per organization.
func (s *Service) SampleData(ctx context.Context, propertyID uuid.UUID) (*Onboarding, error) {
	p := authctx.Must(ctx)
	if !p.HasOnProperty("property.locations.create", propertyID) || !p.Has("operations.work_orders.create") {
		return nil, apperr.Forbidden("Sample data requires permission to create locations and work orders")
	}
	var out *Onboarding
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var prof string
		if err := tx.QueryRow(ctx, `SELECT profile FROM properties WHERE location_id = $1 AND organization_id = $2`, propertyID, p.OrganizationID).Scan(&prof); err != nil {
			return apperr.NotFound("Property")
		}
		var already bool
		_ = tx.QueryRow(ctx, `SELECT settings #> '{onboarding,sample_data_added_at}' IS NOT NULL FROM organizations WHERE id = $1`, p.OrganizationID).Scan(&already)
		if already {
			return apperr.Conflict("SAMPLE_DATA_EXISTS", "Sample data has already been added to this workspace")
		}
		if err := seedSample(ctx, tx, p.OrganizationID, propertyID, profile.Profile(prof)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE organizations SET settings = jsonb_set(coalesce(settings,'{}'::jsonb), '{onboarding,sample_data_added_at}', to_jsonb(now()), true) WHERE id = $1`, p.OrganizationID); err != nil {
			return err
		}
		if err := audit.Log(ctx, tx, audit.AuditEntry{Action: AuditSampleDataAdded, EntityType: "property", EntityID: &propertyID, After: map[string]any{"profile": prof}}); err != nil {
			return err
		}
		var err error
		out, err = s.checklistTx(ctx, tx, p.OrganizationID)
		return err
	})
	if err == nil {
		s.track(ctx, "sample_data_added", &p.OrganizationID, &p.UserID, nil, nil, map[string]any{"property_id": propertyID})
	}
	return out, err
}

func seedSample(ctx context.Context, tx pgx.Tx, orgID, propertyID uuid.UUID, prof profile.Profile) error {
	d := &db.DB{} // seluruh operasi memakai tx
	propSvc := &property.Service{}
	ops := operations.NewService(d, nil, nil)
	assets := &asset.Service{DB: d}
	ts := &tenantservice.Service{DB: d, Ops: ops}

	mk := func(lt property.LocationType, parent *uuid.UUID, name string, details map[string]any) (uuid.UUID, error) {
		return propSvc.CreateLocationInTx(ctx, tx, property.CreateLocationInput{LocationType: lt, ParentID: parent, Name: name, Details: details})
	}
	bld, err := mk(property.LTBuilding, &propertyID, "[Sample] Main Building", map[string]any{"floors_count": 3.0})
	if err != nil {
		return err
	}
	ground, err := mk(property.LTFloor, &bld, "[Sample] Ground Floor", map[string]any{"floor_number": 0.0})
	if err != nil {
		return err
	}
	second, err := mk(property.LTFloor, &bld, "[Sample] Floor 2", map[string]any{"floor_number": 2.0})
	if err != nil {
		return err
	}
	lobby, err := mk(property.LTArea, &ground, "[Sample] Lobby", map[string]any{"area_type": "lobby"})
	if err != nil {
		return err
	}
	mech, err := mk(property.LTArea, &ground, "[Sample] Mechanical Room", map[string]any{"area_type": "mechanical_room"})
	if err != nil {
		return err
	}
	toilet, err := mk(property.LTArea, &second, "[Sample] Toilet Floor 2", map[string]any{"area_type": "toilet"})
	if err != nil {
		return err
	}
	var unit uuid.UUID
	switch prof {
	case profile.Hotel:
		// kamar hotel dikelola modul Hotel (hotel_rooms); di sini cukup area publik + space
		if _, err = mk(property.LTSpace, &second, "[Sample] Meeting Room 201", map[string]any{"space_type": "meeting_room"}); err != nil {
			return err
		}
		unit = toilet
	case profile.Apartment:
		if unit, err = mk(property.LTUnit, &second, "[Sample] Unit 201", map[string]any{"unit_number": "201", "unit_type": "residential"}); err != nil {
			return err
		}
	default:
		if unit, err = mk(property.LTUnit, &second, "[Sample] Office Unit 201", map[string]any{"unit_number": "201", "unit_type": "commercial"}); err != nil {
			return err
		}
	}
	// aset (equipment sudah di-seed per organization)
	eq := func(cat, typ string) *uuid.UUID {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM equipment WHERE organization_id = $1 AND category_code = $2 AND type_name = $3 AND deleted_at IS NULL`, orgID, cat, typ).Scan(&id); err != nil {
			return nil
		}
		return &id
	}
	ahuID, err := assets.CreateAssetTx(ctx, tx, asset.AssetInput{Name: ptr("[Sample] AHU-01"), EquipmentID: eq("HVAC", "AHU"), LocationID: &mech, Criticality: ptr("high"), Manufacturer: ptr("Daikin")})
	if err != nil {
		return err
	}
	if _, err := assets.CreateAssetTx(ctx, tx, asset.AssetInput{Name: ptr("[Sample] Passenger Lift 1"), EquipmentID: eq("LIFT", "Passenger Lift"), LocationID: &lobby, Criticality: ptr("critical"), Manufacturer: ptr("Otis")}); err != nil {
		return err
	}
	now := time.Now()
	if _, err := ops.CreateWorkOrderTx(ctx, tx, operations.CreateWorkOrderInput{WorkOrderType: "corrective", Title: "[Sample] AHU-01 is not cooling", Description: ptr("Sample work order. Start it, complete it, and attach a photo to see the full flow."), LocationID: &mech, AssetID: &ahuID, Priority: "high", DueAt: ptr(now.Add(8 * time.Hour))}); err != nil {
		return err
	}
	if _, err := ops.CreateTaskTx(ctx, tx, operations.CreateTaskInput{TaskType: "general", Title: "[Sample] Check lobby lighting", LocationID: &lobby, Priority: "medium", DueAt: ptr(now.Add(24 * time.Hour))}); err != nil {
		return err
	}
	var cat string
	if err := tx.QueryRow(ctx, `SELECT code FROM service_request_categories WHERE organization_id = $1 AND is_active AND property_id IS NULL ORDER BY (code = 'maintenance') DESC, sort_order LIMIT 1`, orgID).Scan(&cat); err != nil {
		cat = "other"
	}
	if _, err := ts.CreateTx(ctx, tx, tenantservice.CreateInput{PropertyID: &propertyID, CategoryCode: cat, Title: "[Sample] Air conditioning is leaking", Description: ptr("Sample request. Acknowledge it and create a work order from here."), LocationID: &unit, Channel: "walk_in", Priority: "high", RequesterName: ptr("Sample requester")}); err != nil {
		return err
	}
	return nil
}
