package tenantservice

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/events"
)

// EventRecurringIssueDetected: lokasi + kategori berulang ≥ ambang dalam jendela waktu (PRD P3 v2.1 P3-TSH-07).
const EventRecurringIssueDetected = "recurring_issue.detected"

// detectRecurringTx: dipanggil setelah Service Request dibuat. Bila jumlah permintaan (bukan cancelled) pada lokasi & kategori
// yang sama dalam `recurring_issue_window_days` mencapai `recurring_issue_threshold` (konfigurasi profile property; default
// 3 dalam 30 hari), sinyal recurring_issues dibuat/diperbarui (satu sinyal terbuka per lokasi × kategori), SR ditautkan, dan
// Tenant Relation diberi notifikasi saat sinyal pertama kali muncul. Keluhan (request_type complaint) yang berulang otomatis
// naik prioritas ke high (P3-CMP-03) dan SLA dihitung ulang.
func (s *Service) detectRecurringTx(ctx context.Context, tx pgx.Tx, srID, propertyID uuid.UUID, locationID *uuid.UUID, categoryCode, requestType, priority string, now time.Time) error {
	if locationID == nil {
		return nil
	}
	p := authctx.Must(ctx)
	threshold, window := 3, 30
	_ = tx.QueryRow(ctx, `SELECT recurring_issue_threshold, recurring_issue_window_days FROM property_profile_configs WHERE property_id = $1`, propertyID).Scan(&threshold, &window)
	var count, complaints int
	var first, last *time.Time
	var ids []uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE request_type = 'complaint'), min(created_at), max(created_at), COALESCE(array_agg(id ORDER BY created_at), '{}')
		FROM service_requests WHERE location_id = $1 AND category_code = $2 AND status <> 'cancelled' AND created_at >= $3::timestamptz - make_interval(days => $4)`,
		*locationID, categoryCode, now, window).Scan(&count, &complaints, &first, &last, &ids); err != nil {
		return err
	}
	if count < threshold || first == nil {
		return nil
	}
	var riID uuid.UUID
	var inserted bool
	if err := tx.QueryRow(ctx, `INSERT INTO recurring_issues (organization_id, property_id, location_id, category_code, category_id, request_count, complaint_count, window_days, threshold, first_seen_at, last_seen_at, service_request_ids)
		VALUES ($1,$2,$3,$4,(SELECT id FROM service_request_categories WHERE code = $4 AND (property_id IS NULL OR property_id = $2) ORDER BY property_id NULLS LAST LIMIT 1),$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (organization_id, location_id, category_code) WHERE status <> 'resolved'
		DO UPDATE SET request_count = EXCLUDED.request_count, complaint_count = EXCLUDED.complaint_count, last_seen_at = EXCLUDED.last_seen_at,
		  service_request_ids = (SELECT array_agg(DISTINCT x) FROM unnest(recurring_issues.service_request_ids || EXCLUDED.service_request_ids) x)
		RETURNING id, (xmax = 0)`, p.OrganizationID, propertyID, *locationID, categoryCode, count, complaints, window, threshold, *first, *last, ids).Scan(&riID, &inserted); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE service_requests SET recurring_issue_id = $1 WHERE id = ANY($2) AND recurring_issue_id IS DISTINCT FROM $1`, riID, ids); err != nil {
		return err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: srID, Action: "recurring_issue_linked", Payload: map[string]any{"recurring_issue_id": riID, "count": count, "window_days": window}})
	if inserted && s.Jobs != nil {
		label := categoryCode
		_ = tx.QueryRow(ctx, `SELECT COALESCE(c.name, $2) || ' · ' || l.name FROM locations l LEFT JOIN service_request_categories c ON c.code = $2 AND (c.property_id IS NULL OR c.property_id = $3) WHERE l.id = $1 ORDER BY c.property_id NULLS LAST LIMIT 1`, *locationID, categoryCode, propertyID).Scan(&label)
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventRecurringIssueDetected, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "recurring_issue", ObjectID: riID, ObjectLabel: label,
			Payload: map[string]any{"domain": "tenant_relation", "count": count, "window_days": window, "location_id": *locationID, "category_code": categoryCode}})
	}
	// P3-CMP-03: keluhan berulang → prioritas naik (tanpa menurunkan prioritas yang sudah tinggi)
	if requestType == "complaint" && (priority == "low" || priority == "medium") {
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET priority = 'high' WHERE id = $1`, srID); err != nil {
			return err
		}
		if err := s.Ops.ApplySLA(ctx, tx, operations.ObjServiceRequest, srID, propertyID, "high", now); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: srID, Action: "priority_changed", From: priority, To: "high", Payload: map[string]any{"reason": "Keluhan berulang pada lokasi & kategori yang sama", "recurring_issue_id": riID}})
	}
	return nil
}

// ---------- Recurring issues (Tenant Relation) ----------

type RecurringIssue struct {
	ID              uuid.UUID   `json:"id"`
	PropertyID      uuid.UUID   `json:"property_id"`
	LocationID      uuid.UUID   `json:"location_id"`
	LocationName    string      `json:"location_name"`
	LocationPath    string      `json:"location_path"`
	CategoryCode    string      `json:"category_code"`
	CategoryName    *string     `json:"category_name"`
	RequestCount    int         `json:"request_count"`
	ComplaintCount  int         `json:"complaint_count"`
	OpenCount       int         `json:"open_count"`
	WindowDays      int         `json:"window_days"`
	Threshold       int         `json:"threshold"`
	FirstSeenAt     time.Time   `json:"first_seen_at"`
	LastSeenAt      time.Time   `json:"last_seen_at"`
	Status          string      `json:"status"`
	ServiceRequests []uuid.UUID `json:"service_request_ids"`
	AcknowledgedBy  *string     `json:"acknowledged_by_name"`
	AcknowledgedAt  *time.Time  `json:"acknowledged_at"`
	ResolvedAt      *time.Time  `json:"resolved_at"`
	Note            *string     `json:"note"`
	AllowedActions  []string    `json:"allowed_actions"`
	Version         int         `json:"version"`
}

const riSelect = `SELECT ri.id, ri.property_id, ri.location_id, l.name,
	COALESCE((SELECT string_agg(a.name, ' · ' ORDER BY a.depth) FROM locations a WHERE a.path @> l.path AND a.depth > 0), l.name),
	ri.category_code, c.name, ri.request_count, ri.complaint_count,
	(SELECT count(*) FROM service_requests sr WHERE sr.id = ANY(ri.service_request_ids) AND sr.status NOT IN ('resolved','closed','cancelled')),
	ri.window_days, ri.threshold, ri.first_seen_at, ri.last_seen_at, ri.status, ri.service_request_ids, ab.full_name, ri.acknowledged_at, ri.resolved_at, ri.note, ri.version
	FROM recurring_issues ri JOIN locations l ON l.id = ri.location_id LEFT JOIN service_request_categories c ON c.id = ri.category_id LEFT JOIN users ab ON ab.id = ri.acknowledged_by`

func scanRI(row pgx.Row) (*RecurringIssue, error) {
	var r RecurringIssue
	if err := row.Scan(&r.ID, &r.PropertyID, &r.LocationID, &r.LocationName, &r.LocationPath, &r.CategoryCode, &r.CategoryName, &r.RequestCount, &r.ComplaintCount, &r.OpenCount,
		&r.WindowDays, &r.Threshold, &r.FirstSeenAt, &r.LastSeenAt, &r.Status, &r.ServiceRequests, &r.AcknowledgedBy, &r.AcknowledgedAt, &r.ResolvedAt, &r.Note, &r.Version); err != nil {
		return nil, err
	}
	if r.ServiceRequests == nil {
		r.ServiceRequests = []uuid.UUID{}
	}
	return &r, nil
}

func (s *Service) riActions(ctx context.Context, r *RecurringIssue) {
	p := authctx.Must(ctx)
	r.AllowedActions = []string{"view"}
	if !p.HasOnProperty("tenant_relation.recurring_issues.manage", r.PropertyID) {
		return
	}
	switch r.Status {
	case "open":
		r.AllowedActions = append(r.AllowedActions, "acknowledge", "resolve")
	case "acknowledged":
		r.AllowedActions = append(r.AllowedActions, "resolve")
	}
}

// ListRecurringIssues: status kosong = open + acknowledged.
func (s *Service) ListRecurringIssues(ctx context.Context, propertyID *uuid.UUID, statuses []string) ([]RecurringIssue, error) {
	p := authctx.Must(ctx)
	out := []RecurringIssue{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		where := " WHERE true"
		if propertyID != nil {
			if !p.HasOnProperty("tenant_relation.recurring_issues.view", *propertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *propertyID)
			where += " AND ri.property_id = $1"
		} else if pids, all := p.PropertyIDsFor("tenant_relation.recurring_issues.view"); !all {
			args = append(args, pids)
			where += " AND ri.property_id = ANY($1)"
		}
		if len(statuses) == 0 {
			statuses = []string{"open", "acknowledged"}
		}
		args = append(args, statuses)
		where += " AND ri.status = ANY($" + itoa(len(args)) + ")"
		rows, err := tx.Query(ctx, riSelect+where+" ORDER BY ri.last_seen_at DESC LIMIT 200", args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRI(rows)
			if err != nil {
				return err
			}
			s.riActions(ctx, r)
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) GetRecurringIssue(ctx context.Context, id uuid.UUID) (*RecurringIssue, error) {
	var out *RecurringIssue
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := scanRI(tx.QueryRow(ctx, riSelect+` WHERE ri.id = $1`, id))
		if err != nil {
			return apperr.NotFound("Recurring issue")
		}
		if !authctx.Must(ctx).HasOnProperty("tenant_relation.recurring_issues.view", r.PropertyID) {
			return apperr.Forbidden("")
		}
		s.riActions(ctx, r)
		out = r
		return nil
	})
	return out, err
}

// ActRecurringIssue: acknowledge | resolve (catatan opsional).
func (s *Service) ActRecurringIssue(ctx context.Context, id uuid.UUID, action, note string) (*RecurringIssue, error) {
	p := authctx.Must(ctx)
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := scanRI(tx.QueryRow(ctx, riSelect+` WHERE ri.id = $1 FOR UPDATE OF ri`, id))
		if err != nil {
			return apperr.NotFound("Recurring issue")
		}
		s.riActions(ctx, r)
		ok := false
		for _, a := range r.AllowedActions {
			ok = ok || a == action
		}
		if !ok {
			return apperr.InvalidTransition("Aksi " + action + " tidak tersedia untuk status " + r.Status)
		}
		note = strings.TrimSpace(note)
		switch action {
		case "acknowledge":
			_, err = tx.Exec(ctx, `UPDATE recurring_issues SET status = 'acknowledged', acknowledged_by = $2, acknowledged_at = now(), note = COALESCE(NULLIF($3,''), note) WHERE id = $1`, id, p.UserID, note)
		case "resolve":
			_, err = tx.Exec(ctx, `UPDATE recurring_issues SET status = 'resolved', resolved_by = $2, resolved_at = now(), note = COALESCE(NULLIF($3,''), note) WHERE id = $1`, id, p.UserID, note)
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "recurring_issue", EntityID: &id, EntityLabel: r.LocationName + " · " + r.CategoryCode, Before: map[string]any{"status": r.Status}, After: map[string]any{"action": action, "note": note}})
	})
	if err != nil {
		return nil, err
	}
	return s.GetRecurringIssue(ctx, id)
}

// ---------- Log komunikasi per SR (PRD P3 v2.1 P3-TRC-02..03) ----------

// CommunicationEntry: satu komunikasi ke/dari tenant pada SR — notifikasi in-app & push beserta status kirim/baca,
// pesan dua arah, dan WhatsApp manual (aksi staf; sistem tidak tahu apakah benar-benar terkirim).
type CommunicationEntry struct {
	At        time.Time `json:"at"`
	Channel   string    `json:"channel"`   // inapp | push | message | whatsapp_manual
	Direction string    `json:"direction"` // to_tenant | from_tenant
	Recipient *string   `json:"recipient"`
	Actor     *string   `json:"actor"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Status    string    `json:"status"` // delivered | read | unread | failed | sent_manually | not_sent
	Detail    *string   `json:"detail"`
}

func (s *Service) Communications(ctx context.Context, id uuid.UUID) ([]CommunicationEntry, error) {
	out := []CommunicationEntry{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !operations.CanAt(ctx, tx, "tenant.service_requests.view", r.PropertyID, r.Location.ID) {
			return apperr.Forbidden("")
		}
		rows, err := tx.Query(ctx, `SELECT n.created_at, n.title, n.body, u.full_name, n.read_at, n.push_attempted_at, n.delivered_push_at, n.push_error
			FROM notifications n JOIN users u ON u.id = n.user_id JOIN tenant_users tu ON tu.user_id = n.user_id
			WHERE n.object_type = 'service_request' AND n.object_id = $1 ORDER BY n.created_at`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var at time.Time
			var title, body, name string
			var readAt, pushAt, deliveredAt *time.Time
			var pushErr *string
			if err := rows.Scan(&at, &title, &body, &name, &readAt, &pushAt, &deliveredAt, &pushErr); err != nil {
				rows.Close()
				return err
			}
			st := "unread"
			if readAt != nil {
				st = "read"
			}
			nm := name
			out = append(out, CommunicationEntry{At: at, Channel: "inapp", Direction: "to_tenant", Recipient: &nm, Title: title, Body: body, Status: st})
			if pushAt != nil || deliveredAt != nil || pushErr != nil {
				pst, when := "failed", at
				if deliveredAt != nil {
					pst, when = "delivered", *deliveredAt
				} else if pushAt != nil {
					when = *pushAt
				}
				if pushErr != nil && *pushErr == "no_device" {
					pst = "not_sent"
				}
				out = append(out, CommunicationEntry{At: when, Channel: "push", Direction: "to_tenant", Recipient: &nm, Title: title, Body: firstLine(body), Status: pst, Detail: pushErr})
			}
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT m.created_at, m.author_kind, COALESCE(u.full_name,''), m.body, CASE WHEN m.author_kind = 'tenant' THEN m.read_by_staff_at ELSE m.read_by_tenant_at END
			FROM service_request_messages m LEFT JOIN users u ON u.id = m.author_user_id WHERE m.service_request_id = $1 ORDER BY m.created_at`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var at time.Time
			var kind, name, body string
			var readAt *time.Time
			if err := rows.Scan(&at, &kind, &name, &body, &readAt); err != nil {
				rows.Close()
				return err
			}
			dir := "to_tenant"
			if kind == "tenant" {
				dir = "from_tenant"
			}
			st := "unread"
			if readAt != nil {
				st = "read"
			}
			nm := name
			out = append(out, CommunicationEntry{At: at, Channel: "message", Direction: dir, Actor: &nm, Title: "Pesan", Body: body, Status: st})
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT w.sent_at, w.context, w.recipient_name, w.phone, COALESCE(w.message_preview,''), u.full_name FROM manual_whatsapp_logs w JOIN users u ON u.id = w.sent_by
			WHERE w.object_type = 'service_request' AND w.object_id = $1 ORDER BY w.sent_at`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var at time.Time
			var ctxName, phone, preview, actor string
			var rcpt *string
			if err := rows.Scan(&at, &ctxName, &rcpt, &phone, &preview, &actor); err != nil {
				rows.Close()
				return err
			}
			a, detail := actor, "+"+phone
			out = append(out, CommunicationEntry{At: at, Channel: "whatsapp_manual", Direction: "to_tenant", Recipient: rcpt, Actor: &a, Title: "Dikirim manual via WhatsApp", Body: preview, Status: "sent_manually", Detail: &detail})
		}
		rows.Close()
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, err
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
