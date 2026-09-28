package workforce

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/security"
)

// ---------- Serah terima shift (PRD P2 v2.1 P2-SHF-03) ----------
// Security: incident aktif, emergency aktif, patrol tertunda, catatan pos. Housekeeping: area belum selesai, temuan terbuka,
// rework. Snapshot item terbuka disimpan saat serah terima agar shift berikut melihat kondisi persis saat itu.

type OpenItem struct {
	ObjectType string     `json:"object_type"`
	ID         uuid.UUID  `json:"id"`
	Number     string     `json:"number"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	Priority   string     `json:"priority,omitempty"`
	Location   *string    `json:"location,omitempty"`
	DueAt      *time.Time `json:"due_at,omitempty"`
	DeepLink   string     `json:"deep_link"`
}

type HandoverSnapshot struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Groups      map[string][]OpenItem `json:"groups"` // active_incidents | active_emergencies | pending_patrols | open_findings | unfinished_cleaning | rework_tasks
	Counts      map[string]int        `json:"counts"`
}

func collect(ctx context.Context, tx pgx.Tx, q string, args ...any) []OpenItem {
	out := []OpenItem{}
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var it OpenItem
		if rows.Scan(&it.ObjectType, &it.ID, &it.Number, &it.Title, &it.Status, &it.Priority, &it.Location, &it.DueAt, &it.DeepLink) == nil {
			out = append(out, it)
		}
	}
	return out
}

// snapshotTx: item terbuka domain di property saat ini.
func snapshotTx(ctx context.Context, tx pgx.Tx, domain string, propertyID uuid.UUID) HandoverSnapshot {
	snap := HandoverSnapshot{GeneratedAt: time.Now().UTC(), Groups: map[string][]OpenItem{}, Counts: map[string]int{}}
	loc := `(SELECT string_agg(a.name, ' / ' ORDER BY a.depth) FROM locations l JOIN locations a ON a.path @> l.path AND a.depth > 0 WHERE l.id = %s)`
	switch domain {
	case "security":
		snap.Groups["active_emergencies"] = collect(ctx, tx, `SELECT 'emergency_alert', e.id, e.alert_number, 'Emergency — ' || `+security.EmergencyTypeLabelSQL("e.emergency_type")+`, e.status, 'critical', `+fmt.Sprintf(loc, "e.location_id")+`, NULL::timestamptz, '/security/emergency/' || e.id
			FROM emergency_alerts e WHERE e.property_id = $1 AND e.status IN ('raised','acknowledged','responding') ORDER BY e.raised_at`, propertyID)
		snap.Groups["active_incidents"] = collect(ctx, tx, `SELECT 'incident', i.id, i.incident_number, i.title, i.status, i.severity, `+fmt.Sprintf(loc, "i.location_id")+`, NULL::timestamptz, '/operations/incidents/' || i.id
			FROM incidents i WHERE i.property_id = $1 AND i.status NOT IN ('resolved','closed','cancelled') ORDER BY CASE i.severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 ELSE 2 END, i.reported_at LIMIT 50`, propertyID)
		snap.Groups["pending_patrols"] = collect(ctx, tx, `SELECT 'task', t.id, t.task_number, t.title, t.status, t.priority, `+fmt.Sprintf(loc, "t.location_id")+`, t.due_at, '/operations/tasks/' || t.id
			FROM tasks t WHERE t.property_id = $1 AND t.task_type = 'patrol' AND t.status NOT IN ('completed','closed','cancelled') AND COALESCE(t.scheduled_start_at, t.due_at, t.created_at) < now() + interval '12 hours'
			ORDER BY t.due_at NULLS LAST LIMIT 50`, propertyID)
		snap.Groups["open_findings"] = collect(ctx, tx, `SELECT 'finding', f.id, f.finding_number, f.title, f.status, f.severity, `+fmt.Sprintf(loc, "f.location_id")+`, NULL::timestamptz, '/findings/' || f.id
			FROM findings f WHERE f.property_id = $1 AND f.finding_type = 'patrol' AND f.status IN ('open','in_progress') ORDER BY f.reported_at LIMIT 50`, propertyID)
	case "housekeeping":
		snap.Groups["unfinished_cleaning"] = collect(ctx, tx, `SELECT 'task', t.id, t.task_number, t.title, t.status, t.priority, `+fmt.Sprintf(loc, "t.location_id")+`, t.due_at, '/operations/tasks/' || t.id
			FROM tasks t WHERE t.property_id = $1 AND t.task_type = 'cleaning' AND t.status NOT IN ('completed','closed','cancelled') AND COALESCE(t.scheduled_start_at, t.due_at, t.created_at) < now() + interval '12 hours'
			ORDER BY t.due_at NULLS LAST LIMIT 50`, propertyID)
		snap.Groups["open_findings"] = collect(ctx, tx, `SELECT 'finding', f.id, f.finding_number, f.title, f.status, f.severity, `+fmt.Sprintf(loc, "f.location_id")+`, NULL::timestamptz, '/findings/' || f.id
			FROM findings f WHERE f.property_id = $1 AND f.finding_type IN ('housekeeping','inspection','checklist') AND f.status IN ('open','in_progress') ORDER BY f.reported_at LIMIT 50`, propertyID)
		snap.Groups["rework_tasks"] = collect(ctx, tx, `SELECT 'task', t.id, t.task_number, t.title, t.status, t.priority, `+fmt.Sprintf(loc, "t.location_id")+`, t.due_at, '/operations/tasks/' || t.id
			FROM tasks t WHERE t.property_id = $1 AND t.task_type = 'cleaning' AND t.source_type = 'finding' AND t.status NOT IN ('completed','closed','cancelled') ORDER BY t.created_at LIMIT 50`, propertyID)
	}
	for k, v := range snap.Groups {
		snap.Counts[k] = len(v)
	}
	return snap
}

type Handover struct {
	ID             uuid.UUID        `json:"id"`
	PropertyID     uuid.UUID        `json:"property_id"`
	Domain         string           `json:"domain"`
	HandoverNumber string           `json:"handover_number"`
	ShiftID        *uuid.UUID       `json:"shift_id"`
	ShiftName      *string          `json:"shift_name"`
	ShiftDate      *string          `json:"shift_date"`
	HandedOverBy   uuid.UUID        `json:"handed_over_by"`
	HandedOverName string           `json:"handed_over_by_name"`
	ReceivedBy     *uuid.UUID       `json:"received_by"`
	ReceivedByName *string          `json:"received_by_name"`
	Post           *string          `json:"post"`
	Notes          string           `json:"notes"`
	OpenItems      HandoverSnapshot `json:"open_items"`
	Status         string           `json:"status"` // submitted | acknowledged
	HandedOverAt   time.Time        `json:"handed_over_at"`
	AcknowledgedAt *time.Time       `json:"acknowledged_at"`
	AllowedActions []string         `json:"allowed_actions"`
}

const handoverSelect = `SELECT h.id, h.property_id, h.domain, h.handover_number, h.shift_id, sd.name, h.shift_date::text, h.handed_over_by, hu.full_name, h.received_by, ru.full_name, h.post, h.notes, h.open_items, h.status, h.handed_over_at, h.acknowledged_at
	FROM shift_handovers h JOIN users hu ON hu.id = h.handed_over_by LEFT JOIN users ru ON ru.id = h.received_by LEFT JOIN shift_definitions sd ON sd.id = h.shift_id`

func scanHandover(ctx context.Context, row pgx.Row) (*Handover, error) {
	var h Handover
	var raw []byte
	if err := row.Scan(&h.ID, &h.PropertyID, &h.Domain, &h.HandoverNumber, &h.ShiftID, &h.ShiftName, &h.ShiftDate, &h.HandedOverBy, &h.HandedOverName, &h.ReceivedBy, &h.ReceivedByName, &h.Post, &h.Notes, &raw, &h.Status, &h.HandedOverAt, &h.AcknowledgedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &h.OpenItems)
	p := authctx.Must(ctx)
	h.AllowedActions = []string{"view"}
	if h.Status == "submitted" && h.HandedOverBy != p.UserID && ((h.ReceivedBy != nil && *h.ReceivedBy == p.UserID) || p.HasAnyOnProperty(shiftPerm(h.Domain, "manage"), h.PropertyID) ||
		(h.ReceivedBy == nil && p.HasAnyOnProperty(shiftPerm(h.Domain, "view"), h.PropertyID))) {
		h.AllowedActions = append(h.AllowedActions, "acknowledge")
	}
	return &h, nil
}

// HandoverDraft: snapshot item terbuka untuk form serah terima.
func (s *Service) HandoverDraft(ctx context.Context, domain string, propertyID uuid.UUID) (*HandoverSnapshot, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	if !authctx.Must(ctx).HasAnyOnProperty(shiftPerm(domain, "view"), propertyID) {
		return nil, apperr.Forbidden("")
	}
	var out HandoverSnapshot
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		out = snapshotTx(ctx, tx, domain, propertyID)
		return nil
	})
	return &out, err
}

type HandoverInput struct {
	PropertyID uuid.UUID  `json:"property_id"`
	ShiftID    *uuid.UUID `json:"shift_id"`
	ShiftDate  *string    `json:"shift_date"`
	ReceivedBy *uuid.UUID `json:"received_by"`
	Post       *string    `json:"post"`
	Notes      string     `json:"notes"`
}

func (s *Service) CreateHandover(ctx context.Context, domain string, in HandoverInput) (*Handover, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	if strings.TrimSpace(in.Notes) == "" {
		return nil, apperr.Validation("notes wajib (catatan serah terima)").WithField("notes", "wajib")
	}
	var out *Handover
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if !p.HasAnyOnProperty(shiftPerm(domain, "view"), in.PropertyID) {
			return apperr.Forbidden("Memerlukan " + shiftPerm(domain, "view"))
		}
		if in.ShiftID != nil {
			sd, err := s.getShiftTx(ctx, tx, *in.ShiftID)
			if err != nil || sd.Domain != domain || sd.PropertyID != in.PropertyID {
				return apperr.Validation("shift_id tidak valid").WithField("shift_id", "tidak valid")
			}
		}
		if in.ReceivedBy != nil {
			var ok bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND is_active AND deleted_at IS NULL)`, *in.ReceivedBy).Scan(&ok)
			if !ok || *in.ReceivedBy == p.UserID {
				return apperr.Validation("received_by harus staf lain yang aktif").WithField("received_by", "tidak valid")
			}
		}
		var shiftDate any
		if in.ShiftDate != nil && *in.ShiftDate != "" {
			if _, err := time.Parse("2006-01-02", *in.ShiftDate); err != nil {
				return apperr.Validation("shift_date harus YYYY-MM-DD").WithField("shift_date", "format tanggal")
			}
			shiftDate = *in.ShiftDate
		}
		snap := snapshotTx(ctx, tx, domain, in.PropertyID)
		raw, _ := json.Marshal(snap)
		loc := property.PropertyTimezone(ctx, tx, in.PropertyID)
		number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixShiftHandover, time.Now(), loc)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO shift_handovers (organization_id, property_id, domain, handover_number, shift_id, shift_date, handed_over_by, received_by, post, notes, open_items)
			VALUES ($1,$2,$3,$4,$5,$6::date,$7,$8,$9,$10,$11) RETURNING id`, p.OrganizationID, in.PropertyID, domain, number, in.ShiftID, shiftDate, p.UserID, in.ReceivedBy, in.Post, strings.TrimSpace(in.Notes), raw).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "shift_handover", EntityID: &id, EntityLabel: number, After: map[string]any{"domain": domain, "counts": snap.Counts}})
		if s.Jobs != nil {
			payload := map[string]any{"domain": domain, "counts": snap.Counts}
			if in.ReceivedBy != nil {
				payload["assignee_user_id"] = *in.ReceivedBy
			}
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "shift_handover.submitted", OrganizationID: p.OrganizationID, PropertyID: &in.PropertyID, ObjectType: "shift_handover", ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: payload})
		}
		out, err = scanHandover(ctx, tx.QueryRow(ctx, handoverSelect+` WHERE h.id = $1`, id))
		return err
	})
	return out, err
}

func (s *Service) AcknowledgeHandover(ctx context.Context, domain string, id uuid.UUID) (*Handover, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	var out *Handover
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		h, err := scanHandover(ctx, tx.QueryRow(ctx, handoverSelect+` WHERE h.id = $1 AND h.domain = $2`, id, domain))
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Serah terima")
			}
			return err
		}
		if h.Status != "submitted" {
			return apperr.Conflict("OBJECT_TERMINAL", "Serah terima sudah diterima")
		}
		if !contains(h.AllowedActions, "acknowledge") {
			return apperr.Forbidden("Hanya penerima shift berikut atau supervisor")
		}
		if _, err := tx.Exec(ctx, `UPDATE shift_handovers SET status = 'acknowledged', acknowledged_at = now(), received_by = COALESCE(received_by, $2) WHERE id = $1`, id, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "shift_handover", EntityID: &id, EntityLabel: h.HandoverNumber, After: map[string]any{"status": "acknowledged"}})
		out, err = scanHandover(ctx, tx.QueryRow(ctx, handoverSelect+` WHERE h.id = $1`, id))
		return err
	})
	return out, err
}

// GetHandover: satu serah terima (deep link notifikasi `?tab=handovers&id=`).
func (s *Service) GetHandover(ctx context.Context, domain string, id uuid.UUID) (*Handover, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	var out *Handover
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		h, err := scanHandover(ctx, tx.QueryRow(ctx, handoverSelect+` WHERE h.id = $1 AND h.domain = $2`, id, domain))
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Serah terima")
			}
			return err
		}
		p := authctx.Must(ctx)
		if !p.HasAnyOnProperty(shiftPerm(domain, "view"), h.PropertyID) && (h.ReceivedBy == nil || *h.ReceivedBy != p.UserID) && h.HandedOverBy != p.UserID {
			return apperr.NotFound("Serah terima")
		}
		out = h
		return nil
	})
	return out, err
}

func (s *Service) ListHandovers(ctx context.Context, domain string, propertyID *uuid.UUID, limit int) ([]Handover, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	out := []Handover{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{domain}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE h.domain = $1"
		if propertyID != nil {
			if !p.HasAnyOnProperty(shiftPerm(domain, "view"), *propertyID) {
				return apperr.Forbidden("")
			}
			where += " AND h.property_id = " + add(*propertyID)
		}
		where += " AND " + p.ScopeSQL(shiftPerm(domain, "view"), "h.property_id", "", add)
		rows, err := tx.Query(ctx, handoverSelect+where+" ORDER BY h.handed_over_at DESC LIMIT "+add(limit), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			h, err := scanHandover(ctx, rows)
			if err != nil {
				return err
			}
			out = append(out, *h)
		}
		return rows.Err()
	})
	return out, err
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
