package notification

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/events"
)

// tenantResolvers: resolver yang penerimanya akun Tenant App → judul/status tenant-facing & deep link route Tenant App
// (PRD P1 §20; PRD P3 v2.1 P3-INA-01). tenant_property_users dulu dirender versi staf (deep link dashboard) — diperbaiki.
var tenantResolvers = map[string]bool{"tenant_user": true, "tenant_property_users": true}

// collectUUIDs: helper query → daftar user id.
func collectUUIDs(ctx context.Context, tx pgx.Tx, q string, args ...any) []uuid.UUID {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// invoiceAudienceSQL: tenant user aktif yang melihat invoice di Tenant App (mengikuti billing.tenantInvoiceWhere):
// seluruh anggota tenant pemilik invoice; invoice unit tanpa tenant → pemegang akses unit (bug B-10: dulu LIMIT 1 dan
// invoice unit tanpa tenant tidak menotifikasi siapa pun). $1 = invoice id.
const invoiceAudienceSQL = `SELECT DISTINCT tu.user_id FROM invoices i JOIN tenant_users tu ON tu.status = 'active' AND tu.property_id = i.property_id
	WHERE i.id = $1 AND (
	  (i.tenant_id IS NOT NULL AND tu.tenant_id = i.tenant_id)
	  OR (i.tenant_id IS NULL AND i.unit_location_id IS NOT NULL AND i.source NOT IN ('rental','unit_sale','hotel')
	      AND EXISTS (SELECT 1 FROM tenant_access ta WHERE ta.tenant_user_id = tu.id AND ta.location_id = i.unit_location_id AND ta.status = 'active' AND ta.access_type = 'unit')))`

// unitTenantAudienceSQL: anggota tenant ($1 tenant_id) atau pemegang akses unit ($2 unit) — paket, pelanggaran parkir.
const unitTenantAudienceSQL = `SELECT DISTINCT tu.user_id FROM tenant_users tu WHERE tu.status = 'active' AND (
	  ($1::uuid IS NOT NULL AND tu.tenant_id = $1)
	  OR ($2::uuid IS NOT NULL AND EXISTS (SELECT 1 FROM tenant_access ta WHERE ta.tenant_user_id = tu.id AND ta.location_id = $2 AND ta.status = 'active')))`

// tenantRecipients: penerima tenant-facing untuk object (resolver tenant_user tanpa tenant_user_id di payload).
func (s *Service) tenantRecipients(ctx context.Context, tx pgx.Tx, ev events.Event) []uuid.UUID {
	one := func(q string) []uuid.UUID {
		var tu *uuid.UUID
		_ = tx.QueryRow(ctx, q, ev.ObjectID).Scan(&tu)
		if tu == nil {
			return nil
		}
		return []uuid.UUID{*tu}
	}
	switch ev.ObjectType {
	case "service_request":
		return one(`SELECT tenant_user_id FROM service_requests WHERE id = $1`)
	case "tenant_user":
		return one(`SELECT user_id FROM tenant_users WHERE id = $1`)
	case "booking":
		return one(`SELECT tenant_user_id FROM bookings WHERE id = $1`)
	case "visitor":
		return one(`SELECT tenant_user_id FROM visitors WHERE id = $1`)
	case "invoice":
		return collectUUIDs(ctx, tx, invoiceAudienceSQL, ev.ObjectID)
	case "payment", "credit_note":
		table := map[string]string{"payment": "payments", "credit_note": "credit_notes"}[ev.ObjectType]
		var invID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT invoice_id FROM `+table+` WHERE id = $1`, ev.ObjectID).Scan(&invID); err != nil {
			return nil
		}
		return collectUUIDs(ctx, tx, invoiceAudienceSQL, invID)
	case "package":
		var rcpt, tenantID, unitID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT recipient_user_id, tenant_id, unit_location_id FROM packages WHERE id = $1`, ev.ObjectID).Scan(&rcpt, &tenantID, &unitID); err != nil {
			return nil
		}
		if rcpt != nil {
			return []uuid.UUID{*rcpt}
		}
		return collectUUIDs(ctx, tx, unitTenantAudienceSQL, tenantID, unitID)
	case "parking_permit":
		return one(`SELECT COALESCE(pp.requested_by, v.user_id) FROM parking_permits pp JOIN vehicles v ON v.id = pp.vehicle_id WHERE pp.id = $1`)
	case "parking_violation":
		var userID, tenantID, unitID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT v.user_id, v.tenant_id, v.unit_location_id FROM parking_violations pv JOIN vehicles v ON v.id = pv.vehicle_id WHERE pv.id = $1 AND v.owner_type = 'tenant'`, ev.ObjectID).Scan(&userID, &tenantID, &unitID); err != nil {
			return nil
		}
		if userID != nil {
			return []uuid.UUID{*userID}
		}
		return collectUUIDs(ctx, tx, unitTenantAudienceSQL, tenantID, unitID)
	case "tenant_feedback":
		return one(`SELECT tenant_user_id FROM tenant_feedback WHERE id = $1`)
	}
	return nil
}

// announcementAudience: tenant user aktif yang menjadi sasaran pengumuman (P3-ANN-02): property (atau seluruh org bila
// property kosong), dipersempit target lokasi (subtree building/tower/floor/unit dari akses unit/area tenant) dan/atau
// tenant tertentu. Target kosong = seluruh tenant user property.
func announcementAudience(ctx context.Context, tx pgx.Tx, announcementID uuid.UUID) []uuid.UUID {
	return collectUUIDs(ctx, tx, `SELECT DISTINCT tu.user_id FROM announcements a JOIN tenant_users tu ON tu.status = 'active' AND (a.property_id IS NULL OR tu.property_id = a.property_id)
		WHERE a.id = $1 AND (
		  (cardinality(a.target_location_ids) = 0 AND cardinality(a.target_tenant_ids) = 0)
		  OR (tu.tenant_id IS NOT NULL AND tu.tenant_id = ANY(a.target_tenant_ids))
		  OR EXISTS (SELECT 1 FROM tenant_access ta JOIN locations l ON l.id = ta.location_id JOIN locations t ON t.id = ANY(a.target_location_ids)
		             WHERE ta.tenant_user_id = tu.id AND ta.status = 'active' AND t.path @> l.path))`, announcementID)
}

// tenantStatusLabel: status internal → label tenant-facing (PRD §14; TD-P1-008).
var tenantStatusLabel = map[string]string{
	"new": "Submitted", "acknowledged": "Received", "assigned": "Being Assigned", "in_progress": "In Progress",
	"waiting_for_tenant": "Need Your Response", "resolved": "Resolved", "closed": "Closed", "cancelled": "Cancelled",
}

func payloadStr(ev events.Event, k string) string {
	if ev.Payload == nil {
		return ""
	}
	if v, ok := ev.Payload[k]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func rupiah(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-Rp" + b.String()
	}
	return "Rp" + b.String()
}

// renderTenant: notifikasi untuk Mobile Tenant (PRD §20). Tidak memuat catatan internal, cost, atau assignment terbatas.
// Label lama berbahasa Inggris ("Ticket …", "Invoice …") dipertahankan sampai penyelarasan label setelah P5 (P0 D-04, ADJ-06);
// judul object baru P3/P4 langsung berbahasa Indonesia (NC §78.4).
func (s *Service) renderTenant(ctx context.Context, tx pgx.Tx, ev events.Event) (title, body, deepLink string) {
	verb := ev.Type[strings.LastIndex(ev.Type, ".")+1:]
	label := ev.ObjectLabel
	switch ev.ObjectType {
	case "service_request":
		var num, ttl string
		_ = tx.QueryRow(ctx, `SELECT request_number, title FROM service_requests WHERE id = $1`, ev.ObjectID).Scan(&num, &ttl)
		if num != "" {
			label = num
		}
		to := payloadStr(ev, "to") // string atau workflow.Status (in-process)
		switch verb {
		case "created":
			title = "Ticket Created"
		case "message":
			title = "New Message from Building Management"
		case "auto_closed":
			title = "Ticket Closed"
		default:
			if l := tenantStatusLabel[to]; l != "" {
				title = "Ticket " + l
			} else {
				title = "Ticket Updated"
			}
		}
		body = label
		if ttl != "" {
			body += " — " + ttl
		}
		if verb == "closed" && payloadStr(ev, "actor_kind") == "system" {
			body += "\nDitutup otomatis karena tidak ada konfirmasi. Buka kembali bila masalah belum selesai."
		}
		if m := payloadStr(ev, "message_preview"); m != "" {
			body += "\n" + m
		}
		deepLink = "/requests/" + ev.ObjectID.String()
	case "tenant_user":
		switch verb {
		case "approved":
			title, body = "Account Approved", "Akun Tenant App Anda telah divalidasi. Silakan masuk."
		case "rejected":
			title, body = "Account Rejected", "Pendaftaran akun tidak dapat disetujui."
			if r := payloadStr(ev, "reason"); r != "" {
				body += "\n" + r
			}
		case "suspended":
			title, body = "Account Suspended", "Akun Anda ditangguhkan. Hubungi building management."
		default:
			title, body = "Account Updated", ""
		}
		deepLink = "/account" // B-01: route akun PWA (bukan /profile)
	case "announcement":
		var ttl, excerpt, category, severity string
		_ = tx.QueryRow(ctx, `SELECT title, COALESCE(excerpt,''), category, severity FROM announcements WHERE id = $1`, ev.ObjectID).Scan(&ttl, &excerpt, &category, &severity)
		switch category {
		case "news":
			title = "Kabar Gedung"
		case "alert":
			title = "Pemberitahuan Penting"
		default:
			title = "Announcement"
		}
		body = ttl
		if excerpt != "" {
			body += "\n" + excerpt
		}
		deepLink = "/inbox/announcements/" + ev.ObjectID.String()
	case "booking":
		title = "Booking " + strings.Title(strings.ReplaceAll(verb, "_", " "))
		body = label
		deepLink = "/facilities/bookings/" + ev.ObjectID.String()
	case "visitor":
		title = "Visitor " + strings.Title(strings.ReplaceAll(verb, "_", " "))
		body = label
		deepLink = "/visitors/" + ev.ObjectID.String()
	case "invoice":
		var total, paid, credited int64
		var due *time.Time
		_ = tx.QueryRow(ctx, `SELECT total_amount, paid_amount, credited_amount, due_at FROM invoices WHERE id = $1`, ev.ObjectID).Scan(&total, &paid, &credited, &due)
		switch verb {
		case "reminder":
			title = "Pengingat Tagihan"
		case "credited":
			title = "Tagihan Dikoreksi"
		default:
			title = "Invoice " + strings.Title(strings.ReplaceAll(verb, "_", " "))
		}
		body = label
		if out := total - paid - credited; out > 0 && verb != "paid" && verb != "cancelled" {
			body += " — sisa " + rupiah(out)
			if due != nil {
				body += ", jatuh tempo " + due.In(jakarta()).Format("02 Jan 2006")
			}
		}
		if r := payloadStr(ev, "reason"); r != "" && verb == "cancelled" {
			body += "\n" + r
		}
		deepLink = "/bills/" + ev.ObjectID.String()
	case "payment":
		var invID uuid.UUID
		var amount int64
		_ = tx.QueryRow(ctx, `SELECT invoice_id, amount FROM payments WHERE id = $1`, ev.ObjectID).Scan(&invID, &amount)
		if verb == "refunded" {
			title = "Pembayaran Dikembalikan"
		} else {
			title = "Payment " + strings.Title(strings.ReplaceAll(verb, "_", " "))
		}
		body = label
		if amount > 0 {
			body += " — " + rupiah(amount)
		}
		if n := payloadStr(ev, "invoice_number"); n != "" {
			body += " · " + n
		}
		deepLink = "/bills"
		if invID != uuid.Nil {
			deepLink = "/bills/" + invID.String() + "?payment=" + ev.ObjectID.String()
		}
	case "credit_note":
		var invID uuid.UUID
		var amount int64
		_ = tx.QueryRow(ctx, `SELECT invoice_id, amount FROM credit_notes WHERE id = $1`, ev.ObjectID).Scan(&invID, &amount)
		title, body = "Koreksi Tagihan", label+" — "+rupiah(amount)
		deepLink = "/bills/" + invID.String()
	case "package":
		var courier, desc, storage *string
		_ = tx.QueryRow(ctx, `SELECT courier, description, storage_location FROM packages WHERE id = $1`, ev.ObjectID).Scan(&courier, &desc, &storage)
		switch verb {
		case "received":
			title = "Paket Tiba"
		case "reminder":
			title = "Pengingat: Paket Belum Diambil"
		case "picked_up":
			title = "Paket Sudah Diambil"
		case "returned":
			title = "Paket Dikembalikan"
		default:
			title = "Paket Diperbarui"
		}
		body = label
		if courier != nil && *courier != "" {
			body += " · " + *courier
		}
		if desc != nil && *desc != "" {
			body += " — " + *desc
		}
		if storage != nil && *storage != "" && (verb == "received" || verb == "reminder") {
			body += "\nAmbil di: " + *storage
		}
		deepLink = "/packages/" + ev.ObjectID.String()
	case "parking_permit":
		var plate string
		_ = tx.QueryRow(ctx, `SELECT v.plate_number FROM parking_permits pp JOIN vehicles v ON v.id = pp.vehicle_id WHERE pp.id = $1`, ev.ObjectID).Scan(&plate)
		title = map[string]string{"approved": "Izin Parkir Disetujui", "rejected": "Izin Parkir Ditolak", "revoked": "Izin Parkir Dicabut", "expiring": "Izin Parkir Segera Berakhir", "expired": "Izin Parkir Berakhir"}[verb]
		if title == "" {
			title = "Izin Parkir Diperbarui"
		}
		body = label + " · " + plate
		if r := payloadStr(ev, "reason"); r != "" {
			body += "\n" + r
		}
		deepLink = "/parking/permits/" + ev.ObjectID.String()
	case "parking_violation":
		var plate, vtype string
		_ = tx.QueryRow(ctx, `SELECT plate_number, violation_type FROM parking_violations WHERE id = $1`, ev.ObjectID).Scan(&plate, &vtype)
		title = "Pelanggaran Parkir"
		body = label + " · " + plate + " — " + map[string]string{"illegal_parking": "Parkir tidak pada tempatnya", "no_permit": "Tanpa izin parkir", "blocking": "Menghalangi jalan", "overstay": "Melebihi batas waktu", "reserved_spot": "Menempati slot khusus"}[vtype]
		deepLink = "/parking"
	case "tenant_feedback":
		title, body = "Tanggapan atas Masukan Anda", label
		if r := payloadStr(ev, "response_preview"); r != "" {
			body += "\n" + r
		}
		deepLink = "/feedback/" + ev.ObjectID.String()
	case "unit_rental_reservation":
		switch verb {
		case "activated":
			title, body = "Welcome Home", "Sewa unit Anda aktif. Akun Tenant App Anda dapat digunakan untuk permintaan layanan, tagihan, dan informasi gedung."
		case "completed":
			title, body = "Rental Completed", "Masa sewa unit Anda telah berakhir. Terima kasih."
		default:
			title, body = "Rental "+strings.Title(strings.ReplaceAll(verb, "_", " ")), label
		}
		deepLink = "/" // B-01: Home PWA (bukan /home)
	default:
		title = strings.Title(strings.ReplaceAll(ev.ObjectType, "_", " ")) + " " + strings.Title(strings.ReplaceAll(verb, "_", " "))
		body = label
	}
	return
}

var jakartaLoc *time.Location

func jakarta() *time.Location {
	if jakartaLoc == nil {
		l, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			return time.FixedZone("WIB", 7*3600)
		}
		jakartaLoc = l
	}
	return jakartaLoc
}
