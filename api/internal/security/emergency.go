package security

import (
	"context"
	"fmt"
	"sort"
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
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Emergency (PRD P2 v2.1 §6.4; Roadmap v2.1 §9; NC §16) ----------
//
// Panic Button (aksi) → Emergency Alert (entitas) → Security Response (acknowledge → responding) → Incident (otomatis).
// Status: raised → acknowledged → responding → resolved | cancelled (alarm palsu). Tidak di-acknowledge dalam
// EmergencyAckTimeout → eskalasi bertingkat (maks 3). Raise idempoten lewat id dari klien (antrean offline).

var EmergencyTypes = map[string]string{
	"fire": "Kebakaran", "medical": "Medis", "security_threat": "Ancaman keamanan", "intrusion": "Penyusupan",
	"natural_disaster": "Bencana alam", "evacuation": "Evakuasi", "utility_failure": "Gangguan utilitas", "other": "Lainnya",
}

// emergencyIncidentType: tipe incident (security|safety|building) dari tipe emergency.
func emergencyIncidentType(t string) string {
	switch t {
	case "security_threat", "intrusion":
		return "security"
	case "utility_failure":
		return "building"
	}
	return "safety"
}

// EmergencyTypeLabelSQL: label jenis emergency sebagai ekspresi SQL (judul Attention Required, snapshot serah terima).
func EmergencyTypeLabelSQL(col string) string {
	keys := make([]string, 0, len(EmergencyTypes))
	for k := range EmergencyTypes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b := strings.Builder{}
	b.WriteString("CASE " + col)
	for _, k := range keys {
		b.WriteString(" WHEN '" + k + "' THEN '" + strings.ReplaceAll(EmergencyTypes[k], "'", "''") + "'")
	}
	b.WriteString(" ELSE " + col + " END")
	return b.String()
}

var EmergencyContactTypes = map[string]bool{"fire": true, "ambulance": true, "police": true, "electricity": true, "water": true, "internal": true, "other": true}

type EmergencyEvent struct {
	ID         uuid.UUID      `json:"id"`
	EventType  string         `json:"event_type"` // raised | acknowledged | responding | action | escalated | incident_created | resolved | cancelled | note
	Note       *string        `json:"note"`
	ActorID    *uuid.UUID     `json:"actor_user_id"`
	ActorName  *string        `json:"actor_name"`
	Payload    map[string]any `json:"payload"`
	OccurredAt time.Time      `json:"occurred_at"`
}

type EmergencyContact struct {
	ID          uuid.UUID `json:"id"`
	PropertyID  uuid.UUID `json:"property_id"`
	Name        string    `json:"name"`
	ContactType string    `json:"contact_type"`
	Phone       string    `json:"phone"`
	Notes       *string   `json:"notes"`
	SortOrder   int       `json:"sort_order"`
	IsActive    bool      `json:"is_active"`
	Version     int       `json:"version"`
}

type EmergencyAlert struct {
	ID                 uuid.UUID              `json:"id"`
	PropertyID         uuid.UUID              `json:"property_id"`
	AlertNumber        string                 `json:"alert_number"`
	EmergencyType      string                 `json:"emergency_type"`
	EmergencyTypeLabel string                 `json:"emergency_type_label"`
	Status             string                 `json:"status"`
	Location           operations.LocationRef `json:"location"`
	Description        *string                `json:"description"`
	GPSLat             *float64               `json:"gps_lat"`
	GPSLng             *float64               `json:"gps_lng"`
	GPSStatus          *string                `json:"gps_status"`
	Channel            string                 `json:"channel"`
	RaisedBy           *uuid.UUID             `json:"raised_by"`
	RaisedByName       *string                `json:"raised_by_name"`
	RaisedAt           time.Time              `json:"raised_at"`
	ClientRaisedAt     *time.Time             `json:"client_raised_at"`
	AcknowledgedBy     *uuid.UUID             `json:"acknowledged_by"`
	AcknowledgedByName *string                `json:"acknowledged_by_name"`
	AcknowledgedAt     *time.Time             `json:"acknowledged_at"`
	ResponderUserID    *uuid.UUID             `json:"responder_user_id"`
	ResponderName      *string                `json:"responder_name"`
	RespondingAt       *time.Time             `json:"responding_at"`
	ResolvedBy         *uuid.UUID             `json:"resolved_by"`
	ResolvedAt         *time.Time             `json:"resolved_at"`
	Resolution         *string                `json:"resolution"`
	CancelledAt        *time.Time             `json:"cancelled_at"`
	CancelReason       *string                `json:"cancel_reason"`
	EscalationLevel    int                    `json:"escalation_level"`
	EscalatedAt        *time.Time             `json:"escalated_at"`
	IncidentID         *uuid.UUID             `json:"incident_id"`
	IncidentNumber     *string                `json:"incident_number"`
	// AckSeconds: waktu respons (raised → acknowledged) untuk KPI Security Response Time
	AckSeconds     *int               `json:"ack_seconds"`
	Active         bool               `json:"active"`
	AllowedActions []string           `json:"allowed_actions"`
	Timeline       []EmergencyEvent   `json:"timeline,omitempty"`
	Contacts       []EmergencyContact `json:"contacts,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	Version        int                `json:"version"`
}

const emergencySelect = `SELECT e.id, e.property_id, e.alert_number, e.emergency_type, e.status, e.location_id, l.name, e.description, e.gps_lat::float8, e.gps_lng::float8, e.gps_status, e.channel,
	e.raised_by, ru.full_name, e.raised_at, e.client_raised_at, e.acknowledged_by, au.full_name, e.acknowledged_at, e.responder_user_id, rsp.full_name, e.responding_at,
	e.resolved_by, e.resolved_at, e.resolution, e.cancelled_at, e.cancel_reason, e.escalation_level, e.escalated_at, e.incident_id, i.incident_number, e.created_at, e.version
	FROM emergency_alerts e
	LEFT JOIN locations l ON l.id = e.location_id
	LEFT JOIN users ru ON ru.id = e.raised_by
	LEFT JOIN users au ON au.id = e.acknowledged_by
	LEFT JOIN users rsp ON rsp.id = e.responder_user_id
	LEFT JOIN incidents i ON i.id = e.incident_id`

func scanEmergency(row pgx.Row) (*EmergencyAlert, error) {
	var a EmergencyAlert
	if err := row.Scan(&a.ID, &a.PropertyID, &a.AlertNumber, &a.EmergencyType, &a.Status, &a.Location.ID, &a.Location.Name, &a.Description, &a.GPSLat, &a.GPSLng, &a.GPSStatus, &a.Channel,
		&a.RaisedBy, &a.RaisedByName, &a.RaisedAt, &a.ClientRaisedAt, &a.AcknowledgedBy, &a.AcknowledgedByName, &a.AcknowledgedAt, &a.ResponderUserID, &a.ResponderName, &a.RespondingAt,
		&a.ResolvedBy, &a.ResolvedAt, &a.Resolution, &a.CancelledAt, &a.CancelReason, &a.EscalationLevel, &a.EscalatedAt, &a.IncidentID, &a.IncidentNumber, &a.CreatedAt, &a.Version); err != nil {
		return nil, err
	}
	a.EmergencyTypeLabel = EmergencyTypes[a.EmergencyType]
	a.Active = a.Status == "raised" || a.Status == "acknowledged" || a.Status == "responding"
	if a.AcknowledgedAt != nil {
		sec := int(a.AcknowledgedAt.Sub(a.RaisedAt).Seconds())
		a.AckSeconds = &sec
	}
	return &a, nil
}

func (s *Service) getEmergencyTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*EmergencyAlert, error) {
	a, err := scanEmergency(tx.QueryRow(ctx, emergencySelect+` WHERE e.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Emergency Alert")
		}
		return nil, err
	}
	if a.Location.ID != nil {
		pt := property.LocationPathText(ctx, tx, *a.Location.ID)
		a.Location.PathText = &pt
	}
	a.AllowedActions = emergencyActions(authctx.Must(ctx), a)
	return a, nil
}

// canViewEmergency: izin view di property atau pelapor alert itu sendiri.
func canViewEmergency(p *authctx.Principal, a *EmergencyAlert) bool {
	return p.HasAnyOnProperty("security.emergency_alerts.view", a.PropertyID) || (a.RaisedBy != nil && *a.RaisedBy == p.UserID)
}

func emergencyActions(p *authctx.Principal, a *EmergencyAlert) []string {
	out := []string{"view"}
	respond := p.HasAnyOnProperty("security.emergency_alerts.respond", a.PropertyID)
	resolve := p.HasAnyOnProperty("security.emergency_alerts.resolve", a.PropertyID)
	switch a.Status {
	case "raised":
		if respond {
			out = append(out, "acknowledge", "respond", "note")
		}
	case "acknowledged":
		if respond {
			out = append(out, "respond", "note")
		}
	case "responding":
		if respond {
			out = append(out, "note")
		}
	}
	if a.Active && resolve {
		out = append(out, "resolve")
		if a.Status != "responding" {
			out = append(out, "cancel")
		}
	}
	return out
}

func (s *Service) emergencyTimelineTx(ctx context.Context, tx pgx.Tx, alertID uuid.UUID) ([]EmergencyEvent, error) {
	rows, err := tx.Query(ctx, `SELECT ev.id, ev.event_type, ev.note, ev.actor_user_id, u.full_name, ev.payload, ev.occurred_at FROM emergency_alert_events ev LEFT JOIN users u ON u.id = ev.actor_user_id WHERE ev.alert_id = $1 ORDER BY ev.occurred_at, ev.id`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmergencyEvent{}
	for rows.Next() {
		var e EmergencyEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.Note, &e.ActorID, &e.ActorName, &e.Payload, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func addEmergencyEvent(ctx context.Context, tx pgx.Tx, alertID uuid.UUID, eventType string, note *string, payload map[string]any) error {
	p := authctx.Must(ctx)
	if payload == nil {
		payload = map[string]any{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO emergency_alert_events (organization_id, alert_id, event_type, note, actor_user_id, payload) VALUES ($1,$2,$3,$4,$5,$6)`,
		p.OrganizationID, alertID, eventType, note, actorOrNil(p), payload)
	return err
}

func (s *Service) emitEmergency(ctx context.Context, tx pgx.Tx, evType string, a *EmergencyAlert, extra map[string]any) {
	if s.Jobs == nil {
		return
	}
	p := authctx.Must(ctx)
	payload := map[string]any{"domain": "security", "emergency_type": a.EmergencyType, "status": a.Status, "escalation_level": a.EscalationLevel}
	if a.RaisedBy != nil {
		payload["reporter_user_id"] = *a.RaisedBy
	}
	if a.Location.PathText != nil {
		payload["location_path"] = *a.Location.PathText
	}
	for k, v := range extra {
		payload[k] = v
	}
	_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: evType, OrganizationID: p.OrganizationID, PropertyID: &a.PropertyID, ObjectType: "emergency_alert", ObjectID: a.ID,
		ObjectLabel: a.AlertNumber + " · " + a.EmergencyTypeLabel, ActorUserID: actorOrNil(p), Payload: payload})
}

// RaiseEmergencyInput: Panic Button (Staff App) atau lapor emergency dari Web. ID opsional dari klien = kunci idempoten
// (antrean offline, P2-EMG-08).
type RaiseEmergencyInput struct {
	ID             *uuid.UUID `json:"id"`
	PropertyID     *uuid.UUID `json:"property_id"`
	EmergencyType  string     `json:"emergency_type"`
	LocationID     *uuid.UUID `json:"location_id"`
	Description    *string    `json:"description"`
	GPSLat         *float64   `json:"gps_lat"`
	GPSLng         *float64   `json:"gps_lng"`
	GPSStatus      string     `json:"gps_status"`
	Channel        string     `json:"channel"` // panic_button | mobile | web
	ClientRaisedAt *time.Time `json:"client_raised_at"`
	FromSync       bool       `json:"-"`
}

func (s *Service) RaiseEmergency(ctx context.Context, in RaiseEmergencyInput) (*EmergencyAlert, error) {
	var out *EmergencyAlert
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.RaiseEmergencyTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.getEmergencyTx(ctx, tx, id)
		if err == nil {
			out.Contacts, err = s.contactsTx(ctx, tx, out.PropertyID, true)
		}
		return err
	})
	return out, err
}

// RaiseEmergencyTx: buat Emergency Alert + timeline + Incident otomatis + event (notifikasi segera ke security on-duty,
// supervisor, manager). Idempoten per id klien.
func (s *Service) RaiseEmergencyTx(ctx context.Context, tx pgx.Tx, in RaiseEmergencyInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if in.ID != nil {
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM emergency_alerts WHERE id = $1)`, *in.ID).Scan(&exists)
		if exists {
			return *in.ID, nil
		}
	}
	if in.EmergencyType == "" {
		in.EmergencyType = "other"
	}
	if _, ok := EmergencyTypes[in.EmergencyType]; !ok {
		return uuid.Nil, apperr.Validation("emergency_type tidak valid").WithField("emergency_type", "tidak valid")
	}
	switch in.Channel {
	case "":
		in.Channel = "web"
	case "panic_button", "mobile", "web":
	default:
		return uuid.Nil, apperr.Validation("channel harus panic_button|mobile|web").WithField("channel", "tidak valid")
	}
	if in.FromSync && in.Channel == "web" {
		in.Channel = "sync" // antrean offline tanpa kanal eksplisit; panic_button tetap panic_button
	}
	if in.GPSStatus == "" {
		in.GPSStatus = "unavailable"
		if in.GPSLat != nil && in.GPSLng != nil {
			in.GPSStatus = "captured"
		}
	}
	// property: eksplisit → dari lokasi → satu-satunya property user
	var propertyID uuid.UUID
	switch {
	case in.LocationID != nil:
		pid, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
		if err != nil {
			return uuid.Nil, apperr.Validation("location_id tidak ditemukan").WithField("location_id", "tidak valid")
		}
		if in.PropertyID != nil && *in.PropertyID != pid {
			return uuid.Nil, apperr.Validation("location_id tidak berada di property ini").WithField("location_id", "tidak valid")
		}
		propertyID = pid
	case in.PropertyID != nil:
		propertyID = *in.PropertyID
	default:
		pids, all := p.PropertyIDsFor("security.emergency_alerts.raise")
		if !all && len(pids) == 1 {
			propertyID = pids[0]
			break
		}
		// P2-EMG-08: Panic tidak boleh gagal hanya karena lokasi kosong — pakai property sesi on-duty, lalu shift berjalan,
		// lalu satu-satunya property organisasi
		if tx.QueryRow(ctx, `SELECT property_id FROM attendance_records WHERE user_id = $1 AND clock_out_at IS NULL ORDER BY clock_in_at DESC LIMIT 1`, p.UserID).Scan(&propertyID) == nil {
			break
		}
		if tx.QueryRow(ctx, `SELECT property_id FROM shift_assignments WHERE user_id = $1 AND status = 'scheduled' AND now() BETWEEN starts_at - interval '2 hours' AND ends_at ORDER BY starts_at LIMIT 1`, p.UserID).Scan(&propertyID) == nil {
			break
		}
		var n int
		var only *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT count(*), (array_agg(location_id))[1] FROM properties`).Scan(&n, &only)
		if n == 1 && only != nil {
			propertyID = *only
			break
		}
		return uuid.Nil, apperr.Validation("property_id wajib").WithField("property_id", "wajib")
	}
	if !p.HasAnyOnProperty("security.emergency_alerts.raise", propertyID) {
		return uuid.Nil, apperr.Forbidden("Tidak memiliki security.emergency_alerts.raise pada property ini")
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixEmergencyAlert, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if in.ID != nil {
		id = *in.ID
	}
	raisedAt := time.Now().UTC()
	if _, err := tx.Exec(ctx, `INSERT INTO emergency_alerts (id, organization_id, property_id, alert_number, emergency_type, location_id, description, gps_lat, gps_lng, gps_status, channel, raised_by, raised_at, client_raised_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$12)`,
		id, p.OrganizationID, propertyID, number, in.EmergencyType, in.LocationID, in.Description, in.GPSLat, in.GPSLng, in.GPSStatus, in.Channel, actorOrNil(p), raisedAt, in.ClientRaisedAt); err != nil {
		return uuid.Nil, err
	}
	_ = addEmergencyEvent(ctx, tx, id, "raised", in.Description, map[string]any{"channel": in.Channel, "gps_status": in.GPSStatus, "emergency_type": in.EmergencyType})
	// Incident otomatis (P2-EMG-05): kategori emergency, severity critical, pelapor = penekan Panic Button
	sys := *p
	sys.IsSystem = true
	title := "EMERGENCY — " + EmergencyTypes[in.EmergencyType]
	desc := fmt.Sprintf("Emergency Alert %s (%s).", number, in.Channel)
	if in.Description != nil && strings.TrimSpace(*in.Description) != "" {
		desc += " " + strings.TrimSpace(*in.Description)
	}
	st := "emergency_alert"
	incID, err := s.Ops.CreateIncidentTx(authctx.With(ctx, &sys), tx, operations.CreateIncidentInput{
		PropertyID: &propertyID, IncidentType: emergencyIncidentType(in.EmergencyType), Category: "emergency", Title: title, Description: &desc,
		LocationID: in.LocationID, Severity: "critical", Priority: "critical", OccurredAt: &raisedAt, SourceType: &st, SourceID: &id, SuppressCritical: true,
	})
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE emergency_alerts SET incident_id = $2 WHERE id = $1`, id, incID); err != nil {
		return uuid.Nil, err
	}
	var incNumber string
	_ = tx.QueryRow(ctx, `SELECT incident_number FROM incidents WHERE id = $1`, incID).Scan(&incNumber)
	_ = addEmergencyEvent(ctx, tx, id, "incident_created", nil, map[string]any{"incident_id": incID, "incident_number": incNumber})
	ctxAct := ctx
	if in.FromSync {
		ctxAct = authctx.With(ctx, sourceSync(p))
	}
	_ = audit.Record(ctxAct, tx, audit.Entry{ObjectType: "emergency_alert", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "emergency_type": in.EmergencyType, "channel": in.Channel, "incident_number": incNumber}, ClientRecordedAt: in.ClientRaisedAt})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "emergency_alert", EntityID: &id, EntityLabel: number, After: in})
	a, err := s.getEmergencyTx(ctx, tx, id)
	if err != nil {
		return uuid.Nil, err
	}
	s.emitEmergency(ctx, tx, events.EmergencyRaised, a, map[string]any{"incident_number": incNumber})
	return id, nil
}

// EmergencyActionInput: acknowledge / respond / note / resolve / cancel.
type EmergencyActionInput struct {
	Note       *string `json:"note"`
	Resolution string  `json:"resolution"`
	Reason     string  `json:"reason"`
}

// ActEmergency: transisi status + timeline + sinkron incident + event.
func (s *Service) ActEmergency(ctx context.Context, id uuid.UUID, action string, in EmergencyActionInput) (*EmergencyAlert, error) {
	p := authctx.Must(ctx)
	var out *EmergencyAlert
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM emergency_alerts WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		a, err := s.getEmergencyTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !canViewEmergency(p, a) {
			return apperr.Forbidden("")
		}
		if !contains(a.AllowedActions, action) {
			if !a.Active {
				return apperr.Conflict("OBJECT_TERMINAL", "Emergency Alert "+a.AlertNumber+" sudah "+a.Status)
			}
			if action == "acknowledge" || action == "respond" || action == "note" || action == "resolve" || action == "cancel" {
				needs := "security.emergency_alerts.respond"
				if action == "resolve" || action == "cancel" {
					needs = "security.emergency_alerts.resolve"
				}
				if !p.HasAnyOnProperty(needs, a.PropertyID) {
					return apperr.Forbidden("Memerlukan " + needs)
				}
			}
			return apperr.InvalidTransition(fmt.Sprintf("Emergency Alert %s tidak dapat %s dari status %s", a.AlertNumber, action, a.Status))
		}
		note := in.Note
		if note != nil && strings.TrimSpace(*note) == "" {
			note = nil
		}
		switch action {
		case "acknowledge":
			if _, err := tx.Exec(ctx, `UPDATE emergency_alerts SET status = 'acknowledged', acknowledged_by = $2, acknowledged_at = now(), responder_user_id = COALESCE(responder_user_id, $2), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
			if a.IncidentID != nil {
				_, _ = tx.Exec(ctx, `UPDATE incidents SET assignee_user_id = COALESCE(assignee_user_id, $2), status = CASE WHEN status = 'new' THEN 'assigned' ELSE status END, updated_by = $2 WHERE id = $1`, *a.IncidentID, p.UserID)
			}
			_ = addEmergencyEvent(ctx, tx, id, "acknowledged", note, nil)
		case "respond":
			if _, err := tx.Exec(ctx, `UPDATE emergency_alerts SET status = 'responding', responding_at = now(), responder_user_id = COALESCE(responder_user_id, $2),
				acknowledged_by = COALESCE(acknowledged_by, $2), acknowledged_at = COALESCE(acknowledged_at, now()), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
			if a.IncidentID != nil {
				_, _ = tx.Exec(ctx, `UPDATE incidents SET assignee_user_id = COALESCE(assignee_user_id, $2), status = CASE WHEN status IN ('new','assigned') THEN 'in_progress' ELSE status END, updated_by = $2 WHERE id = $1`, *a.IncidentID, p.UserID)
				_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET responded_at = COALESCE(responded_at, now()) WHERE object_type = 'incident' AND object_id = $1`, *a.IncidentID)
			}
			if a.Status == "raised" {
				_ = addEmergencyEvent(ctx, tx, id, "acknowledged", nil, map[string]any{"implicit": true})
			}
			_ = addEmergencyEvent(ctx, tx, id, "responding", note, nil)
		case "note":
			if note == nil {
				return apperr.Validation("note wajib").WithField("note", "wajib")
			}
			_ = addEmergencyEvent(ctx, tx, id, "action", note, nil)
		case "resolve":
			res := strings.TrimSpace(in.Resolution)
			if res == "" && note != nil {
				res = *note
			}
			if res == "" {
				return apperr.Validation("resolution wajib").WithField("resolution", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE emergency_alerts SET status = 'resolved', resolution = $3, resolved_by = $2, resolved_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID, res); err != nil {
				return err
			}
			if a.IncidentID != nil {
				_, _ = tx.Exec(ctx, `UPDATE incidents SET action_taken = COALESCE(action_taken, $2), updated_by = $3 WHERE id = $1`, *a.IncidentID, res, p.UserID)
			}
			_ = addEmergencyEvent(ctx, tx, id, "resolved", &res, nil)
		case "cancel":
			reason := strings.TrimSpace(in.Reason)
			if reason == "" && note != nil {
				reason = *note
			}
			if reason == "" {
				return apperr.Validation("reason wajib (mis. alarm palsu)").WithField("reason", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE emergency_alerts SET status = 'cancelled', cancel_reason = $3, cancelled_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID, reason); err != nil {
				return err
			}
			if a.IncidentID != nil {
				_, _ = tx.Exec(ctx, `UPDATE incidents SET status = 'cancelled', resolution = COALESCE(resolution, $2), updated_by = $3 WHERE id = $1 AND status IN ('new','assigned','in_progress')`, *a.IncidentID, "Alarm dibatalkan: "+reason, p.UserID)
			}
			_ = addEmergencyEvent(ctx, tx, id, "cancelled", &reason, nil)
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "emergency_alert", ObjectID: id, Action: audit.ActStatusChanged, From: a.Status, To: action, Payload: map[string]any{"action": action, "note": note}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "emergency_alert", EntityID: &id, EntityLabel: a.AlertNumber, Before: map[string]any{"status": a.Status}, After: map[string]any{"action": action}})
		na, err := s.getEmergencyTx(ctx, tx, id)
		if err != nil {
			return err
		}
		switch action {
		case "acknowledge":
			s.emitEmergency(ctx, tx, events.EmergencyAcknowledged, na, nil)
		case "respond":
			s.emitEmergency(ctx, tx, events.EmergencyResponding, na, nil)
		case "resolve":
			s.emitEmergency(ctx, tx, events.EmergencyResolved, na, map[string]any{"reason": deref(na.Resolution)})
		case "cancel":
			s.emitEmergency(ctx, tx, events.EmergencyCancelled, na, map[string]any{"reason": deref(na.CancelReason)})
		}
		na.Timeline, err = s.emergencyTimelineTx(ctx, tx, id)
		if err != nil {
			return err
		}
		na.Contacts, err = s.contactsTx(ctx, tx, na.PropertyID, true)
		out = na
		return err
	})
	return out, err
}

func (s *Service) GetEmergency(ctx context.Context, id uuid.UUID) (*EmergencyAlert, error) {
	var out *EmergencyAlert
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := s.getEmergencyTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !canViewEmergency(authctx.Must(ctx), a) {
			return apperr.Forbidden("")
		}
		if a.Timeline, err = s.emergencyTimelineTx(ctx, tx, id); err != nil {
			return err
		}
		a.Contacts, err = s.contactsTx(ctx, tx, a.PropertyID, true)
		out = a
		return err
	})
	return out, err
}

type EmergencyFilter struct {
	PropertyID *uuid.UUID
	Statuses   []string
	Active     *bool
	Mine       bool
	From, To   *time.Time
}

func (s *Service) ListEmergencies(ctx context.Context, f EmergencyFilter, page httpx.Page) ([]EmergencyAlert, *string, error) {
	p := authctx.Must(ctx)
	out := []EmergencyAlert{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE 1=1"
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("security.emergency_alerts.view", *f.PropertyID) && !f.Mine {
				return apperr.Forbidden("")
			}
			where += " AND e.property_id = " + add(*f.PropertyID)
		}
		if f.Mine {
			where += " AND e.raised_by = " + add(p.UserID)
		} else {
			where += " AND (" + p.ScopeSQL("security.emergency_alerts.view", "e.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = e.location_id)", add) + " OR e.raised_by = " + add(p.UserID) + ")"
		}
		if len(f.Statuses) > 0 {
			where += " AND e.status = ANY(" + add(f.Statuses) + ")"
		}
		if f.Active != nil {
			if *f.Active {
				where += " AND e.status IN ('raised','acknowledged','responding')"
			} else {
				where += " AND e.status IN ('resolved','cancelled')"
			}
		}
		if f.From != nil {
			where += " AND e.raised_at >= " + add(*f.From)
		}
		if f.To != nil {
			where += " AND e.raised_at < " + add(*f.To)
		}
		if page.Cursor != nil {
			where += " AND (e.raised_at, e.id) < (" + add(page.Cursor.Value) + "::timestamptz, " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, emergencySelect+where+" ORDER BY e.raised_at DESC, e.id DESC LIMIT "+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			a, err := scanEmergency(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out = append(out, *a)
		}
		rows.Close()
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.RaisedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		for i := range out {
			if out[i].Location.ID != nil {
				pt := property.LocationPathText(ctx, tx, *out[i].Location.ID)
				out[i].Location.PathText = &pt
			}
			out[i].AllowedActions = emergencyActions(p, &out[i])
		}
		return nil
	})
	return out, next, err
}

// EmergencySweep: alert belum di-acknowledge melewati batas → eskalasi bertingkat (P2-EMG-04). Dipanggil worker tiap menit.
func (s *Service) EmergencySweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	timeout := s.EmergencyAckTimeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, escalation_level, raised_at FROM emergency_alerts WHERE status = 'raised' AND escalation_level < 3 AND raised_at < now() - make_interval(secs => $1) FOR UPDATE SKIP LOCKED`, timeout.Seconds())
		if err != nil {
			return err
		}
		type due struct {
			id    uuid.UUID
			level int
			at    time.Time
		}
		var list []due
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.id, &d.level, &d.at); err != nil {
				rows.Close()
				return err
			}
			list = append(list, d)
		}
		rows.Close()
		for _, d := range list {
			level := int(time.Since(d.at) / timeout)
			if level > 3 {
				level = 3
			}
			if level <= d.level {
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE emergency_alerts SET escalation_level = $2, escalated_at = now() WHERE id = $1`, d.id, level); err != nil {
				return err
			}
			note := fmt.Sprintf("Belum di-acknowledge setelah %d menit — eskalasi level %d", int(time.Since(d.at).Minutes()), level)
			_ = addEmergencyEvent(ctx, tx, d.id, "escalated", &note, map[string]any{"level": level})
			if a, err := s.getEmergencyTx(ctx, tx, d.id); err == nil {
				s.emitEmergency(ctx, tx, events.EmergencyEscalated, a, map[string]any{"reason": note, "level": level})
			}
			n++
		}
		return nil
	})
	return n, err
}

// ---------- Emergency contacts (P2-EMG-07) ----------

type EmergencyContactInput struct {
	PropertyID  *uuid.UUID `json:"property_id"`
	Name        *string    `json:"name"`
	ContactType *string    `json:"contact_type"`
	Phone       *string    `json:"phone"`
	Notes       *string    `json:"notes"`
	SortOrder   *int       `json:"sort_order"`
	IsActive    *bool      `json:"is_active"`
}

func (s *Service) contactsTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, activeOnly bool) ([]EmergencyContact, error) {
	rows, err := tx.Query(ctx, `SELECT id, property_id, name, contact_type, phone, notes, sort_order, is_active, version FROM emergency_contacts WHERE property_id = $1 AND ($2 = false OR is_active) ORDER BY sort_order, name`, propertyID, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmergencyContact{}
	for rows.Next() {
		var c EmergencyContact
		if err := rows.Scan(&c.ID, &c.PropertyID, &c.Name, &c.ContactType, &c.Phone, &c.Notes, &c.SortOrder, &c.IsActive, &c.Version); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) ListEmergencyContacts(ctx context.Context, propertyID *uuid.UUID) ([]EmergencyContact, error) {
	p := authctx.Must(ctx)
	out := []EmergencyContact{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pids := []uuid.UUID{}
		if propertyID != nil {
			if !p.HasAnyOnProperty("security.emergency_contacts.view", *propertyID) {
				return apperr.Forbidden("")
			}
			pids = append(pids, *propertyID)
		} else {
			rows, err := tx.Query(ctx, `SELECT location_id FROM properties`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil && p.HasAnyOnProperty("security.emergency_contacts.view", id) {
					pids = append(pids, id)
				}
			}
			rows.Close()
		}
		for _, pid := range pids {
			cs, err := s.contactsTx(ctx, tx, pid, false)
			if err != nil {
				return err
			}
			out = append(out, cs...)
		}
		return nil
	})
	return out, err
}

func (s *Service) SaveEmergencyContact(ctx context.Context, id *uuid.UUID, in EmergencyContactInput) (*EmergencyContact, error) {
	p := authctx.Must(ctx)
	var out *EmergencyContact
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if in.ContactType != nil && !EmergencyContactTypes[*in.ContactType] {
			return apperr.Validation("contact_type harus fire|ambulance|police|electricity|water|internal|other").WithField("contact_type", "tidak valid")
		}
		var cid uuid.UUID
		var pid uuid.UUID
		if id == nil {
			if in.PropertyID == nil || in.Name == nil || strings.TrimSpace(*in.Name) == "" || in.Phone == nil || strings.TrimSpace(*in.Phone) == "" {
				return apperr.Validation("property_id, name, phone wajib")
			}
			if !p.HasOnProperty("security.emergency_contacts.manage", *in.PropertyID) {
				return apperr.Forbidden("Memerlukan security.emergency_contacts.manage")
			}
			ct := "other"
			if in.ContactType != nil {
				ct = *in.ContactType
			}
			order := 0
			if in.SortOrder != nil {
				order = *in.SortOrder
			}
			if err := tx.QueryRow(ctx, `INSERT INTO emergency_contacts (organization_id, property_id, name, contact_type, phone, notes, sort_order, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`,
				p.OrganizationID, *in.PropertyID, strings.TrimSpace(*in.Name), ct, strings.TrimSpace(*in.Phone), in.Notes, order, p.UserID).Scan(&cid); err != nil {
				return err
			}
			pid = *in.PropertyID
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "emergency_contact", EntityID: &cid, EntityLabel: *in.Name, After: in})
		} else {
			cid = *id
			if err := tx.QueryRow(ctx, `SELECT property_id FROM emergency_contacts WHERE id = $1`, cid).Scan(&pid); err != nil {
				return apperr.NotFound("Emergency contact")
			}
			if !p.HasOnProperty("security.emergency_contacts.manage", pid) {
				return apperr.Forbidden("Memerlukan security.emergency_contacts.manage")
			}
			if _, err := tx.Exec(ctx, `UPDATE emergency_contacts SET name = COALESCE(NULLIF(TRIM($2),''), name), contact_type = COALESCE($3, contact_type), phone = COALESCE(NULLIF(TRIM($4),''), phone),
				notes = COALESCE($5, notes), sort_order = COALESCE($6, sort_order), is_active = COALESCE($7, is_active), updated_by = $8 WHERE id = $1`,
				cid, deref(in.Name), in.ContactType, deref(in.Phone), in.Notes, in.SortOrder, in.IsActive, p.UserID); err != nil {
				return err
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "emergency_contact", EntityID: &cid, EntityLabel: deref(in.Name), After: in})
		}
		cs, err := s.contactsTx(ctx, tx, pid, false)
		for i := range cs {
			if cs[i].ID == cid {
				out = &cs[i]
			}
		}
		return err
	})
	return out, err
}

func (s *Service) DeleteEmergencyContact(ctx context.Context, id uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		var name string
		if err := tx.QueryRow(ctx, `SELECT property_id, name FROM emergency_contacts WHERE id = $1`, id).Scan(&pid, &name); err != nil {
			return apperr.NotFound("Emergency contact")
		}
		if !p.HasOnProperty("security.emergency_contacts.manage", pid) {
			return apperr.Forbidden("Memerlukan security.emergency_contacts.manage")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM emergency_contacts WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "emergency_contact", EntityID: &id, EntityLabel: name})
	})
}

// EmergencyContactsForBundle: kontak darurat aktif untuk property yang boleh dilihat user (cache offline Staff App).
func (s *Service) EmergencyContactsForBundle(ctx context.Context, tx pgx.Tx) []EmergencyContact {
	p := authctx.Must(ctx)
	out := []EmergencyContact{}
	rows, err := tx.Query(ctx, `SELECT location_id FROM properties`)
	if err != nil {
		return out
	}
	var pids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil && (p.HasAnyOnProperty("security.emergency_contacts.view", id) || p.HasAnyOnProperty("security.emergency_alerts.raise", id)) {
			pids = append(pids, id)
		}
	}
	rows.Close()
	for _, pid := range pids {
		if cs, err := s.contactsTx(ctx, tx, pid, true); err == nil {
			out = append(out, cs...)
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
