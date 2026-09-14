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
	return nil
}

// seedNotificationRules: tabel PRD §17.1 (+ sync.conflict OD-004) sebagai rule global.
func SeedNotificationRules(ctx context.Context, q db.Querier) error {
	type rule struct {
		event, resolver, ntype, severity string
		dedup                            int
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
		{"finding.created", "property_domain_supervisor", "finding_created", "warning", 0},
		{"service_request.created", "property_domain_supervisor", "service_request_received", "info", 0},
		{"service_request.assigned", "assignee", "service_request_assigned", "info", 0},
		{"service_request.sla_risk", "assignee", "service_request_sla_risk", "warning", 30},
		{"service_request.sla_risk", "assignee_team_supervisor", "service_request_sla_risk", "warning", 30},
		{"work_order.completed", "assignee_team_supervisor", "work_order_completed", "success", 0},
		{"work_order.completed", "requester", "work_order_completed", "success", 0},
		{"work_order.closed", "requester", "work_order_closed", "success", 0},
		{"task.completed", "assignee_team_supervisor", "task_completed", "success", 0},
		{"incident.created", "property_domain_supervisor:security", "incident_reported", "critical", 0},
		{"incident.assigned", "assignee", "incident_assigned", "warning", 0},
		{"maintenance_schedule.due", "property_domain_supervisor:engineering", "maintenance_due", "warning", 720},
		{"sync.conflict", "assignee_team_supervisor", "sync_conflict", "warning", 0},
		{"export.ready", "requester", "export_ready", "info", 0},
	}
	if _, err := q.Exec(ctx, `DELETE FROM notification_rules WHERE organization_id IS NULL`); err != nil {
		return err
	}
	for _, r := range rules {
		if _, err := q.Exec(ctx, `INSERT INTO notification_rules (organization_id, event_type, recipient_resolver, notification_type, severity, dedup_minutes) VALUES (NULL,$1,$2,$3,$4,$5)`,
			r.event, r.resolver, r.ntype, r.severity, r.dedup); err != nil {
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
	return CreateUser(ctx, tx, orgID, adminEmail, "Organization Admin", adminPass, "organization_admin", nil)
}
