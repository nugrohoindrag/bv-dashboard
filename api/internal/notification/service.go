// Package notification: domain event → notification rules → inbox (+ push job) — PRD §17, TAD §5.14,
// Naming Convention §52–§53 ([Object] + [Event]; Title / Context / Action).
package notification

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/jobs"
)

type Service struct {
	DB        *db.DB
	Jobs      jobs.Enqueuer
	Pusher    Pusher    // FCM (APK Staff/Tenant); nil = tanpa push FCM (inbox tetap jalan)
	WebPush   WebPusher // Web Push PWA (VAPID); nil = tanpa Web Push
	PublicURL string
	// VAPIDPublicKey & FCMEnabled: dilaporkan ke klien lewat GET /push/config (PRD P3 v2.1 P3-PSH-01..02)
	VAPIDPublicKey string
	FCMEnabled     bool
	// Channels: adapter channel eksternal selain in-app & push (PRD P0 v2 §14.3), mis. "email". Email di-hold (Roadmap §39.4).
	Channels map[string]ChannelAdapter
}

func (s *Service) Name() string { return "notification" }

type rule struct {
	id        uuid.UUID
	resolver  string
	ntype     string
	severity  string
	channels  []string
	dedupMins int
}

// Handle: subscriber domain event (dipanggil worker; juga dipakai test secara sinkron).
func (s *Service) Handle(ctx context.Context, ev events.Event) error {
	ctx = authctx.With(ctx, authctx.System(ev.OrganizationID))
	return s.DB.WithOrgTx(ctx, ev.OrganizationID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, recipient_resolver, notification_type, severity, channels, dedup_minutes FROM notification_rules
			WHERE event_type = $1 AND is_active AND (organization_id IS NULL OR organization_id = $2) ORDER BY organization_id NULLS LAST`, ev.Type, ev.OrganizationID)
		if err != nil {
			return err
		}
		var rules []rule
		for rows.Next() {
			var r rule
			if err := rows.Scan(&r.id, &r.resolver, &r.ntype, &r.severity, &r.channels, &r.dedupMins); err != nil {
				rows.Close()
				return err
			}
			rules = append(rules, r)
		}
		rows.Close()
		if len(rules) == 0 {
			return nil
		}
		title, body, deepLink := s.render(ctx, tx, ev, false)
		tTitle, tBody, tLink := "", "", ""
		for _, r := range rules {
			recipients := s.resolve(ctx, tx, r.resolver, ev)
			title, body, deepLink := title, body, deepLink
			if tenantResolvers[r.resolver] {
				// P1: penerima tenant → judul/status tenant-facing & deep link Tenant App (PRD §14, §20; guardrail #12)
				if tTitle == "" {
					tTitle, tBody, tLink = s.render(ctx, tx, ev, true)
				}
				title, body, deepLink = tTitle, tBody, tLink
			}
			for _, uid := range recipients {
				if ev.ActorUserID != nil && *ev.ActorUserID == uid && !strings.HasSuffix(ev.Type, ".created") && !strings.HasSuffix(ev.Type, ".assigned") && !strings.HasSuffix(ev.Type, ".confirmed") {
					// aktor tidak perlu diberi tahu atas aksinya sendiri (pesan, konfirmasi, reopen, feedback);
					// self-assign & created tetap dikirim (assignee harus tahu; tenant mendapat konfirmasi Ticket Created)
					continue
				}
				if r.dedupMins > 0 {
					var exists bool
					_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notifications WHERE user_id = $1 AND type = $2 AND object_id = $3 AND created_at > now() - ($4 || ' minutes')::interval)`, uid, r.ntype, ev.ObjectID, r.dedupMins).Scan(&exists)
					if exists {
						continue
					}
				}
				var pref struct {
					inapp, push bool
					email       *bool
				}
				pref.inapp, pref.push = true, true
				_ = tx.QueryRow(ctx, `SELECT inapp, push, email FROM notification_preferences WHERE user_id = $1 AND type = $2`, uid, r.ntype).Scan(&pref.inapp, &pref.push, &pref.email)
				if !pref.inapp {
					continue
				}
				var nid uuid.UUID
				if err := tx.QueryRow(ctx, `INSERT INTO notifications (organization_id, user_id, type, title, body, object_type, object_id, object_label, deep_link, severity) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
					ev.OrganizationID, uid, r.ntype, title, body, ev.ObjectType, ev.ObjectID, ev.ObjectLabel, deepLink, r.severity).Scan(&nid); err != nil {
					return err
				}
				if pref.push && contains(r.channels, "push") && s.Jobs != nil {
					_ = s.Jobs.EnqueueTx(ctx, tx, jobs.NotificationPushArgs{NotificationID: nid, OrganizationID: ev.OrganizationID})
				}
				s.enqueueChannels(ctx, tx, ev.OrganizationID, nid, r.channels, pref.email)
			}
		}
		return nil
	})
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// resolve: recipients resolver (TAD §5.14): assignee | assignee_team_supervisor | property_domain_supervisor[:domain] | requester | reporter
func (s *Service) resolve(ctx context.Context, tx pgx.Tx, resolver string, ev events.Event) []uuid.UUID {
	set := map[uuid.UUID]struct{}{}
	add := func(id uuid.UUID) {
		if id != uuid.Nil {
			set[id] = struct{}{}
		}
	}
	pl := ev.Payload
	getUUID := func(k string) uuid.UUID {
		if pl == nil {
			return uuid.Nil
		}
		switch v := pl[k].(type) {
		case string:
			id, _ := uuid.Parse(v)
			return id
		case uuid.UUID:
			return v
		case *uuid.UUID:
			if v != nil {
				return *v
			}
		}
		return uuid.Nil
	}
	assigneeUser, assigneeTeam := getUUID("assignee_user_id"), getUUID("assignee_team_id")
	if assigneeUser == uuid.Nil && assigneeTeam == uuid.Nil {
		// ambil dari object
		table := map[string]string{"task": "tasks", "work_order": "work_orders", "service_request": "service_requests", "incident": "incidents"}[ev.ObjectType]
		if table != "" {
			var au, at *uuid.UUID
			_ = tx.QueryRow(ctx, `SELECT assignee_user_id, assignee_team_id FROM `+table+` WHERE id = $1`, ev.ObjectID).Scan(&au, &at)
			if au != nil {
				assigneeUser = *au
			}
			if at != nil {
				assigneeTeam = *at
			}
		}
	}
	domain := ""
	if i := strings.Index(resolver, ":"); i > 0 {
		domain = resolver[i+1:]
		resolver = resolver[:i]
	}
	switch resolver {
	case "assignee":
		add(assigneeUser)
		if assigneeTeam != uuid.Nil {
			rows, err := tx.Query(ctx, `SELECT user_id FROM team_members WHERE team_id = $1`, assigneeTeam)
			if err == nil {
				for rows.Next() {
					var id uuid.UUID
					if rows.Scan(&id) == nil {
						add(id)
					}
				}
				rows.Close()
			}
		}
	case "assignee_team_supervisor":
		// lead team assignee; jika user-assignee: lead dari team user; fallback domain supervisor property
		teamID := assigneeTeam
		if teamID == uuid.Nil && assigneeUser != uuid.Nil {
			_ = tx.QueryRow(ctx, `SELECT tm.team_id FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE tm.user_id = $1 AND t.is_active ORDER BY t.property_id NULLS LAST LIMIT 1`, assigneeUser).Scan(&teamID)
		}
		found := false
		if teamID != uuid.Nil {
			rows, err := tx.Query(ctx, `SELECT user_id FROM team_members WHERE team_id = $1 AND is_lead`, teamID)
			if err == nil {
				for rows.Next() {
					var id uuid.UUID
					if rows.Scan(&id) == nil {
						add(id)
						found = true
					}
				}
				rows.Close()
			}
		}
		if !found {
			for _, id := range s.domainSupervisors(ctx, tx, ev, domainOfObject(ctx, tx, ev)) {
				add(id)
			}
		}
	case "property_domain_supervisor":
		if domain == "" {
			domain = domainOfObject(ctx, tx, ev)
		}
		for _, id := range s.domainSupervisors(ctx, tx, ev, domain) {
			add(id)
		}
	case "requester":
		if id := getUUID("requester_user_id"); id != uuid.Nil {
			add(id)
		} else if ev.ObjectType == "work_order" {
			var rq *uuid.UUID
			_ = tx.QueryRow(ctx, `SELECT requester_user_id FROM work_orders WHERE id = $1`, ev.ObjectID).Scan(&rq)
			if rq != nil {
				add(*rq)
			}
		} else if ev.ObjectType == "service_request" || ev.ObjectType == "export" {
			var cb *uuid.UUID
			if ev.ObjectType == "export" {
				_ = tx.QueryRow(ctx, `SELECT requested_by FROM exports WHERE id = $1`, ev.ObjectID).Scan(&cb)
			} else {
				_ = tx.QueryRow(ctx, `SELECT created_by FROM service_requests WHERE id = $1`, ev.ObjectID).Scan(&cb)
			}
			if cb != nil {
				add(*cb)
			}
		}
	case "reporter":
		if id := getUUID("reporter_user_id"); id != uuid.Nil {
			add(id)
		}
	case "tenant_user":
		// P1: pelapor/pemilik object di Tenant App (users.id) — dari payload atau dari objek. Tagihan & pembayaran: seluruh
		// tenant user yang melihat invoice tersebut (PRD P4 v2.1 B-10), ditambah pembayar bila ada.
		payer := getUUID("tenant_user_id")
		if payer != uuid.Nil && ev.ObjectType != "invoice" && ev.ObjectType != "payment" {
			add(payer)
		} else {
			add(payer)
			for _, id := range s.tenantRecipients(ctx, tx, ev) {
				add(id)
			}
		}
	// PRD P1 v2 §49 Notifications Matrix: kolom "Manager" — pemegang role manajer (property/building/operations) pada property.
	case "property_manager":
		rows, err := tx.Query(ctx, `SELECT DISTINCT ur.user_id FROM user_roles ur JOIN roles r ON r.id = ur.role_id JOIN users u ON u.id = ur.user_id
			WHERE r.code = ANY($1) AND u.is_active AND u.deleted_at IS NULL AND ($2::uuid IS NULL OR ur.property_id IS NULL OR ur.property_id = $2)`,
			[]string{"property_manager", "building_manager", "operations_manager"}, ev.PropertyID)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					add(id)
				}
			}
			rows.Close()
		}
	// kolom "Worker": seluruh anggota team domain pada property (mis. Critical Incident → petugas security on duty)
	case "property_domain_team":
		if domain == "" {
			domain = domainOfObject(ctx, tx, ev)
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT tm.user_id FROM team_members tm JOIN teams t ON t.id = tm.team_id
			WHERE t.is_active AND t.domain = $2 AND ($1::uuid IS NULL OR t.property_id IS NULL OR t.property_id = $1)`, ev.PropertyID, domain)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					add(id)
				}
			}
			rows.Close()
		}
	// PRD P2 v2.1 P2-DTY-03 / P2-EMG-03: staf ON-DUTY (clock-in aktif) domain di property; fallback staf yang dijadwalkan
	// pada shift yang sedang berjalan, lalu seluruh anggota team domain (perilaku lama property_domain_team).
	case "property_domain_on_duty":
		if domain == "" {
			domain = domainOfObject(ctx, tx, ev)
		}
		for _, id := range s.onDutyUsers(ctx, tx, ev, domain) {
			add(id)
		}
	// tujuan eskalasi eksplisit (POST /tasks|work-orders/{id}/escalate escalate_to_user_id)
	case "escalate_to":
		add(getUUID("escalate_to_user_id"))
	case "tenant_property_users":
		// P1: seluruh tenant user aktif pada property; pengumuman bertarget (PRD P3 v2.1 P3-ANN-02) hanya ke audiens target
		if ev.ObjectType == "announcement" {
			for _, id := range announcementAudience(ctx, tx, ev.ObjectID) {
				add(id)
			}
			break
		}
		rows, err := tx.Query(ctx, `SELECT user_id FROM tenant_users WHERE status = 'active' AND ($1::uuid IS NULL OR property_id = $1)`, ev.PropertyID)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					add(id)
				}
			}
			rows.Close()
		}
	}
	out := make([]uuid.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// domainSupervisors: lead team domain di property (fallback: user dengan role *_supervisor/manager domain di property).
func (s *Service) domainSupervisors(ctx context.Context, tx pgx.Tx, ev events.Event, domain string) []uuid.UUID {
	var out []uuid.UUID
	if domain == "" || domain == "management" {
		domain = ""
	}
	q := `SELECT DISTINCT tm.user_id FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE tm.is_lead AND t.is_active AND ($1::uuid IS NULL OR t.property_id IS NULL OR t.property_id = $1) AND ($2 = '' OR t.domain = $2)`
	rows, err := tx.Query(ctx, q, ev.PropertyID, domain)
	if err == nil {
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				out = append(out, id)
			}
		}
		rows.Close()
	}
	if len(out) == 0 {
		// fallback: role supervisor/manager domain
		codes := []string{"operations_manager", "building_manager"}
		if domain != "" {
			codes = []string{domain + "_supervisor", domain + "_manager"}
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT ur.user_id FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE r.code = ANY($1) AND ($2::uuid IS NULL OR ur.property_id IS NULL OR ur.property_id = $2)`, codes, ev.PropertyID)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					out = append(out, id)
				}
			}
			rows.Close()
		}
	}
	return out
}

// onDutyUsers: penerima "on-duty" domain (P2-DTY-03) — attendance terbuka → roster shift berjalan → team domain.
func (s *Service) onDutyUsers(ctx context.Context, tx pgx.Tx, ev events.Event, domain string) []uuid.UUID {
	queries := []string{
		`SELECT DISTINCT a.user_id FROM attendance_records a JOIN users u ON u.id = a.user_id WHERE a.clock_out_at IS NULL AND a.domain = $2 AND u.is_active AND ($1::uuid IS NULL OR a.property_id = $1)`,
		`SELECT DISTINCT sa.user_id FROM shift_assignments sa JOIN users u ON u.id = sa.user_id WHERE sa.status = 'scheduled' AND sa.domain = $2 AND u.is_active AND now() BETWEEN sa.starts_at AND sa.ends_at AND ($1::uuid IS NULL OR sa.property_id = $1)`,
		`SELECT DISTINCT tm.user_id FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE t.is_active AND t.domain = $2 AND ($1::uuid IS NULL OR t.property_id IS NULL OR t.property_id = $1)`,
	}
	for _, q := range queries {
		var out []uuid.UUID
		rows, err := tx.Query(ctx, q, ev.PropertyID, domain)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				out = append(out, id)
			}
		}
		rows.Close()
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func domainOfObject(ctx context.Context, tx pgx.Tx, ev events.Event) string {
	if d, ok := ev.Payload["domain"].(string); ok && d != "" {
		return d
	}
	switch ev.ObjectType {
	case "task":
		var tt string
		_ = tx.QueryRow(ctx, `SELECT task_type FROM tasks WHERE id = $1`, ev.ObjectID).Scan(&tt)
		switch tt {
		case "patrol":
			return "security"
		case "cleaning":
			return "housekeeping"
		case "inspection":
			var hk bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM housekeeping_inspections WHERE task_id = $1)`, ev.ObjectID).Scan(&hk)
			if hk {
				return "housekeeping"
			}
			return "engineering"
		default:
			return "engineering"
		}
	case "work_order", "maintenance_schedule", "asset":
		return "engineering"
	case "incident":
		return "security"
	case "finding":
		var ft string
		_ = tx.QueryRow(ctx, `SELECT finding_type FROM findings WHERE id = $1`, ev.ObjectID).Scan(&ft)
		switch ft {
		case "patrol":
			return "security"
		case "housekeeping":
			return "housekeeping"
		}
		return "engineering"
	case "service_request":
		var d *string
		_ = tx.QueryRow(ctx, `SELECT c.default_domain FROM service_requests sr JOIN service_request_categories c ON c.id = sr.category_id WHERE sr.id = $1`, ev.ObjectID).Scan(&d)
		if d != nil {
			return *d
		}
	}
	return ""
}

// render: Title (Naming Convention §52 [Object] + [Event]) / body (Context: business id — title, location) / deep link.
// tenantFacing=true → istilah "Ticket", status tenant-facing, tanpa detail internal, deep link route Tenant App.
func (s *Service) render(ctx context.Context, tx pgx.Tx, ev events.Event, tenantFacing bool) (title, body, deepLink string) {
	if tenantFacing {
		return s.renderTenant(ctx, tx, ev)
	}
	objectLabel := map[string]string{"system": "System", "task": "Task", "work_order": "Work Order", "service_request": "Service Request", "incident": "Incident", "finding": "Finding", "maintenance_schedule": "Maintenance", "export": "Export",
		"tenant_user": "Tenant Account", "announcement": "Announcement", "booking": "Booking", "visitor": "Visitor", "invoice": "Invoice", "payment": "Payment", "hotel_reservation": "Reservation", "unit_rental_reservation": "Rental Reservation", "unit_sales_lead": "Sales Lead", "unit_sale_reservation": "Unit Reservation", "unit_listing": "Unit Listing", "unit_rental_listing": "Rental Listing",
		// PRD P2 v2.1
		"emergency_alert": "Emergency", "parking_violation": "Parking Violation", "lost_found_item": "Lost & Found", "shift_handover": "Shift Handover", "asset_document": "Equipment Document", "asset": "Asset", "cleaning_route_run": "Cleaning Route", "workforce": "Workforce", "attendance": "Attendance",
		"inventory_item": "Inventory Item",
		// PRD P3 v2.1 & P4 v2.1
		"package": "Package", "parking_permit": "Parking Permit", "tenant_feedback": "Tenant Feedback", "recurring_issue": "Recurring Issue",
		"billing_run": "Billing Run", "credit_note": "Credit Note", "meter_reading": "Meter Reading", "collection_log": "Collection", "budget": "Budget",
		"bank_statement_import": "Bank Statement"}[ev.ObjectType]
	if objectLabel == "" {
		objectLabel = ev.ObjectType
	}
	verb := ev.Type[strings.LastIndex(ev.Type, ".")+1:]
	eventLabel := map[string]string{
		"created": "Received", "assigned": "Assigned", "started": "Started", "completed": "Completed", "closed": "Closed", "reopened": "Reopened",
		"cancelled": "Cancelled", "due_soon": "Due Soon", "overdue": "Overdue", "sla_risk": "SLA Risk", "sla_breached": "SLA Breached",
		"resolved": "Resolved", "escalated": "Escalated", "checkpoint_missed": "Checkpoint Missed", "due": "Due", "conflict": "Sync Conflict", "ready": "Ready",
		"confirmed": "Confirmed", "activated": "Activated", "sold": "Sold", "handed_over": "Handed Over", "published": "Published", "status_changed": "Updated",
		"critical": "Critical Alert", "submitted": "Submitted",
		"raised": "Raised", "acknowledged": "Acknowledged", "responding": "Responding", "expiring": "Expiring Soon", "expired": "Expired", "health_changed": "Health Changed", "shortage": "Shortage",
		"detected": "Detected", "received": "Received", "requested": "Requested", "flagged": "Flagged", "generated": "Generated", "issued": "Issued", "approved": "Approved", "pending_approval": "Pending Approval",
	}[verb]
	if eventLabel == "" {
		eventLabel = strings.Title(strings.ReplaceAll(verb, "_", " "))
	}
	if ev.ObjectType == "task" {
		var tt string
		_ = tx.QueryRow(ctx, `SELECT task_type FROM tasks WHERE id = $1`, ev.ObjectID).Scan(&tt)
		switch tt {
		case "patrol":
			objectLabel = "Patrol"
		case "cleaning":
			objectLabel = "Cleaning Task"
		case "inspection":
			objectLabel = "Inspection"
		}
	}
	if ev.Type == events.SyncConflict {
		objectLabel, eventLabel = "Sync", "Conflict"
	}
	title = objectLabel + " " + eventLabel
	label, objTitle, _ := operations.DescribeObject(ctx, tx, ev.ObjectType, ev.ObjectID)
	if label == "" {
		label = ev.ObjectLabel
	}
	locPath := ""
	table := map[string]string{"task": "tasks", "work_order": "work_orders", "service_request": "service_requests", "incident": "incidents", "finding": "findings"}[ev.ObjectType]
	if table != "" {
		var locID *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT location_id FROM `+table+` WHERE id = $1`, ev.ObjectID).Scan(&locID)
		if locID != nil {
			_ = tx.QueryRow(ctx, `SELECT string_agg(a.name, ' · ' ORDER BY a.depth) FROM locations l JOIN locations a ON a.path @> l.path AND a.depth > 0 WHERE l.id = $1`, *locID).Scan(&locPath)
		}
	}
	body = label
	if objTitle != "" {
		body += " — " + objTitle
	}
	if locPath != "" {
		body += "\n" + locPath
	}
	if r, ok := ev.Payload["reason"].(string); ok && r != "" {
		body += "\n" + r
	}
	if lp, ok := ev.Payload["location_path"].(string); ok && lp != "" && locPath == "" {
		body += "\n" + lp
	}
	route := map[string]string{"task": "/operations/tasks/", "work_order": "/operations/work-orders/", "service_request": "/operations/service-requests/", "incident": "/operations/incidents/", "finding": "/findings/", "maintenance_schedule": "/engineering/preventive-maintenance/", "export": "/exports/",
		"tenant_user": "/tenant-relation/tenant-users/", "announcement": "/tenant-relation/announcements/", "booking": "/booking/bookings/", "visitor": "/security/visitors/", "invoice": "/billing/invoices/", "payment": "/billing/payments/", "hotel_reservation": "/commercial/hotel/reservations/", "unit_rental_reservation": "/commercial/rental/reservations/", "unit_sales_lead": "/commercial/sales/leads/", "unit_sale_reservation": "/commercial/sales/reservations/", "unit_listing": "/commercial/sales/listings/", "unit_rental_listing": "/commercial/rental/listings/",
		"emergency_alert": "/security/emergency/", "parking_violation": "/security/parking/violations/", "lost_found_item": "/security/lost-found/", "asset": "/assets/",
		"inventory_item": "/inventory?item=", "cleaning_route_run": "/housekeeping/routes?run=",
		"package": "/security/packages/", "parking_permit": "/security/parking/permits/", "tenant_feedback": "/tenant-relation/feedback/general/", "recurring_issue": "/tenant-relation/recurring-issues/",
		"billing_run": "/billing/runs/", "credit_note": "/billing/credit-notes/", "meter_reading": "/billing/meters/readings/", "budget": "/finance/budgets/", "collection_log": "/billing/collections?log=",
		"bank_statement_import": "/billing/reconciliation/"}[ev.ObjectType]
	if route != "" {
		deepLink = route + ev.ObjectID.String()
	}
	// PRD P2 v2.1 §8: serah terima & kapasitas shift dibuka dari modul domainnya
	if d, ok := ev.Payload["domain"].(string); ok && (d == "security" || d == "housekeeping") {
		switch ev.ObjectType {
		case "shift_handover":
			deepLink = "/" + d + "/shifts?tab=handovers&id=" + ev.ObjectID.String()
		case "attendance":
			deepLink = "/" + d + "/shifts?tab=attendance"
		case "workforce":
			deepLink = "/" + d + "/shifts?tab=on-duty"
		}
	}
	if m, ok := ev.Payload["message_preview"].(string); ok && m != "" {
		body += "\n" + m
	}
	return
}

// ---------- Inbox API ----------

type Notification struct {
	ID          uuid.UUID  `json:"id"`
	Type        string     `json:"type"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	ObjectType  *string    `json:"object_type"`
	ObjectID    *uuid.UUID `json:"object_id"`
	ObjectLabel *string    `json:"object_label"`
	DeepLink    *string    `json:"deep_link"`
	Severity    string     `json:"severity"`
	CreatedAt   time.Time  `json:"created_at"`
	ReadAt      *time.Time `json:"read_at"`
}

func (s *Service) List(ctx context.Context, unreadOnly bool, page httpx.Page) ([]Notification, *string, int, error) {
	p := authctx.Must(ctx)
	var out []Notification
	var next *string
	var unread int
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, p.UserID).Scan(&unread)
		args := []any{p.UserID}
		where := " WHERE user_id = $1"
		if unreadOnly {
			where += " AND read_at IS NULL"
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += " AND (created_at, id) < ($2::timestamptz, $3)"
		}
		args = append(args, page.Limit+1)
		rows, err := tx.Query(ctx, `SELECT id, type, title, body, object_type, object_id, object_label, deep_link, severity, created_at, read_at FROM notifications`+where+fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n Notification
			if err := rows.Scan(&n.ID, &n.Type, &n.Title, &n.Body, &n.ObjectType, &n.ObjectID, &n.ObjectLabel, &n.DeepLink, &n.Severity, &n.CreatedAt, &n.ReadAt); err != nil {
				return err
			}
			out = append(out, n)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			out = out[:page.Limit]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
		}
		return rows.Err()
	})
	if out == nil {
		out = []Notification{}
	}
	return out, next, unread, err
}

func (s *Service) MarkRead(ctx context.Context, id uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE notifications SET read_at = COALESCE(read_at, now()) WHERE id = $1 AND user_id = $2`, id, p.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Notification")
		}
		return nil
	})
}

func (s *Service) MarkAllRead(ctx context.Context) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, p.UserID)
		return err
	})
}

type Preference struct {
	Type  string `json:"type"`
	InApp bool   `json:"inapp"`
	Push  bool   `json:"push"`
	Email bool   `json:"email"` // PRD P0 v2 §14.3 (default mengikuti channel rule)
	// EmailAvailable: rule tipe ini mendukung email
	EmailAvailable bool `json:"email_available"`
	// Category: pengelompokan untuk layar preferensi (Tenant App P3-ACC-09): request | billing | announcement | booking |
	// visitor | package | parking | account | feedback | other
	Category string `json:"category"`
}

// preferenceCategory: kelompok tipe notifikasi untuk UI preferensi.
func preferenceCategory(ntype string) string {
	switch {
	case strings.HasPrefix(ntype, "ticket_") || strings.HasPrefix(ntype, "service_request"):
		return "request"
	case strings.HasPrefix(ntype, "invoice_") || strings.HasPrefix(ntype, "payment_") || strings.HasPrefix(ntype, "credit_note") ||
		strings.HasPrefix(ntype, "billing_run") || strings.HasPrefix(ntype, "meter_reading") || strings.HasPrefix(ntype, "collection_"):
		return "billing"
	case strings.HasPrefix(ntype, "announcement"):
		return "announcement"
	case strings.HasPrefix(ntype, "booking_"):
		return "booking"
	case strings.HasPrefix(ntype, "visitor_"):
		return "visitor"
	case strings.HasPrefix(ntype, "package_"):
		return "package"
	case strings.HasPrefix(ntype, "parking_"):
		return "parking"
	case strings.HasPrefix(ntype, "tenant_account") || strings.HasPrefix(ntype, "rental_"):
		return "account"
	case strings.HasPrefix(ntype, "tenant_feedback"):
		return "feedback"
	}
	return "other"
}

func (s *Service) ListPreferences(ctx context.Context) ([]Preference, error) {
	p := authctx.Must(ctx)
	var out []Preference
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// akun tenant hanya melihat tipe notifikasi tenant-facing; staf tidak melihat tipe khusus tenant (PRD P3 v2.1 P3-ACC-09)
		rows, err := tx.Query(ctx, `SELECT r.notification_type, bool_and(COALESCE(np.inapp, true)), bool_and(COALESCE(np.push, true)),
			bool_or('email' = ANY(r.channels)), bool_and(COALESCE(np.email, 'email' = ANY(r.channels)))
			FROM notification_rules r LEFT JOIN notification_preferences np ON np.type = r.notification_type AND np.user_id = $1
			WHERE r.is_active AND ((r.recipient_resolver IN ('tenant_user','tenant_property_users')) = $2)
			GROUP BY r.notification_type ORDER BY 1`, p.UserID, p.IsTenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var pr Preference
			if err := rows.Scan(&pr.Type, &pr.InApp, &pr.Push, &pr.EmailAvailable, &pr.Email); err != nil {
				return err
			}
			pr.Category = preferenceCategory(pr.Type)
			out = append(out, pr)
		}
		return rows.Err()
	})
	if out == nil {
		out = []Preference{}
	}
	return out, err
}

// PreferenceInput: kanal yang tidak dikirim tidak berubah (nilai lama / default rule).
type PreferenceInput struct {
	Type  string `json:"type"`
	InApp *bool  `json:"inapp"`
	Push  *bool  `json:"push"`
	Email *bool  `json:"email"`
}

func (s *Service) SetPreference(ctx context.Context, pr PreferenceInput) error {
	p := authctx.Must(ctx)
	if strings.TrimSpace(pr.Type) == "" {
		return apperr.Validation("type wajib").WithField("type", "wajib")
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO notification_preferences (user_id, type, inapp, push, email)
			VALUES ($1, $2, COALESCE($3::boolean, true), COALESCE($4::boolean, true),
				COALESCE($5::boolean, (SELECT bool_or('email' = ANY(channels)) FROM notification_rules WHERE notification_type = $2), false))
			ON CONFLICT (user_id, type) DO UPDATE SET inapp = COALESCE($3::boolean, notification_preferences.inapp),
				push = COALESCE($4::boolean, notification_preferences.push), email = COALESCE($5::boolean, notification_preferences.email)`,
			p.UserID, pr.Type, pr.InApp, pr.Push, pr.Email)
		if err != nil {
			return err
		}
		// PRD P0 v2 §16: perubahan konfigurasi tercatat di audit trail
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditConfigChange, EntityType: "notification_preference", EntityID: &p.UserID, EntityLabel: pr.Type, After: pr})
	})
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ErrInvalidToken ditandai adapter push.
type invalidTokenError struct{ error }

func InvalidToken(err error) error { return invalidTokenError{err} }
func IsInvalidToken(err error) bool {
	_, ok := err.(invalidTokenError)
	return ok
}
