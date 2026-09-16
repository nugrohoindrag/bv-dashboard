package app_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type resResp struct {
	ID                uuid.UUID  `json:"id"`
	ReservationNumber string     `json:"reservation_number"`
	Status            string     `json:"status"`
	StayStatus        string     `json:"stay_status"`
	RoomLocationID    *uuid.UUID `json:"room_location_id"`
	RoomNumber        *string    `json:"room_number"`
	RoomStatus        *string    `json:"room_status"`
	TotalAmount       int64      `json:"total_amount"`
	Nights            int        `json:"nights"`
	InvoiceID         *uuid.UUID `json:"invoice_id"`
	InvoiceNumber     *string    `json:"invoice_number"`
	AllowedActions    []string   `json:"allowed_actions"`
}

type roomResp struct {
	LocationID      uuid.UUID `json:"location_id"`
	RoomCode        string    `json:"room_code"`
	RoomNumber      string    `json:"room_number"`
	RoomStatus      string    `json:"room_status"`
	AllowedStatuses []string  `json:"allowed_statuses"`
	OpenCleaning    int       `json:"open_cleaning_tasks"`
}

// P1.6 — Hotel Booking Management (PRD §3.9, WF-P1-007, AT-P1-000C, DoD #23, #39, #42) + Reservation ↔ Room ↔ Housekeeping.
func TestHotelBooking(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	pm := e.login("pm@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	hkSpv := e.login("hk.spv@demo.buildingvision.id")
	siti := e.login("siti@demo.buildingvision.id")

	// property demo = office → hotel endpoints ditolak (AC-15/18)
	if st, body := e.do(pm, http.MethodGet, "/api/v1/hotel/room-types?property_id="+e.refs.PropertyID.String(), nil); st != 403 || !strings.Contains(string(body), "CAPABILITY_NOT_ENABLED") {
		t.Fatalf("hotel pada profile office harus 403 CAPABILITY_NOT_ENABLED: %d %s", st, body)
	}
	// buat property hotel + lantai; beri PM & staf akses (org-wide roles pm/hk sudah per property demo → gunakan admin untuk setup, PM dapat role via user_roles NULL property? PM di seed terikat property demo)
	var hotelProp struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "property", "name": "Grand City Hotel", "details": map[string]any{"timezone": "Asia/Jakarta", "profile": "hotel"}})
	e.mustJSON(st, body, 201, &hotelProp)
	var bld, floor struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "building", "parent_id": hotelProp.ID, "name": "Main Wing", "details": map[string]any{}})
	e.mustJSON(st, body, 201, &bld)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "floor", "parent_id": bld.ID, "name": "Floor 5", "details": map[string]any{"floor_number": 5}})
	e.mustJSON(st, body, 201, &floor)
	// tim housekeeping hotel (untuk turnover task)
	st, body = e.do(admin, http.MethodPost, "/api/v1/teams", map[string]any{"property_id": hotelProp.ID, "name": "Housekeeping Team Hotel", "domain": "housekeeping"})
	e.mustJSON(st, body, 201, nil)

	// ---- room type, rooms, rate ----
	var rt struct {
		ID           uuid.UUID `json:"id"`
		RoomTypeCode string    `json:"room_type_code"`
		RoomCount    int       `json:"room_count"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/room-types", map[string]any{"property_id": hotelProp.ID, "name": "Deluxe", "capacity_adults": 2, "base_rate": 900000, "amenities": []string{"wifi", "ac"}})
	e.mustJSON(st, body, 201, &rt)
	if !strings.HasPrefix(rt.RoomTypeCode, "RT-") {
		t.Fatalf("room type: %+v", rt)
	}
	var r501, r502 roomResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/rooms", map[string]any{"property_id": hotelProp.ID, "floor_id": floor.ID, "room_type_id": rt.ID, "room_number": "501"})
	e.mustJSON(st, body, 201, &r501)
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/rooms", map[string]any{"property_id": hotelProp.ID, "floor_id": floor.ID, "room_type_id": rt.ID, "room_number": "502"})
	e.mustJSON(st, body, 201, &r502)
	if !strings.HasPrefix(r501.RoomCode, "ROOM-") || r501.RoomStatus != "available" {
		t.Fatalf("room: %+v", r501)
	}
	// kamar adalah unit (One Building Data Model): tampil di /locations?type=unit
	st, body = e.do(admin, http.MethodGet, "/api/v1/locations/"+r501.LocationID.String(), nil)
	if st != 200 || !strings.Contains(string(body), `"unit_type":"hotel_room"`) {
		t.Fatalf("kamar harus unit hotel_room: %d %s", st, body)
	}
	// rate weekend lebih spesifik
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/rates", map[string]any{"room_type_id": rt.ID, "name": "Weekend", "rate_per_night": 1200000, "weekdays": []int{5, 6}, "priority": 10})
	e.mustJSON(st, body, 201, nil)

	// ---- availability ----
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(jkt)
	// pilih Senin depan agar tarif deterministik: 2 malam Senin & Selasa → base_rate ×2
	ci := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jkt).AddDate(0, 0, 7)
	for ci.Weekday() != time.Monday {
		ci = ci.AddDate(0, 0, 1)
	}
	co := ci.AddDate(0, 0, 2)
	d := func(t time.Time) string { return t.Format("2006-01-02") }
	var av struct {
		Data []struct {
			AvailableRooms int         `json:"available_rooms"`
			TotalRooms     int         `json:"total_rooms"`
			TotalAmount    int64       `json:"total_amount"`
			FreeRoomIDs    []uuid.UUID `json:"free_room_ids"`
		} `json:"data"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/availability?property_id="+hotelProp.ID.String()+"&check_in="+d(ci)+"&check_out="+d(co), nil)
	e.mustJSON(st, body, 200, &av)
	if len(av.Data) != 1 || av.Data[0].TotalRooms != 2 || av.Data[0].AvailableRooms != 2 || av.Data[0].TotalAmount != 1800000 {
		t.Fatalf("availability awal: %+v", av)
	}
	// ---- reservasi 1 (kamar 501) ----
	var res1 resResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations", map[string]any{"property_id": hotelProp.ID, "room_type_id": rt.ID, "room_location_id": r501.LocationID, "guest_name": "Andi Wijaya", "guest_phone": "0812", "guest_email": "andi@guest.test", "adults": 2, "check_in_date": d(ci), "check_out_date": d(co), "source": "phone", "confirm": true})
	e.mustJSON(st, body, 201, &res1)
	if !strings.HasPrefix(res1.ReservationNumber, "RES-") || res1.Status != "confirmed" || res1.Nights != 2 || res1.TotalAmount != 1800000 || res1.RoomNumber == nil || *res1.RoomNumber != "501" {
		t.Fatalf("reservasi: %+v", res1)
	}
	// DoD #42: kamar 501 sudah dipesan pada tanggal overlap → 409
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations", map[string]any{"property_id": hotelProp.ID, "room_type_id": rt.ID, "room_location_id": r501.LocationID, "guest_name": "Budi", "check_in_date": d(ci.AddDate(0, 0, 1)), "check_out_date": d(co.AddDate(0, 0, 1))})
	if st != 409 || !strings.Contains(string(body), "ROOM_UNAVAILABLE") {
		t.Fatalf("konflik kamar harus 409: %d %s", st, body)
	}
	// reservasi 2 tanpa kamar (tipe masih punya 1 kamar) → OK; reservasi 3 → NO_AVAILABILITY
	var res2 resResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations", map[string]any{"property_id": hotelProp.ID, "room_type_id": rt.ID, "guest_name": "Citra", "check_in_date": d(ci), "check_out_date": d(co)})
	e.mustJSON(st, body, 201, &res2)
	if st, body := e.do(admin, http.MethodPost, "/api/v1/hotel/reservations", map[string]any{"property_id": hotelProp.ID, "room_type_id": rt.ID, "guest_name": "Dedi", "check_in_date": d(ci), "check_out_date": d(co)}); st != 409 || !strings.Contains(string(body), "NO_AVAILABILITY") {
		t.Fatalf("over-capacity harus 409 NO_AVAILABILITY: %d %s", st, body)
	}
	// kapasitas tamu
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/hotel/reservations", map[string]any{"property_id": hotelProp.ID, "room_type_id": rt.ID, "guest_name": "Eka", "adults": 5, "check_in_date": d(co), "check_out_date": d(co.AddDate(0, 0, 1))}); st != 400 {
		t.Fatalf("kapasitas dewasa terlampaui harus 400: %d", st)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/availability?property_id="+hotelProp.ID.String()+"&check_in="+d(ci)+"&check_out="+d(co), nil)
	e.mustJSON(st, body, 200, &av)
	if av.Data[0].AvailableRooms != 0 || len(av.Data[0].FreeRoomIDs) != 1 {
		t.Fatalf("availability setelah 2 reservasi: %+v", av.Data[0])
	}
	// assign room res2 → 502; assign ke 501 (terpakai) → 409
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/hotel/reservations/"+res2.ID.String()+"/assign_room", map[string]any{"room_location_id": r501.LocationID}); st != 409 {
		t.Fatalf("assign kamar terpakai harus 409: %d", st)
	}
	var act struct {
		Reservation       resResp `json:"reservation"`
		TemporaryPassword string  `json:"temporary_password"`
		GuestAccountEmail string  `json:"guest_account_email"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations/"+res2.ID.String()+"/assign_room", map[string]any{"room_location_id": r502.LocationID})
	e.mustJSON(st, body, 200, &act)
	if act.Reservation.RoomNumber == nil || *act.Reservation.RoomNumber != "502" {
		t.Fatalf("assign room: %+v", act.Reservation)
	}
	// kalender
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/reservations/calendar?property_id="+hotelProp.ID.String()+"&from="+d(ci.AddDate(0, 0, -1))+"&to="+d(co.AddDate(0, 0, 3)), nil)
	if st != 200 || !strings.Contains(string(body), res1.ReservationNumber) || !strings.Contains(string(body), res2.ReservationNumber) {
		t.Fatalf("calendar: %d %s", st, body)
	}
	// technician tidak boleh melihat reservasi
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/hotel/reservations?property_id="+hotelProp.ID.String(), nil); st != 403 {
		t.Fatalf("technician reservations: %d", st)
	}

	// ---- check-in memerlukan tanggal tiba ≤ hari ini: buat reservasi hari ini di kamar 501 (bebas hari ini) ----
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jkt)
	var resToday resResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations", map[string]any{"property_id": hotelProp.ID, "room_type_id": rt.ID, "room_location_id": r501.LocationID, "guest_name": "Fajar Nugroho", "guest_email": "fajar@guest.test", "check_in_date": d(today), "check_out_date": d(today.AddDate(0, 0, 1)), "confirm": true})
	e.mustJSON(st, body, 201, &resToday)
	if !has(resToday.AllowedActions, "check_in") || resToday.StayStatus != "arriving_today" {
		t.Fatalf("arriving today: %+v", resToday)
	}
	// check-in + akun Guest App
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations/"+resToday.ID.String()+"/check_in", map[string]any{"create_guest_account": true})
	e.mustJSON(st, body, 200, &act)
	if act.Reservation.Status != "checked_in" || act.Reservation.RoomStatus == nil || *act.Reservation.RoomStatus != "occupied" || act.TemporaryPassword == "" {
		t.Fatalf("check-in: %+v pw=%q", act.Reservation, act.TemporaryPassword)
	}
	// Guest login ke Tenant App → profile hotel, terminologi Guest/Room, unit = kamar 501
	stL, resp := e.loginTenant(t, "fajar@guest.test", act.TemporaryPassword)
	if stL != 200 {
		t.Fatalf("guest login: %d %v", stL, resp)
	}
	guestTok := resp["access_token"].(string)
	st, body = e.do(guestTok, http.MethodGet, "/api/v1/tenant/me", nil)
	if st != 200 || !strings.Contains(string(body), `"profile":"hotel"`) || !strings.Contains(string(body), `"Tamu"`) || !strings.Contains(string(body), "501") {
		t.Fatalf("guest /tenant/me: %d %s", st, body)
	}
	// Guest Request via Tenant App → SR pada kamar
	st, body = e.do(guestTok, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "room_service", "description": "Mohon tambah handuk dan air mineral ke kamar"})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/reservations/"+resToday.ID.String(), nil)
	e.mustJSON(st, body, 200, &resToday)
	if resToday.Status != "checked_in" {
		t.Fatalf("status setelah check-in: %s", resToday.Status)
	}
	var occ struct {
		Occupied          int            `json:"occupied"`
		InHouse           int            `json:"in_house"`
		OpenGuestRequests int            `json:"open_guest_requests"`
		ByStatus          map[string]int `json:"by_status"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/occupancy?property_id="+hotelProp.ID.String(), nil)
	e.mustJSON(st, body, 200, &occ)
	if occ.Occupied != 1 || occ.InHouse != 1 || occ.OpenGuestRequests != 1 {
		t.Fatalf("occupancy: %+v", occ)
	}
	// ---- check-out → kamar Dirty + turnover cleaning task + invoice (Billing linkage) ----
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations/"+resToday.ID.String()+"/check_out", nil)
	e.mustJSON(st, body, 200, &act)
	if act.Reservation.Status != "checked_out" || act.Reservation.RoomStatus == nil || *act.Reservation.RoomStatus != "dirty" || act.Reservation.InvoiceID == nil || act.Reservation.InvoiceNumber == nil || !strings.HasPrefix(*act.Reservation.InvoiceNumber, "INV-") {
		t.Fatalf("check-out: %+v", act.Reservation)
	}
	// akses Guest App berakhir (valid_until = hari ini → masih hari ini valid; cukup pastikan tenant_access valid_until terisi)
	var rooms struct {
		Data []roomResp `json:"data"`
	}
	// daftar kamar tanpa filter status (regresi: filter nil harus mengembalikan semua kamar)
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/rooms?property_id="+hotelProp.ID.String(), nil)
	e.mustJSON(st, body, 200, &rooms)
	if len(rooms.Data) != 2 {
		t.Fatalf("daftar kamar tanpa filter harus 2: %+v", rooms.Data)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/rooms?property_id="+hotelProp.ID.String()+"&status=dirty", nil)
	e.mustJSON(st, body, 200, &rooms)
	if len(rooms.Data) != 1 || rooms.Data[0].RoomNumber != "501" || rooms.Data[0].OpenCleaning != 1 {
		t.Fatalf("kamar dirty + cleaning task turnover: %+v", rooms.Data)
	}
	// Housekeeping: cleaning task turnover ada di daftar cleaning property hotel; selesaikan → kamar Clean
	var tasks struct {
		Data []workItem `json:"data"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/tasks?property_id="+hotelProp.ID.String()+"&type=cleaning", nil)
	e.mustJSON(st, body, 200, &tasks)
	if len(tasks.Data) != 1 || !strings.Contains(tasks.Data[0].Title, "501") {
		t.Fatalf("turnover task: %+v", tasks.Data)
	}
	turn := tasks.Data[0]
	// beri akses hk.spv & siti ke property hotel via role assignment admin (user_roles per property) — pakai admin sebagai eksekutor (system-wide) agar test tetap fokus linkage
	st, body = e.do(admin, http.MethodPost, "/api/v1/tasks/"+turn.ID.String()+"/assign", map[string]any{"assignee_user_id": e.refs.Users["housekeeping_staff"]})
	e.mustJSON(st, body, 200, &turn)
	_ = hkSpv
	_ = siti
	st, body = e.do(admin, http.MethodPost, "/api/v1/tasks/"+turn.ID.String()+"/start", nil)
	if st != 200 {
		// admin bukan assignee → gunakan manage (organization_admin punya *) — start memerlukan IsAssignee||HasManage: admin punya manage
		t.Fatalf("start turnover: %d %s", st, body)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/tasks/"+turn.ID.String()+"/complete", map[string]any{"completion_notes": "Kamar dibersihkan"})
	if st != 200 {
		// requires_photo → evidence; admin sebagai manage boleh bypass? EvidenceSatisfied tidak bypass. Unggah bukti tidak tersedia di test (storage memory ada) → gunakan requires_photo=false? Turnover task memakai requires_photo=true.
		// Pastikan pesan mengacu evidence, lalu lampirkan foto dummy via attachments presign+confirm memory storage.
		if !strings.Contains(string(body), "vidence") && !strings.Contains(string(body), "foto") {
			t.Fatalf("complete turnover: %d %s", st, body)
		}
		var pre struct {
			AttachmentID uuid.UUID `json:"attachment_id"`
			StorageKey   string    `json:"storage_key"`
		}
		st, body = e.do(admin, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "task", "object_id": turn.ID, "attachment_type": "photo", "content_type": "image/jpeg", "size_bytes": 8})
		e.mustJSON(st, body, 201, &pre)
		_ = e.store.Put(context.Background(), pre.StorageKey, "image/jpeg", bytes.NewReader([]byte("jpegdata")), 8)
		st, body = e.do(admin, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{})
		e.mustJSON(st, body, 200, nil)
		st, body = e.do(admin, http.MethodPost, "/api/v1/tasks/"+turn.ID.String()+"/complete", map[string]any{"completion_notes": "Kamar dibersihkan"})
		e.mustJSON(st, body, 200, &turn)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/rooms?property_id="+hotelProp.ID.String()+"&status=clean", nil)
	e.mustJSON(st, body, 200, &rooms)
	if len(rooms.Data) != 1 || rooms.Data[0].RoomNumber != "501" {
		t.Fatalf("kamar harus Clean setelah cleaning selesai: %+v", rooms.Data)
	}
	// Inspeksi housekeeping lulus → Inspected → Available
	var insp workItem
	st, body = e.do(admin, http.MethodPost, "/api/v1/housekeeping-inspections", map[string]any{"cleaning_task_id": turn.ID})
	e.mustJSON(st, body, 201, &insp)
	st, body = e.do(admin, http.MethodPost, "/api/v1/tasks/"+insp.ID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &insp)
	st, body = e.do(admin, http.MethodPost, "/api/v1/tasks/"+insp.ID.String()+"/complete", map[string]any{"completion_notes": "OK"})
	e.mustJSON(st, body, 200, &insp)
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/rooms?property_id="+hotelProp.ID.String()+"&status=available", nil)
	e.mustJSON(st, body, 200, &rooms)
	found := false
	for _, r := range rooms.Data {
		if r.RoomNumber == "501" {
			found = true
		}
	}
	if !found {
		t.Fatalf("kamar 501 harus Available setelah inspeksi lulus: %+v", rooms.Data)
	}
	// status manual: OOO lalu kembali available
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/rooms/"+r502.LocationID.String()+"/status", map[string]any{"room_status": "out_of_order", "note": "AC rusak"})
	e.mustJSON(st, body, 200, &r502)
	if r502.RoomStatus != "out_of_order" {
		t.Fatalf("OOO: %+v", r502)
	}
	// AC-20: profile hotel dengan reservasi aktif tidak bisa diubah ke apartment
	if st, body := e.do(admin, http.MethodPost, "/api/v1/properties/"+hotelProp.ID.String()+"/profile", map[string]any{"profile": "apartment", "reason": "test"}); st != 409 || !strings.Contains(string(body), "PROFILE_CHANGE_BLOCKED") {
		t.Fatalf("profile change harus diblokir: %d %s", st, body)
	}
	// cancel + no_show path
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations/"+res2.ID.String()+"/cancel", map[string]any{"reason": "Tamu membatalkan"})
	e.mustJSON(st, body, 200, &act)
	if act.Reservation.Status != "cancelled" {
		t.Fatalf("cancel: %s", act.Reservation.Status)
	}
	e.dispatch(t)
	tr := e.login("tr.manager@demo.buildingvision.id")
	_ = tr // TR demo terikat property demo, bukan hotel — notifikasi hotel ke supervisor hotel (tidak ada) → tidak diuji di sini
}
