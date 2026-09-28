package operations

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
)

// ---------- Rantai pekerjaan lintas tim (PRD P1 v2.1 §5.15, Roadmap v2.1 §17; PRD P2 v2.1 §10) ----------
//
// Setiap Task / Finding / WO / Incident yang lahir (langsung atau tidak langsung) dari Service Request menyimpan
// origin_service_request_id (P1-XMW-01). SR baru Resolved bila seluruh pekerjaan dalam rantainya selesai (P1-XMW-02).
// Dari WO yang selesai, supervisor membuat Task tindak lanjut untuk team lain — inspeksi akhir, re-clean, verifikasi
// security (P1-XMW-03, P2-XTW-01). Finding/Incident asal baru diselesaikan setelah tindak lanjutnya selesai (P2-XTW-02).

// ChainEvent: satu pekerjaan dalam rantai SR baru saja selesai.
type ChainEvent struct {
	OriginServiceRequestID uuid.UUID
	ObjectType             string
	ObjectID               uuid.UUID
	Label                  string  // business id (WO-…, TSK-…, FND-…, INC-…)
	Resolution             *string // resolusi WO / finding / incident bila ada
}

// ChainListener dipanggil setelah pekerjaan dalam rantai SR asal selesai (task/WO complete/close, finding resolve/close,
// incident resolve/close). tenantservice mendaftarkan resolver SR.
type ChainListener func(ctx context.Context, tx pgx.Tx, ev ChainEvent) error

// OnChainProgress mendaftarkan listener rantai.
func (s *Service) OnChainProgress(fn ChainListener) { s.chainListeners = append(s.chainListeners, fn) }

var chainTables = map[string]string{ObjTask: "tasks", ObjWorkOrder: "work_orders", ObjFinding: "findings", ObjIncident: "incidents"}

// originFromSource: SR asal object baru, diturunkan dari object sumbernya.
func originFromSource(ctx context.Context, tx pgx.Tx, sourceType *string, sourceID *uuid.UUID) *uuid.UUID {
	if sourceType == nil || sourceID == nil {
		return nil
	}
	if *sourceType == ObjServiceRequest {
		id := *sourceID
		return &id
	}
	table, ok := chainTables[*sourceType]
	if !ok {
		return nil
	}
	var o *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT origin_service_request_id FROM `+table+` WHERE id = $1`, *sourceID).Scan(&o); err != nil {
		return nil
	}
	return o
}

// setOriginTx menyimpan SR asal object yang baru dibuat (no-op bila bukan bagian rantai SR).
func setOriginTx(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID, sourceType *string, sourceID *uuid.UUID) error {
	o := originFromSource(ctx, tx, sourceType, sourceID)
	table, ok := chainTables[objectType]
	if o == nil || !ok {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE `+table+` SET origin_service_request_id = $2 WHERE id = $1`, id, *o)
	return err
}

// chainProgressTx memberi tahu listener bila object berada dalam rantai SR.
func (s *Service) chainProgressTx(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID, label string, resolution *string) error {
	table, ok := chainTables[objectType]
	if !ok || len(s.chainListeners) == 0 {
		return nil
	}
	var o *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT origin_service_request_id FROM `+table+` WHERE id = $1`, id).Scan(&o); err != nil || o == nil {
		return nil
	}
	ev := ChainEvent{OriginServiceRequestID: *o, ObjectType: objectType, ObjectID: id, Label: label, Resolution: resolution}
	for _, fn := range s.chainListeners {
		if err := fn(ctx, tx, ev); err != nil {
			return err
		}
	}
	return nil
}

// ChainOpenCount: pekerjaan belum selesai dalam rantai SR (task/WO belum completed/closed/cancelled — draft dihitung
// terbuka —, finding open/in_progress, incident belum resolved/closed/cancelled).
func ChainOpenCount(ctx context.Context, tx pgx.Tx, srID uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT
		  (SELECT count(*) FROM tasks WHERE origin_service_request_id = $1 AND status NOT IN ('completed','closed','cancelled'))
		+ (SELECT count(*) FROM work_orders WHERE origin_service_request_id = $1 AND status NOT IN ('completed','closed','cancelled'))
		+ (SELECT count(*) FROM findings WHERE origin_service_request_id = $1 AND status IN ('open','in_progress'))
		+ (SELECT count(*) FROM incidents WHERE origin_service_request_id = $1 AND status NOT IN ('resolved','closed','cancelled'))`, srID).Scan(&n)
	return n, err
}

// ChainItem: satu langkah dalam timeline lintas tim SR (P1-XMW-04).
type ChainItem struct {
	ObjectType      string     `json:"object_type"` // task | work_order | finding | incident
	ID              uuid.UUID  `json:"id"`
	Number          string     `json:"number"`
	Title           string     `json:"title"`
	Type            string     `json:"type"` // task_type / work_order_type / finding_type / incident category
	Status          string     `json:"status"`
	Priority        string     `json:"priority"` // severity untuk finding/incident
	TeamID          *uuid.UUID `json:"team_id"`
	TeamName        *string    `json:"team_name"`
	TeamDomain      *string    `json:"team_domain"`
	AssigneeName    *string    `json:"assignee_name"`
	SLAStatus       string     `json:"sla_status"`
	DueAt           *time.Time `json:"due_at"`
	CreatedAt       time.Time  `json:"created_at"`
	DoneAt          *time.Time `json:"done_at"` // completed/resolved
	ParentType      *string    `json:"parent_type"`
	ParentID        *uuid.UUID `json:"parent_id"`
	ParentLabel     string     `json:"parent_label"`
	Depth           int        `json:"depth"` // 1 = anak langsung SR
	FollowUpPurpose *string    `json:"follow_up_purpose,omitempty"`
	Open            bool       `json:"open"`
	DeepLink        string     `json:"deep_link"`
}

// ChainItems: seluruh pekerjaan dalam rantai SR (urut waktu dibuat), dengan team, status, dan SLA.
func ChainItems(ctx context.Context, tx pgx.Tx, srID uuid.UUID) ([]ChainItem, error) {
	rows, err := tx.Query(ctx, `
		SELECT 'task', t.id, t.task_number, t.title, t.task_type, t.status, t.priority, t.assignee_team_id, tm.name, tm.domain, u.full_name,
		  t.sla_risk_at, t.sla_breached_at, t.due_at, t.created_at, t.completed_at, t.source_type, t.source_id, t.follow_up_purpose
		FROM tasks t LEFT JOIN teams tm ON tm.id = t.assignee_team_id LEFT JOIN users u ON u.id = t.assignee_user_id WHERE t.origin_service_request_id = $1
		UNION ALL
		SELECT 'work_order', w.id, w.work_order_number, w.title, w.work_order_type, w.status, w.priority, w.assignee_team_id, tm.name, tm.domain, u.full_name,
		  w.sla_risk_at, w.sla_breached_at, w.due_at, w.created_at, w.completed_at, w.source_type, w.source_id, NULL
		FROM work_orders w LEFT JOIN teams tm ON tm.id = w.assignee_team_id LEFT JOIN users u ON u.id = w.assignee_user_id WHERE w.origin_service_request_id = $1
		UNION ALL
		SELECT 'finding', f.id, f.finding_number, f.title, f.finding_type, f.status, f.severity, NULL, NULL, NULL, ru.full_name,
		  NULL, NULL, NULL, f.created_at, f.resolved_at, f.source_type, f.source_id, NULL
		FROM findings f LEFT JOIN users ru ON ru.id = f.reported_by WHERE f.origin_service_request_id = $1
		UNION ALL
		SELECT 'incident', i.id, i.incident_number, i.title, i.category, i.status, i.severity, i.assignee_team_id, tm.name, tm.domain, u.full_name,
		  i.sla_risk_at, i.sla_breached_at, NULL, i.created_at, i.resolved_at, i.source_type, i.source_id, NULL
		FROM incidents i LEFT JOIN teams tm ON tm.id = i.assignee_team_id LEFT JOIN users u ON u.id = i.assignee_user_id WHERE i.origin_service_request_id = $1`, srID)
	if err != nil {
		return nil, err
	}
	out := []ChainItem{}
	for rows.Next() {
		var it ChainItem
		var risk, breach *time.Time
		if err := rows.Scan(&it.ObjectType, &it.ID, &it.Number, &it.Title, &it.Type, &it.Status, &it.Priority, &it.TeamID, &it.TeamName, &it.TeamDomain, &it.AssigneeName,
			&risk, &breach, &it.DueAt, &it.CreatedAt, &it.DoneAt, &it.ParentType, &it.ParentID, &it.FollowUpPurpose); err != nil {
			rows.Close()
			return nil, err
		}
		switch it.ObjectType {
		case ObjFinding:
			it.Open = it.Status == "open" || it.Status == "in_progress"
		case ObjIncident:
			it.Open = !isDoneStatus(it.Status)
			it.SLAStatus = DeriveSLAStatus(it.Status, risk, breach, true)
		default:
			it.Open = !isDoneStatus(it.Status)
			it.SLAStatus = DeriveSLAStatus(it.Status, risk, breach, it.DueAt != nil)
		}
		it.DeepLink = chainDeepLink(it.ObjectType, it.ID)
		out = append(out, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	byID := map[uuid.UUID]*ChainItem{}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	for i := range out {
		it := &out[i]
		if it.ParentID != nil {
			if parent, ok := byID[*it.ParentID]; ok {
				it.ParentLabel = parent.Number
			} else if it.ParentType != nil {
				it.ParentLabel, _, _ = describeObject(ctx, tx, *it.ParentType, *it.ParentID)
			}
		}
		// depth: jumlah langkah ke SR asal
		d, cur := 1, it
		for guard := 0; guard < 20 && cur.ParentID != nil; guard++ {
			parent, ok := byID[*cur.ParentID]
			if !ok {
				break
			}
			d++
			cur = parent
		}
		it.Depth = d
	}
	return out, nil
}

func chainDeepLink(objectType string, id uuid.UUID) string {
	route := map[string]string{ObjTask: "/operations/tasks/", ObjWorkOrder: "/operations/work-orders/", ObjFinding: "/findings/", ObjIncident: "/operations/incidents/"}[objectType]
	return route + id.String()
}

// ---------- Descendant work (P2-XTW-02) ----------

type chainNode struct {
	objectType string
	id         uuid.UUID
	number     string
	status     string
}

func nodeOpen(n chainNode) bool {
	if n.objectType == ObjFinding {
		return n.status == "open" || n.status == "in_progress"
	}
	return !isDoneStatus(n.status)
}

// openDescendants: pekerjaan terbuka di bawah object (mengikuti source_type/source_id, maks 8 tingkat).
func openDescendants(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID) ([]string, error) {
	var open []string
	visited := map[uuid.UUID]bool{id: true}
	frontier := []chainNode{{objectType: objectType, id: id}}
	for depth := 0; depth < 8 && len(frontier) > 0; depth++ {
		var next []chainNode
		for _, n := range frontier {
			rows, err := tx.Query(ctx, `
				SELECT 'task', id, task_number, status FROM tasks WHERE source_type = $1 AND source_id = $2
				UNION ALL SELECT 'work_order', id, work_order_number, status FROM work_orders WHERE source_type = $1 AND source_id = $2
				UNION ALL SELECT 'finding', id, finding_number, status FROM findings WHERE source_type = $1 AND source_id = $2
				UNION ALL SELECT 'incident', id, incident_number, status FROM incidents WHERE source_type = $1 AND source_id = $2`, n.objectType, n.id)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var c chainNode
				if err := rows.Scan(&c.objectType, &c.id, &c.number, &c.status); err != nil {
					rows.Close()
					return nil, err
				}
				if visited[c.id] {
					continue
				}
				visited[c.id] = true
				if nodeOpen(c) {
					open = append(open, c.number)
				}
				next = append(next, c)
			}
			rows.Close()
		}
		frontier = next
	}
	return open, nil
}

// OpenDescendantWork diekspor untuk modul lain (mis. laporan/verifikasi).
func OpenDescendantWork(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID) ([]string, error) {
	return openDescendants(ctx, tx, objectType, id)
}

// guardChainClose: Finding/Incident tidak dapat ditutup selama pekerjaan turunannya (WO, tindak lanjut verifikasi)
// masih terbuka (P2-XTW-02 "Incident/finding asal baru Closed setelah verifikasi security selesai").
func guardChainClose(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID) error {
	open, err := openDescendants(ctx, tx, objectType, id)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		return apperr.Conflict("CHAIN_OPEN_WORK", fmt.Sprintf("%s belum dapat ditutup: pekerjaan turunan masih terbuka (%s)", objectLabel(objectType), strings.Join(open, ", ")))
	}
	return nil
}

// resolveAncestorsAfterFollowUp: tindak lanjut (inspeksi akhir / re-clean / verifikasi security) selesai → Finding &
// Incident leluhur yang seluruh pekerjaan turunannya sudah selesai di-resolve otomatis (P2-XTW-02, §17 contoh 1–2).
func (s *Service) resolveAncestorsAfterFollowUp(ctx context.Context, tx pgx.Tx, task *WorkItem) error {
	p := authctx.Must(ctx)
	curType, curID := task.SourceType, task.SourceID
	for depth := 0; depth < 8 && curType != nil && curID != nil; depth++ {
		table, ok := chainTables[*curType]
		if !ok {
			return nil
		}
		var status, number string
		var propertyID uuid.UUID
		var nextType *string
		var nextID *uuid.UUID
		numberCol := map[string]string{ObjTask: "task_number", ObjWorkOrder: "work_order_number", ObjFinding: "finding_number", ObjIncident: "incident_number"}[*curType]
		if err := tx.QueryRow(ctx, `SELECT status, `+numberCol+`, property_id, source_type, source_id FROM `+table+` WHERE id = $1`, *curID).Scan(&status, &number, &propertyID, &nextType, &nextID); err != nil {
			if db.IsNoRows(err) {
				return nil
			}
			return err
		}
		switch *curType {
		case ObjFinding, ObjIncident:
			pending := (*curType == ObjFinding && (status == "open" || status == "in_progress")) || (*curType == ObjIncident && (status == "new" || status == "assigned" || status == "in_progress"))
			if pending {
				open, err := openDescendants(ctx, tx, *curType, *curID)
				if err != nil {
					return err
				}
				if len(open) == 0 {
					res := "Diverifikasi melalui " + task.Number
					if _, err := tx.Exec(ctx, `UPDATE `+table+` SET status = 'resolved', resolution = COALESCE(resolution, $2), resolved_at = now(), updated_by = $3 WHERE id = $1`, *curID, res, actorOrNil(p)); err != nil {
						return err
					}
					if *curType == ObjIncident {
						_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET resolved_at = now() WHERE object_type = 'incident' AND object_id = $1`, *curID)
					}
					_ = audit.Record(ctx, tx, audit.Entry{ObjectType: *curType, ObjectID: *curID, Action: audit.ActStatusChanged, From: status, To: "resolved", Payload: map[string]any{"via": task.Number, "follow_up_purpose": task.FollowUpPurpose}})
					_ = audit.Record(ctx, tx, audit.Entry{ObjectType: *curType, ObjectID: *curID, Action: audit.ActResolved, Payload: map[string]any{"resolution": res, "via": task.Number}})
					if s.Jobs != nil {
						_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: *curType + ".resolved", OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: *curType, ObjectID: *curID, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: map[string]any{"from": status, "to": "resolved", "via": task.Number}})
						_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, *curType, *curID))
					}
				}
			}
		}
		curType, curID = nextType, nextID
	}
	return nil
}

// ---------- Follow-up task dari WO (P1-XMW-03, P2-XTW-01) ----------

// FollowUpPurposes: tujuan Task tindak lanjut → task_type default & awalan judul.
var FollowUpPurposes = map[string]struct{ TaskType, TitlePrefix string }{
	"final_inspection":      {"inspection", "Inspeksi akhir"},
	"re_clean":              {"cleaning", "Re-clean"},
	"security_verification": {"inspection", "Verifikasi security"},
	"inspection":            {"inspection", "Inspeksi"},
	"follow_up":             {"general", "Tindak lanjut"},
}

// FollowUpTaskInput: Task tindak lanjut untuk team lain dari WO yang sudah selesai.
type FollowUpTaskInput struct {
	Purpose             string     `json:"purpose"` // final_inspection | re_clean | security_verification | inspection | follow_up
	Title               string     `json:"title"`
	Description         *string    `json:"description"`
	TaskType            string     `json:"task_type"`
	Priority            string     `json:"priority"`
	LocationID          *uuid.UUID `json:"location_id"`
	ScheduledStartAt    *time.Time `json:"scheduled_start_at"`
	DueAt               *time.Time `json:"due_at"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	RequiresPhoto       bool       `json:"requires_photo"`
	AssigneeUserID      *uuid.UUID `json:"assignee_user_id"`
	AssigneeTeamID      *uuid.UUID `json:"assignee_team_id"`
}

// CreateFollowUpTask: dari WO Completed/Closed → Task tindak lanjut (link follow_up_of, source = WO, rantai SR ikut).
func (s *Service) CreateFollowUpTask(ctx context.Context, workOrderID uuid.UUID, in FollowUpTaskInput) (*WorkItem, error) {
	if in.Purpose == "" {
		in.Purpose = "follow_up"
	}
	fp, ok := FollowUpPurposes[in.Purpose]
	if !ok {
		return nil, apperr.Validation("purpose harus final_inspection|re_clean|security_verification|inspection|follow_up").WithField("purpose", "tidak valid")
	}
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		wo, t, err := s.loadTx(ctx, tx, ObjWorkOrder, workOrderID, false)
		if err != nil {
			return err
		}
		if !canWI(ctx, tx, t.perm("view"), wo) {
			return apperr.Forbidden("")
		}
		if wo.Status != workflow.Completed && wo.Status != workflow.Closed {
			return apperr.Conflict("WORK_ORDER_NOT_COMPLETED", "Task tindak lanjut hanya dari Work Order yang sudah Completed/Closed (status saat ini "+workflow.Label(wo.Status)+")")
		}
		ct := CreateTaskInput{Title: strings.TrimSpace(in.Title), Description: in.Description, TaskType: in.TaskType, Priority: in.Priority, LocationID: in.LocationID,
			ScheduledStartAt: in.ScheduledStartAt, DueAt: in.DueAt, ChecklistTemplateID: in.ChecklistTemplateID, RequiresPhoto: in.RequiresPhoto,
			AssigneeUserID: in.AssigneeUserID, AssigneeTeamID: in.AssigneeTeamID}
		if ct.Title == "" {
			ct.Title = fp.TitlePrefix + " — " + wo.Title
		}
		if ct.TaskType == "" {
			ct.TaskType = fp.TaskType
		}
		if ct.Priority == "" {
			ct.Priority = wo.Priority
		}
		if ct.LocationID == nil {
			ct.LocationID = wo.Location.ID
		}
		if ct.Description == nil {
			d := fmt.Sprintf("Tindak lanjut %s (%s).", wo.Number, wo.Title)
			if wo.Resolution != nil && *wo.Resolution != "" {
				d += " Resolusi: " + *wo.Resolution
			}
			ct.Description = &d
		}
		pid := wo.PropertyID
		ct.PropertyID = &pid
		st := ObjWorkOrder
		ct.SourceType, ct.SourceID = &st, &workOrderID
		ct.LinkTo = &LinkRef{ObjectType: ObjWorkOrder, ObjectID: workOrderID, LinkType: "follow_up_of"}
		id, err := s.CreateTaskTx(ctx, tx, ct)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE tasks SET follow_up_purpose = $2 WHERE id = $1`, id, in.Purpose); err != nil {
			return err
		}
		var number string
		_ = tx.QueryRow(ctx, `SELECT task_number FROM tasks WHERE id = $1`, id).Scan(&number)
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjWorkOrder, ObjectID: workOrderID, Action: "follow_up_created", Payload: map[string]any{"task_id": id, "task_number": number, "purpose": in.Purpose}})
		out, err = s.GetTx(ctx, tx, ObjTask, id)
		return err
	})
	return out, err
}
