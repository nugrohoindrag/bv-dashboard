package notification

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/mailer"
)

// ---------- Channel abstraction (PRD P0 v2 §14.3) ----------
//
// In-app selalu ditulis ke inbox (sumber kebenaran). Channel lain dikirim asinkron lewat job
// notification.deliver per (notification, channel). Push memakai job notification.push (FCM) yang sudah ada.
// Menambah channel baru (mis. WhatsApp) cukup: implementasi ChannelAdapter + daftarkan di Service.Channels +
// kolom preferensi bila perlu — tanpa mengubah rule engine.

// Delivery: isi notifikasi yang dikirim ke channel eksternal.
type Delivery struct {
	NotificationID uuid.UUID
	UserID         uuid.UUID
	FullName       string
	Email          *string
	Phone          *string
	Title          string
	Body           string
	DeepLink       string // URL absolut ke dashboard
	Severity       string
}

// ChannelAdapter: adapter pengiriman satu channel.
type ChannelAdapter interface {
	Send(ctx context.Context, d Delivery) error
}

// EmailChannel: email transaksional lewat platform/mailer (SMTP; log-only bila SMTP belum dikonfigurasi).
type EmailChannel struct{ Mailer mailer.Mailer }

func (c EmailChannel) Send(ctx context.Context, d Delivery) error {
	if d.Email == nil || *d.Email == "" || c.Mailer == nil {
		return nil
	}
	text := d.Body
	if d.DeepLink != "" {
		text += "\n\nBuka: " + d.DeepLink
	}
	return c.Mailer.Send(ctx, mailer.Message{To: []string{*d.Email}, Subject: "[BuildingVision] " + d.Title, Text: text + "\n\n— BuildingVision"})
}

// channelDefaults: channel eksternal default aktif bila rule mencantumkannya (preferensi user dapat mematikan).
func channelEnabled(ruleChannels []string, ch string, pref *bool) bool {
	if !contains(ruleChannels, ch) {
		return false
	}
	if pref != nil {
		return *pref
	}
	return true
}

// enqueueChannels: jadwalkan pengiriman channel eksternal (selain push) untuk satu notifikasi.
func (s *Service) enqueueChannels(ctx context.Context, tx pgx.Tx, orgID, nid uuid.UUID, ruleChannels []string, emailPref *bool) {
	if s.Jobs == nil {
		return
	}
	for ch := range s.Channels {
		if ch == "email" && !channelEnabled(ruleChannels, ch, emailPref) {
			continue
		}
		if ch != "email" && !contains(ruleChannels, ch) {
			continue
		}
		_ = s.Jobs.EnqueueTx(ctx, tx, jobs.NotificationDeliverArgs{NotificationID: nid, OrganizationID: orgID, Channel: ch})
	}
}

// Deliver (worker): kirim satu notifikasi ke channel adapter terdaftar.
func (s *Service) Deliver(ctx context.Context, orgID, notificationID uuid.UUID, channel string) error {
	adapter, ok := s.Channels[channel]
	if !ok {
		return nil
	}
	var d Delivery
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var deepLink *string
		if err := tx.QueryRow(ctx, `SELECT n.id, n.user_id, u.full_name, u.email, u.phone, n.title, n.body, n.deep_link, n.severity
			FROM notifications n JOIN users u ON u.id = n.user_id WHERE n.id = $1 AND u.is_active`, notificationID).
			Scan(&d.NotificationID, &d.UserID, &d.FullName, &d.Email, &d.Phone, &d.Title, &d.Body, &deepLink, &d.Severity); err != nil {
			return err
		}
		if deepLink != nil && *deepLink != "" {
			d.DeepLink = strings.TrimRight(s.PublicURL, "/") + *deepLink
		}
		return nil
	})
	if err != nil {
		return nil // notifikasi/penerima sudah tidak ada → tidak perlu retry
	}
	return adapter.Send(ctx, d)
}

// ---------- System notification / broadcast (PRD P0 v2 §14.2) ----------

type BroadcastInput struct {
	Title      string      `json:"title"`
	Body       string      `json:"body"`
	Severity   string      `json:"severity"` // info | warning | critical | success
	PropertyID *uuid.UUID  `json:"property_id"`
	RoleCodes  []string    `json:"role_codes"`
	UserIDs    []uuid.UUID `json:"user_ids"`
	DeepLink   *string     `json:"deep_link"`
	Email      bool        `json:"email"` // kirim juga lewat email
}

// Broadcast: notifikasi sistem ke user staf aktif organization (opsional difilter property/role/user).
func (s *Service) Broadcast(ctx context.Context, in BroadcastInput) (int, error) {
	p := authctx.Must(ctx)
	in.Title, in.Body = strings.TrimSpace(in.Title), strings.TrimSpace(in.Body)
	if in.Title == "" || in.Body == "" {
		return 0, apperr.Validation("title dan body wajib")
	}
	if in.Severity == "" {
		in.Severity = "info"
	}
	if in.Severity != "info" && in.Severity != "warning" && in.Severity != "critical" && in.Severity != "success" {
		return 0, apperr.Validation("severity harus info|warning|critical|success")
	}
	if in.DeepLink != nil && !strings.HasPrefix(*in.DeepLink, "/") {
		return 0, apperr.Validation("deep_link harus path relatif dashboard (diawali /)")
	}
	if in.PropertyID != nil && !p.HasOnProperty("platform.notifications.broadcast", *in.PropertyID) {
		return 0, apperr.Forbidden("")
	}
	if in.PropertyID == nil {
		if _, all := p.PropertyIDsFor("platform.notifications.broadcast"); !all {
			return 0, apperr.Validation("property_id wajib untuk broadcast ber-scope property")
		}
	}
	n := 0
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// staf aktif (bukan akun tenant/vendor), sesuai filter
		rows, err := tx.Query(ctx, `SELECT DISTINCT u.id FROM users u
			JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
			WHERE u.is_active AND u.deleted_at IS NULL AND u.vendor_id IS NULL AND COALESCE(r.domain,'') NOT IN ('tenant','vendor')
			  AND ($1::uuid IS NULL OR ur.property_id IS NULL OR ur.property_id = $1)
			  AND (COALESCE(cardinality($2::text[]), 0) = 0 OR r.code = ANY($2))
			  AND (COALESCE(cardinality($3::uuid[]), 0) = 0 OR u.id = ANY($3))`, in.PropertyID, in.RoleCodes, in.UserIDs)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		channels := []string{"inapp", "push"}
		if in.Email {
			channels = append(channels, "email")
		}
		for _, uid := range ids {
			var nid uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO notifications (organization_id, user_id, type, title, body, object_type, deep_link, severity)
				VALUES ($1,$2,'system_broadcast',$3,$4,'system',$5,$6) RETURNING id`, p.OrganizationID, uid, in.Title, in.Body, in.DeepLink, in.Severity).Scan(&nid); err != nil {
				return err
			}
			if s.Jobs != nil {
				_ = s.Jobs.EnqueueTx(ctx, tx, jobs.NotificationPushArgs{NotificationID: nid, OrganizationID: p.OrganizationID})
			}
			s.enqueueChannels(ctx, tx, p.OrganizationID, nid, channels, nil)
			n++
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditBroadcast, EntityType: "notification", EntityLabel: in.Title,
			After: map[string]any{"title": in.Title, "severity": in.Severity, "property_id": in.PropertyID, "role_codes": in.RoleCodes, "recipients": n, "email": in.Email}})
	})
	return n, err
}
