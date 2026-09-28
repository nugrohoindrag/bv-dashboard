package seed

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/profile"
)

func EnsureOrganization(ctx context.Context, d *db.DB, name, slug string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := d.Pool.QueryRow(ctx, `SELECT id FROM organizations WHERE slug = $1`, slug).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !db.IsNoRows(err) {
		return uuid.Nil, false, err
	}
	var n int
	_ = d.Pool.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&n)
	code := fmt.Sprintf("ORG-%06d", n+1)
	err = d.Pool.QueryRow(ctx, `INSERT INTO organizations (code, slug, name) VALUES ($1,$2,$3) RETURNING id`, code, slug, name).Scan(&id)
	return id, err == nil, err
}

func CreateUser(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, email, name, password, roleCode string, propertyID *uuid.UUID) (uuid.UUID, error) {
	hash, err := iam.HashPassword(password)
	if err != nil {
		return uuid.Nil, err
	}
	code, err := ids.NextPlain(ctx, tx, orgID, ids.PrefixUser)
	if err != nil {
		return uuid.Nil, err
	}
	username := strings.Split(email, "@")[0]
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO users (organization_id, user_code, email, username, full_name, password_hash) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		orgID, code, email, username, name, hash).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	var roleID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE organization_id = $1 AND code = $2`, orgID, roleCode).Scan(&roleID); err != nil {
		return uuid.Nil, fmt.Errorf("role %s: %w", roleCode, err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id, property_id) VALUES ($1,$2,$3)`, id, roleID, propertyID)
	return id, err
}

func SeedEquipment(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	cats := []struct {
		code, name string
		types      []string
	}{
		{"HVAC", "HVAC", []string{"AHU", "Chiller", "FCU", "Cooling Tower", "Exhaust Fan"}},
		{"LIFT", "Lift", []string{"Passenger Lift", "Service Lift", "Escalator"}},
		{"GEN", "Generator", []string{"Diesel Generator", "ATS Panel"}},
		{"PUMP", "Pump", []string{"Transfer Pump", "Booster Pump", "Sump Pump"}},
		{"ELEC", "Electrical", []string{"LVMDP", "Panel Distribusi", "Trafo", "UPS", "Capacitor Bank"}},
		{"FIRE", "Fire Protection", []string{"Fire Pump", "Hydrant", "Sprinkler Zone", "Fire Alarm Panel", "APAR"}},
		{"PLMB", "Plumbing", []string{"Water Tank", "STP", "Water Treatment", "Grease Trap"}},
		{"SECU", "Security", []string{"CCTV", "Access Control", "Metal Detector", "Barrier Gate"}},
		{"FAC", "Building Facility", []string{"Toilet Fixture", "Lighting", "Signage", "Door"}},
		{"OTH", "Other", []string{"Other"}},
	}
	for _, c := range cats {
		var catID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO equipment (organization_id, category_code, category_name) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING RETURNING id`, orgID, c.code, c.name).Scan(&catID); err != nil {
			if db.IsNoRows(err) {
				continue
			}
			return err
		}
		for _, t := range c.types {
			if _, err := tx.Exec(ctx, `INSERT INTO equipment (organization_id, category_code, category_name, type_name, parent_id) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, orgID, c.code, c.name, t, catID); err != nil {
				return err
			}
		}
	}
	return nil
}

func SeedSLADefaults(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	// Naming Convention §30: Critical 1h · High 4h · Medium 8h · Low 24h (resolution). Response = 25%.
	def := map[string]int{"critical": 60, "high": 240, "medium": 480, "low": 1440}
	for _, ot := range []string{"task", "work_order", "service_request", "incident"} {
		for prio, mins := range def {
			if _, err := tx.Exec(ctx, `INSERT INTO sla_policies (organization_id, property_id, object_type, priority, response_minutes, resolution_minutes) VALUES ($1,NULL,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
				orgID, ot, prio, mins/4, mins); err != nil {
				return err
			}
		}
	}
	return nil
}

func SeedSRCategories(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	cats := []struct{ code, name, domain, prio string }{
		{"complaint", "Complaint", "management", "high"},
		{"inquiry", "Inquiry", "management", "low"},
		{"maintenance", "Maintenance", "engineering", "medium"},
		{"facility", "Facility", "engineering", "medium"},
		{"cleaning", "Cleaning", "housekeeping", "medium"},
		{"security", "Security", "security", "high"},
		{"other", "Other", "management", "low"},
	}
	for i, c := range cats {
		if _, err := tx.Exec(ctx, `INSERT INTO service_request_categories (organization_id, code, name, default_domain, default_priority, sort_order) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			orgID, c.code, c.name, c.domain, c.prio, i); err != nil {
			return err
		}
	}
	// P1: kategori tenant PRD §11 (profile-aware, dengan ikon) — idempotent
	return profile.SeedCategoriesTx(ctx, tx, orgID)
}

// seedNotificationRules: tabel PRD §17.1 (+ sync.conflict OD-004) sebagai rule global.
func SeedNotificationRules(ctx context.Context, q db.Querier) error {
	type rule struct {
		event, resolver, ntype, severity string
		dedup                            int
	}
	// PRD P0 v2 §14.2: update Work Order (start/hold/reopen/cancel) & Task reopen/cancel
	extra := []rule{
		{"work_order.started", "requester", "work_order_started", "info", 0},
		{"work_order.held", "assignee_team_supervisor", "work_order_on_hold", "warning", 0},
		{"work_order.reopened", "assignee", "work_order_reopened", "warning", 0},
		{"work_order.cancelled", "assignee", "work_order_cancelled", "info", 0},
		{"work_order.cancelled", "requester", "work_order_cancelled", "info", 0},
		{"task.reopened", "assignee", "task_reopened", "warning", 0},
		{"task.cancelled", "assignee", "task_cancelled", "info", 0},
		// ---- PRD P1 v2 §35 & §49 Notifications Matrix (Manager · Supervisor · Worker · Tenant) ----
		{"task.overdue", "property_manager", "task_overdue", "critical", 60},
		{"work_order.overdue", "property_manager", "work_order_overdue", "critical", 60},
		{"task.sla_risk", "property_manager", "task_sla_risk", "warning", 60},
		{"work_order.sla_risk", "property_manager", "work_order_sla_risk", "warning", 60},
		{"service_request.sla_risk", "property_manager", "service_request_sla_risk", "warning", 60},
		{"task.sla_breached", "assignee", "task_sla_breached", "critical", 30},
		{"task.sla_breached", "property_manager", "task_sla_breached", "critical", 60},
		{"work_order.sla_breached", "assignee", "work_order_sla_breached", "critical", 30},
		{"work_order.sla_breached", "property_manager", "work_order_sla_breached", "critical", 60},
		{"service_request.sla_breached", "assignee", "service_request_sla_breached", "critical", 30},
		{"service_request.sla_breached", "assignee_team_supervisor", "service_request_sla_breached", "critical", 30},
		{"service_request.sla_breached", "property_manager", "service_request_sla_breached", "critical", 60},
		{"incident.sla_risk", "assignee", "incident_sla_risk", "warning", 30},
		{"incident.sla_breached", "property_domain_supervisor:security", "incident_sla_breached", "critical", 30},
		{"incident.critical", "property_manager", "incident_critical", "critical", 0},
		{"incident.critical", "property_domain_supervisor:security", "incident_critical", "critical", 0},
		{"incident.critical", "property_domain_team:security", "incident_critical", "critical", 0},
		{"work_order.submitted", "property_domain_supervisor", "work_order_submitted", "info", 0},
		{"task.escalated", "assignee_team_supervisor", "task_escalated", "warning", 0},
		{"task.escalated", "property_manager", "task_escalated", "warning", 0},
		{"task.escalated", "escalate_to", "task_escalated", "warning", 0},
		{"work_order.escalated", "assignee_team_supervisor", "work_order_escalated", "warning", 0},
		{"work_order.escalated", "property_manager", "work_order_escalated", "warning", 0},
		{"work_order.escalated", "escalate_to", "work_order_escalated", "warning", 0},
		{"service_request.created", "property_manager", "service_request_received", "info", 0},
		{"service_request.resolved", "property_domain_supervisor:tenant_relation", "service_request_resolved", "success", 0},
		{"service_request.resolved", "property_manager", "service_request_resolved", "success", 0},
		{"service_request.reopened", "tenant_user", "ticket_reopened", "warning", 0}, // reopen oleh staf; tenant yang me-reopen sendiri tidak diberi tahu (aktor)
		// ---- PRD P2 v2.1 §6.4 Emergency: notifikasi segera ke security on-duty, supervisor, manager (P2-EMG-03) ----
		{"emergency_alert.raised", "property_domain_on_duty:security", "emergency_raised", "critical", 0},
		{"emergency_alert.raised", "property_domain_supervisor:security", "emergency_raised", "critical", 0},
		{"emergency_alert.raised", "property_manager", "emergency_raised", "critical", 0},
		{"emergency_alert.acknowledged", "reporter", "emergency_acknowledged", "info", 0},
		{"emergency_alert.acknowledged", "property_manager", "emergency_acknowledged", "info", 0},
		{"emergency_alert.responding", "reporter", "emergency_responding", "info", 0},
		{"emergency_alert.escalated", "property_manager", "emergency_escalated", "critical", 0},
		{"emergency_alert.escalated", "property_domain_supervisor:security", "emergency_escalated", "critical", 0},
		{"emergency_alert.resolved", "reporter", "emergency_resolved", "success", 0},
		{"emergency_alert.resolved", "property_domain_supervisor:security", "emergency_resolved", "success", 0},
		{"emergency_alert.cancelled", "property_domain_supervisor:security", "emergency_cancelled", "info", 0},
		// §6.3 eskalasi incident (P2-SIN-05)
		{"incident.escalated", "property_domain_supervisor:security", "incident_escalated", "warning", 0},
		{"incident.escalated", "property_manager", "incident_escalated", "warning", 0},
		{"incident.escalated", "escalate_to", "incident_escalated", "warning", 0},
		// §8 Workforce: serah terima shift → penerima + supervisor domain; sertifikat/lisensi kedaluwarsa (P2-TEC-02, P2-SPN-02)
		{"shift_handover.submitted", "assignee", "shift_handover", "info", 0},
		{"shift_handover.submitted", "property_domain_supervisor", "shift_handover", "info", 0},
		// §5.6 dokumen equipment & warranty kedaluwarsa (P2-DOC-02), health equipment turun ke critical (P2-EQH-03)
		{"asset_document.expiring", "property_domain_supervisor:engineering", "document_expiring", "warning", 0},
		{"asset_document.expired", "property_domain_supervisor:engineering", "document_expired", "critical", 0},
		{"asset_document.expired", "property_manager", "document_expired", "critical", 0},
		{"asset.health_changed", "property_domain_supervisor:engineering", "asset_health_critical", "warning", 0},
	}
	rules := []rule{
		{"task.assigned", "assignee", "task_assigned", "info", 0},
		{"work_order.assigned", "assignee", "work_order_assigned", "info", 0},
		{"task.due_soon", "assignee", "task_due_soon", "warning", 30},
		{"work_order.due_soon", "assignee", "work_order_due_soon", "warning", 30},
		{"task.sla_risk", "assignee", "task_sla_risk", "warning", 30},
		{"task.sla_risk", "assignee_team_supervisor", "task_sla_risk", "warning", 30},
		{"work_order.sla_risk", "assignee", "work_order_sla_risk", "warning", 30},
		{"work_order.sla_risk", "assignee_team_supervisor", "work_order_sla_risk", "warning", 30},
		{"task.overdue", "assignee", "task_overdue", "critical", 30},
		{"task.overdue", "assignee_team_supervisor", "task_overdue", "critical", 30},
		{"work_order.overdue", "assignee", "work_order_overdue", "critical", 30},
		{"work_order.overdue", "assignee_team_supervisor", "work_order_overdue", "critical", 30},
		{"task.sla_breached", "assignee_team_supervisor", "task_sla_breached", "critical", 30},
		{"work_order.sla_breached", "assignee_team_supervisor", "work_order_sla_breached", "critical", 30},
		{"patrol_task.overdue", "property_domain_supervisor:security", "patrol_overdue", "critical", 30},
		// PRD P2 v2.1 P2-PAT-05: event checkpoint_missed sudah dipancarkan security service tetapi belum punya rule
		{"patrol_task.checkpoint_missed", "property_domain_supervisor:security", "checkpoint_missed", "warning", 0},
		{"finding.created", "property_domain_supervisor", "finding_created", "warning", 0},
		{"service_request.created", "property_domain_supervisor", "service_request_received", "info", 0},
		{"service_request.assigned", "assignee", "service_request_assigned", "info", 0},
		{"service_request.sla_risk", "assignee", "service_request_sla_risk", "warning", 30},
		{"service_request.sla_risk", "assignee_team_supervisor", "service_request_sla_risk", "warning", 30},
		{"work_order.completed", "assignee_team_supervisor", "work_order_completed", "success", 0},
		{"work_order.completed", "requester", "work_order_completed", "success", 0},
		{"work_order.closed", "requester", "work_order_closed", "success", 0},
		{"task.completed", "assignee_team_supervisor", "task_completed", "success", 0},
		{"incident.created", "property_domain_supervisor:security", "incident_reported", "warning", 0}, // severity critical → event incident.critical (PRD P1 v2 §35)
		{"incident.assigned", "assignee", "incident_assigned", "warning", 0},
		{"maintenance_schedule.due", "property_domain_supervisor:engineering", "maintenance_due", "warning", 720},
		{"sync.conflict", "assignee_team_supervisor", "sync_conflict", "warning", 0},
		{"export.ready", "requester", "export_ready", "info", 0},
		// ---- P1 (PRD v1.3 §20 Notifications & Inbox; §32) ----
		{"service_request.created", "tenant_user", "ticket_created", "success", 0},
		{"service_request.created", "property_domain_supervisor:tenant_relation", "service_request_received", "info", 0},
		{"service_request.acknowledged", "tenant_user", "ticket_status", "info", 0},
		{"service_request.assigned", "tenant_user", "ticket_status", "info", 0},
		{"service_request.started", "tenant_user", "ticket_status", "info", 0},
		{"service_request.waiting_for_tenant", "tenant_user", "ticket_need_response", "warning", 0},
		{"service_request.resolved", "tenant_user", "ticket_resolved", "success", 0},
		{"service_request.closed", "tenant_user", "ticket_closed", "success", 0},
		{"service_request.cancelled", "tenant_user", "ticket_status", "info", 0},
		{"service_request.reopened", "assignee", "service_request_reopened", "warning", 0},
		{"service_request.reopened", "property_domain_supervisor:tenant_relation", "service_request_reopened", "warning", 0},
		{"service_request.message", "tenant_user", "ticket_message", "info", 0},
		{"service_request.message", "assignee", "service_request_message", "info", 0},
		{"service_request.message", "property_domain_supervisor:tenant_relation", "service_request_message", "info", 0},
		{"service_request.feedback", "property_domain_supervisor:tenant_relation", "service_request_feedback", "info", 0},
		{"tenant_user.registered", "property_domain_supervisor:tenant_relation", "tenant_account_pending", "info", 0},
		{"tenant_user.approved", "tenant_user", "tenant_account_approved", "success", 0},
		{"tenant_user.rejected", "tenant_user", "tenant_account_rejected", "warning", 0},
		{"tenant_user.suspended", "tenant_user", "tenant_account_suspended", "warning", 0},
		{"announcement.published", "tenant_property_users", "announcement", "info", 0},
		// Facility Booking (PRD §21) & Visitor (PRD §22)
		{"booking.created", "property_domain_supervisor:tenant_relation", "booking_received", "info", 0},
		{"booking.confirmed", "tenant_user", "booking_confirmed", "success", 0},
		{"booking.rejected", "tenant_user", "booking_rejected", "warning", 0},
		{"booking.cancelled", "tenant_user", "booking_cancelled", "info", 0},
		{"booking.cancelled", "property_domain_supervisor:tenant_relation", "booking_cancelled", "info", 0},
		{"visitor.registered", "property_domain_supervisor:security", "visitor_registered", "info", 0},
		{"visitor.approved", "tenant_user", "visitor_approved", "success", 0},
		{"visitor.denied", "tenant_user", "visitor_denied", "warning", 0},
		{"visitor.checked_in", "tenant_user", "visitor_arrived", "info", 0},
		{"visitor.checked_out", "tenant_user", "visitor_left", "info", 0},
		// Billing & Payment (PRD §23)
		{"invoice.issued", "tenant_user", "invoice_issued", "info", 0},
		{"invoice.due_soon", "tenant_user", "invoice_due_soon", "warning", 720},
		{"invoice.overdue", "tenant_user", "invoice_overdue", "critical", 1440},
		{"invoice.paid", "tenant_user", "invoice_paid", "success", 0},
		{"payment.paid", "tenant_user", "payment_received", "success", 0},
		{"payment.paid", "property_domain_supervisor:finance", "payment_received", "success", 0},
		{"payment.failed", "tenant_user", "payment_failed", "warning", 0},
		{"payment.initiated", "property_domain_supervisor:finance", "payment_pending", "info", 0},
		// Vendor & Inventory (PRD §24–§25)
		{"work_order.vendor_assigned", "assignee_team_supervisor", "work_order_vendor_assigned", "info", 0},
		// PRD P2 v2.1 P2-CNS-03: domain dari payload (consumable → housekeeping, spare part/tool → engineering)
		{"inventory.low_stock", "property_domain_supervisor", "inventory_low_stock", "warning", 720},
		// Hotel Booking (PRD §3.9)
		{"hotel_reservation.created", "property_domain_supervisor:tenant_relation", "reservation_received", "info", 0},
		{"hotel_reservation.checked_out", "property_domain_supervisor:housekeeping", "room_turnover", "info", 0},
		{"hotel_room.status_changed", "property_domain_supervisor", "room_status_changed", "warning", 0}, // domain dari payload (housekeeping: dirty / engineering: out_of_order)
		// Apartment Unit Sales & Rental (PRD §3.10)
		{"unit_sales_lead.created", "assignee", "sales_lead_assigned", "info", 0},
		{"unit_sales_lead.created", "property_domain_supervisor:management", "sales_inquiry_received", "info", 0},
		{"unit_sale_reservation.created", "property_domain_supervisor:management", "unit_reserved", "info", 0},
		{"unit_sale_reservation.sold", "property_domain_supervisor:management", "unit_sold", "success", 0},
		{"unit_sale_reservation.handed_over", "property_domain_supervisor:tenant_relation", "unit_handed_over", "info", 0},
		{"unit_rental_reservation.created", "property_domain_supervisor:management", "rental_inquiry_received", "info", 0},
		{"unit_rental_reservation.confirmed", "property_domain_supervisor:management", "rental_reserved", "info", 0},
		{"unit_rental_reservation.confirmed", "property_domain_supervisor:finance", "rental_reserved", "info", 0},
		{"unit_rental_reservation.activated", "property_domain_supervisor:tenant_relation", "rental_activated", "info", 0},
		{"unit_rental_reservation.activated", "tenant_user", "rental_activated", "success", 0},
		{"unit_rental_reservation.completed", "property_domain_supervisor:housekeeping", "rental_completed", "info", 0},
		{"unit_rental_reservation.completed", "tenant_user", "rental_completed", "info", 0},
		{"unit_rental_reservation.cancelled", "property_domain_supervisor:management", "rental_cancelled", "warning", 0},
		// ---- PRD P3 v2.1 (Tenant Experience) ----
		// B-03: service_request.auto_closed tidak lagi punya rule tenant — event closed (actor_kind system) sudah memberi satu notifikasi
		{"recurring_issue.detected", "property_domain_supervisor:tenant_relation", "recurring_issue_detected", "warning", 0},
		{"recurring_issue.detected", "property_manager", "recurring_issue_detected", "warning", 0},
		{"tenant_feedback.submitted", "property_domain_supervisor:tenant_relation", "tenant_feedback_received", "info", 0},
		{"tenant_feedback.responded", "tenant_user", "tenant_feedback_responded", "info", 0},
		{"announcement.broadcast", "tenant_property_users", "announcement_alert", "warning", 0},
		{"package.received", "tenant_user", "package_received", "info", 0},
		{"package.reminder", "tenant_user", "package_reminder", "warning", 0},
		{"package.picked_up", "tenant_user", "package_picked_up", "success", 0},
		{"package.returned", "tenant_user", "package_returned", "warning", 0},
		{"parking_permit.requested", "property_domain_supervisor:security", "parking_permit_requested", "info", 0},
		{"parking_permit.approved", "tenant_user", "parking_permit_approved", "success", 0},
		{"parking_permit.rejected", "tenant_user", "parking_permit_rejected", "warning", 0},
		{"parking_permit.revoked", "tenant_user", "parking_permit_revoked", "warning", 0},
		{"parking_permit.expiring", "tenant_user", "parking_permit_expiring", "warning", 0},
		{"parking_permit.expired", "tenant_user", "parking_permit_expired", "info", 0},
		{"parking_violation.recorded", "tenant_user", "parking_violation", "warning", 0},
		// ---- PRD P4 v2.1 (Financial Operations) ----
		{"invoice.reminder", "tenant_user", "invoice_reminder", "warning", 0}, // P4-COL-02 pengingat bertahap H+n (H-n = invoice.due_soon)
		{"invoice.cancelled", "tenant_user", "invoice_cancelled", "info", 0},  // B-18
		{"payment.refunded", "tenant_user", "payment_refunded", "info", 0},
		{"credit_note.requested", "property_domain_supervisor:finance", "credit_note_requested", "info", 0},
		{"credit_note.approved", "tenant_user", "credit_note_approved", "success", 0},
		{"billing_run.generated", "property_domain_supervisor:finance", "billing_run_generated", "info", 0},
		{"meter_reading.flagged", "property_domain_supervisor:finance", "meter_reading_flagged", "warning", 0},
		{"collection_promise.broken", "property_domain_supervisor:finance", "collection_promise_broken", "warning", 0},
	}
	rules = append(rules, extra...)
	if _, err := q.Exec(ctx, `DELETE FROM notification_rules WHERE organization_id IS NULL`); err != nil {
		return err
	}
	for _, r := range rules {
		// channel email aktif default untuk kejadian kritis (SLA breach, overdue, incident kritis) — PRD P0 v2 §14.3
		channels := []string{"inapp", "push"}
		if r.severity == "critical" {
			channels = append(channels, "email")
		}
		if _, err := q.Exec(ctx, `INSERT INTO notification_rules (organization_id, event_type, recipient_resolver, notification_type, severity, dedup_minutes, channels) VALUES (NULL,$1,$2,$3,$4,$5,$6)`,
			r.event, r.resolver, r.ntype, r.severity, r.dedup, channels); err != nil {
			return err
		}
	}
	return nil
}

// SeedGlobal: katalog permission + notification rules (idempotent).
func SeedGlobal(ctx context.Context, d *db.DB) error {
	if err := iam.SeedPermissionCatalog(ctx, d.Pool); err != nil {
		return fmt.Errorf("seed permissions: %w", err)
	}
	if err := SeedNotificationRules(ctx, d.Pool); err != nil {
		return fmt.Errorf("seed notification rules: %w", err)
	}
	return nil
}

// SeedOrganization: role template, equipment, SLA default, SR categories, admin user. Mengembalikan admin user id.
func SeedOrganization(ctx context.Context, tx pgx.Tx, iamSvc *iam.Service, orgID uuid.UUID, adminEmail, adminPass string) (uuid.UUID, error) {
	if err := iamSvc.SeedSystemRoles(ctx, tx, orgID); err != nil {
		return uuid.Nil, err
	}
	if err := SeedEquipment(ctx, tx, orgID); err != nil {
		return uuid.Nil, err
	}
	if err := SeedSLADefaults(ctx, tx, orgID); err != nil {
		return uuid.Nil, err
	}
	if err := SeedSRCategories(ctx, tx, orgID); err != nil {
		return uuid.Nil, err
	}
	if err := SeedPaymentProviders(ctx, tx, orgID); err != nil {
		return uuid.Nil, err
	}
	return CreateUser(ctx, tx, orgID, adminEmail, "Organization Admin", adminPass, "organization_admin", nil)
}

// SeedPaymentProviders: provider default P1 (TD-P1-007) — manual aktif; mock_gateway nonaktif sampai secret diatur.
func SeedPaymentProviders(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO payment_providers (organization_id, code, name, is_active, methods, config) VALUES
		($1,'manual','Transfer / Tunai (verifikasi staf)',true,'{transfer,cash}','{"instructions":"Transfer ke rekening building management dan konfirmasi ke Tenant Relation."}'),
		($1,'mock_gateway','Mock Gateway (uji coba)',false,'{va,qris,ewallet}','{}')
		ON CONFLICT DO NOTHING`, orgID)
	return err
}

// SyncExistingOrganizations: upgrade path — role sistem & kategori default untuk organization yang sudah ada
// (katalog permission bertambah di P1). Idempotent; dipanggil `bvctl seed`.
func SyncExistingOrganizations(ctx context.Context, d *db.DB, iamSvc *iam.Service) (int, error) {
	ids, err := d.ListOrganizationIDs(ctx)
	if err != nil {
		return 0, err
	}
	for _, orgID := range ids {
		err := d.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
			if err := iamSvc.SeedSystemRoles(ctx, tx, orgID); err != nil {
				return err
			}
			if err := profile.SeedCategoriesTx(ctx, tx, orgID); err != nil {
				return err
			}
			return SeedPaymentProviders(ctx, tx, orgID)
		})
		if err != nil {
			return 0, fmt.Errorf("sync org %s: %w", orgID, err)
		}
	}
	return len(ids), nil
}

// SeedInternalOrganization: organization internal BuildingVision (organizations.is_internal = true) dengan satu user
// admin_internal untuk mengelola App Downloads (Website PRD §16–§19). Idempotent: organization/user yang sudah ada dilewati.
func SeedInternalOrganization(ctx context.Context, d *db.DB, iamSvc *iam.Service, email, password string) error {
	orgID, created, err := EnsureOrganization(ctx, d, "BuildingVision Internal", "buildingvision-internal")
	if err != nil {
		return err
	}
	if _, err := d.Pool.Exec(ctx, `UPDATE organizations SET is_internal = true, signup_source = 'seed' WHERE id = $1`, orgID); err != nil {
		return err
	}
	return d.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		if err := iamSvc.SeedSystemRoles(ctx, tx, orgID); err != nil { // termasuk admin_internal karena is_internal = true
			return err
		}
		var userID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM users WHERE organization_id = $1 AND lower(email) = lower($2) AND deleted_at IS NULL`, orgID, email).Scan(&userID)
		if err != nil && !db.IsNoRows(err) {
			return err
		}
		if db.IsNoRows(err) {
			if !created && email == "" {
				return nil
			}
			if userID, err = CreateUser(ctx, tx, orgID, email, "Admin Internal", password, "admin_internal", nil); err != nil {
				return err
			}
		}
		// PRD P0 v2 §8.2: Admin Internal juga Platform Admin (registry organization pelanggan)
		_, err = tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id, property_id)
			SELECT $1, r.id, NULL FROM roles r WHERE r.organization_id = $2 AND r.code = 'platform_admin' ON CONFLICT DO NOTHING`, userID, orgID)
		return err
	})
}
