package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// fakeWebPush: menangkap payload Web Push (PRD P3 v2.1 P3-PSH-01) tanpa jaringan.
type fakeWebPush struct {
	mu   sync.Mutex
	sent []map[string]any
	eps  []string
}

func (f *fakeWebPush) SendWebPush(_ context.Context, endpoint, p256dh, auth string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	f.sent = append(f.sent, m)
	f.eps = append(f.eps, endpoint)
	return nil
}

type tenantMe struct {
	ID                 uuid.UUID `json:"id"`
	TenantUserID       uuid.UUID `json:"tenant_user_id"`
	MustChangePassword bool      `json:"must_change_password"`
	IsTenantAdmin      bool      `json:"is_tenant_admin"`
	WhatsAppNumber     *string   `json:"whatsapp_number"`
	Tenant             *struct {
		ID uuid.UUID `json:"id"`
	} `json:"tenant"`
}

func (e *env) tenantMe(t *testing.T, tok string) tenantMe {
	t.Helper()
	var me tenantMe
	st, body := e.do(tok, http.MethodGet, "/api/v1/tenant/me", nil)
	e.mustJSON(st, body, 200, &me)
	return me
}

// uploadTenantAttachment: presign → PUT (storage memori) → confirm lewat endpoint Tenant App.
func (e *env) uploadTenantAttachment(token, objectType string, objectID uuid.UUID, attType, contentType string) uuid.UUID {
	e.t.Helper()
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
	}
	content := "\x89PNG\r\n\x1a\n0000"
	if contentType == "application/pdf" {
		content = "%PDF-1.4\n0000"
	}
	st, body := e.do(token, http.MethodPost, "/api/v1/tenant/attachments/presign", map[string]any{"object_type": objectType, "object_id": objectID, "attachment_type": attType, "content_type": contentType, "size_bytes": len(content)})
	e.mustJSON(st, body, 201, &pre)
	if err := e.store.Put(context.Background(), pre.StorageKey, contentType, strings.NewReader(content), int64(len(content))); err != nil {
		e.t.Fatal(err)
	}
	st, body = e.do(token, http.MethodPost, "/api/v1/tenant/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	return pre.AttachmentID
}

// changeTempPassword: akun password sementara wajib ganti password sebelum memakai API lain (P3-ACC-03, PasswordChangeGuard).
func (e *env) changeTempPassword(t *testing.T, tok, temp string) {
	t.Helper()
	if st, body := e.do(tok, http.MethodPost, "/api/v1/me/password", map[string]any{"current_password": temp, "new_password": "Tenant99999"}); st != 204 {
		t.Fatalf("ganti password sementara: %d %s", st, body)
	}
}

func inboxHas(ib inbox, typ, titleContains string) bool {
	for _, n := range ib.Data {
		if n.Type == typ && (titleContains == "" || strings.Contains(n.Title, titleContains)) {
			return true
		}
	}
	return false
}

// TestP3v21TenantExperience: PRD P3 v2.1 — bug B-01..B-10, push per platform, WhatsApp manual, My Unit, pengumuman
// bertarget/terjadwal/baca, Package, parkir tenant, feedback umum, isu berulang, KPI layanan tenant, akun (reset password,
// wajib ganti, status akun di login, anggota Tenant Admin), lampiran pesan, log komunikasi SR.
func TestP3v21TenantExperience(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	tenA, tenB := e.setupTenants(t)
	tr := e.login("tr.manager@demo.buildingvision.id")
	reception := e.login("reception@demo.buildingvision.id")
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	wawan := e.login("wawan@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	meA, meB := e.tenantMe(t, tenA), e.tenantMe(t, tenB)
	fake := &fakeWebPush{}
	e.app.Notification.WebPush = fake

	// ---------- B-10 / P3-PSH-01..03: registrasi Web Push per platform ----------
	if st, body := e.do(tenA, http.MethodPost, "/api/v1/me/devices", map[string]any{"platform": "web", "token": "abc"}); st != 400 {
		t.Fatalf("B-10: web tanpa subscription harus 400, got %d %s", st, body)
	}
	st, body := e.do(tenA, http.MethodPost, "/api/v1/me/devices", map[string]any{"platform": "web", "subscription": map[string]any{"endpoint": "https://push.example.test/sub/dewi", "keys": map[string]any{"p256dh": "BPkey", "auth": "authkey"}}})
	if st != 204 {
		t.Fatalf("registrasi web push: %d %s", st, body)
	}
	var pc struct {
		Devices []struct {
			PushKind string `json:"push_kind"`
			App      string `json:"app"`
		} `json:"devices"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/push/config", nil)
	e.mustJSON(st, body, 200, &pc)
	if len(pc.Devices) != 1 || pc.Devices[0].PushKind != "webpush" || pc.Devices[0].App != "tenant" {
		t.Fatalf("push config: %s", body)
	}

	// ---------- P3-UNT: My Unit ----------
	var units struct {
		Data []struct {
			Unit struct {
				ID uuid.UUID `json:"id"`
			} `json:"unit"`
			People []struct {
				IsSelf bool `json:"is_self"`
			} `json:"people"`
			Counts struct {
				OpenRequests int `json:"open_requests"`
			} `json:"counts"`
		} `json:"data"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/units", nil)
	e.mustJSON(st, body, 200, &units)
	if len(units.Data) != 1 || units.Data[0].Unit.ID != e.refs.UnitA1201 || len(units.Data[0].People) == 0 {
		t.Fatalf("my unit: %s", body)
	}
	if st, _ := e.do(tenA, http.MethodGet, "/api/v1/tenant/units/"+e.refs.UnitA1202.String(), nil); st != 404 {
		t.Fatalf("unit tenant lain harus 404, got %d", st)
	}

	// ---------- P3-ANN-02..05: pengumuman bertarget unit 1201 + wajib konfirmasi ----------
	var ann struct {
		ID              uuid.UUID `json:"id"`
		Status          string    `json:"status"`
		RecipientsCount *int      `json:"recipients_count"`
		Targets         []struct {
			Label string `json:"label"`
		} `json:"targets"`
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/announcements", map[string]any{"property_id": e.refs.PropertyID, "title": "Pemeliharaan AC lantai 12", "body": "Besok pukul 10.00", "audience": "tenant", "category": "announcement", "requires_ack": true, "target_location_ids": []uuid.UUID{e.refs.UnitA1201}})
	e.mustJSON(st, body, 201, &ann)
	if len(ann.Targets) != 1 {
		t.Fatalf("target label: %s", body)
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/announcements/"+ann.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, &ann)
	if ann.RecipientsCount == nil || *ann.RecipientsCount != 1 {
		t.Fatalf("recipients_count harus 1 (hanya unit 1201): %s", body)
	}
	e.dispatch(t)
	if !hasType(e.inboxOf(t, tenA), "announcement") || hasType(e.inboxOf(t, tenB), "announcement") {
		t.Fatalf("pengumuman bertarget: A harus menerima, B tidak")
	}
	var tl struct {
		Data []struct {
			ID          uuid.UUID `json:"id"`
			RequiresAck bool      `json:"requires_ack"`
			ReadAt      *string   `json:"read_at"`
		} `json:"data"`
	}
	st, body = e.do(tenB, http.MethodGet, "/api/v1/tenant/announcements", nil)
	e.mustJSON(st, body, 200, &tl)
	if len(tl.Data) != 0 {
		t.Fatalf("tenant B tidak boleh melihat pengumuman bertarget: %s", body)
	}
	if st, _ := e.do(tenB, http.MethodGet, "/api/v1/tenant/announcements/"+ann.ID.String(), nil); st != 404 {
		t.Fatalf("detail pengumuman bertarget untuk B harus 404, got %d", st)
	}
	var one struct {
		ReadAt         *string `json:"read_at"`
		AcknowledgedAt *string `json:"acknowledged_at"`
		RequiresAck    bool    `json:"requires_ack"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/announcements/"+ann.ID.String(), nil)
	e.mustJSON(st, body, 200, &one)
	if one.ReadAt == nil || !one.RequiresAck {
		t.Fatalf("membuka pengumuman = dibaca: %s", body)
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/announcements/"+ann.ID.String()+"/acknowledge", nil)
	e.mustJSON(st, body, 200, &one)
	if one.AcknowledgedAt == nil {
		t.Fatalf("acknowledge: %s", body)
	}
	var reads struct {
		Recipients   int `json:"recipients"`
		Read         int `json:"read"`
		Acknowledged int `json:"acknowledged"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/announcements/"+ann.ID.String()+"/reads", nil)
	e.mustJSON(st, body, 200, &reads)
	if reads.Recipients != 1 || reads.Read != 1 || reads.Acknowledged != 1 {
		t.Fatalf("reads: %s", body)
	}
	// P3-ANN-03: jadwal publish → terbit otomatis oleh sweep
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	var sched struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/announcements", map[string]any{"property_id": e.refs.PropertyID, "title": "Kabar: lobby baru", "body": "Renovasi selesai", "category": "news", "publish_at": future})
	e.mustJSON(st, body, 201, &sched)
	st, body = e.do(tr, http.MethodPost, "/api/v1/announcements/"+sched.ID.String()+"/schedule", nil)
	e.mustJSON(st, body, 200, &sched)
	if sched.Status != "scheduled" {
		t.Fatalf("schedule: %s", body)
	}
	e.orgTx(t, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE announcements SET publish_at = now() - interval '1 minute' WHERE id = $1`, sched.ID)
		return err
	})
	if n, err := e.app.TenantRelation.PublishScheduledSweep(ctx, e.refs.OrgID); err != nil || n != 1 {
		t.Fatalf("publish sweep: %d %v", n, err)
	}
	st, body = e.do(tenB, http.MethodGet, "/api/v1/tenant/announcements?category=news", nil)
	e.mustJSON(st, body, 200, &tl)
	if len(tl.Data) != 1 {
		t.Fatalf("news terjadwal harus terbit & terlihat semua tenant: %s", body)
	}
	// P3-BRC-01: broadcast darurat
	var bc struct {
		Category string `json:"category"`
		Status   string `json:"status"`
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/announcements/broadcast", map[string]any{"property_id": e.refs.PropertyID, "title": "Pemadaman listrik", "body": "PLN padam 13.00–15.00", "severity": "critical"})
	e.mustJSON(st, body, 201, &bc)
	if bc.Category != "alert" || bc.Status != "published" {
		t.Fatalf("broadcast: %s", body)
	}
	e.dispatch(t)
	if !inboxHas(e.inboxOf(t, tenB), "announcement_alert", "Pemberitahuan Penting") {
		t.Fatalf("broadcast harus sampai ke tenant B")
	}
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/announcements/broadcast", map[string]any{"property_id": e.refs.PropertyID, "title": "x", "body": "y"}); st != 403 {
		t.Fatalf("technician broadcast harus 403, got %d", st)
	}

	// ---------- P3-PKG: paket masuk → notifikasi → serah terima ----------
	var pkg struct {
		ID            uuid.UUID `json:"id"`
		PackageNumber string    `json:"package_number"`
		Status        string    `json:"status"`
		TenantID      *string   `json:"tenant_id"`
	}
	st, body = e.do(reception, http.MethodPost, "/api/v1/packages", map[string]any{"unit_location_id": e.refs.UnitA1201, "courier": "JNE", "tracking_number": "JNE123", "description": "Dokumen", "storage_location": "Front desk lobby A"})
	e.mustJSON(st, body, 201, &pkg)
	if !strings.HasPrefix(pkg.PackageNumber, "PKG-") || pkg.Status != "notified" || pkg.TenantID == nil {
		t.Fatalf("paket: %s", body)
	}
	e.dispatch(t)
	ibA := e.inboxOf(t, tenA)
	if !inboxHas(ibA, "package_received", "Paket Tiba") || hasType(e.inboxOf(t, tenB), "package_received") {
		t.Fatalf("notifikasi paket hanya ke tenant unit 1201")
	}
	for _, n := range ibA.Data {
		if n.Type == "package_received" && (n.DeepLink == nil || *n.DeepLink != "/packages/"+pkg.ID.String()) {
			t.Fatalf("deep link paket: %v", n.DeepLink)
		}
	}
	var pl struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/packages?waiting=true", nil)
	e.mustJSON(st, body, 200, &pl)
	if len(pl.Data) != 1 {
		t.Fatalf("paket menunggu tenant A: %s", body)
	}
	if st, _ := e.do(tenB, http.MethodGet, "/api/v1/tenant/packages/"+pkg.ID.String(), nil); st != 404 {
		t.Fatalf("privasi paket: tenant B harus 404, got %d", st)
	}
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/packages", nil); st != 403 {
		t.Fatalf("technician paket harus 403, got %d", st)
	}
	st, body = e.do(reception, http.MethodPost, "/api/v1/packages/"+pkg.ID.String()+"/pickup", map[string]any{})
	if st != 400 {
		t.Fatalf("pickup tanpa nama penerima harus 400: %d %s", st, body)
	}
	st, body = e.do(reception, http.MethodPost, "/api/v1/packages/"+pkg.ID.String()+"/pickup", map[string]any{"picked_up_by_name": "Dewi Lestari"})
	e.mustJSON(st, body, 200, &pkg)
	if pkg.Status != "picked_up" {
		t.Fatalf("pickup: %s", body)
	}
	e.dispatch(t)
	if !hasType(e.inboxOf(t, tenA), "package_picked_up") {
		t.Fatalf("notifikasi paket diambil")
	}
	// P3-PKG-04: pengingat paket belum diambil
	var pkg2 struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(reception, http.MethodPost, "/api/v1/packages", map[string]any{"unit_location_id": e.refs.UnitA1202, "courier": "SiCepat", "received_at": time.Now().Add(-5 * 24 * time.Hour)})
	e.mustJSON(st, body, 201, &pkg2)
	if n, err := e.app.Parcels.ReminderSweep(ctx, e.refs.OrgID); err != nil || n != 1 {
		t.Fatalf("reminder sweep: %d %v", n, err)
	}
	e.dispatch(t)
	if !inboxHas(e.inboxOf(t, tenB), "package_reminder", "Pengingat") {
		t.Fatalf("pengingat paket ke tenant B")
	}

	// ---------- P3-PRK: kendaraan + izin parkir tenant ----------
	var veh struct {
		ID          uuid.UUID `json:"id"`
		PlateNumber string    `json:"plate_number"`
		PermitValid bool      `json:"permit_valid"`
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/vehicles", map[string]any{"plate_number": "b 1234 xyz", "vehicle_type": "car", "brand": "Toyota", "color": "Hitam"})
	e.mustJSON(st, body, 201, &veh)
	if veh.PlateNumber != "B1234XYZ" {
		t.Fatalf("plat dinormalisasi: %s", body)
	}
	if st, body := e.do(tenB, http.MethodPost, "/api/v1/tenant/vehicles", map[string]any{"plate_number": "B 1234 XYZ"}); st != 409 || !strings.Contains(string(body), "VEHICLE_EXISTS") {
		t.Fatalf("plat ganda harus 409: %d %s", st, body)
	}
	var permit struct {
		ID           uuid.UUID `json:"id"`
		PermitNumber string    `json:"permit_number"`
		Status       string    `json:"status"`
		IsActive     bool      `json:"is_active"`
		ValidUntil   *string   `json:"valid_until"`
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/parking-permits", map[string]any{"vehicle_id": veh.ID, "permit_type": "monthly"})
	e.mustJSON(st, body, 201, &permit)
	if !strings.HasPrefix(permit.PermitNumber, "PRM-") || permit.Status != "requested" {
		t.Fatalf("permohonan izin: %s", body)
	}
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/parking-permits", map[string]any{"vehicle_id": veh.ID}); st != 409 {
		t.Fatalf("permohonan ganda harus 409, got %d", st)
	}
	e.dispatch(t)
	if !hasType(e.inboxOf(t, secSpv), "parking_permit_requested") {
		t.Fatalf("security supervisor harus menerima permohonan izin parkir")
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/parking-permits/"+permit.ID.String()+"/approve", map[string]any{"sticker_number": "STK-001"})
	e.mustJSON(st, body, 200, &permit)
	if permit.Status != "approved" || !permit.IsActive || permit.ValidUntil == nil {
		t.Fatalf("approve izin: %s", body)
	}
	e.dispatch(t)
	if !inboxHas(e.inboxOf(t, tenA), "parking_permit_approved", "Izin Parkir Disetujui") {
		t.Fatalf("notifikasi izin disetujui")
	}
	var vl struct {
		Data []struct {
			PermitValid bool `json:"permit_valid"`
		} `json:"data"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/vehicles", nil)
	e.mustJSON(st, body, 200, &vl)
	if len(vl.Data) != 1 || !vl.Data[0].PermitValid {
		t.Fatalf("kendaraan ber-permit: %s", body)
	}
	st, body = e.do(wawan, http.MethodPost, "/api/v1/parking-violations", map[string]any{"property_id": e.refs.PropertyID, "plate_number": "B1234XYZ", "violation_type": "blocking"})
	e.mustJSON(st, body, 201, nil)
	e.dispatch(t)
	if !inboxHas(e.inboxOf(t, tenA), "parking_violation", "Pelanggaran Parkir") {
		t.Fatalf("P3-PRK-03: pelanggaran tertaut kendaraan tenant harus dinotifikasi")
	}

	// ---------- P3-FDB: feedback umum (anonim) ----------
	var fb struct {
		ID             uuid.UUID `json:"id"`
		FeedbackNumber string    `json:"feedback_number"`
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/feedback", map[string]any{"category": "suggestion", "subject": "Taman", "body": "Mohon tambah tempat duduk di taman", "is_anonymous": true})
	e.mustJSON(st, body, 201, &fb)
	if !strings.HasPrefix(fb.FeedbackNumber, "FDB-") {
		t.Fatalf("feedback: %s", body)
	}
	e.dispatch(t)
	if !hasType(e.inboxOf(t, tr), "tenant_feedback_received") {
		t.Fatalf("TR menerima feedback baru")
	}
	var gfl struct {
		Data []struct {
			ID         uuid.UUID `json:"id"`
			SenderName *string   `json:"sender_name"`
		} `json:"data"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/tenant-relation/general-feedback", nil)
	e.mustJSON(st, body, 200, &gfl)
	if len(gfl.Data) != 1 || gfl.Data[0].SenderName != nil {
		t.Fatalf("feedback anonim tidak boleh menampilkan pengirim: %s", body)
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/tenant-relation/general-feedback/"+fb.ID.String()+"/respond", map[string]any{"response": "Terima kasih, akan kami tindak lanjuti"})
	e.mustJSON(st, body, 200, nil)
	e.dispatch(t)
	if !hasType(e.inboxOf(t, tenA), "tenant_feedback_responded") {
		t.Fatalf("tanggapan feedback sampai ke tenant")
	}
	if st, _ := e.do(tenB, http.MethodGet, "/api/v1/tenant/feedback/"+fb.ID.String(), nil); st != 404 {
		t.Fatalf("feedback tenant lain harus 404, got %d", st)
	}

	// ---------- Service Request: B-02 judul status, isu berulang, eskalasi keluhan ----------
	createSR := func(title, reqType string) uuid.UUID {
		var sr struct {
			ID uuid.UUID `json:"id"`
		}
		st, body := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"location_id": e.refs.UnitA1201, "category_code": "air_conditioning", "title": title, "description": "AC tidak dingin sejak pagi di unit kami", "request_type": reqType})
		e.mustJSON(st, body, 201, &sr)
		return sr.ID
	}
	sr1 := createSR("AC tidak dingin", "service_request")
	createSR("AC bocor", "service_request")
	sr3 := createSR("AC berisik lagi", "complaint")
	var sr3v struct {
		Priority string `json:"priority"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/service-requests/"+sr3.String(), nil)
	e.mustJSON(st, body, 200, &sr3v)
	if sr3v.Priority != "high" {
		t.Fatalf("P3-CMP-03: keluhan berulang harus naik prioritas ke high, got %s", sr3v.Priority)
	}
	var ris struct {
		Data []struct {
			ID           uuid.UUID `json:"id"`
			RequestCount int       `json:"request_count"`
			Status       string    `json:"status"`
		} `json:"data"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/recurring-issues", nil)
	e.mustJSON(st, body, 200, &ris)
	if len(ris.Data) != 1 || ris.Data[0].RequestCount != 3 || ris.Data[0].Status != "open" {
		t.Fatalf("recurring issue: %s", body)
	}
	e.dispatch(t)
	if !hasType(e.inboxOf(t, tr), "recurring_issue_detected") {
		t.Fatalf("TR menerima sinyal isu berulang")
	}
	var att struct {
		Data []struct {
			Category string `json:"category"`
		} `json:"data"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/overview/attention-required?limit=50", nil)
	e.mustJSON(st, body, 200, &att)
	foundRI := false
	for _, a := range att.Data {
		foundRI = foundRI || a.Category == "recurring_issue"
	}
	if !foundRI {
		t.Fatalf("Attention Required harus memuat recurring_issue: %s", body)
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/service-requests?recurring_issue_id="+ris.Data[0].ID.String(), nil)
	var srl struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &srl)
	if len(srl.Data) != 3 {
		t.Fatalf("drill-down recurring issue: %s", body)
	}
	// B-02: assign → "Ticket Being Assigned"
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr1.String()+"/assign", map[string]any{"assignee_team_id": e.refs.Teams["engineering"]})
	e.mustJSON(st, body, 200, nil)
	e.dispatch(t)
	if !inboxHas(e.inboxOf(t, tenA), "ticket_status", "Being Assigned") {
		t.Fatalf("B-02: judul assign harus 'Ticket Being Assigned'")
	}
	// B-03: auto-close → satu notifikasi Closed
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr1.String()+"/resolve", map[string]any{"reason": "Freon diisi ulang", "resolution": "Freon diisi ulang"})
	e.mustJSON(st, body, 200, nil)
	e.orgTx(t, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE service_requests SET resolved_at = now() - interval '100 hours' WHERE id = $1`, sr1)
		return err
	})
	if n, err := e.app.TenantApp.AutoCloseSweep(ctx, e.refs.OrgID); err != nil || n != 1 {
		t.Fatalf("auto close: %d %v", n, err)
	}
	e.dispatch(t)
	closed := 0
	for _, n := range e.inboxOf(t, tenA).Data {
		if n.Type == "ticket_closed" && strings.Contains(n.Body, "Ditutup otomatis") {
			closed++
		}
	}
	if closed != 1 {
		t.Fatalf("B-03: auto-close harus tepat satu notifikasi Closed, got %d", closed)
	}

	// ---------- P3-TSH: KPI layanan tenant (B-05/B-06/B-07) ----------
	var m struct {
		Overdue             int               `json:"overdue"`
		ResolvedToday       int               `json:"resolved_today"`
		Complaints30d       int               `json:"complaints_30d"`
		Requests30d         int               `json:"requests_30d"`
		RecurringIssuesOpen int               `json:"recurring_issues_open"`
		GeneralFeedbackNew  int               `json:"general_feedback_new"`
		DrillDown           map[string]string `json:"drill_down"`
		Series              []any             `json:"series"`
		ByCategory          []any             `json:"by_category"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/tenant-relation/metrics?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &m)
	if m.Complaints30d < 1 || m.Requests30d < 3 || m.RecurringIssuesOpen != 1 || len(m.Series) != 30 || len(m.ByCategory) == 0 {
		t.Fatalf("metrics P3: %s", body)
	}
	if !strings.Contains(m.DrillDown["overdue"], "sla_status=breached") || !strings.Contains(m.DrillDown["complaints_30d"], "request_type=complaint") {
		t.Fatalf("B-07 / P3-TSH-09 drill-down: %v", m.DrillDown)
	}
	if m.ResolvedToday != 0 {
		// sr1 di-resolve 100 jam lalu (tanggal digeser) → bukan "hari ini" di zona waktu property
		t.Fatalf("B-05: resolved_today harus memakai hari zona waktu property: %d", m.ResolvedToday)
	}

	// ---------- P3-WAM: WhatsApp manual + log komunikasi SR (P3-TRC-03) ----------
	st, body = e.do(tenA, http.MethodPatch, "/api/v1/tenant/me", map[string]any{"phone": "0812-3456-7890"})
	e.mustJSON(st, body, 200, nil)
	var wa struct {
		Phone          string     `json:"phone"`
		PhoneValid     bool       `json:"phone_valid"`
		URL            string     `json:"url"`
		Text           string     `json:"text"`
		DisabledReason *string    `json:"disabled_reason"`
		LogID          *uuid.UUID `json:"log_id"`
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/whatsapp/compose", map[string]any{"context": "service_request", "object_id": sr3})
	e.mustJSON(st, body, 200, &wa)
	if wa.Phone != "6281234567890" || !wa.PhoneValid || !strings.HasPrefix(wa.URL, "https://wa.me/6281234567890?text=") || !strings.Contains(wa.Text, "tenant service request") {
		t.Fatalf("compose WA: %s", body)
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/whatsapp/send", map[string]any{"context": "service_request", "object_id": sr3})
	e.mustJSON(st, body, 201, &wa)
	if wa.LogID == nil {
		t.Fatalf("send WA harus mencatat log: %s", body)
	}
	var comm struct {
		Data []struct {
			Channel string `json:"channel"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/service-requests/"+sr3.String()+"/communications", nil)
	e.mustJSON(st, body, 200, &comm)
	chans := map[string]bool{}
	for _, c := range comm.Data {
		chans[c.Channel] = true
	}
	if !chans["whatsapp_manual"] || !chans["inapp"] {
		t.Fatalf("log komunikasi SR: %s", body)
	}
	// nomor kosong → tombol nonaktif
	st, body = e.do(tr, http.MethodPost, "/api/v1/whatsapp/compose", map[string]any{"context": "account_approved", "object_id": meB.TenantUserID})
	e.mustJSON(st, body, 200, &wa)
	if wa.PhoneValid || wa.DisabledReason == nil {
		t.Fatalf("P3-WAM-04: nomor kosong harus nonaktif: %s", body)
	}
	if st, _ := e.do(tr, http.MethodPost, "/api/v1/whatsapp/send", map[string]any{"context": "account_approved", "object_id": meB.TenantUserID}); st != 400 {
		t.Fatalf("send tanpa nomor harus 400, got %d", st)
	}
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/whatsapp/compose", map[string]any{"context": "service_request", "object_id": sr3}); st != 403 {
		t.Fatalf("tenant tidak boleh memakai WhatsApp manual, got %d", st)
	}

	// ---------- P3-ACC-05 / P3-ACC-03: reset password → wajib ganti ----------
	var reset struct {
		TemporaryPassword string `json:"temporary_password"`
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/tenant-users/"+meB.TenantUserID.String()+"/reset-password", nil)
	e.mustJSON(st, body, 200, &reset)
	if reset.TemporaryPassword == "" {
		t.Fatalf("reset password: %s", body)
	}
	st, lr := e.loginTenant(t, "rudi@tenant.test", reset.TemporaryPassword)
	if st != 200 || lr["must_change_password"] != true {
		t.Fatalf("login password sementara harus must_change_password: %d %v", st, lr)
	}
	tenB2 := lr["access_token"].(string)
	if st, body := e.do(tenB2, http.MethodPost, "/api/v1/me/password", map[string]any{"current_password": reset.TemporaryPassword, "new_password": "Tenant99999"}); st != 204 {
		t.Fatalf("ganti password: %d %s", st, body)
	}
	if me := e.tenantMe(t, tenB2); me.MustChangePassword {
		t.Fatalf("flag wajib ganti password harus hilang setelah ganti")
	}
	// teks WA password sementara tersusun; log tidak menyimpan password
	st, body = e.do(tr, http.MethodPost, "/api/v1/whatsapp/compose", map[string]any{"context": "password_reset", "object_id": meA.TenantUserID, "temporary_password": "Bvtemp123!"})
	e.mustJSON(st, body, 200, &wa)
	if !strings.Contains(wa.Text, "Bvtemp123!") {
		t.Fatalf("teks reset password: %s", body)
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/whatsapp/send", map[string]any{"context": "password_reset", "object_id": meA.TenantUserID, "temporary_password": "Bvtemp123!"})
	e.mustJSON(st, body, 201, nil)
	var logs struct {
		Data []struct {
			MessagePreview *string `json:"message_preview"`
		} `json:"data"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/whatsapp/logs?object_type=tenant_user&object_id="+meA.TenantUserID.String(), nil)
	e.mustJSON(st, body, 200, &logs)
	if len(logs.Data) != 1 || logs.Data[0].MessagePreview == nil || strings.Contains(*logs.Data[0].MessagePreview, "Bvtemp123!") {
		t.Fatalf("log WA tidak boleh menyimpan password sementara: %s", body)
	}

	// ---------- B-08 / P3-ACC-07 / P3-WAM-05: status & alasan akun di layar login ----------
	admin := e.login("admin@org-a.test")
	st, body = e.do(admin, http.MethodPatch, "/api/v1/locations/"+e.refs.PropertyID.String(), map[string]any{"details": map[string]any{"whatsapp_number": "0811-2222-333"}})
	e.mustJSON(st, body, 200, nil)
	if me := e.tenantMe(t, tenA); me.WhatsAppNumber == nil || *me.WhatsAppNumber != "628112222333" {
		t.Fatalf("nomor WhatsApp pengelola di /tenant/me: %+v", me.WhatsAppNumber)
	}
	var reg struct {
		TenantUserID uuid.UUID `json:"tenant_user_id"`
	}
	st, body = e.do("", http.MethodPost, "/api/v1/tenant/register", map[string]any{"organization_slug": "org-a", "full_name": "Sinta", "email": "sinta@tenant.test", "password": "Tenant12345", "property_id": e.refs.PropertyID, "unit_id": e.refs.UnitA1202})
	e.mustJSON(st, body, 201, &reg)
	st, body = e.do(tr, http.MethodPost, "/api/v1/tenant-users/"+reg.TenantUserID.String()+"/reject", map[string]any{"reason": "Data unit tidak sesuai"})
	e.mustJSON(st, body, 200, nil)
	st, lr = e.loginTenant(t, "sinta@tenant.test", "Tenant12345")
	meta, _ := lr["meta"].(map[string]any)
	if st != 403 || problemCode(lr) != "ACCOUNT_REJECTED" || meta["reason"] != "Data unit tidak sesuai" || meta["whatsapp_number"] != "628112222333" {
		t.Fatalf("B-08: login akun ditolak harus menampilkan alasan & kontak: %d %v", st, lr)
	}

	// ---------- B-01: deep link akun → /account ----------
	st, body = e.do("", http.MethodPost, "/api/v1/tenant/register", map[string]any{"organization_slug": "org-a", "full_name": "Tomi", "email": "tomi@tenant.test", "password": "Tenant12345", "property_id": e.refs.PropertyID, "unit_id": e.refs.UnitA1202})
	e.mustJSON(st, body, 201, &reg)
	st, body = e.do(tr, http.MethodPost, "/api/v1/tenant-users/"+reg.TenantUserID.String()+"/approve", nil)
	e.mustJSON(st, body, 200, nil)
	e.dispatch(t)
	_, lt := e.loginTenant(t, "tomi@tenant.test", "Tenant12345")
	for _, n := range e.inboxOf(t, lt["access_token"].(string)).Data {
		if n.Type == "tenant_account_approved" && (n.DeepLink == nil || *n.DeepLink != "/account") {
			t.Fatalf("B-01: deep link akun harus /account: %v", n.DeepLink)
		}
	}

	// ---------- P3-ACC-08: Tenant Admin mengelola anggota ----------
	st, body = e.do(tr, http.MethodPatch, "/api/v1/tenant-users/"+meA.TenantUserID.String(), map[string]any{"role": "tenant_admin"})
	e.mustJSON(st, body, 200, nil)
	delete(e.tokens, "dewi@tenant.test")
	_, la := e.loginTenant(t, "dewi@tenant.test", "Tenant12345")
	tenA = la["access_token"].(string)
	if me := e.tenantMe(t, tenA); !me.IsTenantAdmin {
		t.Fatalf("dewi harus tenant admin")
	}
	var mc struct {
		Member struct {
			TenantUserID uuid.UUID `json:"tenant_user_id"`
			Status       string    `json:"status"`
		} `json:"member"`
		TemporaryPassword string `json:"temporary_password"`
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/members", map[string]any{"full_name": "Staf Dewi", "email": "staf.dewi@tenant.test", "phone": "081299990000", "unit_ids": []uuid.UUID{e.refs.UnitA1201}})
	e.mustJSON(st, body, 201, &mc)
	if mc.TemporaryPassword == "" || mc.Member.Status != "active" {
		t.Fatalf("anggota baru: %s", body)
	}
	if st, _ := e.do(tenB2, http.MethodPost, "/api/v1/tenant/members", map[string]any{"full_name": "X", "email": "x@tenant.test"}); st != 403 {
		t.Fatalf("tenant_user biasa tidak boleh menambah anggota, got %d", st)
	}
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/members", map[string]any{"full_name": "Y", "email": "y@tenant.test", "unit_ids": []uuid.UUID{e.refs.UnitA1202}}); st != 400 {
		t.Fatalf("anggota tidak boleh diberi unit di luar kelolaan admin, got %d", st)
	}
	st, lm := e.loginTenant(t, "staf.dewi@tenant.test", mc.TemporaryPassword)
	if st != 200 || lm["must_change_password"] != true {
		t.Fatalf("anggota baru login dengan password sementara: %d %v", st, lm)
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/members/"+mc.Member.TenantUserID.String()+"/deactivate", nil)
	e.mustJSON(st, body, 200, nil)
	if st, lm := e.loginTenant(t, "staf.dewi@tenant.test", mc.TemporaryPassword); st != 403 || problemCode(lm) != "ACCOUNT_SUSPENDED" {
		t.Fatalf("anggota dinonaktifkan tidak boleh login: %d %v", st, lm)
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/members/"+mc.Member.TenantUserID.String()+"/reactivate", nil)
	e.mustJSON(st, body, 200, nil)

	// ---------- P3-SRQ-05: lampiran pada pesan (tenant → staf) ----------
	attID := e.uploadTenantAttachment(tenA, "service_request", sr3, "photo", "image/png")
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+sr3.String()+"/messages", map[string]any{"body": "Ini fotonya", "attachment_ids": []uuid.UUID{attID}})
	e.mustJSON(st, body, 201, nil)
	var msgs struct {
		Data []struct {
			Body        string `json:"body"`
			Attachments []struct {
				ID  uuid.UUID `json:"id"`
				URL string    `json:"url"`
			} `json:"attachments"`
		} `json:"data"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/service-requests/"+sr3.String()+"/messages", nil)
	e.mustJSON(st, body, 200, &msgs)
	okAtt := false
	for _, mm := range msgs.Data {
		for _, a := range mm.Attachments {
			okAtt = okAtt || (a.ID == attID && a.URL != "")
		}
	}
	if !okAtt {
		t.Fatalf("lampiran pesan harus terlihat staf dengan URL: %s", body)
	}

	// ---------- P3-PSH: push terkirim lewat adapter Web Push (payload + deep link) ----------
	var nid uuid.UUID
	e.orgTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM notifications WHERE user_id = $1 AND type = 'package_received' ORDER BY created_at DESC LIMIT 1`, meA.ID).Scan(&nid)
	})
	if err := e.app.Notification.SendPush(ctx, e.refs.OrgID, nid); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 1 || fake.eps[0] != "https://push.example.test/sub/dewi" || fake.sent[0]["deep_link"] != "/packages/"+pkg.ID.String() {
		t.Fatalf("web push payload: %+v %v", fake.sent, fake.eps)
	}
	var delivered *time.Time
	e.orgTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT delivered_push_at FROM notifications WHERE id = $1`, nid).Scan(&delivered)
	})
	if delivered == nil {
		t.Fatalf("delivered_push_at harus terisi")
	}
	// P3-ACC-09: preferensi push tenant hanya tipe tenant-facing + kategori
	var prefs struct {
		Data []struct {
			Type     string `json:"type"`
			Category string `json:"category"`
		} `json:"data"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/notifications/preferences", nil)
	e.mustJSON(st, body, 200, &prefs)
	for _, p := range prefs.Data {
		if p.Type == "work_order_assigned" || p.Type == "service_request_received" {
			t.Fatalf("preferensi tenant tidak boleh memuat tipe staf: %s", p.Type)
		}
	}
	if len(prefs.Data) == 0 {
		t.Fatalf("preferensi tenant kosong")
	}

	p3ClientGaps(t, e, p3GapCtx{tenA: tenA, sr3: sr3, msgAtt: attID, vehicleID: veh.ID, permitID: permit.ID, pkgID: pkg.ID, memberEmail: "staf.dewi@tenant.test", memberTemp: mc.TemporaryPassword, prefType: prefs.Data[0].Type, fake: fake})
}
