package operations

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/events"
)

// ---------- Incident lengkap (PRD P2 v2.1 §6.3; Roadmap v2.1 §9 incident checklist) ----------
// People involved (P2-SIN-03), eskalasi manual & otomatis saat SLA breach (P2-SIN-05), investigasi (P2-SIN-06).
// Data orang terlibat bersifat pribadi: hanya security.incident_people.* (role Security/Management), setiap perubahan diaudit.

// IncidentInvestigation: hasil investigasi incident.
type IncidentInvestigation struct {
	Status                string     `json:"status"` // not_started | in_progress | completed
	InvestigatorUserID    *uuid.UUID `json:"investigator_user_id"`
	InvestigatorName      *string    `json:"investigator_name"`
	Findings              *string    `json:"findings"`
	RootCause             *string    `json:"root_cause"`
	CorrectiveAction      *string    `json:"corrective_action"`
	CorrectiveOwnerUserID *uuid.UUID `json:"corrective_owner_user_id"`
	CorrectiveOwnerName   *string    `json:"corrective_owner_name"`
	InvestigatedAt        *time.Time `json:"investigated_at"`
}

// loadIncidentExtTx melengkapi Incident dengan eskalasi, investigasi, dan jumlah orang terlibat.
func (s *Service) loadIncidentExtTx(ctx context.Context, tx pgx.Tx, i *Incident) {
	var inv IncidentInvestigation
	_ = tx.QueryRow(ctx, `SELECT i.escalated_at, i.escalation_level, i.investigation_status, i.investigator_user_id, iu.full_name, i.investigation_findings, i.root_cause,
		i.corrective_action, i.corrective_owner_user_id, cu.full_name, i.investigated_at, (SELECT count(*) FROM incident_people ip WHERE ip.incident_id = i.id)
		FROM incidents i LEFT JOIN users iu ON iu.id = i.investigator_user_id LEFT JOIN users cu ON cu.id = i.corrective_owner_user_id WHERE i.id = $1`, i.ID).
		Scan(&i.EscalatedAt, &i.EscalationLevel, &inv.Status, &inv.InvestigatorUserID, &inv.InvestigatorName, &inv.Findings, &inv.RootCause,
			&inv.CorrectiveAction, &inv.CorrectiveOwnerUserID, &inv.CorrectiveOwnerName, &inv.InvestigatedAt, &i.PeopleCount)
	i.Investigation = &inv
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE object_type = 'incident' AND object_id = $1 AND attachment_type = 'video' AND deleted_at IS NULL`, i.ID).Scan(&i.VideoCount)
	if i.EscalationLevel > 0 && !isDoneStatus(string(i.Status)) {
		i.Flags = append(i.Flags, "escalated")
	}
	p := authctx.Must(ctx)
	// scope Building/Tower (P2-NFR-05): izin dievaluasi pada lokasi incident
	can := func(perm string) bool { return canAt(ctx, tx, perm, i.PropertyID, i.Location.ID) }
	if !workflow.Incident.IsTerminal(i.Status) && (can("operations.incidents.escalate") || can("operations.incidents.manage")) {
		i.AllowedActions = append(i.AllowedActions, "escalate")
	}
	if can("operations.incidents.update") {
		i.AllowedActions = append(i.AllowedActions, "investigate")
	}
	if can("security.incident_people.view") {
		i.AllowedActions = append(i.AllowedActions, "view_people")
	}
	if can("security.incident_people.manage") {
		i.AllowedActions = append(i.AllowedActions, "manage_people")
	}
	_ = p
}

// IncidentInvestigationInput (PATCH /incidents/{id} — field investigasi, P2-SIN-06).
type IncidentInvestigationInput struct {
	InvestigationStatus   *string    `json:"investigation_status"`
	InvestigatorUserID    *uuid.UUID `json:"investigator_user_id"`
	InvestigationFindings *string    `json:"investigation_findings"`
	RootCause             *string    `json:"root_cause"`
	CorrectiveAction      *string    `json:"corrective_action"`
	CorrectiveOwnerUserID *uuid.UUID `json:"corrective_owner_user_id"`
}

func (in IncidentInvestigationInput) empty() bool {
	return in.InvestigationStatus == nil && in.InvestigatorUserID == nil && in.InvestigationFindings == nil && in.RootCause == nil && in.CorrectiveAction == nil && in.CorrectiveOwnerUserID == nil
}

var investigationStatuses = map[string]bool{"not_started": true, "in_progress": true, "completed": true}

func (s *Service) updateInvestigationTx(ctx context.Context, tx pgx.Tx, before *Incident, in IncidentInvestigationInput) error {
	if in.empty() {
		return nil
	}
	p := authctx.Must(ctx)
	if in.InvestigationStatus != nil && !investigationStatuses[*in.InvestigationStatus] {
		return apperr.Validation("investigation_status harus not_started|in_progress|completed").WithField("investigation_status", "tidak valid")
	}
	// UUID nol = kosongkan (field opsional dapat dihapus lewat PATCH)
	for _, ref := range []*uuid.UUID{in.InvestigatorUserID, in.CorrectiveOwnerUserID} {
		if ref != nil && *ref != uuid.Nil {
			if err := s.validateRefs(ctx, tx, before.PropertyID, nil, nil, ref, nil, nil); err != nil {
				return err
			}
		}
	}
	// nilai efektif setelah PATCH: input (string kosong = dikosongkan) atau nilai tersimpan
	var rc, ca, status *string
	_ = tx.QueryRow(ctx, `SELECT root_cause, corrective_action, investigation_status FROM incidents WHERE id = $1`, before.ID).Scan(&rc, &ca, &status)
	effective := func(in, cur *string) string {
		if in != nil {
			return strings.TrimSpace(*in)
		}
		return strings.TrimSpace(derefStr(cur))
	}
	finalStatus := derefStr(status)
	if in.InvestigationStatus != nil {
		finalStatus = *in.InvestigationStatus
	}
	if finalStatus == "completed" {
		// investigasi selesai wajib punya akar masalah & tindakan korektif
		if effective(in.RootCause, rc) == "" {
			return apperr.Validation("root_cause wajib sebelum investigasi selesai").WithField("root_cause", "wajib")
		}
		if effective(in.CorrectiveAction, ca) == "" {
			return apperr.Validation("corrective_action wajib sebelum investigasi selesai").WithField("corrective_action", "wajib")
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE incidents SET investigation_status = COALESCE($2, investigation_status),
		investigator_user_id = CASE WHEN $3::uuid IS NULL THEN investigator_user_id WHEN $3::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL ELSE $3::uuid END,
		investigation_findings = CASE WHEN $4::text IS NULL THEN investigation_findings ELSE NULLIF(btrim($4::text), '') END,
		root_cause = CASE WHEN $5::text IS NULL THEN root_cause ELSE NULLIF(btrim($5::text), '') END,
		corrective_action = CASE WHEN $6::text IS NULL THEN corrective_action ELSE NULLIF(btrim($6::text), '') END,
		corrective_owner_user_id = CASE WHEN $7::uuid IS NULL THEN corrective_owner_user_id WHEN $7::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL ELSE $7::uuid END,
		investigated_at = CASE WHEN $2 = 'completed' THEN now() ELSE investigated_at END WHERE id = $1`,
		before.ID, in.InvestigationStatus, in.InvestigatorUserID, in.InvestigationFindings, in.RootCause, in.CorrectiveAction, in.CorrectiveOwnerUserID); err != nil {
		return err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: before.ID, Action: "investigation_updated", Payload: map[string]any{"status": in.InvestigationStatus, "root_cause": in.RootCause, "corrective_action": in.CorrectiveAction}})
	_ = p
	return nil
}

// ---------- Escalation ----------

type IncidentEscalateInput struct {
	Reason         string     `json:"reason"`
	EscalateToUser *uuid.UUID `json:"escalate_to_user_id"`
	RaiseSeverity  bool       `json:"raise_severity"` // naikkan severity & priority satu tingkat
}

var nextLevel = map[string]string{"low": "medium", "medium": "high", "high": "critical", "critical": "critical"}

// EscalateIncident (P2-SIN-05): level +1, alasan wajib, opsional naikkan severity; event incident.escalated →
// Security Supervisor, manager property, dan tujuan eskalasi.
func (s *Service) EscalateIncident(ctx context.Context, id uuid.UUID, in IncidentEscalateInput) (*Incident, error) {
	p := authctx.Must(ctx)
	if strings.TrimSpace(in.Reason) == "" {
		return nil, apperr.Validation("reason wajib").WithField("reason", "wajib")
	}
	var out *Incident
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inc, err := s.getIncidentTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !canAt(ctx, tx, "operations.incidents.escalate", inc.PropertyID, inc.Location.ID) && !canAt(ctx, tx, "operations.incidents.manage", inc.PropertyID, inc.Location.ID) {
			return apperr.Forbidden("Memerlukan operations.incidents.escalate")
		}
		if workflow.Incident.IsTerminal(inc.Status) || inc.Status == workflow.Resolved {
			return apperr.Conflict("OBJECT_TERMINAL", "Incident "+inc.IncidentNumber+" sudah "+workflow.Label(inc.Status))
		}
		if err := s.validateRefs(ctx, tx, inc.PropertyID, nil, nil, in.EscalateToUser, nil, nil); err != nil {
			return err
		}
		sev, prio := inc.Severity, inc.Priority
		if in.RaiseSeverity {
			sev, prio = nextLevel[inc.Severity], nextLevel[inc.Priority]
		}
		if _, err := tx.Exec(ctx, `UPDATE incidents SET escalation_level = escalation_level + 1, escalated_at = now(), severity = $2, priority = $3, updated_by = $4 WHERE id = $1`, id, sev, prio, p.UserID); err != nil {
			return err
		}
		if prio != inc.Priority {
			_ = s.applySLATx(ctx, tx, ObjIncident, id, inc.PropertyID, prio, inc.CreatedAt)
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: id, Action: audit.ActEscalated, From: inc.Severity, To: sev, Payload: map[string]any{"reason": in.Reason, "level": inc.EscalationLevel + 1, "escalate_to_user_id": in.EscalateToUser}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "escalate", EntityType: ObjIncident, EntityID: &id, EntityLabel: inc.IncidentNumber, Before: map[string]any{"severity": inc.Severity, "level": inc.EscalationLevel}, After: map[string]any{"severity": sev, "reason": in.Reason}})
		if s.Jobs != nil {
			payload := map[string]any{"reason": in.Reason, "level": inc.EscalationLevel + 1, "domain": "security", "assignee_user_id": inc.Assignee.UserID, "assignee_team_id": inc.Assignee.TeamID}
			if in.EscalateToUser != nil {
				payload["escalate_to_user_id"] = *in.EscalateToUser
			}
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.IncidentEscalated, OrganizationID: p.OrganizationID, PropertyID: &inc.PropertyID, ObjectType: ObjIncident, ObjectID: id, ObjectLabel: inc.IncidentNumber, ActorUserID: actorOrNil(p), Payload: payload})
			if sev == "critical" && inc.Severity != "critical" {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.IncidentCritical, OrganizationID: p.OrganizationID, PropertyID: &inc.PropertyID, ObjectType: ObjIncident, ObjectID: id, ObjectLabel: inc.IncidentNumber, ActorUserID: actorOrNil(p),
					Payload: map[string]any{"severity": "critical", "from": inc.Severity, "domain": "security"}})
			}
			_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, ObjIncident, id))
		}
		out, err = s.getIncidentTx(ctx, tx, id)
		return err
	})
	return out, err
}

// ---------- People involved ----------

var incidentPersonRoles = map[string]bool{"reporter": true, "victim": true, "witness": true, "suspect": true, "other": true}

type IncidentPerson struct {
	ID             uuid.UUID  `json:"id"`
	IncidentID     uuid.UUID  `json:"incident_id"`
	PersonRole     string     `json:"person_role"` // reporter | victim | witness | suspect | other
	Name           string     `json:"name"`
	Contact        *string    `json:"contact"`
	IdentityNumber *string    `json:"identity_number"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	TenantName     *string    `json:"tenant_name"`
	Notes          *string    `json:"notes"`
	CreatedAt      time.Time  `json:"created_at"`
	Version        int        `json:"version"`
}

type IncidentPersonInput struct {
	PersonRole     *string    `json:"person_role"`
	Name           *string    `json:"name"`
	Contact        *string    `json:"contact"`
	IdentityNumber *string    `json:"identity_number"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	Notes          *string    `json:"notes"`
}

func (s *Service) peopleTx(ctx context.Context, tx pgx.Tx, incidentID uuid.UUID) ([]IncidentPerson, error) {
	rows, err := tx.Query(ctx, `SELECT ip.id, ip.incident_id, ip.person_role, ip.name, ip.contact, ip.identity_number, ip.tenant_id, t.name, ip.notes, ip.created_at, ip.version
		FROM incident_people ip LEFT JOIN tenants t ON t.id = ip.tenant_id WHERE ip.incident_id = $1 ORDER BY ip.created_at`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IncidentPerson{}
	for rows.Next() {
		var x IncidentPerson
		if err := rows.Scan(&x.ID, &x.IncidentID, &x.PersonRole, &x.Name, &x.Contact, &x.IdentityNumber, &x.TenantID, &x.TenantName, &x.Notes, &x.CreatedAt, &x.Version); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) incidentPropertyTx(ctx context.Context, tx pgx.Tx, incidentID uuid.UUID) (uuid.UUID, string, string, error) {
	var pid uuid.UUID
	var number, status string
	if err := tx.QueryRow(ctx, `SELECT property_id, incident_number, status FROM incidents WHERE id = $1`, incidentID).Scan(&pid, &number, &status); err != nil {
		return uuid.Nil, "", "", apperr.NotFound("Incident")
	}
	return pid, number, status, nil
}

// canIncident: permission pada lokasi incident (scope Building/Tower, P2-NFR-05).
func canIncident(ctx context.Context, tx pgx.Tx, perm string, incidentID uuid.UUID) bool {
	var pid uuid.UUID
	var loc *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT property_id, location_id FROM incidents WHERE id = $1`, incidentID).Scan(&pid, &loc); err != nil {
		return false
	}
	return canAt(ctx, tx, perm, pid, loc)
}

func (s *Service) ListIncidentPeople(ctx context.Context, incidentID uuid.UUID) ([]IncidentPerson, error) {
	var out []IncidentPerson
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, number, _, err := s.incidentPropertyTx(ctx, tx, incidentID)
		if err != nil {
			return err
		}
		if !canIncident(ctx, tx, "security.incident_people.view", incidentID) {
			return apperr.Forbidden("Data orang terlibat hanya untuk role Security/Management")
		}
		out, err = s.peopleTx(ctx, tx, incidentID)
		if err == nil && len(out) > 0 {
			// akses data pribadi tercatat (P2-NFR-04)
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "view_people", EntityType: ObjIncident, EntityID: &incidentID, EntityLabel: number, After: map[string]any{"count": len(out)}})
		}
		return err
	})
	return out, err
}

func (s *Service) SaveIncidentPerson(ctx context.Context, incidentID uuid.UUID, personID *uuid.UUID, in IncidentPersonInput) (*IncidentPerson, error) {
	p := authctx.Must(ctx)
	var out *IncidentPerson
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if personID != nil {
			if err := tx.QueryRow(ctx, `SELECT incident_id FROM incident_people WHERE id = $1`, *personID).Scan(&incidentID); err != nil {
				return apperr.NotFound("Incident person")
			}
		}
		_, number, status, err := s.incidentPropertyTx(ctx, tx, incidentID)
		if err != nil {
			return err
		}
		if !canIncident(ctx, tx, "security.incident_people.manage", incidentID) {
			return apperr.Forbidden("Memerlukan security.incident_people.manage")
		}
		if status == "cancelled" {
			return apperr.Conflict("OBJECT_TERMINAL", "Incident sudah dibatalkan")
		}
		if in.PersonRole != nil && !incidentPersonRoles[*in.PersonRole] {
			return apperr.Validation("person_role harus reporter|victim|witness|suspect|other").WithField("person_role", "tidak valid")
		}
		if in.TenantID != nil {
			var ok bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1)`, *in.TenantID).Scan(&ok)
			if !ok {
				return apperr.Validation("tenant_id tidak ditemukan").WithField("tenant_id", "tidak valid")
			}
		}
		var id uuid.UUID
		if personID == nil {
			if in.Name == nil || strings.TrimSpace(*in.Name) == "" || in.PersonRole == nil {
				return apperr.Validation("person_role dan name wajib")
			}
			if err := tx.QueryRow(ctx, `INSERT INTO incident_people (organization_id, incident_id, person_role, name, contact, identity_number, tenant_id, notes, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9) RETURNING id`,
				p.OrganizationID, incidentID, *in.PersonRole, strings.TrimSpace(*in.Name), in.Contact, in.IdentityNumber, in.TenantID, in.Notes, p.UserID).Scan(&id); err != nil {
				return err
			}
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: incidentID, Action: "person_added", Payload: map[string]any{"person_role": *in.PersonRole}})
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "incident_person", EntityID: &id, EntityLabel: number + " · " + *in.PersonRole})
		} else {
			id = *personID
			if _, err := tx.Exec(ctx, `UPDATE incident_people SET person_role = COALESCE($2, person_role), name = COALESCE(NULLIF(TRIM($3),''), name), contact = COALESCE($4, contact),
				identity_number = COALESCE($5, identity_number), tenant_id = COALESCE($6, tenant_id), notes = COALESCE($7, notes), updated_by = $8 WHERE id = $1`,
				id, in.PersonRole, derefStr(in.Name), in.Contact, in.IdentityNumber, in.TenantID, in.Notes, p.UserID); err != nil {
				return err
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "incident_person", EntityID: &id, EntityLabel: number})
		}
		people, err := s.peopleTx(ctx, tx, incidentID)
		for i := range people {
			if people[i].ID == id {
				out = &people[i]
			}
		}
		return err
	})
	return out, err
}

func (s *Service) DeleteIncidentPerson(ctx context.Context, personID uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var incidentID uuid.UUID
		var role string
		if err := tx.QueryRow(ctx, `SELECT incident_id, person_role FROM incident_people WHERE id = $1`, personID).Scan(&incidentID, &role); err != nil {
			return apperr.NotFound("Incident person")
		}
		_, number, _, err := s.incidentPropertyTx(ctx, tx, incidentID)
		if err != nil {
			return err
		}
		if !canIncident(ctx, tx, "security.incident_people.manage", incidentID) {
			return apperr.Forbidden("Memerlukan security.incident_people.manage")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM incident_people WHERE id = $1`, personID); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: incidentID, Action: "person_removed", Payload: map[string]any{"person_role": role}})
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "incident_person", EntityID: &personID, EntityLabel: number})
	})
}
