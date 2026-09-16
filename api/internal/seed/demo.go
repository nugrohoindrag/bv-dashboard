package seed

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/property"
)

// DemoRefs dipakai seed lanjutan (asset, schedule, dst) setelah struktur dasar ada.
type DemoRefs struct {
	OrgID       uuid.UUID
	AdminID     uuid.UUID
	PropertyID  uuid.UUID
	TowerA      uuid.UUID
	TowerB      uuid.UUID
	FloorA12    uuid.UUID
	FloorALG    uuid.UUID
	FloorB3     uuid.UUID
	MechRoomA12 uuid.UUID
	LobbyA      uuid.UUID
	ToiletA12   uuid.UUID
	ToiletB3    uuid.UUID
	ParkingLG   uuid.UUID
	UnitA1201   uuid.UUID
	UnitA1202   uuid.UUID
	Users       map[string]uuid.UUID // by role code
	Teams       map[string]uuid.UUID // by domain
}

// SeedDemo (dalam tx yang sama dengan seed dasar): property hierarchy, teams, users per role.
func SeedDemo(ctx context.Context, tx pgx.Tx, orgID, adminID uuid.UUID) error {
	_, err := SeedDemoRefs(ctx, tx, orgID, adminID, true)
	return err
}

// SeedDemoRefs: seperti SeedDemo tetapi mengembalikan referensi (untuk test & seed lanjutan).
// withOps=false → hanya struktur (property, team, user, tenant) tanpa data operasional (dipakai integration test).
func SeedDemoRefs(ctx context.Context, tx pgx.Tx, orgID, adminID uuid.UUID, withOps bool) (*DemoRefs, error) {
	// Struktur dibuat di tx yang sama lewat SQL langsung agar atomic dengan seed dasar.
	refs := &DemoRefs{OrgID: orgID, AdminID: adminID, Users: map[string]uuid.UUID{}, Teams: map[string]uuid.UUID{}}
	sys := &authctx.Principal{UserID: adminID, OrganizationID: orgID, IsSystem: true, FullName: "Seed", Source: authctx.SourceSystem}
	ctx = authctx.With(ctx, sys)

	mk := func(lt property.LocationType, parent *uuid.UUID, name string, details map[string]any) (uuid.UUID, error) {
		return createLocationTx(ctx, tx, orgID, adminID, lt, parent, name, details)
	}
	var err error
	if refs.PropertyID, err = mk(property.LTProperty, nil, "Graha Pangeran", map[string]any{"timezone": "Asia/Jakarta", "address": "Jl. Pangeran No. 1", "city": "Jakarta", "property_type": "office", "profile": "office"}); err != nil {
		return nil, err
	}
	bld, err := mk(property.LTBuilding, &refs.PropertyID, "Graha Pangeran Main", map[string]any{"floors_count": 20.0})
	if err != nil {
		return nil, err
	}
	if refs.TowerA, err = mk(property.LTTower, &bld, "Tower A", map[string]any{"floors_count": 20.0}); err != nil {
		return nil, err
	}
	if refs.TowerB, err = mk(property.LTTower, &bld, "Tower B", map[string]any{"floors_count": 12.0}); err != nil {
		return nil, err
	}
	if refs.FloorALG, err = mk(property.LTFloor, &refs.TowerA, "Lantai LG", map[string]any{"floor_number": -1.0, "floor_label": "Lower Ground"}); err != nil {
		return nil, err
	}
	floorAG, err := mk(property.LTFloor, &refs.TowerA, "Ground Floor", map[string]any{"floor_number": 0.0})
	if err != nil {
		return nil, err
	}
	if refs.FloorA12, err = mk(property.LTFloor, &refs.TowerA, "Lantai 12", map[string]any{"floor_number": 12.0}); err != nil {
		return nil, err
	}
	if refs.FloorB3, err = mk(property.LTFloor, &refs.TowerB, "Lantai 3", map[string]any{"floor_number": 3.0}); err != nil {
		return nil, err
	}
	if refs.LobbyA, err = mk(property.LTArea, &floorAG, "Lobby Utama", map[string]any{"area_type": "lobby"}); err != nil {
		return nil, err
	}
	if refs.ParkingLG, err = mk(property.LTArea, &refs.FloorALG, "Parking Area LG", map[string]any{"area_type": "parking"}); err != nil {
		return nil, err
	}
	if refs.MechRoomA12, err = mk(property.LTArea, &refs.FloorA12, "Mechanical Room", map[string]any{"area_type": "mechanical_room"}); err != nil {
		return nil, err
	}
	if refs.ToiletA12, err = mk(property.LTArea, &refs.FloorA12, "Toilet Lantai 12", map[string]any{"area_type": "toilet"}); err != nil {
		return nil, err
	}
	if refs.ToiletB3, err = mk(property.LTArea, &refs.FloorB3, "Toilet Lantai 3", map[string]any{"area_type": "toilet"}); err != nil {
		return nil, err
	}
	if _, err = mk(property.LTArea, &refs.FloorA12, "Corridor Lantai 12", map[string]any{"area_type": "corridor"}); err != nil {
		return nil, err
	}
	if _, err = mk(property.LTSpace, &refs.FloorA12, "Meeting Room 1201", map[string]any{"space_type": "meeting_room", "area_m2": 42.0}); err != nil {
		return nil, err
	}
	if refs.UnitA1201, err = mk(property.LTUnit, &refs.FloorA12, "Office Unit 1201", map[string]any{"unit_number": "1201", "unit_type": "commercial", "area_m2": 180.0}); err != nil {
		return nil, err
	}
	if refs.UnitA1202, err = mk(property.LTUnit, &refs.FloorA12, "Office Unit 1202", map[string]any{"unit_number": "1202", "unit_type": "commercial", "area_m2": 120.0}); err != nil {
		return nil, err
	}

	// Teams
	for _, t := range []struct{ name, domain string }{{"Engineering Team", "engineering"}, {"Security Team", "security"}, {"Housekeeping Team", "housekeeping"}, {"Building Management Team", "management"}, {"Tenant Relation Team", "tenant_relation"}, {"Finance Team", "finance"}} {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO teams (organization_id, property_id, name, domain, created_by) VALUES ($1,$2,$3,$4,$5) RETURNING id`, orgID, refs.PropertyID, t.name, t.domain, adminID).Scan(&id); err != nil {
			return nil, err
		}
		refs.Teams[t.domain] = id
	}

	// Users per role (password demo: Demo12345!)
	users := []struct {
		email, name, role, team string
		lead                    bool
	}{
		{"pm@demo.buildingvision.id", "Rina Property Manager", "property_manager", "management", true},
		{"bm@demo.buildingvision.id", "Andi Building Manager", "building_manager", "management", false},
		{"ops@demo.buildingvision.id", "Dewi Operations Manager", "operations_manager", "management", false},
		{"eng.manager@demo.buildingvision.id", "Bambang Engineering Manager", "engineering_manager", "engineering", false},
		{"eng.spv@demo.buildingvision.id", "Agus Engineering Supervisor", "engineering_supervisor", "engineering", true},
		{"budi@demo.buildingvision.id", "Budi Santoso", "technician", "engineering", false},
		{"joko@demo.buildingvision.id", "Joko Prasetyo", "technician", "engineering", false},
		{"sec.manager@demo.buildingvision.id", "Hendra Security Manager", "security_manager", "security", false},
		{"sec.spv@demo.buildingvision.id", "Tono Security Supervisor", "security_supervisor", "security", true},
		{"wawan@demo.buildingvision.id", "Wawan Setiawan", "security_officer", "security", false},
		{"hk.manager@demo.buildingvision.id", "Maria Housekeeping Manager", "housekeeping_manager", "housekeeping", false},
		{"hk.spv@demo.buildingvision.id", "Sari Housekeeping Supervisor", "housekeeping_supervisor", "housekeeping", true},
		{"siti@demo.buildingvision.id", "Siti Aminah", "housekeeping_staff", "housekeeping", false},
		// P1: Tenant Relation, Receptionist, Finance (PRD v1.3 §6; NC §32)
		{"tr.manager@demo.buildingvision.id", "Lestari Tenant Relation Manager", "tenant_relation_manager", "tenant_relation", true},
		{"tr.officer@demo.buildingvision.id", "Nadia Tenant Relation Officer", "tenant_relation_officer", "tenant_relation", false},
		{"reception@demo.buildingvision.id", "Putri Receptionist", "receptionist", "tenant_relation", false},
		{"finance@demo.buildingvision.id", "Yusuf Finance Staff", "finance_staff", "finance", true},
	}
	for _, u := range users {
		id, err := CreateUser(ctx, tx, orgID, u.email, u.name, "Demo12345!", u.role, &refs.PropertyID)
		if err != nil {
			return nil, err
		}
		if u.email == "joko@demo.buildingvision.id" {
			refs.Users["technician_2"] = id
		} else {
			refs.Users[u.role] = id
		}
		if _, err := tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id, is_lead) VALUES ($1,$2,$3)`, refs.Teams[u.team], id, u.lead); err != nil {
			return nil, err
		}
	}

	// Tenants
	var tenantID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO tenants (organization_id, property_id, tenant_code, name, tenant_type, contact_name, contact_phone, contact_email, created_by)
		VALUES ($1,$2,'TEN-000001','PT Maju Bersama','company','Ibu Lestari','+62811000001','lestari@majubersama.co.id',$3) RETURNING id`, orgID, refs.PropertyID, adminID).Scan(&tenantID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE units SET tenant_id = $1, occupancy_status = 'occupied' WHERE location_id = $2`, tenantID, refs.UnitA1201); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO business_id_sequences (organization_id, prefix, year, last_value) VALUES ($1,'TEN',0,1) ON CONFLICT (organization_id, prefix, year) DO UPDATE SET last_value = GREATEST(business_id_sequences.last_value, 1)`, orgID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO occupants (organization_id, tenant_id, full_name, phone, is_primary_contact, created_by) VALUES ($1,$2,'Ibu Lestari','+62811000001',true,$3)`, orgID, tenantID, adminID); err != nil {
		return nil, err
	}

	if withOps {
		if err := SeedDemoOperations(ctx, tx, refs); err != nil {
			return nil, err
		}
		if err := SeedBVRoomsDemo(ctx, tx, refs); err != nil {
			return nil, err
		}
	}
	fmt.Printf("seed demo: property %s, %d users (password Demo12345!), 4 teams\n", refs.PropertyID, len(users))
	return refs, nil
}

// createLocationTx: versi seed dari property.Service.CreateLocation (tanpa event/job) di tx yang sama.
func createLocationTx(ctx context.Context, tx pgx.Tx, orgID, actor uuid.UUID, lt property.LocationType, parent *uuid.UUID, name string, details map[string]any) (uuid.UUID, error) {
	svc := &property.Service{}
	return svc.CreateLocationInTx(ctx, tx, property.CreateLocationInput{LocationType: lt, ParentID: parent, Name: name, Details: details})
}
