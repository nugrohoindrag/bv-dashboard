package app_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PRD P2 v2.1 (Roadmap v2.1 §9) — Security: Emergency Alert, incident lengkap, Parking, Lost & Found, perbaikan cepat
// GAP-P2-01. Lihat docs/p2-v21-readiness.md & ADR-017.

type emergencyResp struct {
	ID              uuid.UUID `json:"id"`
	AlertNumber     string    `json:"alert_number"`
	Status          string    `json:"status"`
	Channel         string    `json:"channel"`
	IncidentID      *string   `json:"incident_id"`
	IncidentNumber  *string   `json:"incident_number"`
	EscalationLevel int       `json:"escalation_level"`
	AllowedActions  []string  `json:"allowed_actions"`
	AckSeconds      *int      `json:"ack_seconds"`
	Timeline        []struct {
		EventType string `json:"event_type"`
	} `json:"timeline"`
	Contacts []struct {
		Name string `json:"name"`
	} `json:"contacts"`
}

func TestP2v21EmergencyAlert(t *testing.T) {
	e := setup(t)
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	officer := e.login("wawan@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	bm := e.login("bm@demo.buildingvision.id")
	pid := e.refs.PropertyID

	// kontak darurat (P2-EMG-07)
	for i, c := range []map[string]any{{"name": "Damkar Jakarta", "contact_type": "fire", "phone": "113"}, {"name": "Ambulans", "contact_type": "ambulance", "phone": "118"}} {
		c["property_id"], c["sort_order"] = pid, i
		st, body := e.do(secSpv, http.MethodPost, "/api/v1/emergency-contacts", c)
		e.mustJSON(st, body, 201, nil)
	}
	if st, _ := e.do(officer, http.MethodPost, "/api/v1/emergency-contacts", map[string]any{"property_id": pid, "name": "x", "phone": "1"}); st != 403 {
		t.Fatalf("officer tidak boleh mengelola kontak darurat: %d", st)
	}
	// ---- Panic Button oleh teknisi (P2-EMG-01/02/05): idempoten per id klien ----
	clientID := uuid.New()
	raise := map[string]any{"id": clientID, "emergency_type": "medical", "location_id": e.refs.LobbyA, "channel": "panic_button", "description": "Tamu pingsan di lobby", "gps_lat": -6.2, "gps_lng": 106.8}
	var a emergencyResp
	st, body := e.do(tech, http.MethodPost, "/api/v1/emergency-alerts", raise)
	e.mustJSON(st, body, 201, &a)
	if a.ID != clientID || a.Status != "raised" || a.Channel != "panic_button" || a.IncidentNumber == nil || !strings.HasPrefix(a.AlertNumber, "EMG-") || len(a.Contacts) != 2 {
		t.Fatalf("raise emergency: %s", body)
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/emergency-alerts", raise)
	e.mustJSON(st, body, 201, &a)
	var n int
	e.orgTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM emergency_alerts`).Scan(&n)
	})
	if n != 1 {
		t.Fatalf("raise harus idempoten per id klien: %d alert", n)
	}
	// incident otomatis: kategori emergency, critical, tertaut
	var inc struct {
		Category   string  `json:"category"`
		Severity   string  `json:"severity"`
		SourceType *string `json:"source_type"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/incidents/"+*a.IncidentID, nil)
	e.mustJSON(st, body, 200, &inc)
	if inc.Category != "emergency" || inc.Severity != "critical" || inc.SourceType == nil || *inc.SourceType != "emergency_alert" {
		t.Fatalf("incident otomatis: %s", body)
	}
	// notifikasi segera: security on-duty (fallback team), supervisor, manager; tanpa duplikasi incident_critical
	e.dispatch(t)
	for name, tok := range map[string]string{"officer": officer, "supervisor": secSpv, "manager": bm} {
		ib := e.inboxOf(t, tok)
		if !hasType(ib, "emergency_raised") {
			t.Fatalf("P2-EMG-03 %s harus menerima emergency_raised: %+v", name, ib.Data)
		}
		if hasType(ib, "incident_critical") {
			t.Fatalf("%s tidak boleh menerima incident_critical ganda untuk emergency: %+v", name, ib.Data)
		}
	}
	// Attention Required: emergency aktif (P2-EMG-09)
	var att struct {
		Data []struct {
			Category string    `json:"category"`
			ObjectID uuid.UUID `json:"object_id"`
			Severity string    `json:"severity"`
			DeepLink string    `json:"deep_link"`
		} `json:"data"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/attention-required?limit=50&domain=security&property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &att)
	found := false
	for _, it := range att.Data {
		if it.Category == "active_emergency" && it.ObjectID == a.ID && it.Severity == "critical" && strings.HasPrefix(it.DeepLink, "/security/emergency/") {
			found = true
		}
	}
	if !found {
		t.Fatalf("attention active_emergency: %s", body)
	}
	// pelapor tanpa .view tetap melihat alert-nya; teknisi lain tidak
	if st, body := e.do(tech, http.MethodGet, "/api/v1/emergency-alerts/"+a.ID.String(), nil); st != 200 {
		t.Fatalf("pelapor melihat alert sendiri: %d %s", st, body)
	}
	if st, _ := e.do(e.login("joko@demo.buildingvision.id"), http.MethodGet, "/api/v1/emergency-alerts/"+a.ID.String(), nil); st != 403 {
		t.Fatalf("teknisi lain tidak boleh melihat alert: %d", st)
	}
	// teknisi tidak boleh acknowledge
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/emergency-alerts/"+a.ID.String()+"/acknowledge", nil); st != 403 {
		t.Fatalf("teknisi acknowledge harus 403: %d", st)
	}
	// Security response: acknowledge → respond → catatan tindakan → resolve (P2-EMG-06 timeline)
	st, body = e.do(officer, http.MethodPost, "/api/v1/emergency-alerts/"+a.ID.String()+"/acknowledge", map[string]any{"note": "Menuju lobby"})
	e.mustJSON(st, body, 200, &a)
	if a.Status != "acknowledged" || a.AckSeconds == nil {
		t.Fatalf("acknowledge: %s", body)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tech); !hasType(ib, "emergency_acknowledged") {
		t.Fatalf("pelapor harus diberi tahu bantuan merespons: %+v", ib.Data)
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/emergency-alerts/"+a.ID.String()+"/respond", nil)
	e.mustJSON(st, body, 200, &a)
	st, body = e.do(officer, http.MethodPost, "/api/v1/emergency-alerts/"+a.ID.String()+"/note", map[string]any{"note": "Tamu sadar, ambulans dihubungi"})
	e.mustJSON(st, body, 200, &a)
	if st, _ := e.do(officer, http.MethodPost, "/api/v1/emergency-alerts/"+a.ID.String()+"/resolve", map[string]any{"resolution": "x"}); st != 403 {
		t.Fatalf("officer tanpa .resolve harus 403: %d", st)
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/emergency-alerts/"+a.ID.String()+"/resolve", map[string]any{"resolution": "Tamu dibawa ambulans ke RS"})
	e.mustJSON(st, body, 200, &a)
	got := []string{}
	for _, ev := range a.Timeline {
		got = append(got, ev.EventType)
	}
	want := "raised,incident_created,acknowledged,responding,action,resolved"
	if a.Status != "resolved" || strings.Join(got, ",") != want {
		t.Fatalf("timeline: status=%s events=%v", a.Status, got)
	}
	// incident mengikuti respons: in_progress, assignee responder
	var inc2 struct {
		Status      string  `json:"status"`
		ActionTaken *string `json:"action_taken"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/incidents/"+*a.IncidentID, nil)
	e.mustJSON(st, body, 200, &inc2)
	if inc2.Status != "in_progress" || inc2.ActionTaken == nil {
		t.Fatalf("incident setelah respons: %s", body)
	}

	// ---- Eskalasi bila tidak di-acknowledge (P2-EMG-04) ----
	var b emergencyResp
	st, body = e.do(officer, http.MethodPost, "/api/v1/emergency-alerts", map[string]any{"emergency_type": "fire", "location_id": e.refs.MechRoomA12, "channel": "panic_button"})
	e.mustJSON(st, body, 201, &b)
	e.orgTx(t, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE emergency_alerts SET raised_at = now() - interval '7 minutes' WHERE id = $1`, b.ID)
		return err
	})
	if n, err := e.app.Security.EmergencySweep(context.Background(), e.refs.OrgID); err != nil || n != 1 {
		t.Fatalf("emergency sweep: n=%d err=%v", n, err)
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/emergency-alerts/"+b.ID.String(), nil)
	e.mustJSON(st, body, 200, &b)
	if b.EscalationLevel != 2 {
		t.Fatalf("eskalasi level setelah 7 menit (batas 3 menit) harus 2: %s", body)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, bm); !hasType(ib, "emergency_escalated") {
		t.Fatalf("manager harus menerima emergency_escalated: %+v", ib.Data)
	}
	// alarm palsu → cancel (incident ikut dibatalkan)
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/emergency-alerts/"+b.ID.String()+"/cancel", map[string]any{}); st != 400 {
		t.Fatalf("cancel tanpa alasan harus 400: %d", st)
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/emergency-alerts/"+b.ID.String()+"/cancel", map[string]any{"reason": "Alarm asap dari uap dapur"})
	e.mustJSON(st, body, 200, &b)
	var inc3 struct {
		Status string `json:"status"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/incidents/"+*b.IncidentID, nil)
	e.mustJSON(st, body, 200, &inc3)
	if b.Status != "cancelled" || inc3.Status != "cancelled" {
		t.Fatalf("cancel: alert=%s incident=%s", b.Status, inc3.Status)
	}
	// ---- Panic offline via sync (P2-EMG-08) ----
	offID := uuid.New()
	var res struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-panic", "mutations": []map[string]any{
		mut(uuid.New(), "emergency_alert", offID, "raise_emergency", 1, map[string]any{"emergency_type": "security_threat", "location_id": e.refs.ParkingLG, "channel": "panic_button"}, time.Now().Add(-2*time.Minute)),
	}})
	e.mustJSON(st, body, 200, &res)
	if len(res.Results) != 1 || res.Results[0].Status != "applied" {
		t.Fatalf("raise_emergency sync: %s", body)
	}
	var c emergencyResp
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/emergency-alerts/"+offID.String(), nil)
	e.mustJSON(st, body, 200, &c)
	if c.Status != "raised" || c.Channel != "panic_button" {
		t.Fatalf("alert offline: %s", body)
	}
	// work bundle memuat kontak darurat untuk cache offline
	var bundle struct {
		EmergencyContacts []struct {
			Phone string `json:"phone"`
		} `json:"emergency_contacts"`
	}
	st, body = e.do(officer, http.MethodGet, "/api/v1/sync/work-bundle?device_id=dev-panic", nil)
	e.mustJSON(st, body, 200, &bundle)
	if len(bundle.EmergencyContacts) != 2 {
		t.Fatalf("bundle emergency_contacts: %s", body)
	}
	// list: aktif dulu, filter active
	var list struct {
		Data []emergencyResp `json:"data"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/emergency-alerts?active=true&property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 1 || list.Data[0].ID != offID {
		t.Fatalf("list aktif: %s", body)
	}
	// Panic tanpa lokasi & tanpa property oleh officer ber-role org-wide → property fallback (P2-EMG-08)
	admin := e.login("admin@org-a.test")
	e.createUser(admin, "officer.org@org-a.test", []map[string]any{{"role_id": e.roleID(admin, "security_officer")}}, nil)
	orgOfficer := e.login("officer.org@org-a.test")
	var fb struct {
		PropertyID uuid.UUID `json:"property_id"`
	}
	st, body = e.do(orgOfficer, http.MethodPost, "/api/v1/emergency-alerts", map[string]any{"emergency_type": "medical", "channel": "panic_button"})
	e.mustJSON(st, body, 201, &fb)
	if fb.PropertyID != pid {
		t.Fatalf("fallback property Panic: %s", body)
	}
}

func TestP2v21IncidentComplete(t *testing.T) {
	e := setup(t)
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	bm := e.login("bm@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	var inc struct {
		ID              uuid.UUID `json:"id"`
		Severity        string    `json:"severity"`
		EscalationLevel int       `json:"escalation_level"`
		PeopleCount     int       `json:"people_count"`
		VideoCount      int       `json:"video_count"`
		Flags           []string  `json:"flags"`
		AllowedActions  []string  `json:"allowed_actions"`
		Investigation   *struct {
			Status         string  `json:"status"`
			RootCause      *string `json:"root_cause"`
			InvestigatedAt *string `json:"investigated_at"`
		} `json:"investigation"`
	}
	st, body := e.do(secSpv, http.MethodPost, "/api/v1/incidents", map[string]any{"title": "Perkelahian di area parkir", "category": "suspicious_activity", "severity": "medium", "location_id": e.refs.ParkingLG})
	e.mustJSON(st, body, 201, &inc)
	if !has(inc.AllowedActions, "escalate") || !has(inc.AllowedActions, "manage_people") {
		t.Fatalf("allowed actions incident: %v", inc.AllowedActions)
	}
	// people involved (P2-SIN-03)
	for _, pp := range []map[string]any{{"person_role": "victim", "name": "Andi", "contact": "0812"}, {"person_role": "witness", "name": "Budi Saksi", "identity_number": "3171xxxx"}} {
		st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/people", pp)
		e.mustJSON(st, body, 201, nil)
	}
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/people", map[string]any{"person_role": "alien", "name": "X"}); st != 400 {
		t.Fatalf("person_role tidak valid harus 400: %d", st)
	}
	var people struct {
		Data []struct {
			ID         uuid.UUID `json:"id"`
			PersonRole string    `json:"person_role"`
		} `json:"data"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/incidents/"+inc.ID.String()+"/people", nil)
	e.mustJSON(st, body, 200, &people)
	if len(people.Data) != 2 {
		t.Fatalf("people: %s", body)
	}
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/incidents/"+inc.ID.String()+"/people", nil); st != 403 {
		t.Fatalf("P2-NFR-04: teknisi tidak boleh melihat data orang terlibat: %d", st)
	}
	st, body = e.do(secSpv, http.MethodPatch, "/api/v1/incident-people/"+people.Data[0].ID.String(), map[string]any{"notes": "Luka ringan"})
	e.mustJSON(st, body, 200, nil)
	// investigasi (P2-SIN-06): selesai wajib akar masalah & tindakan korektif
	if st, body := e.do(secSpv, http.MethodPatch, "/api/v1/incidents/"+inc.ID.String(), map[string]any{"investigation_status": "completed"}); st != 400 {
		t.Fatalf("investigasi selesai tanpa root cause harus 400: %d %s", st, body)
	}
	st, body = e.do(secSpv, http.MethodPatch, "/api/v1/incidents/"+inc.ID.String(), map[string]any{"investigation_status": "completed", "investigation_findings": "CCTV menunjukkan cekcok parkir", "root_cause": "Marka parkir tidak jelas", "corrective_action": "Cat ulang marka & tambah patroli jam sibuk", "investigator_user_id": e.refs.Users["security_supervisor"]})
	e.mustJSON(st, body, 200, &inc)
	if inc.Investigation == nil || inc.Investigation.Status != "completed" || inc.Investigation.InvestigatedAt == nil || inc.PeopleCount != 2 {
		t.Fatalf("investigasi: %s", body)
	}
	// root cause tidak boleh dikosongkan setelah investigasi selesai; investigator boleh dikosongkan (UUID nol)
	if st, body := e.do(secSpv, http.MethodPatch, "/api/v1/incidents/"+inc.ID.String(), map[string]any{"root_cause": "  "}); st != 400 {
		t.Fatalf("mengosongkan root cause pada investigasi selesai harus 400: %d %s", st, body)
	}
	st, body = e.do(secSpv, http.MethodPatch, "/api/v1/incidents/"+inc.ID.String(), map[string]any{"investigator_user_id": uuid.Nil})
	e.mustJSON(st, body, 200, &inc)
	if inc.Investigation == nil || inc.Investigation.RootCause == nil || *inc.Investigation.RootCause == "" {
		t.Fatalf("root cause tetap tersimpan: %s", body)
	}
	// eskalasi manual (P2-SIN-05)
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/escalate", map[string]any{}); st != 400 {
		t.Fatalf("eskalasi tanpa alasan harus 400: %d", st)
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/escalate", map[string]any{"reason": "Pelaku kembali ke lokasi", "raise_severity": true, "escalate_to_user_id": e.refs.Users["security_manager"]})
	e.mustJSON(st, body, 200, &inc)
	if inc.EscalationLevel != 1 || inc.Severity != "high" || !has(inc.Flags, "escalated") {
		t.Fatalf("eskalasi incident: %s", body)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, bm); !hasType(ib, "incident_escalated") {
		t.Fatalf("manager harus menerima incident_escalated: %+v", ib.Data)
	}
	if ib := e.inboxOf(t, e.login("sec.manager@demo.buildingvision.id")); !hasType(ib, "incident_escalated") {
		t.Fatalf("tujuan eskalasi harus menerima incident_escalated: %+v", ib.Data)
	}
	// video evidence (P2-SIN-04)
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "incident", "object_id": inc.ID, "attachment_type": "video", "content_type": "video/mp4", "size_bytes": 5 * 1024 * 1024})
	e.mustJSON(st, body, 201, &pre)
	head := append([]byte{0, 0, 0, 0x18}, []byte("ftypmp42")...)
	_ = e.store.Put(context.Background(), pre.StorageKey, "video/mp4", bytes.NewReader(append(head, make([]byte, 1000)...)), int64(len(head)+1000))
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "incident", "object_id": inc.ID, "attachment_type": "photo", "content_type": "video/mp4", "size_bytes": 1000}); st != 400 {
		t.Fatalf("video dengan attachment_type photo harus 400: %d", st)
	}
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "incident", "object_id": inc.ID, "attachment_type": "video", "content_type": "video/mp4", "size_bytes": 80 * 1024 * 1024}); st != 400 {
		t.Fatalf("video > batas harus 400: %d", st)
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/incidents/"+inc.ID.String(), nil)
	e.mustJSON(st, body, 200, &inc)
	if inc.VideoCount != 1 {
		t.Fatalf("video_count: %s", body)
	}
	// eskalasi otomatis saat SLA breach (P2-SIN-05)
	var inc2 struct {
		ID              uuid.UUID `json:"id"`
		EscalationLevel int       `json:"escalation_level"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents", map[string]any{"title": "Pintu darurat terganjal", "category": "safety", "severity": "high", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &inc2)
	e.orgTx(t, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE sla_tracking SET started_at = now() - interval '10 hours', response_due_at = now() - interval '8 hours', resolution_due_at = now() - interval '1 hour' WHERE object_type = 'incident' AND object_id = $1`, inc2.ID)
		return err
	})
	if _, err := e.app.Operations.SLASweep(context.Background(), e.refs.OrgID); err != nil {
		t.Fatal(err)
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/incidents/"+inc2.ID.String(), nil)
	e.mustJSON(st, body, 200, &inc2)
	if inc2.EscalationLevel != 1 {
		t.Fatalf("SLA breach incident harus eskalasi otomatis level 1: %s", body)
	}
}

func TestP2v21ParkingAndLostFound(t *testing.T) {
	e := setup(t)
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	officer := e.login("wawan@demo.buildingvision.id")
	siti := e.login("siti@demo.buildingvision.id")
	reception := e.login("reception@demo.buildingvision.id")
	pid := e.refs.PropertyID

	// ---- Parking (P2-PRK-01..04) ----
	var area struct {
		ID        uuid.UUID `json:"id"`
		Capacity  int       `json:"capacity"`
		Occupied  int       `json:"occupied"`
		Available int       `json:"available"`
	}
	st, body := e.do(secSpv, http.MethodPost, "/api/v1/parking-areas", map[string]any{"property_id": pid, "code": "p-b1", "name": "Parkir Basement 1", "area_type": "tenant", "capacity": 2, "location_id": e.refs.ParkingLG})
	e.mustJSON(st, body, 201, &area)
	if st, _ := e.do(officer, http.MethodPost, "/api/v1/parking-areas", map[string]any{"property_id": pid, "code": "x", "name": "x"}); st != 403 {
		t.Fatalf("officer tidak boleh membuat area parkir: %d", st)
	}
	var veh struct {
		ID          uuid.UUID `json:"id"`
		PlateNumber string    `json:"plate_number"`
		PermitValid bool      `json:"permit_valid"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/vehicles", map[string]any{"property_id": pid, "plate_number": "b 1234 xyz", "owner_type": "tenant", "owner_name": "PT Maju", "parking_area_id": area.ID, "permit_until": time.Now().AddDate(0, 1, 0).Format("2006-01-02")})
	e.mustJSON(st, body, 201, &veh)
	if veh.PlateNumber != "B1234XYZ" || !veh.PermitValid {
		t.Fatalf("kendaraan: %s", body)
	}
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/vehicles", map[string]any{"property_id": pid, "plate_number": "B-1234-XYZ"}); st != 409 {
		t.Fatalf("plat duplikat harus 409: %d", st)
	}
	var log struct {
		ID         uuid.UUID `json:"id"`
		Registered bool      `json:"registered"`
		ExitedAt   *string   `json:"exited_at"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/parking-logs", map[string]any{"parking_area_id": area.ID, "plate_number": "B 1234 XYZ", "gate": "Gate 1"})
	e.mustJSON(st, body, 201, &log)
	if !log.Registered {
		t.Fatalf("log masuk kendaraan terdaftar: %s", body)
	}
	if st, _ := e.do(officer, http.MethodPost, "/api/v1/parking-logs", map[string]any{"parking_area_id": area.ID, "plate_number": "B1234XYZ"}); st != 409 {
		t.Fatalf("masuk ganda harus 409: %d", st)
	}
	var areas struct {
		Data []struct {
			Occupied  int `json:"occupied"`
			Available int `json:"available"`
		} `json:"data"`
	}
	st, body = e.do(officer, http.MethodGet, "/api/v1/parking-areas?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &areas)
	if len(areas.Data) != 1 || areas.Data[0].Occupied != 1 || areas.Data[0].Available != 1 {
		t.Fatalf("okupansi area parkir: %s", body)
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/parking-logs/exit", map[string]any{"property_id": pid, "plate_number": "b1234xyz"})
	e.mustJSON(st, body, 200, &log)
	if log.ExitedAt == nil {
		t.Fatalf("log keluar: %s", body)
	}
	// pelanggaran → incident
	var vio struct {
		ID              uuid.UUID `json:"id"`
		ViolationNumber string    `json:"violation_number"`
		Status          string    `json:"status"`
		VehicleID       *string   `json:"vehicle_id"`
		IncidentNumber  *string   `json:"incident_number"`
		AllowedActions  []string  `json:"allowed_actions"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/parking-violations", map[string]any{"parking_area_id": area.ID, "plate_number": "D 999 ZZ", "violation_type": "blocking", "description": "Menghalangi jalur darurat", "action_taken": "warning"})
	e.mustJSON(st, body, 201, &vio)
	if !strings.HasPrefix(vio.ViolationNumber, "PKV-") || vio.VehicleID != nil || vio.Status != "open" {
		t.Fatalf("pelanggaran: %s", body)
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/parking-violations/"+vio.ID.String()+"/create-incident", map[string]any{"severity": "medium"})
	e.mustJSON(st, body, 200, &vio)
	if vio.Status != "escalated" || vio.IncidentNumber == nil {
		t.Fatalf("pelanggaran → incident: %s", body)
	}
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/parking-violations/"+vio.ID.String()+"/resolve", map[string]any{"resolution": "x"}); st != 409 {
		t.Fatalf("resolve pelanggaran escalated harus 409: %d", st)
	}

	// ---- Lost & Found (P2-LNF-01..03) ----
	var item struct {
		ID             uuid.UUID `json:"id"`
		ItemNumber     string    `json:"item_number"`
		Status         string    `json:"status"`
		RetentionUntil string    `json:"retention_until"`
		AllowedActions []string  `json:"allowed_actions"`
		ClaimantIdent  *string   `json:"claimant_identity"`
	}
	st, body = e.do(siti, http.MethodPost, "/api/v1/lost-found/items", map[string]any{"category": "wallet", "description": "Dompet kulit coklat", "found_location_id": e.refs.LobbyA, "storage_location": "Pos Security Lobby"})
	e.mustJSON(st, body, 201, &item)
	if !strings.HasPrefix(item.ItemNumber, "LNF-") || item.Status != "stored" || item.RetentionUntil == "" {
		t.Fatalf("barang temuan: %s", body)
	}
	var rep struct {
		ID           uuid.UUID `json:"id"`
		ReportNumber string    `json:"report_number"`
		Status       string    `json:"status"`
	}
	st, body = e.do(reception, http.MethodPost, "/api/v1/lost-found/reports", map[string]any{"category": "wallet", "description": "Dompet coklat berisi KTP", "reporter_name": "Ibu Rina", "reporter_contact": "0813", "lost_location_id": e.refs.LobbyA, "lost_at": time.Now().Add(-3 * time.Hour)})
	e.mustJSON(st, body, 201, &rep)
	var matches struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/lost-found/reports/"+rep.ID.String()+"/matches", nil)
	e.mustJSON(st, body, 200, &matches)
	if len(matches.Data) != 1 || matches.Data[0].ID != item.ID {
		t.Fatalf("P2-LNF-02 kandidat pencocokan: %s", body)
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/lost-found/items/"+item.ID.String()+"/match", map[string]any{"report_id": rep.ID})
	e.mustJSON(st, body, 200, &item)
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/lost-found/items/"+item.ID.String()+"/return", map[string]any{"claimant_name": "Ibu Rina"}); st != 400 {
		t.Fatalf("serah terima tanpa identitas harus 400: %d", st)
	}
	sig := e.uploadAttachment(secSpv, "lost_found_item", item.ID, "signature", "image/png")
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/lost-found/items/"+item.ID.String()+"/return", map[string]any{"claimant_name": "Ibu Rina", "claimant_identity": "KTP 3171234567890001", "claimant_contact": "0813", "signature_attachment_id": sig})
	e.mustJSON(st, body, 200, &item)
	if item.Status != "returned" || item.ClaimantIdent == nil {
		t.Fatalf("serah terima: %s", body)
	}
	var reports struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/lost-found/reports?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &reports)
	if len(reports.Data) != 1 || reports.Data[0].Status != "closed" {
		t.Fatalf("laporan kehilangan ditutup setelah serah terima: %s", body)
	}
	// officer melihat barang tanpa identitas pengklaim (data pribadi)
	var viewOfficer struct {
		ClaimantIdentity *string `json:"claimant_identity"`
	}
	st, body = e.do(officer, http.MethodGet, "/api/v1/lost-found/items/"+item.ID.String(), nil)
	e.mustJSON(st, body, 200, &viewOfficer)
	if viewOfficer.ClaimantIdentity != nil {
		t.Fatalf("identitas pengklaim hanya untuk security.lost_found.manage: %s", body)
	}
	// disposal hanya setelah masa simpan
	var old struct {
		ID             uuid.UUID `json:"id"`
		AllowedActions []string  `json:"allowed_actions"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/lost-found/items", map[string]any{"property_id": pid, "category": "clothing", "description": "Jaket hitam", "found_at": time.Now().AddDate(0, 0, -100)})
	e.mustJSON(st, body, 201, &old)
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/lost-found/items/"+old.ID.String(), nil)
	e.mustJSON(st, body, 200, &old)
	if !has(old.AllowedActions, "dispose") {
		t.Fatalf("barang lewat masa simpan harus bisa di-disposal: %s", body)
	}
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/lost-found/items/"+item.ID.String()+"/dispose", map[string]any{"disposal_method": "donated"}); st != 409 {
		t.Fatalf("dispose barang yang sudah dikembalikan harus 409: %d", st)
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/lost-found/items/"+old.ID.String()+"/dispose", map[string]any{"disposal_method": "donated", "disposal_note": "Disumbangkan ke yayasan"})
	e.mustJSON(st, body, 200, nil)
}

// GAP-P2-01: notifikasi checkpoint missed, low stock consumable ke Housekeeping, skor inspeksi HK.
func TestP2v21QuickFixes(t *testing.T) {
	e := setup(t)
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	officer := e.login("wawan@demo.buildingvision.id")
	bm := e.login("bm@demo.buildingvision.id")
	// ---- P2-PAT-05: checkpoint missed → Security Supervisor + Attention Required ----
	var cps []uuid.UUID
	for _, loc := range []uuid.UUID{e.refs.LobbyA, e.refs.ParkingLG} {
		var cp struct {
			ID uuid.UUID `json:"id"`
		}
		st, body := e.do(secSpv, http.MethodPost, "/api/v1/checkpoints", map[string]any{"name": "CP " + loc.String()[:4], "location_id": loc})
		e.mustJSON(st, body, 201, &cp)
		cps = append(cps, cp.ID)
	}
	var route struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(secSpv, http.MethodPost, "/api/v1/patrol-routes", map[string]any{"property_id": e.refs.PropertyID, "name": "Rute malam", "checkpoints": []map[string]any{{"checkpoint_id": cps[0], "sort_order": 1}, {"checkpoint_id": cps[1], "sort_order": 2}}})
	e.mustJSON(st, body, 201, &route)
	var patrol workItem
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/patrol-routes/"+route.ID.String()+"/patrol-tasks", map[string]any{"assignee_user_id": e.refs.Users["security_officer"]})
	e.mustJSON(st, body, 201, &patrol)
	e.do(officer, http.MethodPost, "/api/v1/tasks/"+patrol.ID.String()+"/start", map[string]any{})
	st, body = e.do(officer, http.MethodPost, "/api/v1/patrol-tasks/"+patrol.ID.String()+"/scans", map[string]any{"checkpoint_id": cps[0]})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(officer, http.MethodPost, "/api/v1/tasks/"+patrol.ID.String()+"/complete", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	e.dispatch(t)
	if ib := e.inboxOf(t, secSpv); !hasType(ib, "checkpoint_missed") {
		t.Fatalf("P2-PAT-05 supervisor harus menerima checkpoint_missed: %+v", ib.Data)
	}
	var att struct {
		Data []struct {
			Category string    `json:"category"`
			ObjectID uuid.UUID `json:"object_id"`
		} `json:"data"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/attention-required?limit=50&property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &att)
	ok := false
	for _, it := range att.Data {
		if it.Category == "checkpoint_missed" && it.ObjectID == patrol.ID {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("attention checkpoint_missed: %s", body)
	}

	// ---- P2-CNS-03: low stock consumable → Housekeeping Supervisor (bukan Engineering) ----
	hkMgr := e.login("hk.manager@demo.buildingvision.id")
	hkSpv := e.login("hk.spv@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	engMgr := e.login("eng.manager@demo.buildingvision.id")
	var loc, item struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/inventory/stock-locations", map[string]any{"property_id": e.refs.PropertyID, "name": "Gudang HK", "is_default": true})
	e.mustJSON(st, body, 201, &loc)
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/inventory/items", map[string]any{"name": "Cairan pembersih lantai", "category": "consumable", "unit": "liter", "min_stock": 5, "unit_cost": 25000})
	e.mustJSON(st, body, 201, &item)
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/inventory/stock-transactions", map[string]any{"item_id": item.ID, "stock_location_id": loc.ID, "transaction_type": "in", "quantity": 6})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/inventory/stock-transactions", map[string]any{"item_id": item.ID, "stock_location_id": loc.ID, "transaction_type": "out", "quantity": 2})
	e.mustJSON(st, body, 201, nil)
	e.dispatch(t)
	if ib := e.inboxOf(t, hkSpv); !hasType(ib, "inventory_low_stock") {
		t.Fatalf("P2-CNS-03 HK supervisor harus menerima low stock consumable: %+v", ib.Data)
	}
	if ib := e.inboxOf(t, engSpv); hasType(ib, "inventory_low_stock") {
		t.Fatalf("P2-CNS-03 engineering supervisor tidak boleh menerima low stock consumable: %+v", ib.Data)
	}

	// ---- P2-HKI-03: skor inspeksi HK 0–100 (2 dari 3 item OK = 67) ----
	var tpl struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(hkMgr, http.MethodPost, "/api/v1/checklist-templates", map[string]any{"name": "Inspeksi Toilet", "domain": "housekeeping", "applies_to": []string{"cleaning", "inspection"},
		"items": []map[string]any{{"label": "Lantai", "item_type": "ok_notok_na"}, {"label": "Cermin", "item_type": "ok_notok_na"}, {"label": "Wastafel", "item_type": "ok_notok_na"}, {"label": "Catatan", "item_type": "text", "is_required": false}}})
	e.mustJSON(st, body, 201, &tpl)
	st, body = e.do(hkMgr, http.MethodPost, "/api/v1/checklist-templates/"+tpl.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, nil)
	var ct workItem
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-tasks", map[string]any{"title": "Spot cleaning toilet", "location_id": e.refs.ToiletA12, "assignee_user_id": e.refs.Users["housekeeping_staff"]})
	e.mustJSON(st, body, 201, &ct)
	siti := e.login("siti@demo.buildingvision.id")
	e.do(siti, http.MethodPost, "/api/v1/tasks/"+ct.ID.String()+"/start", nil)
	st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+ct.ID.String()+"/complete", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	var insp workItem
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/housekeeping-inspections", map[string]any{"cleaning_task_id": ct.ID, "checklist_template_id": tpl.ID})
	e.mustJSON(st, body, 201, &insp)
	e.do(hkSpv, http.MethodPost, "/api/v1/tasks/"+insp.ID.String()+"/start", nil)
	var runs struct {
		Data []struct {
			Items []struct {
				ID       uuid.UUID `json:"id"`
				ItemType string    `json:"item_type"`
			} `json:"items"`
		} `json:"data"`
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/tasks/"+insp.ID.String()+"/checklist-runs", nil)
	e.mustJSON(st, body, 200, &runs)
	okCount := 0
	for _, it := range runs.Data[0].Items {
		if it.ItemType != "ok_notok_na" {
			continue
		}
		ans := map[string]any{"result_value": "ok"}
		if okCount == 2 {
			ans = map[string]any{"result_value": "not_ok", "note": "Wastafel berkerak"}
		}
		okCount++
		st, body = e.do(hkSpv, http.MethodPost, "/api/v1/checklist-run-items/"+it.ID.String()+"/answer", ans)
		e.mustJSON(st, body, 200, nil)
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/tasks/"+insp.ID.String()+"/complete", map[string]any{})
	e.mustJSON(st, body, 200, &insp)
	if score, _ := insp.Extension["score"].(float64); score != 67 {
		t.Fatalf("P2-HKI-03 skor inspeksi: %v (%s)", insp.Extension["score"], body)
	}
}
