package seed

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/security"
	"github.com/buildingvision/api/internal/tenantservice"
)

// SeedDemoOperations: assets, checklist templates, maintenance plan, patrol route/schedule, cleaning schedule,
// contoh Work Order / Task / Service Request / Incident — dijalankan setelah struktur dasar (tx yang sama).
func SeedDemoOperations(ctx context.Context, tx pgx.Tx, refs *DemoRefs) error {
	// principal = Engineering Manager agar audit tercatat sebagai user nyata
	adminP := &authctx.Principal{UserID: refs.AdminID, OrganizationID: refs.OrgID, IsSystem: true, FullName: "Organization Admin", Source: authctx.SourceWeb}
	ctx = authctx.With(ctx, adminP)
	d := &db.DB{} // tidak dipakai (semua *Tx)
	ops := operations.NewService(d, nil, nil)
	assets := &asset.Service{DB: d}
	eng := &engineering.Service{DB: d, Ops: ops, ScheduleHorizonDays: 60}
	sec := &security.Service{DB: d, Ops: ops, MissedPolicy: "auto", HorizonDays: 7}
	hk := &housekeeping.Service{DB: d, Ops: ops, HorizonDays: 7}
	ts := &tenantservice.Service{DB: d, Ops: ops}
	_ = hk

	// ---- Equipment ids ----
	eq := func(cat, typ string) uuid.UUID {
		var id uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT id FROM equipment WHERE category_code = $1 AND type_name = $2 AND deleted_at IS NULL`, cat, typ).Scan(&id)
		return id
	}
	// ---- Assets ----
	mkAsset := func(name, cat, typ string, loc uuid.UUID, crit string, manu string) uuid.UUID {
		id, err := assets.CreateAssetTx(ctx, tx, asset.AssetInput{Name: &name, EquipmentID: ptr(eq(cat, typ)), LocationID: &loc, Criticality: &crit, Manufacturer: &manu})
		if err != nil {
			panic(err)
		}
		return id
	}
	ahu := mkAsset("AHU-03", "HVAC", "AHU", refs.MechRoomA12, "high", "Daikin")
	_ = mkAsset("AHU-01", "HVAC", "AHU", refs.MechRoomA12, "high", "Daikin")
	lift := mkAsset("Passenger Lift 2", "LIFT", "Passenger Lift", refs.LobbyA, "critical", "Otis")
	_ = mkAsset("Genset 500 kVA", "GEN", "Diesel Generator", refs.ParkingLG, "critical", "Cummins")
	_ = mkAsset("Fire Pump 1", "FIRE", "Fire Pump", refs.ParkingLG, "critical", "Grundfos")
	_ = mkAsset("CCTV Lobby", "SECU", "CCTV", refs.LobbyA, "medium", "Hikvision")

	// ---- Checklist templates ----
	mkTpl := func(name, domain string, applies []string, items []operations.ChecklistTemplateItem) uuid.UUID {
		t, err := ops.CreateChecklistTemplateTx(ctx, tx, operations.ChecklistTemplateInput{Name: &name, Domain: &domain, AppliesTo: &applies, Items: &items})
		if err != nil {
			panic(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE checklist_templates SET status = 'published' WHERE id = $1`, t); err != nil {
			panic(err)
		}
		return t
	}
	pmTpl := mkTpl("PM Bulanan AHU", "engineering", []string{"work_order"}, []operations.ChecklistTemplateItem{
		{Label: "Filter udara bersih", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Belt tidak retak", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Suhu supply (°C)", ItemType: "numeric", IsRequired: true, NumericUnit: ptr("°C"), NumericMin: ptrF(12), NumericMax: ptrF(18)},
		{Label: "Foto kondisi unit", ItemType: "photo", IsRequired: true, PhotoRequired: true},
		{Label: "Catatan teknisi", ItemType: "text", IsRequired: false},
	})
	patrolTpl := mkTpl("Patrol Malam", "security", []string{"patrol"}, []operations.ChecklistTemplateItem{
		{Label: "Pintu darurat tertutup & tidak terganjal", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Penerangan berfungsi", ItemType: "yes_no", IsRequired: true},
		{Label: "Tidak ada orang tidak berwenang", ItemType: "yes_no", IsRequired: true},
	})
	cleanTpl := mkTpl("Cleaning Toilet", "housekeeping", []string{"cleaning", "inspection"}, []operations.ChecklistTemplateItem{
		{Label: "Lantai kering & bersih", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Wastafel & cermin bersih", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Tissue & sabun terisi", ItemType: "yes_no", IsRequired: true},
		{Label: "Tidak ada bau", ItemType: "yes_no", IsRequired: true},
	})

	// ---- Maintenance plan (published + schedules) ----
	engTeam := refs.Teams["engineering"]
	planID, err := eng.CreatePlanTx(ctx, tx, engineering.PlanInput{Name: ptr("PM Bulanan AHU-03"), AssetID: &ahu, Frequency: ptr("monthly"), StartDate: ptr(time.Now().Format("2006-01-02")), ChecklistTemplateID: &pmTpl, DefaultPriority: ptr("medium"), ResponsibleTeamID: &engTeam, LeadTimeDays: ptrI(2)})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE maintenance_plans SET status = 'published' WHERE id = $1`, planID); err != nil {
		return err
	}
	if _, err := eng.GenerateSchedulesTx(ctx, tx, planID); err != nil {
		return err
	}
	planLift, err := eng.CreatePlanTx(ctx, tx, engineering.PlanInput{Name: ptr("Inspeksi Mingguan Lift"), AssetID: &lift, Frequency: ptr("weekly"), StartDate: ptr(time.Now().Format("2006-01-02")), DefaultPriority: ptr("high"), ResponsibleTeamID: &engTeam})
	if err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `UPDATE maintenance_plans SET status = 'published' WHERE id = $1`, planLift)
	if _, err := eng.GenerateSchedulesTx(ctx, tx, planLift); err != nil {
		return err
	}

	// ---- Patrol route + checkpoints + schedule ----
	secTeam := refs.Teams["security"]
	cp1, err := sec.CreateCheckpointTx(ctx, tx, security.CheckpointInput{Name: ptr("CP-01 Lobby Utama"), LocationID: &refs.LobbyA})
	if err != nil {
		return err
	}
	cp2, err := sec.CreateCheckpointTx(ctx, tx, security.CheckpointInput{Name: ptr("CP-02 Parkir LG"), LocationID: &refs.ParkingLG})
	if err != nil {
		return err
	}
	cp3, err := sec.CreateCheckpointTx(ctx, tx, security.CheckpointInput{Name: ptr("CP-03 Mechanical Room L12"), LocationID: &refs.MechRoomA12})
	if err != nil {
		return err
	}
	routeID, err := sec.CreateRouteTx(ctx, tx, security.RouteInput{PropertyID: &refs.PropertyID, Name: ptr("Rute A — Tower A"), EstimatedMinutes: ptrI(45), ChecklistTemplateID: &patrolTpl,
		Checkpoints: &[]security.RouteCheckpointInput{{CheckpointID: cp1, SortOrder: 1}, {CheckpointID: cp2, SortOrder: 2}, {CheckpointID: cp3, SortOrder: 3}}})
	if err != nil {
		return err
	}
	officer := refs.Users["security_officer"]
	if _, err := sec.CreateScheduleTx(ctx, tx, security.ScheduleInput{RouteID: &routeID, Name: ptr("Patrol Malam 22:00"), StartTime: ptr("22:00"), DurationMinutes: ptrI(60), ResponsibleTeamID: &secTeam, DefaultAssigneeUserID: &officer, Priority: ptr("medium")}); err != nil {
		return err
	}
	if _, err := sec.CreateScheduleTx(ctx, tx, security.ScheduleInput{RouteID: &routeID, Name: ptr("Patrol Pagi 06:00"), StartTime: ptr("06:00"), DurationMinutes: ptrI(60), ResponsibleTeamID: &secTeam, Priority: ptr("medium")}); err != nil {
		return err
	}

	// ---- Cleaning schedules ----
	hkTeam := refs.Teams["housekeeping"]
	staff := refs.Users["housekeeping_staff"]
	for _, cs := range []struct {
		name  string
		loc   uuid.UUID
		start string
	}{{"Toilet Lantai 12", refs.ToiletA12, "08:00"}, {"Toilet Lantai 12 (siang)", refs.ToiletA12, "13:00"}, {"Toilet Lantai 3 Tower B", refs.ToiletB3, "09:00"}, {"Lobby Utama", refs.LobbyA, "07:00"}} {
		if _, err := hk.CreateScheduleTx(ctx, tx, housekeeping.ScheduleInput{Name: ptr(cs.name), LocationID: ptr(cs.loc), CleaningType: ptr("routine"), StartTime: ptr(cs.start), DurationMinutes: ptrI(45), ChecklistTemplateID: &cleanTpl, ResponsibleTeamID: &hkTeam, DefaultAssigneeUserID: &staff, RequiresPhoto: ptrB(true)}); err != nil {
			return err
		}
	}

	// ---- Contoh Work Order / Task / SR / Incident ----
	budi := refs.Users["technician"]
	now := time.Now()
	if _, err := ops.CreateWorkOrderTx(ctx, tx, operations.CreateWorkOrderInput{WorkOrderType: "corrective", Title: "Perbaikan AHU-03 tidak dingin", LocationID: &refs.MechRoomA12, AssetID: &ahu, Priority: "high", AssigneeUserID: &budi, DueAt: ptrT(now.Add(6 * time.Hour)), ChecklistTemplateID: &pmTpl}); err != nil {
		return err
	}
	if _, err := ops.CreateWorkOrderTx(ctx, tx, operations.CreateWorkOrderInput{WorkOrderType: "repair", Title: "Lift 2 bunyi tidak normal", LocationID: &refs.LobbyA, AssetID: &lift, Priority: "critical", AssigneeTeamID: &engTeam, DueAt: ptrT(now.Add(-3 * time.Hour))}); err != nil {
		return err
	}
	if _, err := ops.CreateWorkOrderTx(ctx, tx, operations.CreateWorkOrderInput{WorkOrderType: "service", Title: "Pasang stop kontak tambahan Unit 1202", LocationID: &refs.UnitA1202, Priority: "low"}); err != nil {
		return err
	}
	var tenantID uuid.UUID
	_ = tx.QueryRow(ctx, `SELECT id FROM tenants WHERE organization_id = $1 LIMIT 1`, refs.OrgID).Scan(&tenantID)
	if _, err := ts.CreateTx(ctx, tx, tenantservice.CreateInput{CategoryCode: "maintenance", Title: "AC unit 1201 bocor", TenantID: &tenantID, Channel: "phone", Priority: "high"}); err != nil {
		return err
	}
	if _, err := ts.CreateTx(ctx, tx, tenantservice.CreateInput{CategoryCode: "cleaning", Title: "Karpet koridor lantai 12 kotor", LocationID: &refs.FloorA12, Channel: "walk_in", RequesterName: ptr("Pak Dedi (Tenant 1202)"), RequesterPhone: ptr("+62812000002")}); err != nil {
		return err
	}
	if _, err := ops.CreateIncidentTx(ctx, tx, operations.CreateIncidentInput{Category: "suspicious_activity", Title: "Orang tidak dikenal di area parkir LG", LocationID: &refs.ParkingLG, Severity: "medium", AssigneeTeamID: &secTeam}); err != nil {
		return err
	}
	if _, err := ops.CreateTaskTx(ctx, tx, operations.CreateTaskInput{TaskType: "general", Title: "Cek tekanan hydrant Tower B", LocationID: &refs.FloorB3, Priority: "medium", AssigneeUserID: &budi, DueAt: ptrT(now.Add(24 * time.Hour))}); err != nil {
		return err
	}
	return nil
}

func ptr[T any](v T) *T           { return &v }
func ptrF(v float64) *float64     { return &v }
func ptrI(v int) *int             { return &v }
func ptrB(v bool) *bool           { return &v }
func ptrT(v time.Time) *time.Time { return &v }
