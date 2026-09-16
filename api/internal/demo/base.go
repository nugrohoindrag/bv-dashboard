package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/booking"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/inventory"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/security"
	"github.com/buildingvision/api/internal/seed"
	"github.com/buildingvision/api/internal/visitor"
)

// prop: referensi satu property demo (dipakai lintas langkah seed).
type prop struct {
	ID       uuid.UUID
	Name     string
	Profile  string
	Prefix   string // hotel | apartment | office (email & marker)
	Building uuid.UUID
	Floors   map[int]uuid.UUID
	Areas    map[string]uuid.UUID
	Teams    map[string]uuid.UUID
	Staff    map[string]string // key → email (manager, tech, tech2, hk, hk2, sec, sec2, tr)
	StockLoc uuid.UUID
	Assets   map[string]uuid.UUID
	// tenant/guest yang dipakai skenario (diisi seeder profile)
	Tenants []tenantRef
	admin   context.Context
}

type tenantRef struct {
	TenantID   uuid.UUID
	Name       string
	UnitID     uuid.UUID
	UnitLabel  string
	AppEmail   string // akun Tenant App (kosong = tanpa akun)
	OccupantID *uuid.UUID
}

type propSpec struct {
	Name, Profile, Prefix, Address, City, Timezone string
	Building                                       string
	FloorFrom, FloorTo                             int
	Lat, Lng                                       float64
}

// seedBase: struktur property, tim, staf, stok, aset, PM, patrol, cleaning (bagian umum §4, §15–§19).
func (s *Service) seedBase(ctx context.Context, env *Env, sp propSpec, logf func(string, ...any)) (*prop, error) {
	admin := s.asEmail(ctx, env, AdminEmail)
	p := &prop{Name: sp.Name, Profile: sp.Profile, Prefix: sp.Prefix, Floors: map[int]uuid.UUID{}, Areas: map[string]uuid.UUID{}, Teams: map[string]uuid.UUID{}, Staff: map[string]string{}, Assets: map[string]uuid.UUID{}, admin: admin}
	loc, err := s.Property.CreateLocation(admin, property.CreateLocationInput{LocationType: property.LTProperty, Name: sp.Name, Details: map[string]any{"timezone": sp.Timezone, "address": sp.Address, "city": sp.City, "profile": sp.Profile, "property_type": sp.Profile}})
	if err != nil {
		return nil, fmt.Errorf("property: %w", err)
	}
	p.ID = loc.ID
	// marker dataset (§33) + konfigurasi profile: konfirmasi tenant sebelum close (E2E), CSAT aktif
	_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE properties SET settings = jsonb_set(COALESCE(settings,'{}'::jsonb), '{demo_dataset}', to_jsonb($2::text), true) WHERE location_id = $1`, p.ID, "DEMO-"+upper(sp.Prefix)+"-V1")
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO property_profile_configs (property_id, organization_id, tenant_confirmation_required, csat_enabled, visitor_approval_required, booking_approval_required)
			VALUES ($1,$2,true,true,$3,$3) ON CONFLICT (property_id) DO UPDATE SET tenant_confirmation_required = true, csat_enabled = true, visitor_approval_required = $3, booking_approval_required = $3`, p.ID, env.OrgID, sp.Profile != ProfileHotel)
		return err
	})
	mk := func(lt property.LocationType, parent uuid.UUID, name string, details map[string]any) (uuid.UUID, error) {
		l, err := s.Property.CreateLocation(admin, property.CreateLocationInput{LocationType: lt, ParentID: &parent, Name: name, Details: details})
		if err != nil {
			return uuid.Nil, fmt.Errorf("location %s: %w", name, err)
		}
		return l.ID, nil
	}
	if p.Building, err = mk(property.LTBuilding, p.ID, sp.Building, map[string]any{"floors_count": float64(sp.FloorTo)}); err != nil {
		return nil, err
	}
	lg, err := mk(property.LTFloor, p.Building, "Lower Ground", map[string]any{"floor_number": -1.0, "floor_label": "LG"})
	if err != nil {
		return nil, err
	}
	p.Floors[-1] = lg
	for f := sp.FloorFrom; f <= sp.FloorTo; f++ {
		name := fmt.Sprintf("Lantai %d", f)
		if f == 0 {
			name = "Ground Floor"
		}
		id, err := mk(property.LTFloor, p.Building, name, map[string]any{"floor_number": float64(f)})
		if err != nil {
			return nil, err
		}
		p.Floors[f] = id
	}
	gf := p.Floors[sp.FloorFrom]
	top := p.Floors[sp.FloorTo]
	for _, a := range []struct {
		key, name, typ string
		floor          uuid.UUID
	}{
		{"lobby", "Lobby Utama", "lobby", gf}, {"parking", "Area Parkir LG", "parking", lg}, {"mech", "Ruang Mesin / Mechanical Room", "mechanical_room", top},
		{"genset", "Ruang Genset", "mechanical_room", lg}, {"pump", "Ruang Pompa", "mechanical_room", lg}, {"panel", "Ruang Panel Listrik", "mechanical_room", lg},
		{"corridor", "Koridor Lantai " + fmt.Sprint(sp.FloorFrom+1), "corridor", p.Floors[sp.FloorFrom+1]}, {"toilet", "Toilet Umum Lantai " + fmt.Sprint(sp.FloorFrom), "toilet", gf},
		{"lift_lobby", "Lift Lobby", "lobby", gf}, {"loading", "Loading Dock", "other", lg},
	} {
		id, err := mk(property.LTArea, a.floor, a.name, map[string]any{"area_type": a.typ})
		if err != nil {
			return nil, err
		}
		p.Areas[a.key] = id
	}
	if err := s.markReportable(ctx, env, p.ID); err != nil {
		return nil, err
	}
	// tim per domain (§4 mandatory modules) + management & finance
	for _, t := range []struct{ name, domain string }{{"Engineering " + sp.Name, "engineering"}, {"Security " + sp.Name, "security"}, {"Housekeeping " + sp.Name, "housekeeping"}, {"Tenant Relation " + sp.Name, "tenant_relation"}, {"Management " + sp.Name, "management"}, {"Finance " + sp.Name, "finance"}} {
		var id uuid.UUID
		if err := s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `INSERT INTO teams (organization_id, property_id, name, domain, created_by) VALUES ($1,$2,$3,$4,$5) RETURNING id`, env.OrgID, p.ID, t.name, t.domain, env.AdminID).Scan(&id)
		}); err != nil {
			return nil, err
		}
		p.Teams[t.domain] = id
	}
	// staf per property (role scoped ke property) + keanggotaan tim; supervisor bersama jadi lead tim
	trRole := "receptionist"
	if sp.Profile == ProfileOffice || sp.Profile == ProfileApartment {
		trRole = "tenant_relation_officer"
	}
	staff := []struct {
		key, email, name, role, team string
		lead                         bool
	}{
		{"manager", sp.Prefix + ".manager@buildingvision.local", titleCase(sp.Prefix) + " Manager", "property_manager", "management", true},
		{"bm", sp.Prefix + ".building.manager@buildingvision.local", titleCase(sp.Prefix) + " Building Manager", "building_manager", "management", false},
		{"tech", sp.Prefix + ".technician@buildingvision.local", "Budi Santoso (Technician " + titleCase(sp.Prefix) + ")", "technician", "engineering", false},
		{"tech2", sp.Prefix + ".technician2@buildingvision.local", "Rizky Pratama (Technician " + titleCase(sp.Prefix) + ")", "technician", "engineering", false},
		{"hk", sp.Prefix + ".housekeeper@buildingvision.local", "Siti Rahma (Housekeeping " + titleCase(sp.Prefix) + ")", "housekeeping_staff", "housekeeping", false},
		{"hk2", sp.Prefix + ".housekeeper2@buildingvision.local", "Wati Lestari (Housekeeping " + titleCase(sp.Prefix) + ")", "housekeeping_staff", "housekeeping", false},
		{"sec", sp.Prefix + ".security@buildingvision.local", "Joko Susilo (Security Officer " + titleCase(sp.Prefix) + ")", "security_officer", "security", false},
		{"sec2", sp.Prefix + ".security2@buildingvision.local", "Andi Wijaya (Security Officer " + titleCase(sp.Prefix) + ")", "security_officer", "security", false},
		{"tr", sp.Prefix + ".tenantrelation@buildingvision.local", "Dewi Anggraini (" + map[string]string{"receptionist": "Guest Relation Officer", "tenant_relation_officer": "Tenant Relation Officer"}[trRole] + ")", trRole, "tenant_relation", false},
		{"finance", sp.Prefix + ".finance@buildingvision.local", titleCase(sp.Prefix) + " Finance Staff", "finance_staff", "finance", false},
	}
	err = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		for _, u := range staff {
			id, err := seed.CreateUser(ctx, tx, env.OrgID, u.email, u.name, Password, u.role, &p.ID)
			if err != nil {
				return fmt.Errorf("user %s: %w", u.email, err)
			}
			env.Users[u.email] = id
			p.Staff[u.key] = u.email
			if _, err := tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id, is_lead) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, p.Teams[u.team], id, u.lead); err != nil {
				return err
			}
		}
		for _, su := range sharedUsers {
			dom := map[string]string{"engineering_supervisor": "engineering", "security_supervisor": "security", "housekeeping_supervisor": "housekeeping", "tenant_relation_manager": "tenant_relation", "operations_manager": "management", "finance_manager": "finance"}[su.Role]
			if _, err := tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id, is_lead) VALUES ($1,$2,true) ON CONFLICT DO NOTHING`, p.Teams[dom], env.Users[su.Email]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	logf("%s: property %s, %d lantai, %d area, %d tim, %d staf", sp.Prefix, p.ID, len(p.Floors), len(p.Areas), len(p.Teams), len(staff))

	// ---- inventory: gudang default + stok awal + adjustment + low stock (§15) ----
	engSpv := s.asEmail(ctx, env, "engineering.demo@buildingvision.local")
	sl, err := s.Inventory.CreateStockLocation(admin, inventory.StockLocationInput{PropertyID: &p.ID, Name: ptr("Gudang Engineering " + sp.Name), LocationID: ptr(p.Areas["mech"]), IsDefault: ptr(true)})
	if err != nil {
		return nil, fmt.Errorf("stock location: %w", err)
	}
	p.StockLoc = sl.ID
	for _, it := range inventoryItems {
		qty := it.min * 3
		if it.key == "refrigerant" || it.key == "door_lock" { // low stock (di bawah minimum)
			qty = it.min - 1
		}
		if _, err := s.Inventory.CreateTransaction(admin, inventory.TransactionInput{ItemID: env.Items[it.key], StockLocationID: sl.ID, TransactionType: "in", Quantity: qty, UnitCost: ptr(it.cost), Note: ptr("Stok awal (opening stock)")}); err != nil {
			return nil, fmt.Errorf("opening stock %s: %w", it.key, err)
		}
	}
	_, _ = s.Inventory.CreateTransaction(admin, inventory.TransactionInput{ItemID: env.Items["led_lamp"], StockLocationID: sl.ID, TransactionType: "adjustment", Quantity: -2, Note: ptr("Stock opname: 2 pcs rusak")})
	_, _ = s.Inventory.CreateTransaction(admin, inventory.TransactionInput{ItemID: env.Items["chemical"], StockLocationID: sl.ID, TransactionType: "out", Quantity: 4, Note: ptr("Pemakaian rutin housekeeping")})
	_, _ = s.Inventory.CreateTransaction(admin, inventory.TransactionInput{ItemID: env.Items["ac_filter"], StockLocationID: sl.ID, TransactionType: "in", Quantity: 10, UnitCost: ptr(int64(85000)), Note: ptr("PO-DEMO-0021 (Stock In)")})

	// ---- aset (§16) ----
	eq := func(cat, typ string) uuid.UUID {
		var id uuid.UUID
		_ = s.scanOne(ctx, env.OrgID, `SELECT id FROM equipment WHERE category_code = $1 AND type_name = $2 AND is_active ORDER BY created_at LIMIT 1`, []any{cat, typ}, &id)
		return id
	}
	assets := []struct{ key, name, cat, typ, area, crit, status, manu string }{
		{"ac", "AC Central / AHU-01", "HVAC", "AHU", "mech", "high", "active", "Daikin"},
		{"ac2", "AHU-02", "HVAC", "AHU", "mech", "high", "under_maintenance", "Daikin"},
		{"lift", "Passenger Lift 1", "LIFT", "Passenger Lift", "lift_lobby", "critical", "active", "Otis"},
		{"lift2", "Passenger Lift 2", "LIFT", "Passenger Lift", "lift_lobby", "critical", "active", "Otis"},
		{"genset", "Generator 500 kVA", "GEN", "Diesel Generator", "genset", "critical", "active", "Cummins"},
		{"pump", "Water Transfer Pump 1", "PUMP", "Transfer Pump", "pump", "high", "active", "Grundfos"},
		{"panel", "Electrical Panel LVMDP", "ELEC", "LVMDP", "panel", "critical", "active", "Schneider"},
		{"fire", "Fire Alarm Panel", "FIRE", "Fire Alarm Panel", "lobby", "critical", "active", "Notifier"},
		{"boiler", "Boiler / Water Heater", "PLMB", "Water Treatment", "mech", "medium", "inactive", "Rinnai"},
		{"cctv", "CCTV Lobby", "SECU", "CCTV", "lobby", "medium", "active", "Hikvision"},
	}
	for _, a := range assets {
		eid := eq(a.cat, a.typ)
		if eid == uuid.Nil {
			continue
		}
		out, err := s.Asset.CreateAsset(admin, asset.AssetInput{Name: ptr(a.name), EquipmentID: &eid, LocationID: ptr(p.Areas[a.area]), Criticality: ptr(a.crit), Status: ptr(a.status), Manufacturer: ptr(a.manu), Notes: ptr("Aset demo " + sp.Name)})
		if err != nil {
			return nil, fmt.Errorf("asset %s: %w", a.name, err)
		}
		p.Assets[a.key] = out.ID
	}

	// ---- preventive maintenance (§17): plan → schedule → due WO → completed / overdue ----
	engTeam := p.Teams["engineering"]
	techID := env.Users[p.Staff["tech"]]
	mkPlan := func(name, key, tpl, freq, prio string, start time.Time) (uuid.UUID, error) {
		aid := p.Assets[key]
		var tplID *uuid.UUID
		if id, ok := env.Templates[tpl]; ok {
			tplID = &id
		}
		pl, err := s.Eng.CreatePlan(admin, engineering.PlanInput{Name: ptr(name), AssetID: &aid, Frequency: ptr(freq), StartDate: ptr(date(start)), ChecklistTemplateID: tplID, DefaultPriority: ptr(prio), ResponsibleTeamID: &engTeam, LeadTimeDays: ptr(2), DurationMinutes: ptr(90)})
		if err != nil {
			return uuid.Nil, fmt.Errorf("plan %s: %w", name, err)
		}
		if _, err := s.Eng.SetPlanStatus(admin, pl.ID, "published"); err != nil {
			return uuid.Nil, err
		}
		return pl.ID, nil
	}
	planAC, err := mkPlan("Monthly AC Inspection", "ac", "pm_ac", "monthly", "medium", today().AddDate(0, -2, 0))
	if err != nil {
		return nil, err
	}
	if _, err := mkPlan("Weekly Generator Inspection", "genset", "pm_genset", "weekly", "high", today().AddDate(0, 0, -21)); err != nil {
		return nil, err
	}
	if _, err := mkPlan("Monthly Pump Inspection", "pump", "pm_pump", "monthly", "medium", today().AddDate(0, 0, 10)); err != nil {
		return nil, err
	}
	if _, err := mkPlan("Quarterly Elevator Inspection", "lift", "pm_lift", "quarterly", "high", today().AddDate(0, 0, 5)); err != nil {
		return nil, err
	}
	if _, err := s.Eng.GenerateSchedules(admin, env.OrgID, nil); err != nil {
		return nil, fmt.Errorf("generate schedules: %w", err)
	}
	// jadwal lampau (dua bulan lalu) untuk plan AC agar ada Overdue PM + Completed PM
	_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		for _, d := range []time.Time{today().AddDate(0, 0, -40), today().AddDate(0, 0, -9)} {
			if _, err := tx.Exec(ctx, `INSERT INTO maintenance_schedules (organization_id, property_id, plan_id, asset_id, due_date, due_at, status)
				SELECT $1, $2, $3, $4, $5::date, ($5::date + interval '8 hours'), 'scheduled' WHERE NOT EXISTS (SELECT 1 FROM maintenance_schedules WHERE plan_id = $3 AND due_date = $5::date)`, env.OrgID, p.ID, planAC, p.Assets["ac"], date(d)); err != nil {
				return err
			}
		}
		return nil
	})
	if _, err := s.Eng.CreateDueWorkOrders(admin, env.OrgID); err != nil {
		return nil, fmt.Errorf("due work orders: %w", err)
	}
	// WO PM: yang paling lama → selesai (Completed PM, evidence + checklist), berikutnya dibiarkan overdue
	var pmWOs []uuid.UUID
	_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT w.id FROM work_orders w JOIN maintenance_schedules ms ON ms.work_order_id = w.id WHERE ms.plan_id = $1 ORDER BY ms.due_date`, planAC)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			_ = rows.Scan(&id)
			pmWOs = append(pmWOs, id)
		}
		return nil
	})
	if len(pmWOs) > 0 {
		wo := pmWOs[0]
		if _, err := s.Ops.Assign(engSpv, operations.ObjWorkOrder, wo, operations.AssignInput{AssigneeUserID: &techID}); err != nil {
			return nil, err
		}
		tctx := s.as(ctx, env, techID)
		if _, err := s.Ops.Transition(tctx, operations.ObjWorkOrder, wo, "start", operations.TransitionInput{}); err != nil {
			return nil, fmt.Errorf("start pm wo: %w", err)
		}
		photo, err := s.evidence(ctx, env, techID, operations.ObjWorkOrder, wo, "photo_after", "PM selesai", time.Now().Add(-38*24*time.Hour))
		if err != nil {
			return nil, err
		}
		if _, err := s.evidence(ctx, env, techID, operations.ObjWorkOrder, wo, "photo_before", "Sebelum PM", time.Now().Add(-38*24*time.Hour)); err != nil {
			return nil, err
		}
		if err := s.answerChecklist(tctx, operations.ObjWorkOrder, wo, photo); err != nil {
			return nil, err
		}
		if _, err := s.Ops.Transition(tctx, operations.ObjWorkOrder, wo, "complete", operations.TransitionInput{CompletionNotes: ptr("PM bulanan selesai, filter diganti"), GPSStatus: "unavailable"}); err != nil {
			return nil, fmt.Errorf("complete pm wo: %w", err)
		}
		if _, err := s.Ops.Transition(engSpv, operations.ObjWorkOrder, wo, "close", operations.TransitionInput{}); err != nil {
			return nil, err
		}
		_ = s.backdate(ctx, env, "work_orders", wo, time.Now().Add(-40*24*time.Hour), map[string]time.Time{"started_at": time.Now().Add(-39 * 24 * time.Hour), "completed_at": time.Now().Add(-38 * 24 * time.Hour)})
	}
	if len(pmWOs) > 1 {
		_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE work_orders SET due_at = now() - interval '7 days', created_at = now() - interval '11 days' WHERE id = $1`, pmWOs[1])
			return err
		})
		_, _ = s.Ops.Assign(engSpv, operations.ObjWorkOrder, pmWOs[1], operations.AssignInput{AssigneeUserID: ptr(env.Users[p.Staff["tech2"]])})
	}

	// ---- security (§19): checkpoint, rute, jadwal, patrol selesai / berjalan / missed, insiden ----
	secSpv := s.asEmail(ctx, env, "security.demo@buildingvision.local")
	var cps []security.RouteCheckpointInput
	for i, a := range []struct{ name, area string }{{"CP-01 Lobby Utama", "lobby"}, {"CP-02 Area Parkir LG", "parking"}, {"CP-03 Loading Dock", "loading"}, {"CP-04 Ruang Genset", "genset"}} {
		cp, err := s.Sec.CreateCheckpoint(secSpv, security.CheckpointInput{Name: ptr(a.name), LocationID: ptr(p.Areas[a.area])})
		if err != nil {
			return nil, fmt.Errorf("checkpoint: %w", err)
		}
		cps = append(cps, security.RouteCheckpointInput{CheckpointID: cp.ID, SortOrder: i + 1})
	}
	patrolTpl := env.Templates["patrol"]
	route, err := s.Sec.CreateRoute(secSpv, security.RouteInput{PropertyID: &p.ID, Name: ptr("Rute Patroli Utama " + sp.Name), EstimatedMinutes: ptr(45), ChecklistTemplateID: &patrolTpl, Checkpoints: &cps})
	if err != nil {
		return nil, fmt.Errorf("route: %w", err)
	}
	secOfficer := env.Users[p.Staff["sec"]]
	secTeam := p.Teams["security"]
	sch1, err := s.Sec.CreateSchedule(secSpv, security.ScheduleInput{RouteID: &route.ID, Name: ptr("Patroli Malam 22:00"), StartTime: ptr("22:00"), DurationMinutes: ptr(60), ResponsibleTeamID: &secTeam, DefaultAssigneeUserID: &secOfficer, Priority: ptr("medium")})
	if err != nil {
		return nil, err
	}
	if _, err := s.Sec.CreateSchedule(secSpv, security.ScheduleInput{RouteID: &route.ID, Name: ptr("Patroli Pagi 06:00"), StartTime: ptr("06:00"), DurationMinutes: ptr(60), ResponsibleTeamID: &secTeam, DefaultAssigneeUserID: ptr(env.Users[p.Staff["sec2"]]), Priority: ptr("medium")}); err != nil {
		return nil, err
	}
	if _, err := s.Sec.GeneratePatrolTasks(secSpv, env.OrgID, &sch1.ID); err != nil {
		return nil, fmt.Errorf("patrol tasks: %w", err)
	}
	// patrol adhoc: selesai penuh (kemarin), berjalan (sekarang), missed checkpoint (2 hari lalu)
	offCtx := s.as(ctx, env, secOfficer)
	mkPatrol := func(title string, stage string, age time.Duration) error {
		t, err := s.Sec.CreateAdhocPatrol(secSpv, route.ID, operations.CreateTaskInput{Title: title, Priority: "medium", AssigneeUserID: &secOfficer})
		if err != nil {
			return fmt.Errorf("adhoc patrol: %w", err)
		}
		if _, err := s.Ops.Transition(offCtx, operations.ObjTask, t.ID, "start", operations.TransitionInput{}); err != nil {
			return err
		}
		n := len(cps)
		if stage == "in_progress" {
			n = 2
		}
		if stage == "missed" {
			n = 3
		}
		for i := 0; i < n; i++ {
			if _, err := s.Sec.ScanCheckpoint(offCtx, t.ID, security.ScanInput{CheckpointID: &cps[i].CheckpointID, ScanMethod: "qr", GPSStatus: "unavailable", Note: ptr("Aman")}); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
		}
		if stage == "missed" {
			if _, err := s.Sec.MarkMissed(secSpv, t.ID, cps[3].CheckpointID, "QR rusak / tidak terbaca"); err != nil {
				return err
			}
		}
		if stage != "in_progress" {
			photo, _ := s.evidence(ctx, env, secOfficer, operations.ObjTask, t.ID, "photo", "Kondisi area", time.Now().Add(-age))
			_ = s.answerChecklist(offCtx, operations.ObjTask, t.ID, photo)
			if _, err := s.Ops.Transition(offCtx, operations.ObjTask, t.ID, "complete", operations.TransitionInput{CompletionNotes: ptr("Patroli selesai, area aman"), GPSStatus: "unavailable"}); err != nil {
				return fmt.Errorf("complete patrol: %w", err)
			}
		}
		return s.backdate(ctx, env, "tasks", t.ID, time.Now().Add(-age), map[string]time.Time{"started_at": time.Now().Add(-age + 5*time.Minute)})
	}
	if err := mkPatrol("Patroli Malam — selesai", "completed", 26*time.Hour); err != nil {
		return nil, err
	}
	if err := mkPatrol("Patroli Siang — berjalan", "in_progress", 30*time.Minute); err != nil {
		return nil, err
	}
	if err := mkPatrol("Patroli Malam — checkpoint terlewat", "missed", 50*time.Hour); err != nil {
		return nil, err
	}
	for _, inc := range []struct {
		cat, title, sev, stage string
		area                   string
		age                    time.Duration
	}{
		{"suspicious_activity", "Orang tidak dikenal berkeliaran di area parkir LG", "medium", "resolved", "parking", 72 * time.Hour},
		{"theft", "Laporan kehilangan barang di lobby", "high", "in_progress", "lobby", 5 * time.Hour},
	} {
		in, err := s.Ops.CreateIncident(secSpv, operations.CreateIncidentInput{PropertyID: &p.ID, IncidentType: "security", Category: inc.cat, Title: inc.title, LocationID: ptr(p.Areas[inc.area]), Severity: inc.sev, Priority: "high", AssigneeUserID: &secOfficer, Description: ptr("Dilaporkan oleh petugas jaga; CCTV ditinjau.")})
		if err != nil {
			return nil, fmt.Errorf("incident: %w", err)
		}
		if _, err := s.Ops.TransitionIncident(offCtx, in.ID, "start", operations.IncidentTransitionInput{}); err != nil {
			s.Log.Warn("demo incident start", "err", err)
		}
		if inc.stage == "resolved" {
			if _, err := s.Ops.TransitionIncident(secSpv, in.ID, "resolve", operations.IncidentTransitionInput{Reason: "Orang tersebut ternyata kurir; diverifikasi dan diarahkan ke loading dock"}); err != nil {
				s.Log.Warn("demo incident resolve", "err", err)
			}
		}
		_ = s.backdate(ctx, env, "incidents", in.ID, time.Now().Add(-inc.age), nil)
	}

	// ---- housekeeping (§18): jadwal → task → selesai (evidence + inspeksi), berjalan, pending ----
	hkSpv := s.asEmail(ctx, env, "housekeeping.demo@buildingvision.local")
	hkTeam := p.Teams["housekeeping"]
	hkStaff := env.Users[p.Staff["hk"]]
	cleanTpl := env.Templates["cleaning_common"]
	for _, cs := range []struct {
		name, area, start string
		assignee          uuid.UUID
	}{{"Cleaning Lobby Pagi", "lobby", "07:00", hkStaff}, {"Cleaning Toilet Umum", "toilet", "09:00", env.Users[p.Staff["hk2"]]}, {"Cleaning Koridor", "corridor", "13:00", hkStaff}} {
		if _, err := s.HK.CreateSchedule(hkSpv, housekeeping.ScheduleInput{Name: ptr(cs.name + " " + sp.Name), LocationID: ptr(p.Areas[cs.area]), CleaningType: ptr("routine"), StartTime: ptr(cs.start), DurationMinutes: ptr(45), ChecklistTemplateID: &cleanTpl, ResponsibleTeamID: &hkTeam, DefaultAssigneeUserID: ptr(cs.assignee), RequiresPhoto: ptr(true)}); err != nil {
			return nil, fmt.Errorf("cleaning schedule: %w", err)
		}
	}
	if _, err := s.HK.GenerateCleaningTasks(hkSpv, env.OrgID, nil); err != nil {
		return nil, fmt.Errorf("cleaning tasks: %w", err)
	}
	hkCtx := s.as(ctx, env, hkStaff)
	mkClean := func(title string, area uuid.UUID, stage string, age time.Duration, inspect bool) error {
		t, err := s.HK.CreateAdhocCleaning(hkSpv, housekeeping.AdhocCleaningInput{CreateTaskInput: operations.CreateTaskInput{Title: title, LocationID: &area, Priority: "medium", AssigneeUserID: &hkStaff, ChecklistTemplateID: &cleanTpl, RequiresPhoto: true}, CleaningType: "spot"})
		if err != nil {
			return fmt.Errorf("adhoc cleaning: %w", err)
		}
		if stage == "pending" {
			return s.backdate(ctx, env, "tasks", t.ID, time.Now().Add(-age), nil)
		}
		if _, err := s.Ops.Transition(hkCtx, operations.ObjTask, t.ID, "start", operations.TransitionInput{}); err != nil {
			return err
		}
		if stage == "completed" {
			photo, _ := s.evidence(ctx, env, hkStaff, operations.ObjTask, t.ID, "photo", "Hasil cleaning", time.Now().Add(-age+40*time.Minute))
			_, _ = s.evidence(ctx, env, hkStaff, operations.ObjTask, t.ID, "photo_before", "Sebelum cleaning", time.Now().Add(-age+5*time.Minute))
			if err := s.answerChecklist(hkCtx, operations.ObjTask, t.ID, photo); err != nil {
				return err
			}
			if _, err := s.Ops.Transition(hkCtx, operations.ObjTask, t.ID, "complete", operations.TransitionInput{CompletionNotes: ptr("Area bersih"), GPSStatus: "unavailable"}); err != nil {
				return fmt.Errorf("complete cleaning: %w", err)
			}
			if inspect {
				ins, err := s.HK.CreateInspection(hkSpv, housekeeping.CreateInspectionInput{CleaningTaskID: t.ID, ChecklistTemplateID: &cleanTpl, InspectorUserID: ptr(env.Users["housekeeping.demo@buildingvision.local"])})
				if err != nil {
					s.Log.Warn("demo hk inspection", "err", err)
				} else {
					if _, err := s.Ops.Transition(hkSpv, operations.ObjTask, ins.ID, "start", operations.TransitionInput{}); err == nil {
						ph, _ := s.evidence(ctx, env, env.Users["housekeeping.demo@buildingvision.local"], operations.ObjTask, ins.ID, "photo", "Inspeksi", time.Now().Add(-age+60*time.Minute))
						_ = s.answerChecklist(hkSpv, operations.ObjTask, ins.ID, ph)
						_, _ = s.Ops.Transition(hkSpv, operations.ObjTask, ins.ID, "complete", operations.TransitionInput{CompletionNotes: ptr("Inspeksi lulus"), GPSStatus: "unavailable"})
					}
				}
			}
		}
		return s.backdate(ctx, env, "tasks", t.ID, time.Now().Add(-age), map[string]time.Time{"started_at": time.Now().Add(-age + 5*time.Minute)})
	}
	if err := mkClean("Deep cleaning lobby (selesai + inspeksi)", p.Areas["lobby"], "completed", 20*time.Hour, true); err != nil {
		return nil, err
	}
	if err := mkClean("Spot cleaning tumpahan di koridor (berjalan)", p.Areas["corridor"], "in_progress", 25*time.Minute, false); err != nil {
		return nil, err
	}
	if err := mkClean("Cleaning tambahan toilet umum (menunggu)", p.Areas["toilet"], "pending", 2*time.Hour, false); err != nil {
		return nil, err
	}
	s.flush(ctx)
	logf("%s: inventory %d item, aset %d, PM plan 4 (WO PM %d), patroli 3 + insiden 2, cleaning 3 jadwal + 3 adhoc", sp.Prefix, len(inventoryItems), len(p.Assets), len(pmWOs))
	return p, nil
}

// ---------- fasilitas & booking (§12) ----------

type facilitySpec struct {
	Name, Type string
	Capacity   int
	Approval   bool
}

func (s *Service) seedFacilities(ctx context.Context, env *Env, p *prop, specs []facilitySpec, tenants []tenantRef, logf func(string, ...any)) error {
	admin := p.admin
	ids := []uuid.UUID{}
	for _, f := range specs {
		fac, err := s.Booking.CreateFacility(admin, booking.FacilityInput{PropertyID: &p.ID, Name: ptr(f.Name), FacilityType: ptr(f.Type), Capacity: ptr(f.Capacity), RequiresApproval: ptr(f.Approval), SlotMinutes: ptr(30), MinDurationMinutes: ptr(60), MaxDurationMinutes: ptr(240), AdvanceBookingDays: ptr(30), OpenTime: ptr("07:00"), CloseTime: ptr("22:00"), Rules: ptr("Booking maksimal 4 jam; batalkan minimal 2 jam sebelum jadwal.")})
		if err != nil {
			return fmt.Errorf("facility %s: %w", f.Name, err)
		}
		ids = append(ids, fac.ID)
	}
	// penutupan terjadwal (maintenance) fasilitas pertama minggu depan
	nextWeek := today().AddDate(0, 0, 7)
	if _, err := s.Booking.AddSchedule(admin, ids[0], booking.ScheduleInput{Kind: "closure", StartsAt: at(nextWeek, 8, 0), EndsAt: at(nextWeek, 17, 0), Reason: ptr("Maintenance rutin")}); err != nil {
		s.Log.Warn("demo facility schedule", "err", err)
	}
	if len(tenants) == 0 {
		return nil
	}
	tr := s.asEmail(ctx, env, "tenantrelation.demo@buildingvision.local")
	tomorrow := today().AddDate(0, 0, 1)
	mkBooking := func(fac uuid.UUID, t tenantRef, start time.Time, hours int, purpose, stage string) (uuid.UUID, error) {
		var b *booking.Booking
		var err error
		in := booking.CreateBookingInput{PropertyID: &p.ID, FacilityID: fac, StartsAt: start, EndsAt: start.Add(time.Duration(hours) * time.Hour), Attendees: ptr(6), Purpose: ptr(purpose), TenantID: ptr(t.TenantID)}
		if t.AppEmail != "" {
			b, err = s.Booking.TenantCreate(s.asEmail(ctx, env, t.AppEmail), in)
		} else {
			b, err = s.Booking.Create(tr, in)
		}
		if err != nil {
			return uuid.Nil, err
		}
		switch stage {
		case "confirmed", "completed", "checked_in":
			if b.Status == "pending" {
				if _, err := s.Booking.Act(tr, b.ID, "approve", booking.ActionInput{}); err != nil {
					return b.ID, err
				}
			}
			if stage != "confirmed" {
				if _, err := s.Booking.Act(tr, b.ID, "check_in", booking.ActionInput{}); err != nil {
					return b.ID, err
				}
			}
			if stage == "completed" {
				if _, err := s.Booking.Act(tr, b.ID, "complete", booking.ActionInput{}); err != nil {
					return b.ID, err
				}
			}
		case "cancelled":
			if _, err := s.Booking.Act(tr, b.ID, "cancel", booking.ActionInput{Reason: "Dibatalkan oleh tenant"}); err != nil {
				return b.ID, err
			}
		case "rejected":
			if _, err := s.Booking.Act(tr, b.ID, "reject", booking.ActionInput{Reason: "Bentrok dengan kegiatan gedung"}); err != nil {
				return b.ID, err
			}
		}
		return b.ID, nil
	}
	t0 := tenants[0]
	t1 := tenants[len(tenants)-1]
	if _, err := mkBooking(ids[0], t0, at(tomorrow, 10, 0), 2, "Rapat koordinasi", "confirmed"); err != nil {
		return fmt.Errorf("booking confirmed: %w", err)
	}
	if _, err := mkBooking(ids[1%len(ids)], t1, at(tomorrow.AddDate(0, 0, 1), 14, 0), 2, "Acara keluarga / gathering", "pending"); err != nil {
		return fmt.Errorf("booking pending: %w", err)
	}
	if _, err := mkBooking(ids[2%len(ids)], t0, at(tomorrow.AddDate(0, 0, 3), 9, 0), 1, "Sesi olahraga", "cancelled"); err != nil {
		return fmt.Errorf("booking cancelled: %w", err)
	}
	// completed: kemarin (dibuat lalu di-backdate)
	bid, err := mkBooking(ids[0], t1, at(today().AddDate(0, 0, 2), 15, 0), 2, "Presentasi", "completed")
	if err != nil {
		return fmt.Errorf("booking completed: %w", err)
	}
	_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE bookings SET starts_at = starts_at - interval '3 days', ends_at = ends_at - interval '3 days', created_at = created_at - interval '5 days' WHERE id = $1`, bid)
		return err
	})
	// availability conflict prevention (§12): slot yang sama dengan booking confirmed → ditolak service
	if _, err := mkBooking(ids[0], t1, at(tomorrow, 11, 0), 1, "Uji konflik slot", "pending"); err == nil {
		logf("%s: PERINGATAN conflict prevention tidak menolak booking bentrok", p.Prefix)
	} else {
		logf("%s: conflict prevention OK (booking bentrok ditolak: %v)", p.Prefix, shortErr(err))
	}
	s.flush(ctx)
	return nil
}

// ---------- visitor (§13) ----------

func (s *Service) seedVisitors(ctx context.Context, env *Env, p *prop, tenants []tenantRef, logf func(string, ...any)) error {
	if len(tenants) == 0 {
		return nil
	}
	tr := p.admin // registrasi & approval oleh staf (admin org); verifikasi masuk/keluar oleh Security
	sec := s.asEmail(ctx, env, "security.demo@buildingvision.local")
	tomorrow := today().AddDate(0, 0, 1)
	mk := func(t tenantRef, name, phone, purpose, plate string, when time.Time, stage string, viaTenant bool) error {
		// validasi menolak expected_at lampau → buat di masa depan lalu geser (backdate) untuk riwayat
		shift := time.Duration(0)
		if when.Before(time.Now().Add(time.Hour)) {
			shift = time.Now().Add(2*time.Hour).Sub(when) + time.Hour
		}
		when0 := when.Add(shift)
		in := visitor.CreateInput{PropertyID: &p.ID, TenantID: ptr(t.TenantID), HostUnitLocationID: ptr(t.UnitID), VisitorName: name, VisitorPhone: ptr(phone), Purpose: ptr(purpose), VehiclePlate: ptr(plate), Headcount: ptr(1), ExpectedAt: when0, ExpectedUntil: ptr(when0.Add(3 * time.Hour))}
		var v *visitor.Visitor
		var err error
		if viaTenant && t.AppEmail != "" {
			v, err = s.Visitor.TenantCreate(s.asEmail(ctx, env, t.AppEmail), in)
		} else {
			v, err = s.Visitor.Create(tr, in)
		}
		if err != nil {
			return fmt.Errorf("visitor %s: %w", name, err)
		}
		if stage == "pending" {
			return nil
		}
		if v.Status == "pending_approval" {
			if _, err := s.Visitor.Act(tr, v.ID, "approve", visitor.ActionInput{}); err != nil {
				return err
			}
		}
		if stage == "cancelled" {
			_, err := s.Visitor.Act(tr, v.ID, "cancel", visitor.ActionInput{Reason: "Tamu membatalkan kunjungan"})
			return err
		}
		if stage == "checked_in" || stage == "checked_out" {
			if _, err := s.Visitor.Act(sec, v.ID, "check_in", visitor.ActionInput{Note: "KTP diverifikasi"}); err != nil {
				return err
			}
		}
		if stage == "checked_out" {
			if _, err := s.Visitor.Act(sec, v.ID, "check_out", visitor.ActionInput{}); err != nil {
				return err
			}
		}
		if shift > 0 {
			return s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE visitors SET expected_at = expected_at - $2::interval, expected_until = expected_until - $2::interval, checked_in_at = checked_in_at - $2::interval, checked_out_at = checked_out_at - $2::interval, created_at = created_at - $2::interval WHERE id = $1`, v.ID, shift)
				return err
			})
		}
		return nil
	}
	t0, t1 := tenants[0], tenants[len(tenants)-1]
	if err := mk(t0, "Rina Kusuma", "+62813100001", "Kunjungan bisnis", "B 1234 ABC", at(tomorrow, 10, 0), "registered", false); err != nil {
		return err
	}
	if err := mk(t1, "Fajar Nugroho", "+62813100002", "Pengantaran dokumen", "", at(today(), 9, 30), "checked_in", false); err != nil {
		return err
	}
	if err := mk(t0, "Maya Putri", "+62813100003", "Meeting", "B 5678 DEF", at(today().AddDate(0, 0, -1), 13, 0), "checked_out", false); err != nil {
		return err
	}
	if err := mk(t1, "Dimas Aditya", "+62813100004", "Kunjungan keluarga", "", at(tomorrow.AddDate(0, 0, 1), 16, 0), "cancelled", false); err != nil {
		return err
	}
	if err := mk(t0, "Lukman Hakim", "+62813100005", "Teknisi internet", "B 9012 GHI", at(tomorrow, 14, 0), "pending", true); err != nil {
		return err
	}
	s.flush(ctx)
	logf("%s: visitor 5 (upcoming, checked-in, checked-out, cancelled, pending verification)", p.Prefix)
	return nil
}

// ---------- billing (§14) ----------

func (s *Service) seedBilling(ctx context.Context, env *Env, p *prop, tenants []tenantRef, invoiceType string, amount int64, logf func(string, ...any)) error {
	if len(tenants) == 0 {
		return nil
	}
	fin := s.asEmail(ctx, env, "finance.demo@buildingvision.local")
	period := today().AddDate(0, 0, -today().Day()+1) // awal bulan ini
	mk := func(t tenantRef, monthOffset int, desc string, issue bool, dueIn int) (*billing.Invoice, error) {
		ps := period.AddDate(0, monthOffset, 0)
		pe := ps.AddDate(0, 1, -1)
		due := ps.AddDate(0, 0, dueIn)
		items := []billing.Item{{Description: desc + " " + ps.Format("Jan 2006"), Quantity: 1, UnitPrice: amount, Amount: amount}}
		inv, err := s.Billing.Create(fin, billing.InvoiceInput{PropertyID: &p.ID, TenantID: &t.TenantID, UnitLocationID: &t.UnitID, InvoiceType: &invoiceType, PeriodStart: ptr(date(ps)), PeriodEnd: ptr(date(pe)), DueAt: &due, TaxAmount: ptr(amount * 11 / 100), Items: &items, IssueNow: issue, Description: ptr(desc)})
		if err != nil {
			return nil, fmt.Errorf("invoice %s: %w", desc, err)
		}
		return inv, nil
	}
	t0 := tenants[0]
	t1 := tenants[len(tenants)-1]
	tm := tenants[len(tenants)/2]
	// paid (bulan lalu, dibayar manual & diverifikasi)
	inv, err := mk(t0, -1, "Tagihan "+invoiceType, true, 14)
	if err != nil {
		return err
	}
	if _, err := s.Billing.RecordManual(fin, inv.ID, billing.RecordInput{Amount: inv.TotalAmount, Method: "transfer", PaidAt: ptr(period.AddDate(0, 0, -20)), Notes: ptr("Transfer BCA — verifikasi Finance")}); err != nil {
		return fmt.Errorf("record manual: %w", err)
	}
	// overdue (2 bulan lalu, belum dibayar)
	if _, err := mk(t1, -2, "Tagihan "+invoiceType, true, 14); err != nil {
		return err
	}
	// outstanding/due soon (bulan ini) + pembayaran pending dari tenant (manual transfer, menunggu verifikasi)
	inv3, err := mk(t0, 0, "Tagihan "+invoiceType, true, 25)
	if err != nil {
		return err
	}
	if t0.AppEmail != "" {
		if pay, err := s.Billing.TenantPay(s.asEmail(ctx, env, t0.AppEmail), inv3.ID, billing.InitiateInput{ProviderCode: "manual", Method: "transfer"}); err != nil {
			s.Log.Warn("demo tenant pay", "err", err)
		} else {
			_ = pay
		}
	}
	// outstanding lain (tenant tengah) — belum ada pembayaran
	if _, err := mk(tm, 0, "Tagihan "+invoiceType, true, 20); err != nil {
		return err
	}
	// pembayaran gagal (invoice bulan ini t1): inisiasi manual lalu ditolak Finance
	inv5, err := mk(t1, 0, "Tagihan "+invoiceType, true, 20)
	if err != nil {
		return err
	}
	if pay, err := s.Billing.RecordManual(fin, inv5.ID, billing.RecordInput{Amount: inv5.TotalAmount / 2, Method: "transfer", Notes: ptr("Bukti transfer tidak valid")}); err == nil {
		if _, err := s.Billing.FailPayment(fin, pay.ID, "Nominal tidak sesuai bukti transfer"); err != nil {
			s.Log.Debug("demo fail payment", "err", err)
		}
	}
	// draft (bulan depan) & cancelled
	if _, err := mk(t0, 1, "Tagihan "+invoiceType, false, 14); err != nil {
		return err
	}
	inv7, err := mk(tm, 1, "Tagihan tambahan (dibatalkan)", true, 14)
	if err != nil {
		return err
	}
	if _, err := s.Billing.Act(fin, inv7.ID, "cancel", billing.ActionInput{Reason: "Duplikat tagihan"}); err != nil {
		s.Log.Warn("demo invoice cancel", "err", err)
	}
	s.flush(ctx)
	logf("%s: invoice 7 (paid, overdue, due soon + payment pending, outstanding, failed payment, draft, cancelled)", p.Prefix)
	return nil
}

// markReportable: seluruh area/space property dapat dilaporkan tenant (common area, guardrail #2–#4).
func (s *Service) markReportable(ctx context.Context, env *Env, propertyID uuid.UUID) error {
	return s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE locations SET tenant_reportable = true WHERE property_id = $1 AND location_type IN ('area','space') AND deleted_at IS NULL`, propertyID)
		return err
	})
}

func upper(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'a' && c <= 'z' {
			out[i] = c - 32
		}
	}
	return string(out)
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return upper(s[:1]) + s[1:]
}

func shortErr(err error) string {
	msg := err.Error()
	if len(msg) > 80 {
		return msg[:80]
	}
	return msg
}
