// Package waassist: WhatsApp manual click-to-chat (PRD P3 v2.1 §6.4 P3-WAM-01..05; Roadmap v2.1 §39.4). BUKAN integrasi
// WhatsApp API (D-P3-05): BuildingVision hanya menyusun teks + tautan wa.me ke nomor penerima yang sudah dinormalisasi,
// staf menekan kirim di aplikasi WhatsApp Business miliknya sendiri. Setiap klik tercatat (manual_whatsapp_logs + activity
// object) sebagai "Dikirim manual via WhatsApp" — sistem tidak tahu apakah pesan benar-benar terkirim. Menggantikan email
// selama SMTP di-hold (D-P3-08). Generik agar dipakai ulang untuk undangan staf (P0) dan penagihan (P4).
package waassist

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/phone"
	"github.com/buildingvision/api/internal/profile"
)

type Service struct {
	DB           *db.DB
	Profile      *profile.Service
	TenantAppURL string // URL Tenant PWA (BV_TENANT_APP_URL) untuk tautan di pesan
	PublicURL    string // dashboard (undangan staf)
}

// NormalizePhone: lihat platform/phone.Normalize (PRD P3 v2.1 P3-WAM-04).
func NormalizePhone(raw string) (string, bool) { return phone.Normalize(raw) }

// Contexts: jenis pesan yang didukung (P3-WAM-01) — object_type yang sesuai ada di contextObject.
var contextObject = map[string]string{
	"account_approved":    "tenant_user",
	"account_rejected":    "tenant_user",
	"account_suspended":   "tenant_user",
	"account_created":     "tenant_user",
	"password_reset":      "tenant_user",
	"service_request":     "service_request",
	"invoice":             "invoice",
	"package":             "package",
	"parking_permit":      "parking_permit",
	"collection_reminder": "tenant",
	"staff_invite":        "user",
}

// objectPerm: permission lihat object (dicek pada property object).
var objectPerm = map[string]string{
	"tenant_user": "tenant_relation.tenant_users.view", "service_request": "tenant.service_requests.view", "invoice": "billing.invoices.view",
	"package": "security.packages.view", "parking_permit": "security.parking_permits.view", "tenant": "billing.collections.view", "user": "iam.users.update",
}

type Input struct {
	ObjectType        string    `json:"object_type"`
	ObjectID          uuid.UUID `json:"object_id"`
	Context           string    `json:"context"`
	Recipient         string    `json:"recipient"`          // "" = default; "user:<uuid>" | "tenant_contact" | "requester"
	TemporaryPassword string    `json:"temporary_password"` // hanya account_created / password_reset (tidak dicatat)
	Link              string    `json:"link"`               // staff_invite: tautan undangan
	Note              string    `json:"note"`               // baris tambahan dari staf
}

type Candidate struct {
	Key        string  `json:"key"`
	Name       string  `json:"name"`
	Phone      *string `json:"phone"`
	PhoneValid bool    `json:"phone_valid"`
	Source     string  `json:"source"` // tenant_user | tenant_contact | requester | user
}

type Composed struct {
	Context        string      `json:"context"`
	RecipientKey   string      `json:"recipient_key"`
	RecipientName  string      `json:"recipient_name"`
	Phone          string      `json:"phone"`
	PhoneValid     bool        `json:"phone_valid"`
	DisabledReason *string     `json:"disabled_reason"`
	Text           string      `json:"text"`
	URL            string      `json:"url"`
	Candidates     []Candidate `json:"candidates"`
	LogID          *uuid.UUID  `json:"log_id,omitempty"`
}

type objectInfo struct {
	propertyID  *uuid.UUID
	userID      *uuid.UUID // penerima utama (akun)
	candidates  []Candidate
	vars        map[string]string
	activityObj string
}

func (s *Service) appURL(path string) string {
	base := strings.TrimRight(s.TenantAppURL, "/")
	if base == "" {
		base = strings.TrimRight(s.PublicURL, "/")
	}
	return base + path
}

func rupiah(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	str := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-Rp" + b.String()
	}
	return "Rp" + b.String()
}

func strp(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func addCandidate(list []Candidate, key, name string, phone *string, source string) []Candidate {
	for _, c := range list {
		if c.Key == key {
			return list
		}
	}
	c := Candidate{Key: key, Name: name, Phone: phone, Source: source}
	if phone != nil {
		n, ok := NormalizePhone(*phone)
		c.PhoneValid = ok
		if n != "" {
			c.Phone = &n
		}
	}
	return append(list, c)
}

// tenantCandidates: akun tenant aktif pada tenant/unit (Tenant Admin lebih dulu) + kontak tenant.
func tenantCandidates(ctx context.Context, tx pgx.Tx, list []Candidate, tenantID, unitID *uuid.UUID) []Candidate {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (tu.user_id) tu.user_id, u.full_name, u.phone, tu.role FROM tenant_users tu JOIN users u ON u.id = tu.user_id
		WHERE tu.status = 'active' AND (($1::uuid IS NOT NULL AND tu.tenant_id = $1) OR ($2::uuid IS NOT NULL AND EXISTS (SELECT 1 FROM tenant_access ta WHERE ta.tenant_user_id = tu.id AND ta.location_id = $2 AND ta.status = 'active')))
		ORDER BY tu.user_id, tu.role DESC`, tenantID, unitID)
	if err == nil {
		type c struct {
			id         uuid.UUID
			name, role string
			phone      *string
		}
		var cs []c
		for rows.Next() {
			var x c
			if rows.Scan(&x.id, &x.name, &x.phone, &x.role) == nil {
				cs = append(cs, x)
			}
		}
		rows.Close()
		for _, x := range cs {
			if x.role == "tenant_admin" {
				list = addCandidate(list, "user:"+x.id.String(), x.name, x.phone, "tenant_user")
			}
		}
		for _, x := range cs {
			list = addCandidate(list, "user:"+x.id.String(), x.name, x.phone, "tenant_user")
		}
	}
	if tenantID != nil {
		var name string
		var contact, phone *string
		if tx.QueryRow(ctx, `SELECT name, contact_name, contact_phone FROM tenants WHERE id = $1`, *tenantID).Scan(&name, &contact, &phone) == nil && phone != nil {
			n := name
			if contact != nil && *contact != "" {
				n = *contact + " (" + name + ")"
			}
			list = addCandidate(list, "tenant_contact", n, phone, "tenant_contact")
		}
	}
	return list
}

func (s *Service) loadObject(ctx context.Context, tx pgx.Tx, in Input) (*objectInfo, error) {
	oi := &objectInfo{vars: map[string]string{}, activityObj: in.ObjectType}
	v := oi.vars
	switch in.ObjectType {
	case "tenant_user":
		var pid, uid uuid.UUID
		var name, prop string
		var email, phone, rej, susp *string
		if err := tx.QueryRow(ctx, `SELECT tu.property_id, tu.user_id, u.full_name, u.email, u.phone, l.name, tu.rejection_reason, tu.suspension_reason
			FROM tenant_users tu JOIN users u ON u.id = tu.user_id JOIN locations l ON l.id = tu.property_id WHERE tu.id = $1`, in.ObjectID).Scan(&pid, &uid, &name, &email, &phone, &prop, &rej, &susp); err != nil {
			return nil, apperr.NotFound("Akun tenant")
		}
		oi.propertyID, oi.userID = &pid, &uid
		oi.candidates = addCandidate(nil, "user:"+uid.String(), name, phone, "tenant_user")
		v["name"], v["email"], v["property"] = name, strp(email), prop
		v["reason"] = strp(rej)
		if in.Context == "account_suspended" {
			v["reason"] = strp(susp)
		}
	case "service_request":
		var pid uuid.UUID
		var num, title, status string
		var tuID *uuid.UUID
		var reqName, reqPhone *string
		if err := tx.QueryRow(ctx, `SELECT property_id, request_number, title, status, tenant_user_id, requester_name, requester_phone FROM service_requests WHERE id = $1`, in.ObjectID).
			Scan(&pid, &num, &title, &status, &tuID, &reqName, &reqPhone); err != nil {
			return nil, apperr.NotFound("Service Request")
		}
		oi.propertyID = &pid
		if tuID != nil {
			var n string
			var ph *string
			if tx.QueryRow(ctx, `SELECT full_name, phone FROM users WHERE id = $1`, *tuID).Scan(&n, &ph) == nil {
				oi.userID = tuID
				oi.candidates = addCandidate(oi.candidates, "user:"+tuID.String(), n, ph, "tenant_user")
			}
		}
		if reqPhone != nil && *reqPhone != "" {
			oi.candidates = addCandidate(oi.candidates, "requester", strp(reqName), reqPhone, "requester")
		}
		v["number"], v["title"], v["status"] = num, title, map[string]string{"new": "diterima", "acknowledged": "sedang ditinjau", "assigned": "sedang ditugaskan", "in_progress": "sedang dikerjakan",
			"waiting_for_tenant": "menunggu respons Anda", "resolved": "selesai dikerjakan", "closed": "ditutup", "cancelled": "dibatalkan"}[status]
		v["url"] = s.appURL("/requests/" + in.ObjectID.String())
	case "invoice":
		var pid uuid.UUID
		var num *string
		var itype, status string
		var total, paid, credited int64
		var due time.Time
		var tenantID, unitID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id, invoice_number, invoice_type, status, total_amount, paid_amount, credited_amount, due_at, tenant_id, unit_location_id FROM invoices WHERE id = $1`, in.ObjectID).
			Scan(&pid, &num, &itype, &status, &total, &paid, &credited, &due, &tenantID, &unitID); err != nil {
			return nil, apperr.NotFound("Invoice")
		}
		if num == nil {
			return nil, apperr.Conflict("INVOICE_NOT_ISSUED", "Invoice draft belum dapat dikirim ke tenant")
		}
		oi.propertyID = &pid
		oi.candidates = tenantCandidates(ctx, tx, nil, tenantID, unitID)
		loc, _ := time.LoadLocation("Asia/Jakarta")
		v["number"], v["total"], v["outstanding"], v["due"] = *num, rupiah(total), rupiah(total-paid-credited), due.In(loc).Format("02 Jan 2006")
		v["status"] = status
		v["url"] = s.appURL("/bills/" + in.ObjectID.String())
	case "package":
		var pid uuid.UUID
		var num, rname string
		var courier, tracking, storage *string
		var rcpt, tenantID, unitID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id, package_number, recipient_name, courier, tracking_number, storage_location, recipient_user_id, tenant_id, unit_location_id FROM packages WHERE id = $1`, in.ObjectID).
			Scan(&pid, &num, &rname, &courier, &tracking, &storage, &rcpt, &tenantID, &unitID); err != nil {
			return nil, apperr.NotFound("Paket")
		}
		oi.propertyID = &pid
		if rcpt != nil {
			var n string
			var ph *string
			if tx.QueryRow(ctx, `SELECT full_name, phone FROM users WHERE id = $1`, *rcpt).Scan(&n, &ph) == nil {
				oi.candidates = addCandidate(oi.candidates, "user:"+rcpt.String(), n, ph, "tenant_user")
			}
		}
		oi.candidates = tenantCandidates(ctx, tx, oi.candidates, tenantID, unitID)
		v["number"], v["recipient"], v["courier"], v["tracking"], v["storage"] = num, rname, strp(courier), strp(tracking), strp(storage)
		v["url"] = s.appURL("/packages/" + in.ObjectID.String())
	case "parking_permit":
		var pid uuid.UUID
		var num, status, plate string
		var reqBy *uuid.UUID
		var until *time.Time
		if err := tx.QueryRow(ctx, `SELECT pp.property_id, pp.permit_number, pp.status, v.plate_number, pp.requested_by, pp.valid_until FROM parking_permits pp JOIN vehicles v ON v.id = pp.vehicle_id WHERE pp.id = $1`, in.ObjectID).
			Scan(&pid, &num, &status, &plate, &reqBy, &until); err != nil {
			return nil, apperr.NotFound("Izin parkir")
		}
		oi.propertyID = &pid
		if reqBy != nil {
			var n string
			var ph *string
			if tx.QueryRow(ctx, `SELECT full_name, phone FROM users WHERE id = $1`, *reqBy).Scan(&n, &ph) == nil {
				oi.candidates = addCandidate(oi.candidates, "user:"+reqBy.String(), n, ph, "tenant_user")
			}
		}
		v["number"], v["plate"] = num, plate
		v["status"] = map[string]string{"requested": "sedang diproses", "approved": "disetujui", "rejected": "ditolak", "revoked": "dicabut", "expired": "berakhir", "cancelled": "dibatalkan"}[status]
		if until != nil {
			v["until"] = until.Format("02 Jan 2006")
		}
		v["url"] = s.appURL("/parking/permits/" + in.ObjectID.String())
	case "tenant":
		var pid uuid.UUID
		var name string
		if err := tx.QueryRow(ctx, `SELECT property_id, name FROM tenants WHERE id = $1 AND deleted_at IS NULL`, in.ObjectID).Scan(&pid, &name); err != nil {
			return nil, apperr.NotFound("Tenant")
		}
		oi.propertyID = &pid
		tid := in.ObjectID
		oi.candidates = tenantCandidates(ctx, tx, nil, &tid, nil)
		var out int64
		var cnt int
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(total_amount - paid_amount - credited_amount),0), count(*) FROM invoices WHERE tenant_id = $1 AND status = 'overdue'`, in.ObjectID).Scan(&out, &cnt)
		v["tenant"], v["outstanding"], v["count"] = name, rupiah(out), fmt.Sprint(cnt)
		v["url"] = s.appURL("/bills")
	case "user":
		var name string
		var phone *string
		if err := tx.QueryRow(ctx, `SELECT full_name, phone FROM users WHERE id = $1 AND deleted_at IS NULL`, in.ObjectID).Scan(&name, &phone); err != nil {
			return nil, apperr.NotFound("User")
		}
		uid := in.ObjectID
		oi.userID = &uid
		oi.candidates = addCandidate(nil, "user:"+uid.String(), name, phone, "user")
		v["name"] = name
	default:
		return nil, apperr.Validation("object_type tidak didukung")
	}
	if oi.propertyID != nil {
		var prop string
		_ = tx.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, *oi.propertyID).Scan(&prop)
		if v["property"] == "" {
			v["property"] = prop
		}
	}
	var org string
	_ = tx.QueryRow(ctx, `SELECT name FROM organizations WHERE id = $1`, authctx.Must(ctx).OrganizationID).Scan(&org)
	v["org"] = org
	if oi.candidates == nil {
		oi.candidates = []Candidate{}
	}
	return oi, nil
}

// text: templat pesan per konteks (P3-WAM-02) — istilah permintaan mengikuti terminologi profile property.
func (s *Service) text(ctx context.Context, tx pgx.Tx, in Input, oi *objectInfo, recipientName string) string {
	v := oi.vars
	reqTerm := "permintaan"
	if oi.propertyID != nil && s.Profile != nil {
		if pc, err := s.Profile.ResolveTx(ctx, tx, *oi.propertyID); err == nil {
			if t, ok := pc.Terminology[profile.TermRequest]; ok && t.ID != "" {
				reqTerm = t.ID
			}
		}
	}
	hello := "Halo"
	if recipientName != "" {
		hello = "Halo " + recipientName
	}
	login := s.appURL("/login")
	var t string
	switch in.Context {
	case "account_approved":
		t = fmt.Sprintf("%s, akun Tenant App Anda di %s telah disetujui. Silakan masuk di %s dengan email %s.", hello, v["property"], login, v["email"])
	case "account_rejected":
		t = fmt.Sprintf("%s, mohon maaf pendaftaran akun Tenant App Anda di %s belum dapat disetujui.", hello, v["property"])
		if v["reason"] != "" {
			t += "\nAlasan: " + v["reason"]
		}
		t += "\nSilakan balas pesan ini bila ada pertanyaan."
	case "account_suspended":
		t = fmt.Sprintf("%s, akun Tenant App Anda di %s saat ini ditangguhkan.", hello, v["property"])
		if v["reason"] != "" {
			t += "\nAlasan: " + v["reason"]
		}
	case "account_created":
		t = fmt.Sprintf("%s, akun Tenant App Anda di %s sudah dibuat.\nEmail: %s\nPassword sementara: %s\nMasuk di %s lalu ganti password Anda.", hello, v["property"], v["email"], in.TemporaryPassword, login)
	case "password_reset":
		t = fmt.Sprintf("%s, password Tenant App Anda di %s telah direset.\nPassword sementara: %s\nMasuk di %s lalu ganti password Anda.", hello, v["property"], in.TemporaryPassword, login)
	case "service_request":
		t = fmt.Sprintf("%s, kabar terbaru %s %s — %s: %s.\nDetail: %s", hello, strings.ToLower(reqTerm), v["number"], v["title"], v["status"], v["url"])
	case "invoice":
		t = fmt.Sprintf("%s, tagihan %s di %s sebesar %s jatuh tempo %s.", hello, v["number"], v["property"], v["total"], v["due"])
		if v["outstanding"] != v["total"] {
			t += " Sisa tagihan: " + v["outstanding"] + "."
		}
		t += "\nLihat tagihan: " + v["url"]
	case "package":
		t = fmt.Sprintf("%s, paket untuk %s (%s) sudah tiba", hello, v["recipient"], v["number"])
		if v["courier"] != "" {
			t += " dari " + v["courier"]
		}
		if v["tracking"] != "" {
			t += ", resi " + v["tracking"]
		}
		t += "."
		if v["storage"] != "" {
			t += "\nSilakan ambil di: " + v["storage"]
		}
	case "parking_permit":
		t = fmt.Sprintf("%s, izin parkir %s untuk kendaraan %s %s.", hello, v["number"], v["plate"], v["status"])
		if v["until"] != "" {
			t += " Berlaku s/d " + v["until"] + "."
		}
		t += "\nDetail: " + v["url"]
	case "collection_reminder":
		t = fmt.Sprintf("%s, kami mengingatkan %s tagihan %s di %s yang telah melewati jatuh tempo dengan total %s. Mohon segera melakukan pembayaran.\nRincian: %s", hello, v["count"], v["tenant"], v["property"], v["outstanding"], v["url"])
	case "staff_invite":
		t = fmt.Sprintf("%s, Anda diundang bergabung di BuildingVision (%s). Aktifkan akun Anda melalui tautan berikut: %s", hello, v["org"], in.Link)
	}
	if note := strings.TrimSpace(in.Note); note != "" {
		t += "\n\n" + note
	}
	return t + "\n\n— " + firstNonEmpty(v["property"], v["org"])
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// normalize: object_type default dari context.
func (in *Input) normalize() {
	if in.ObjectType == "" {
		in.ObjectType = contextObject[in.Context]
	}
}

func (s *Service) compose(ctx context.Context, tx pgx.Tx, in Input) (*Composed, *objectInfo, error) {
	p := authctx.Must(ctx)
	want, ok := contextObject[in.Context]
	if !ok {
		return nil, nil, apperr.Validation("context tidak dikenal").WithField("context", "tidak valid")
	}
	if in.ObjectType == "" {
		in.ObjectType = want
	}
	if in.ObjectType != want {
		return nil, nil, apperr.Validation("object_type " + in.ObjectType + " tidak sesuai context " + in.Context)
	}
	if (in.Context == "account_created" || in.Context == "password_reset") && strings.TrimSpace(in.TemporaryPassword) == "" {
		return nil, nil, apperr.Validation("temporary_password wajib untuk context ini").WithField("temporary_password", "wajib")
	}
	if in.Context == "staff_invite" && !strings.HasPrefix(in.Link, "http") {
		return nil, nil, apperr.Validation("link undangan wajib").WithField("link", "wajib")
	}
	oi, err := s.loadObject(ctx, tx, in)
	if err != nil {
		return nil, nil, err
	}
	perm := objectPerm[in.ObjectType]
	if oi.propertyID != nil {
		if !p.HasAnyOnProperty(perm, *oi.propertyID) {
			return nil, nil, apperr.Forbidden("Memerlukan " + perm)
		}
	} else if !p.Has(perm) {
		return nil, nil, apperr.Forbidden("Memerlukan " + perm)
	}
	if in.Context == "password_reset" || in.Context == "account_created" {
		if oi.propertyID == nil || !(p.HasOnProperty("tenant_relation.tenant_users.reset_password", *oi.propertyID) || p.HasOnProperty("tenant_relation.tenant_users.update", *oi.propertyID)) {
			return nil, nil, apperr.Forbidden("Memerlukan tenant_relation.tenant_users.reset_password")
		}
	}
	out := &Composed{Context: in.Context, Candidates: oi.candidates}
	var chosen *Candidate
	for i := range oi.candidates {
		c := &oi.candidates[i]
		if in.Recipient != "" && c.Key == in.Recipient {
			chosen = c
			break
		}
		if in.Recipient == "" && chosen == nil && c.PhoneValid {
			chosen = c
		}
	}
	if chosen == nil && in.Recipient == "" && len(oi.candidates) > 0 {
		chosen = &oi.candidates[0]
	}
	if in.Recipient != "" && chosen == nil {
		return nil, nil, apperr.Validation("recipient tidak termasuk kandidat penerima").WithField("recipient", "tidak valid")
	}
	if chosen != nil {
		out.RecipientKey, out.RecipientName = chosen.Key, chosen.Name
		out.Phone, out.PhoneValid = strp(chosen.Phone), chosen.PhoneValid
	}
	switch {
	case chosen == nil || chosen.Phone == nil || strp(chosen.Phone) == "":
		r := "Nomor WhatsApp penerima belum diisi"
		out.DisabledReason = &r
	case !chosen.PhoneValid:
		r := "Nomor WhatsApp penerima tidak valid (" + strp(chosen.Phone) + ")"
		out.DisabledReason = &r
	}
	out.Text = s.text(ctx, tx, in, oi, out.RecipientName)
	if out.PhoneValid {
		out.URL = "https://wa.me/" + out.Phone + "?text=" + url.QueryEscape(out.Text)
	}
	return out, oi, nil
}

// Compose: pratinjau (tanpa log) — dipakai tombol untuk status aktif/nonaktif & teks.
func (s *Service) Compose(ctx context.Context, in Input) (*Composed, error) {
	var out *Composed
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, _, err = s.compose(ctx, tx, in)
		return err
	})
	return out, err
}

// Send: susun + catat klik (P3-WAM-03). Klien lalu membuka URL wa.me. Password sementara tidak pernah disimpan di log.
func (s *Service) Send(ctx context.Context, in Input) (*Composed, error) {
	p := authctx.Must(ctx)
	in.normalize()
	var out *Composed
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		c, oi, err := s.compose(ctx, tx, in)
		if err != nil {
			return err
		}
		if !c.PhoneValid {
			return apperr.Validation(strp(c.DisabledReason)).WithField("phone", "tidak valid")
		}
		preview := c.Text
		if in.TemporaryPassword != "" {
			preview = strings.ReplaceAll(preview, in.TemporaryPassword, "••••••")
		}
		if len([]rune(preview)) > 500 {
			preview = string([]rune(preview)[:497]) + "…"
		}
		var rcptUser *uuid.UUID
		if strings.HasPrefix(c.RecipientKey, "user:") {
			if id, err := uuid.Parse(strings.TrimPrefix(c.RecipientKey, "user:")); err == nil {
				rcptUser = &id
			}
		}
		var logID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO manual_whatsapp_logs (organization_id, property_id, object_type, object_id, context, recipient_user_id, recipient_name, phone, message_preview, sent_by)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10) RETURNING id`, p.OrganizationID, oi.propertyID, in.ObjectType, in.ObjectID, in.Context, rcptUser, c.RecipientName, c.Phone, preview, p.UserID).Scan(&logID); err != nil {
			return err
		}
		masked := c.Phone
		if len(masked) > 6 {
			masked = masked[:4] + strings.Repeat("•", len(masked)-7) + masked[len(masked)-3:]
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: oi.activityObj, ObjectID: in.ObjectID, Action: "whatsapp_manual_sent", Payload: map[string]any{"context": in.Context, "recipient": c.RecipientName, "phone": masked, "log_id": logID}})
		c.LogID = &logID
		out = c
		return nil
	})
	return out, err
}

type LogEntry struct {
	ID             uuid.UUID `json:"id"`
	Context        string    `json:"context"`
	RecipientName  *string   `json:"recipient_name"`
	Phone          string    `json:"phone"`
	MessagePreview *string   `json:"message_preview"`
	SentByName     string    `json:"sent_by_name"`
	SentAt         time.Time `json:"sent_at"`
}

// Logs: riwayat WhatsApp manual untuk satu object (panel timeline).
func (s *Service) Logs(ctx context.Context, objectType string, objectID uuid.UUID) ([]LogEntry, error) {
	out := []LogEntry{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		p := authctx.Must(ctx)
		perm, ok := objectPerm[objectType]
		if !ok {
			return apperr.Validation("object_type tidak didukung")
		}
		var pid *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT property_id FROM manual_whatsapp_logs WHERE object_type = $1 AND object_id = $2 LIMIT 1`, objectType, objectID).Scan(&pid)
		if pid != nil && !p.HasAnyOnProperty(perm, *pid) {
			return apperr.Forbidden("")
		}
		if pid == nil && !p.Has(perm) {
			return apperr.Forbidden("")
		}
		rows, err := tx.Query(ctx, `SELECT w.id, w.context, w.recipient_name, w.phone, w.message_preview, u.full_name, w.sent_at FROM manual_whatsapp_logs w JOIN users u ON u.id = w.sent_by
			WHERE w.object_type = $1 AND w.object_id = $2 ORDER BY w.sent_at DESC LIMIT 100`, objectType, objectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e LogEntry
			if err := rows.Scan(&e.ID, &e.Context, &e.RecipientName, &e.Phone, &e.MessagePreview, &e.SentByName, &e.SentAt); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}
