package demo

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/inventory"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/storage"
	"github.com/buildingvision/api/internal/seed"
	"github.com/buildingvision/api/internal/tenantapp"
	"github.com/buildingvision/api/internal/tenantrelation"
	"github.com/buildingvision/api/internal/tenantservice"
	"github.com/buildingvision/api/internal/vendor"
)

// ---------- akun bersama (§5–§6) ----------

// sharedUsers: akun lintas property (role diberikan ke seluruh property demo saat property dibuat).
var sharedUsers = []struct{ Email, Name, Role string }{
	{"engineering.demo@buildingvision.local", "Engineering Demo (Supervisor)", "engineering_supervisor"},
	{"security.demo@buildingvision.local", "Security Demo (Supervisor)", "security_supervisor"},
	{"housekeeping.demo@buildingvision.local", "Housekeeping Demo (Supervisor)", "housekeeping_supervisor"},
	{"tenantrelation.demo@buildingvision.local", "Tenant Relation Demo (Manager)", "tenant_relation_manager"},
	{"operations.demo@buildingvision.local", "Operations Demo (Manager)", "operations_manager"},
	{"finance.demo@buildingvision.local", "Finance Demo (Manager)", "finance_manager"},
}

// seedCommon: user bersama, vendor, item master inventory, checklist template org-level (idempotent per email/nama).
func (s *Service) seedCommon(ctx context.Context, env *Env, logf func(string, ...any)) error {
	env.Users[AdminEmail] = env.AdminID
	err := s.DB.WithOrgTx(env.sysCtx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		for _, u := range sharedUsers {
			var id uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM users WHERE organization_id = $1 AND lower(email) = $2 AND deleted_at IS NULL`, env.OrgID, u.Email).Scan(&id)
			if err != nil {
				if id, err = seed.CreateUser(ctx, tx, env.OrgID, u.Email, u.Name, Password, u.Role, nil); err != nil {
					return err
				}
			}
			env.Users[u.Email] = id
		}
		return nil
	})
	if err != nil {
		return err
	}
	// vendor (§5): 4 vendor org-level
	vctx := s.asEmail(ctx, env, AdminEmail)
	for _, v := range []struct {
		name, cat, contact, phone, email string
	}{
		{"PT Sejuk Teknik (HVAC Vendor)", "hvac", "Rudi Hartono", "+62811100001", "rudi@sejukteknik.co.id"},
		{"CV Terang Listrik (Electrical Vendor)", "electrical", "Sari Dewi", "+62811100002", "sari@teranglistrik.co.id"},
		{"PT Bersih Sentosa (Cleaning Vendor)", "cleaning", "Agus Salim", "+62811100003", "agus@bersihsentosa.co.id"},
		{"PT Karya Perawatan (General Maintenance Vendor)", "civil", "Nina Kartika", "+62811100004", "nina@karyaperawatan.co.id"},
	} {
		var id uuid.UUID
		if err := s.scanOne(ctx, env.OrgID, `SELECT id FROM vendors WHERE organization_id = $1 AND name = $2`, []any{env.OrgID, v.name}, &id); err == nil {
			env.Vendors[v.cat] = id
			continue
		}
		out, err := s.Vendor.Create(vctx, vendor.Input{Name: ptr(v.name), ServiceCategories: ptr([]string{v.cat}), ContactName: ptr(v.contact), ContactPhone: ptr(v.phone), ContactEmail: ptr(v.email), ContractRef: ptr("CTR-DEMO-" + strings.ToUpper(v.cat)), ContractStart: ptr(date(today().AddDate(0, -6, 0))), ContractEnd: ptr(date(today().AddDate(1, 0, 0)))})
		if err != nil {
			return fmt.Errorf("vendor %s: %w", v.name, err)
		}
		env.Vendors[v.cat] = out.ID
	}
	// item master inventory (§15) — org-level; stok per property dibuat di seeder profile
	for _, it := range inventoryItems {
		var id uuid.UUID
		if err := s.scanOne(ctx, env.OrgID, `SELECT id FROM inventory_items WHERE organization_id = $1 AND name = $2`, []any{env.OrgID, it.name}, &id); err == nil {
			env.Items[it.key] = id
			continue
		}
		out, err := s.Inventory.CreateItem(vctx, inventory.ItemInput{Name: ptr(it.name), Category: ptr(it.cat), Unit: ptr(it.unit), MinStock: ptr(it.min), UnitCost: ptr(it.cost), Barcode: ptr("DEMO-" + it.key)})
		if err != nil {
			return fmt.Errorf("item %s: %w", it.name, err)
		}
		env.Items[it.key] = out.ID
	}
	// checklist template org-level
	for _, t := range checklistTemplates {
		var id uuid.UUID
		if err := s.scanOne(ctx, env.OrgID, `SELECT id FROM checklist_templates WHERE organization_id = $1 AND name = $2 ORDER BY created_at LIMIT 1`, []any{env.OrgID, t.name}, &id); err == nil {
			env.Templates[t.key] = id
			continue
		}
		out, err := s.Ops.CreateChecklistTemplate(vctx, operations.ChecklistTemplateInput{Name: ptr(t.name), Domain: ptr(t.domain), AppliesTo: ptr(t.applies), Items: ptr(t.items)})
		if err != nil {
			return fmt.Errorf("template %s: %w", t.name, err)
		}
		if _, err := s.Ops.SetChecklistTemplateStatus(vctx, out.ID, "published"); err != nil {
			return err
		}
		env.Templates[t.key] = out.ID
	}
	s.flush(ctx)
	logf("akun bersama %d, vendor %d, item inventory %d, checklist template %d", len(sharedUsers), len(env.Vendors), len(env.Items), len(env.Templates))
	return nil
}

var inventoryItems = []struct {
	key, name, cat, unit string
	min                  float64
	cost                 int64
}{
	{"ac_filter", "AC Filter", "spare_part", "pcs", 10, 85000},
	{"refrigerant", "Refrigerant R32", "consumable", "kg", 5, 320000},
	{"led_lamp", "LED Lamp 12W", "spare_part", "pcs", 20, 45000},
	{"cable", "Electrical Cable NYM 2x1.5", "consumable", "m", 50, 9500},
	{"water_valve", "Water Valve 1/2\"", "spare_part", "pcs", 5, 65000},
	{"pipe", "PVC Pipe 1/2\"", "consumable", "m", 20, 18000},
	{"pipe_connector", "Pipe Connector 1/2\"", "spare_part", "pcs", 10, 7500},
	{"chemical", "Cleaning Chemical (Floor)", "consumable", "L", 10, 42000},
	{"cleaning_tools", "Cleaning Tools Set", "tool", "set", 3, 150000},
	{"door_lock", "Door Lock Cylinder", "spare_part", "pcs", 4, 210000},
	{"battery", "Battery AA (Alkaline)", "consumable", "pcs", 24, 6000},
}

var checklistTemplates = []struct {
	key, name, domain string
	applies           []string
	items             []operations.ChecklistTemplateItem
}{
	{"pm_ac", "PM Bulanan AC / HVAC", "engineering", []string{"work_order"}, []operations.ChecklistTemplateItem{
		{Label: "Filter udara bersih", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Belt & bearing normal", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Suhu supply (°C)", ItemType: "numeric", IsRequired: true, NumericUnit: ptr("°C"), NumericMin: ptr(12.0), NumericMax: ptr(18.0)},
		{Label: "Foto kondisi unit", ItemType: "photo", IsRequired: true, PhotoRequired: true},
		{Label: "Catatan teknisi", ItemType: "text"},
	}},
	{"pm_genset", "Inspeksi Mingguan Generator", "engineering", []string{"work_order"}, []operations.ChecklistTemplateItem{
		{Label: "Level bahan bakar > 50%", ItemType: "yes_no", IsRequired: true},
		{Label: "Level oli normal", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Test running 10 menit", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Foto panel", ItemType: "photo", IsRequired: true, PhotoRequired: true},
	}},
	{"pm_pump", "Inspeksi Bulanan Pompa", "engineering", []string{"work_order"}, []operations.ChecklistTemplateItem{
		{Label: "Tekanan discharge (bar)", ItemType: "numeric", IsRequired: true, NumericUnit: ptr("bar"), NumericMin: ptr(2.0), NumericMax: ptr(6.0)},
		{Label: "Tidak ada kebocoran seal", ItemType: "ok_notok_na", IsRequired: true},
	}},
	{"pm_lift", "Inspeksi Triwulan Lift", "engineering", []string{"work_order"}, []operations.ChecklistTemplateItem{
		{Label: "Door sensor berfungsi", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Emergency call berfungsi", ItemType: "yes_no", IsRequired: true},
		{Label: "Leveling lantai ± 5 mm", ItemType: "ok_notok_na", IsRequired: true},
	}},
	{"patrol", "Patrol Keamanan", "security", []string{"patrol"}, []operations.ChecklistTemplateItem{
		{Label: "Pintu darurat tertutup & tidak terganjal", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Penerangan berfungsi", ItemType: "yes_no", IsRequired: true},
		{Label: "Tidak ada orang tidak berwenang", ItemType: "yes_no", IsRequired: true},
	}},
	{"cleaning_room", "Cleaning Kamar / Unit", "housekeeping", []string{"cleaning", "inspection"}, []operations.ChecklistTemplateItem{
		{Label: "Tempat tidur & linen diganti", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Kamar mandi bersih & kering", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Amenities terisi", ItemType: "yes_no", IsRequired: true},
		{Label: "Foto hasil", ItemType: "photo", IsRequired: true, PhotoRequired: true},
	}},
	{"cleaning_common", "Cleaning Area Umum", "housekeeping", []string{"cleaning", "inspection"}, []operations.ChecklistTemplateItem{
		{Label: "Lantai kering & bersih", ItemType: "ok_notok_na", IsRequired: true},
		{Label: "Tempat sampah dikosongkan", ItemType: "yes_no", IsRequired: true},
		{Label: "Tidak ada bau", ItemType: "yes_no", IsRequired: true},
	}},
}

// ---------- evidence (§10) ----------

// demoJPEG: JPEG 1x1 deterministik (bukan URL eksternal).
var demoJPEG = []byte{
	0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
	0xFF, 0xDB, 0x00, 0x43, 0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09, 0x09, 0x08, 0x0A, 0x0C, 0x14, 0x0D, 0x0C, 0x0B, 0x0B, 0x0C, 0x19, 0x12, 0x13, 0x0F, 0x14, 0x1D, 0x1A, 0x1F, 0x1E, 0x1D, 0x1A, 0x1C, 0x1C, 0x20, 0x24, 0x2E, 0x27, 0x20, 0x22, 0x2C, 0x23, 0x1C, 0x1C, 0x28, 0x37, 0x29, 0x2C, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1F, 0x27, 0x39, 0x3D, 0x38, 0x32, 0x3C, 0x2E, 0x33, 0x34, 0x32,
	0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x01, 0x00, 0x01, 0x01, 0x01, 0x11, 0x00,
	0xFF, 0xC4, 0x00, 0x1F, 0x00, 0x00, 0x01, 0x05, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B,
	0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, 0x7F, 0xFF, 0xD9,
}

// evidence: attachment siap pakai (status ready) untuk WO/Task/SR — file demo kecil diunggah ke storage bila tersedia.
func (s *Service) evidence(ctx context.Context, env *Env, uploadedBy uuid.UUID, objectType string, objectID uuid.UUID, attachmentType, caption string, capturedAt time.Time) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	key := storage.ObjectKey(env.OrgID.String(), "demo-"+id.String(), capturedAt, ".jpg")
	if s.Storage != nil {
		if err := s.Storage.Put(context.WithoutCancel(ctx), key, "image/jpeg", bytes.NewReader(demoJPEG), int64(len(demoJPEG))); err != nil {
			s.Log.Debug("demo evidence upload skipped (storage unavailable)", "err", err)
		}
	}
	err := s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO attachments (id, organization_id, object_type, object_id, attachment_type, storage_key, original_filename, content_type, size_bytes, width, height, captured_at, uploaded_by, uploaded_at, gps_status, caption, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'image/jpeg',$8,1,1,$9,$10,$9,'unavailable',$11,'ready')`,
			id, env.OrgID, objectType, objectID, attachmentType, key, "demo-"+attachmentType+".jpg", len(demoJPEG), capturedAt, uploadedBy, caption)
		return err
	})
	return id, err
}

// ---------- backdate ----------

// backdate: geser created_at (dan kolom waktu lain bila ada) agar timeline demo realistis (§8, §11).
func (s *Service) backdate(ctx context.Context, env *Env, table string, id uuid.UUID, createdAt time.Time, extra map[string]time.Time) error {
	return s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		set := []string{"created_at = $2"}
		args := []any{id, createdAt}
		for col, t := range extra {
			args = append(args, t)
			set = append(set, fmt.Sprintf("%s = $%d", col, len(args)))
		}
		_, err := tx.Exec(ctx, `UPDATE `+table+` SET `+strings.Join(set, ", ")+` WHERE id = $1`, args...)
		if err != nil {
			return err
		}
		// activities ikut digeser relatif (pertama = created_at)
		_, _ = tx.Exec(ctx, `UPDATE activities SET occurred_at = $2 + (occurred_at - (SELECT min(occurred_at) FROM activities a2 WHERE a2.object_id = $1)) WHERE object_id = $1 AND occurred_at > $2`, id, createdAt)
		return nil
	})
}

// ---------- Service Request lifecycle (§8, §9, §20) ----------

// srSpec: satu tiket demo beserta jalur lifecycle-nya.
type srSpec struct {
	Category  string // complaint | inquiry | maintenance | facility | cleaning | security | other (+ kategori profile)
	Title     string
	Desc      string
	Priority  string
	Location  *uuid.UUID
	Tenant    *uuid.UUID // tenants.id (requester)
	TenantApp string     // email akun Tenant App yang melapor (kanal tenant_app) — kosong = dibuat staf (walk_in/phone)
	Channel   string
	Age       time.Duration // usia tiket (created_at = now - Age)
	Stage     string        // new | acknowledged | assigned | in_progress | waiting_for_tenant | resolved | closed | cancelled
	AssignTo  string        // email staf
	Team      *uuid.UUID
	Message   string // pesan staf ke tenant (komunikasi)
	Rating    int    // CSAT saat closed (1..5), 0 = tanpa feedback
	Reopen    bool   // resolved → reopen (SC-008)
	// work order/task turunan (§9)
	WO *woSpec
}

type woSpec struct {
	Kind      string // work_order | task
	Domain    string // engineering | housekeeping | security
	Title     string
	Type      string // work_order_type / task_type (cleaning, general, ...)
	Priority  string
	Assignee  string // email
	Asset     *uuid.UUID
	Stage     string // new | assigned | in_progress | completed | closed
	Template  string // checklist template key
	Parts     []partSpec
	Evidence  bool
	DueIn     time.Duration // due_at relatif created (negatif = overdue)
	Vendor    string        // kategori vendor (assign vendor)
	Reference *uuid.UUID
}

type partSpec struct {
	Item string
	Qty  float64
}

type srResult struct {
	ID     uuid.UUID
	Number string
	WOID   *uuid.UUID
	WOType string
}

// runSR: membuat SR lewat service (kanal Tenant App bila TenantApp diisi) dan menjalankan lifecycle sampai Stage.
func (s *Service) runSR(ctx context.Context, env *Env, propertyID uuid.UUID, sp srSpec, stockLoc *uuid.UUID) (*srResult, error) {
	created := time.Now().Add(-sp.Age)
	if len(strings.TrimSpace(sp.Desc)) < 10 {
		sp.Desc = strings.TrimSpace(sp.Desc + " Mohon ditindaklanjuti oleh tim terkait. Terima kasih.")
	}
	trCtx := s.asEmail(ctx, env, "tenantrelation.demo@buildingvision.local")
	var srID uuid.UUID
	var number string
	if sp.TenantApp != "" {
		tctx := s.asEmail(ctx, env, sp.TenantApp)
		req, err := s.TenantApp.CreateRequest(tctx, tenantapp.CreateRequestInput{CategoryCode: sp.Category, Title: sp.Title, Description: sp.Desc, LocationID: sp.Location})
		if err != nil {
			return nil, fmt.Errorf("tenant request %q: %w", sp.Title, err)
		}
		srID, number = req.ID, req.RequestNumber
	} else {
		ch := sp.Channel
		if ch == "" {
			ch = "phone"
		}
		sr, err := s.TS.Create(trCtx, tenantservice.CreateInput{PropertyID: &propertyID, CategoryCode: sp.Category, Title: sp.Title, Description: ptr(sp.Desc), TenantID: sp.Tenant, LocationID: sp.Location, Priority: sp.Priority, Channel: ch})
		if err != nil {
			return nil, fmt.Errorf("sr %q: %w", sp.Title, err)
		}
		srID, number = sr.ID, sr.RequestNumber
	}
	res := &srResult{ID: srID, Number: number}
	stages := []string{"new", "acknowledged", "assigned", "in_progress", "waiting_for_tenant", "resolved", "closed"}
	target := indexOf(stages, sp.Stage)
	if sp.Stage == "cancelled" {
		if _, err := s.TS.Transition(trCtx, srID, "cancel", tenantservice.TransitionInput{Reason: "Dibatalkan atas permintaan tenant (duplikat laporan)"}); err != nil {
			return nil, err
		}
		return res, s.backdate(ctx, env, "service_requests", srID, created, nil)
	}
	if target >= 1 {
		if _, err := s.TS.Transition(trCtx, srID, "acknowledge", tenantservice.TransitionInput{}); err != nil {
			return nil, fmt.Errorf("ack %s: %w", number, err)
		}
	}
	if target >= 2 {
		in := operations.AssignInput{}
		if sp.AssignTo != "" {
			id := env.Users[sp.AssignTo]
			if id == uuid.Nil {
				s.asEmail(ctx, env, sp.AssignTo)
				id = env.Users[sp.AssignTo]
			}
			in.AssigneeUserID = &id
		}
		if sp.Team != nil {
			in.AssigneeTeamID = sp.Team
		}
		if in.AssigneeUserID != nil || in.AssigneeTeamID != nil {
			if _, err := s.TS.Assign(trCtx, srID, in); err != nil {
				return nil, fmt.Errorf("assign %s: %w", number, err)
			}
		}
	}
	if sp.Message != "" {
		if _, err := s.Ops.AddComment(trCtx, operations.ObjServiceRequest, srID, sp.Message, nil, nil, false); err != nil {
			return nil, err
		}
	}
	// work order / task turunan (bidirectional link SR ↔ WO)
	if sp.WO != nil && target >= 2 {
		woID, err := s.runWO(ctx, env, propertyID, srID, *sp.WO, created, stockLoc)
		if err != nil {
			return nil, err
		}
		res.WOID = &woID
		res.WOType = sp.WO.Kind
	}
	// status saat ini (WO/Task selesai dapat mengubah SR otomatis lewat linkage)
	status := func() string {
		var st string
		_ = s.scanOne(ctx, env.OrgID, `SELECT status FROM service_requests WHERE id = $1`, []any{srID}, &st)
		return st
	}
	if target >= 3 && indexOf(stages, status()) < 3 {
		if _, err := s.TS.Transition(trCtx, srID, "start", tenantservice.TransitionInput{}); err != nil {
			return nil, fmt.Errorf("start %s: %w", number, err)
		}
	}
	if sp.Stage == "waiting_for_tenant" && status() != "waiting_for_tenant" {
		if _, err := s.TS.Transition(trCtx, srID, "wait_tenant", tenantservice.TransitionInput{Reason: "Mohon informasi jam kunjungan yang nyaman bagi Anda"}); err != nil {
			return nil, err
		}
	}
	if target >= 5 {
		if st := status(); st != "resolved" && st != "closed" {
			if _, err := s.TS.Transition(trCtx, srID, "resolve", tenantservice.TransitionInput{Reason: "Selesai dikerjakan", Resolution: "Permasalahan telah ditangani dan diverifikasi oleh tim."}); err != nil {
				return nil, fmt.Errorf("resolve %s: %w", number, err)
			}
		}
		if sp.Reopen {
			if _, err := s.TS.Transition(trCtx, srID, "reopen", tenantservice.TransitionInput{Reason: "Tenant melaporkan masalah muncul kembali"}); err != nil {
				return nil, err
			}
			if _, err := s.TS.Transition(trCtx, srID, "resolve", tenantservice.TransitionInput{Reason: "Perbaikan ulang selesai", Resolution: "Dilakukan perbaikan lanjutan; kondisi normal."}); err != nil {
				return nil, err
			}
		}
	}
	if target >= 6 && status() != "closed" {
		if sp.TenantApp != "" {
			tctx := s.asEmail(ctx, env, sp.TenantApp)
			if _, err := s.TenantApp.Act(tctx, srID, "confirm", tenantapp.ActionInput{}); err != nil {
				// bila konfirmasi tenant tidak diwajibkan, tutup oleh staf
				if _, err2 := s.TS.Transition(trCtx, srID, "close", tenantservice.TransitionInput{}); err2 != nil {
					return nil, fmt.Errorf("close %s: %v / %w", number, err, err2)
				}
			}
			if sp.Rating > 0 {
				comment := csatComment(sp.Rating)
				if _, err := s.TenantApp.Feedback(tctx, srID, tenantapp.FeedbackInput{Rating: sp.Rating, Comment: &comment}); err != nil {
					s.Log.Warn("demo feedback", "sr", number, "err", err)
				}
			}
		} else {
			if _, err := s.TS.Transition(trCtx, srID, "close", tenantservice.TransitionInput{}); err != nil {
				return nil, fmt.Errorf("close %s: %w", number, err)
			}
		}
	}
	s.flush(ctx)
	extra := map[string]time.Time{}
	if target >= 5 {
		extra["resolved_at"] = created.Add(sp.Age * 6 / 10)
	}
	if target >= 6 {
		extra["closed_at"] = created.Add(sp.Age * 7 / 10)
	}
	return res, s.backdate(ctx, env, "service_requests", srID, created, extra)
}

func csatComment(r int) string {
	switch r {
	case 5:
		return "Sangat cepat dan ramah, terima kasih!"
	case 4:
		return "Penanganan baik, sedikit lebih lama dari perkiraan."
	case 3:
		return "Cukup, tapi komunikasinya perlu ditingkatkan."
	case 2:
		return "Masalah sempat muncul lagi setelah diperbaiki."
	default:
		return "Lama sekali ditangani dan tidak ada kabar."
	}
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}

// runWO: Work Order / Task dari SR (source_type service_request) dengan assignment → start → parts usage → evidence → complete → close.
func (s *Service) runWO(ctx context.Context, env *Env, propertyID uuid.UUID, srID uuid.UUID, w woSpec, created time.Time, stockLoc *uuid.UUID) (uuid.UUID, error) {
	spvEmail := map[string]string{"engineering": "engineering.demo@buildingvision.local", "housekeeping": "housekeeping.demo@buildingvision.local", "security": "security.demo@buildingvision.local"}[w.Domain]
	if spvEmail == "" {
		spvEmail = "operations.demo@buildingvision.local"
	}
	spv := s.asEmail(ctx, env, spvEmail)
	var assignee *uuid.UUID
	if w.Assignee != "" {
		s.asEmail(ctx, env, w.Assignee)
		id := env.Users[w.Assignee]
		assignee = &id
	}
	var tpl *uuid.UUID
	if w.Template != "" {
		id := env.Templates[w.Template]
		tpl = &id
	}
	var due *time.Time
	if w.DueIn != 0 {
		d := created.Add(w.DueIn)
		due = &d
	}
	var wi *operations.WorkItem
	var err error
	objType := operations.ObjWorkOrder
	if w.Kind == "task" {
		objType = operations.ObjTask
		tt := w.Type
		if tt == "" {
			tt = "general"
		}
		wi, err = s.TS.CreateTaskFromSR(spv, srID, operations.CreateTaskInput{TaskType: tt, Title: w.Title, Priority: w.Priority, AssigneeUserID: assignee, ChecklistTemplateID: tpl, RequiresPhoto: w.Evidence, DueAt: due})
	} else {
		wt := w.Type
		if wt == "" {
			wt = "corrective"
		}
		wi, err = s.TS.CreateWorkOrderFromSR(spv, srID, operations.CreateWorkOrderInput{WorkOrderType: wt, Title: w.Title, Priority: w.Priority, AssigneeUserID: assignee, AssetID: w.Asset, ChecklistTemplateID: tpl, RequiresEvidence: ptr(w.Evidence), DueAt: due})
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("wo %q: %w", w.Title, err)
	}
	if w.Vendor != "" && objType == operations.ObjWorkOrder {
		if vid, ok := env.Vendors[w.Vendor]; ok {
			if _, err := s.Vendor.AssignWorkOrder(spv, wi.ID, vendor.AssignInput{VendorID: &vid, Notes: ptr("Vendor kontrak — eskalasi bila perlu spare part khusus")}); err != nil {
				s.Log.Warn("demo vendor assign", "err", err)
			}
		}
	}
	stages := []string{"new", "assigned", "in_progress", "completed", "closed"}
	target := indexOf(stages, w.Stage)
	if target < 0 {
		target = 0
	}
	if target >= 2 && assignee != nil {
		actx := s.as(ctx, env, *assignee)
		if _, err := s.Ops.Transition(actx, objType, wi.ID, "start", operations.TransitionInput{}); err != nil {
			return uuid.Nil, fmt.Errorf("start wo %s: %w", wi.Number, err)
		}
		if objType == operations.ObjWorkOrder {
			for _, p := range w.Parts {
				if item, ok := env.Items[p.Item]; ok {
					if _, err := s.Inventory.AddPart(actx, wi.ID, inventory.PartUsageInput{ItemID: item, StockLocationID: stockLoc, Quantity: p.Qty, Note: ptr("Dipakai saat perbaikan")}); err != nil {
						s.Log.Warn("demo parts usage", "wo", wi.Number, "err", err)
					}
				}
			}
		}
		if target >= 3 {
			if w.Evidence || tpl != nil {
				if _, err := s.evidence(ctx, env, *assignee, objType, wi.ID, "photo_before", "Kondisi sebelum", created.Add(30*time.Minute)); err != nil {
					return uuid.Nil, err
				}
				atype := "photo_after"
				if objType == operations.ObjTask {
					atype = "photo"
				}
				afterID, err := s.evidence(ctx, env, *assignee, objType, wi.ID, atype, "Kondisi setelah", created.Add(90*time.Minute))
				if err != nil {
					return uuid.Nil, err
				}
				if tpl != nil {
					if err := s.answerChecklist(actx, objType, wi.ID, afterID); err != nil {
						return uuid.Nil, err
					}
				}
			}
			notes := "Pekerjaan selesai; kondisi normal kembali."
			if _, err := s.Ops.Transition(actx, objType, wi.ID, "complete", operations.TransitionInput{CompletionNotes: &notes, GPSStatus: "unavailable"}); err != nil {
				return uuid.Nil, fmt.Errorf("complete wo %s: %w", wi.Number, err)
			}
		}
		if target >= 4 {
			if _, err := s.Ops.Transition(spv, objType, wi.ID, "close", operations.TransitionInput{}); err != nil {
				return uuid.Nil, fmt.Errorf("close wo %s: %w", wi.Number, err)
			}
		}
	}
	s.flush(ctx)
	table := "work_orders"
	if objType == operations.ObjTask {
		table = "tasks"
	}
	extra := map[string]time.Time{}
	if target >= 2 {
		extra["started_at"] = created.Add(20 * time.Minute)
	}
	if target >= 3 {
		extra["completed_at"] = created.Add(2 * time.Hour)
	}
	return wi.ID, s.backdate(ctx, env, table, wi.ID, created.Add(10*time.Minute), extra)
}

// answerChecklist: jawab seluruh item checklist run aktif pada object (ok / yes / nilai tengah / foto).
func (s *Service) answerChecklist(ctx context.Context, objectType string, objectID uuid.UUID, photoID uuid.UUID) error {
	runs, err := s.Ops.ListRuns(ctx, objectType, objectID)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		return nil
	}
	run, err := s.Ops.GetRun(ctx, runs[0].ID)
	if err != nil {
		return err
	}
	for _, it := range run.Items {
		in := operations.AnswerInput{}
		switch it.ItemType {
		case "ok_notok_na":
			in.ResultValue = ptr("ok")
		case "yes_no":
			in.ResultValue = ptr("yes")
		case "numeric":
			v := 15.0
			if it.NumericMin != nil && it.NumericMax != nil {
				v = (*it.NumericMin + *it.NumericMax) / 2
			}
			in.ResultNumber = &v
		case "photo":
			in.AttachmentID = &photoID
		case "text":
			in.ResultText = ptr("Kondisi baik, tidak ada temuan.")
		default:
			in.ResultValue = ptr("ok")
		}
		if it.PhotoRequired && in.AttachmentID == nil {
			in.AttachmentID = &photoID
		}
		if _, err := s.Ops.AnswerItem(ctx, it.ID, in); err != nil {
			return fmt.Errorf("answer item %s: %w", it.Label, err)
		}
	}
	return nil
}

// ---------- announcement (§25) ----------

func (s *Service) announce(ctx context.Context, env *Env, propertyID uuid.UUID, title, excerpt, body, importance string, expiresIn time.Duration) error {
	trCtx := s.asEmail(ctx, env, "tenantrelation.demo@buildingvision.local")
	exp := time.Now().Add(expiresIn)
	a, err := s.TR.CreateAnnouncement(trCtx, tenantrelation.AnnouncementInput{PropertyID: &propertyID, Title: &title, Excerpt: &excerpt, Body: &body, Audience: ptr("all"), Importance: &importance, ExpiresAt: &exp})
	if err != nil {
		return fmt.Errorf("announcement %q: %w", title, err)
	}
	if _, err := s.TR.TransitionAnnouncement(trCtx, a.ID, "publish"); err != nil {
		return err
	}
	s.flush(ctx)
	return nil
}
