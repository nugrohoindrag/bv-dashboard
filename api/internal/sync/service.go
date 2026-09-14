// Package sync: Offline-Lite (PRD §21, AT-009; TAD §6.5–6.6, OD-004/TD-005) — work bundle pull, mutation push
// dengan aturan konflik deterministik C1–C10 (server-authoritative, evidence selalu dipertahankan), daftar konflik.
package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/storage"
	"github.com/buildingvision/api/internal/security"
)

type Service struct {
	DB          *db.DB
	Jobs        jobs.Enqueuer
	Ops         *operations.Service
	Security    *security.Service
	Attachments *attachments.Service
	Storage     storage.Storage
	ClockSkew   time.Duration // C8: default 10 menit
}

// ---------- Pull: work bundle ----------

type Bundle struct {
	ServerTime    time.Time                 `json:"server_time"`
	Cursor        string                    `json:"cursor"`
	Tasks         []operations.WorkItem     `json:"tasks"`
	WorkOrders    []operations.WorkItem     `json:"work_orders"`
	PatrolTasks   []PatrolBundle            `json:"patrol_tasks"`
	CleaningTasks []operations.WorkItem     `json:"cleaning_tasks"`
	ChecklistRuns []operations.ChecklistRun `json:"checklist_runs"`
	Locations     []LocationLite            `json:"locations"`
	Assets        []AssetLite               `json:"assets"`
	Master        Master                    `json:"master"`
	Removed       []ObjectRef               `json:"removed"`
	Me            MeLite                    `json:"me"`
}

type PatrolBundle struct {
	Task        operations.WorkItem `json:"task"`
	Checkpoints []security.Scan     `json:"checkpoints"`
}
type LocationLite struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Type     string    `json:"location_type"`
	PathText string    `json:"path_text"`
	QRCode   *string   `json:"qr_code"`
}
type AssetLite struct {
	ID         uuid.UUID `json:"id"`
	AssetCode  string    `json:"asset_code"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	LocationID uuid.UUID `json:"location_id"`
	QRCode     *string   `json:"qr_code"`
}
type Master struct {
	FindingCategories  []string `json:"finding_categories"`
	IncidentCategories []string `json:"incident_categories"`
	Priorities         []string `json:"priorities"`
	Severities         []string `json:"severities"`
	GPSStatuses        []string `json:"gps_statuses"`
}
type ObjectRef struct {
	ObjectType string    `json:"object_type"`
	ObjectID   uuid.UUID `json:"object_id"`
}
type MeLite struct {
	UserID      uuid.UUID   `json:"user_id"`
	FullName    string      `json:"full_name"`
	Roles       []string    `json:"roles"`
	TeamIDs     []uuid.UUID `json:"team_ids"`
	Permissions []string    `json:"permissions"`
}

// WorkBundle: work item hari ini (tz property) + open overdue milik user/team + referensi (TAD §6.5).
func (s *Service) WorkBundle(ctx context.Context, deviceID string, since string) (*Bundle, error) {
	p := authctx.Must(ctx)
	now := time.Now()
	b := &Bundle{ServerTime: now.UTC(), Tasks: []operations.WorkItem{}, WorkOrders: []operations.WorkItem{}, PatrolTasks: []PatrolBundle{}, CleaningTasks: []operations.WorkItem{}, ChecklistRuns: []operations.ChecklistRun{}, Locations: []LocationLite{}, Assets: []AssetLite{}, Removed: []ObjectRef{}}
	page := httpx.Page{Limit: 500}
	mine := operations.ListFilter{Mine: true, ScheduledOn: &now, Sort: "due_at"}
	overdue := true
	openMine := operations.ListFilter{Mine: true, Overdue: &overdue, Sort: "due_at"}
	collect := func(ot string) ([]operations.WorkItem, error) {
		seen := map[uuid.UUID]bool{}
		var out []operations.WorkItem
		for _, f := range []operations.ListFilter{mine, openMine} {
			items, _, err := s.Ops.List(ctx, ot, f, page)
			if err != nil {
				return nil, err
			}
			for _, it := range items {
				if seen[it.ID] || it.Status == workflow.Closed || it.Status == workflow.Cancelled {
					continue
				}
				seen[it.ID] = true
				full, err := s.Ops.Get(ctx, ot, it.ID)
				if err != nil {
					continue
				}
				out = append(out, *full)
			}
		}
		return out, nil
	}
	tasks, err := collect(operations.ObjTask)
	if err != nil {
		return nil, err
	}
	wos, err := collect(operations.ObjWorkOrder)
	if err != nil {
		return nil, err
	}
	b.WorkOrders = wos
	locSet := map[uuid.UUID]bool{}
	assetSet := map[uuid.UUID]bool{}
	var currentIDs []ObjectRef
	for _, t := range tasks {
		switch t.Type {
		case "patrol":
			scans, _ := s.Security.ListScans(ctx, t.ID)
			b.PatrolTasks = append(b.PatrolTasks, PatrolBundle{Task: t, Checkpoints: scans})
		case "cleaning":
			b.CleaningTasks = append(b.CleaningTasks, t)
		default:
			b.Tasks = append(b.Tasks, t)
		}
		currentIDs = append(currentIDs, ObjectRef{"task", t.ID})
		if t.Location.ID != nil {
			locSet[*t.Location.ID] = true
		}
		if t.Asset.ID != nil {
			assetSet[*t.Asset.ID] = true
		}
		runs, _ := s.Ops.ListRuns(ctx, operations.ObjTask, t.ID)
		b.ChecklistRuns = append(b.ChecklistRuns, runs...)
	}
	for _, w := range wos {
		currentIDs = append(currentIDs, ObjectRef{"work_order", w.ID})
		if w.Location.ID != nil {
			locSet[*w.Location.ID] = true
		}
		if w.Asset.ID != nil {
			assetSet[*w.Asset.ID] = true
		}
		runs, _ := s.Ops.ListRuns(ctx, operations.ObjWorkOrder, w.ID)
		b.ChecklistRuns = append(b.ChecklistRuns, runs...)
	}
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for id := range locSet {
			var l LocationLite
			if err := tx.QueryRow(ctx, `SELECT id, name, location_type, qr_code FROM locations WHERE id = $1`, id).Scan(&l.ID, &l.Name, &l.Type, &l.QRCode); err == nil {
				_ = tx.QueryRow(ctx, `SELECT string_agg(a.name, ' / ' ORDER BY a.depth) FROM locations l JOIN locations a ON a.path @> l.path AND a.depth > 0 WHERE l.id = $1`, id).Scan(&l.PathText)
				b.Locations = append(b.Locations, l)
			}
		}
		for id := range assetSet {
			var a AssetLite
			if err := tx.QueryRow(ctx, `SELECT id, asset_code, name, status, location_id, qr_code FROM assets WHERE id = $1`, id).Scan(&a.ID, &a.AssetCode, &a.Name, &a.Status, &a.LocationID, &a.QRCode); err == nil {
				b.Assets = append(b.Assets, a)
			}
		}
		// removed: object yang ada di bundle sebelumnya (device ini) tapi tidak lagi
		var prev []byte
		_ = tx.QueryRow(ctx, `SELECT bundle_object_ids FROM sync_cursors WHERE user_id = $1 AND device_id = $2`, p.UserID, deviceID).Scan(&prev)
		var prevRefs []ObjectRef
		_ = json.Unmarshal(prev, &prevRefs)
		cur := map[string]bool{}
		for _, r := range currentIDs {
			cur[r.ObjectType+":"+r.ObjectID.String()] = true
		}
		for _, r := range prevRefs {
			if !cur[r.ObjectType+":"+r.ObjectID.String()] {
				b.Removed = append(b.Removed, r)
			}
		}
		curJSON, _ := json.Marshal(currentIDs)
		_, err := tx.Exec(ctx, `INSERT INTO sync_cursors (user_id, device_id, cursor_at, bundle_object_ids) VALUES ($1,$2,$3,$4) ON CONFLICT (user_id, device_id) DO UPDATE SET cursor_at = EXCLUDED.cursor_at, bundle_object_ids = EXCLUDED.bundle_object_ids`, p.UserID, deviceID, now.UTC(), curJSON)
		return err
	})
	if err != nil {
		return nil, err
	}
	b.Master = Master{
		FindingCategories:  []string{"water_leakage", "fire_exit_blocked", "odor", "damage", "dirty", "lighting", "safety_hazard", "other"},
		IncidentCategories: operations.IncidentCategories,
		Priorities:         []string{"low", "medium", "high", "critical"},
		Severities:         []string{"low", "medium", "high", "critical"},
		GPSStatuses:        []string{"captured", "unavailable", "denied"},
	}
	b.Me = MeLite{UserID: p.UserID, FullName: p.FullName, Roles: p.RoleCodes, TeamIDs: p.TeamIDs, Permissions: p.AllPermissions()}
	b.Cursor = now.UTC().Format(time.RFC3339Nano)
	return b, nil
}

// ---------- Push: mutations ----------

type Mutation struct {
	ClientMutationID uuid.UUID       `json:"client_mutation_id"`
	ObjectType       string          `json:"object_type"`
	ObjectID         uuid.UUID       `json:"object_id"`
	Action           string          `json:"action"` // start | hold | resume | checklist_item_result | checkpoint_scan | add_comment | attach_photo | add_finding | complete
	Payload          json.RawMessage `json:"payload"`
	ClientTime       *time.Time      `json:"client_time"`
	Seq              int64           `json:"seq"`
}

type PushInput struct {
	DeviceID  string     `json:"device_id"`
	Mutations []Mutation `json:"mutations"`
}

type MutationResult struct {
	ClientMutationID uuid.UUID       `json:"client_mutation_id"`
	Status           string          `json:"status"` // applied | duplicate | rejected | conflict
	ReasonCode       string          `json:"reason_code,omitempty"`
	Detail           string          `json:"detail,omitempty"`
	ServerVersion    *int            `json:"server_version,omitempty"`
	Response         json.RawMessage `json:"response,omitempty"` // mis. attachment_id + upload_url untuk attach_photo
}

type PushOutput struct {
	Results    []MutationResult `json:"results"`
	ServerTime time.Time        `json:"server_time"`
}

const (
	ReasonInvalidTransition = "INVALID_TRANSITION"
	ReasonObjectTerminal    = "OBJECT_TERMINAL"
	ReasonReassigned        = "REASSIGNED"
	ReasonDuplicateSession  = "DUPLICATE_SESSION"
	ReasonSeqGap            = "SEQ_GAP"
	ReasonValidation        = "VALIDATION_ERROR"
	ReasonForbidden         = "FORBIDDEN"
	ReasonNotFound          = "NOT_FOUND"
)

// Push: diproses berurutan per seq per object; satu transaksi per mutation; idempotent via client_mutation_id.
func (s *Service) Push(ctx context.Context, in PushInput) (*PushOutput, error) {
	p := authctx.Must(ctx)
	if in.DeviceID == "" {
		return nil, apperr.Validation("device_id wajib")
	}
	if len(in.Mutations) > 200 {
		return nil, apperr.Validation("maksimal 200 mutation per batch")
	}
	out := &PushOutput{Results: make([]MutationResult, 0, len(in.Mutations)), ServerTime: time.Now().UTC()}
	// urutkan per seq (stabil) tanpa mengubah urutan objek
	lastSeq := map[uuid.UUID]int64{}
	blocked := map[uuid.UUID]bool{} // C9: object diblokir setelah SEQ_GAP dalam batch
	for _, m := range in.Mutations {
		res := MutationResult{ClientMutationID: m.ClientMutationID}
		if m.ClientMutationID == uuid.Nil || m.ObjectID == uuid.Nil || m.Action == "" {
			res.Status, res.ReasonCode, res.Detail = "rejected", ReasonValidation, "client_mutation_id, object_id, action wajib"
			out.Results = append(out.Results, res)
			continue
		}
		// C6 duplicate
		if prev := s.previousResult(ctx, p.OrganizationID, m.ClientMutationID); prev != nil {
			out.Results = append(out.Results, *prev)
			continue
		}
		// C9 seq monoton per (device, object)
		if blocked[m.ObjectID] {
			res.Status, res.ReasonCode, res.Detail = "rejected", ReasonSeqGap, "mutation sebelumnya untuk object ini gagal urutan (SEQ_GAP); kirim ulang dari mutation yang hilang"
			s.record(ctx, p, in.DeviceID, m, res, nil)
			out.Results = append(out.Results, res)
			continue
		}
		last, ok := lastSeq[m.ObjectID]
		if !ok {
			_ = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) FROM sync_mutations WHERE device_id = $1 AND object_id = $2 AND result IN ('applied','conflict','duplicate')`, in.DeviceID, m.ObjectID).Scan(&last)
			})
		}
		if m.Seq <= last {
			res.Status, res.ReasonCode, res.Detail = "rejected", ReasonSeqGap, fmt.Sprintf("seq %d tidak monoton (terakhir %d)", m.Seq, last)
			blocked[m.ObjectID] = true
			s.record(ctx, p, in.DeviceID, m, res, nil)
			out.Results = append(out.Results, res)
			continue
		}
		lastSeq[m.ObjectID] = m.Seq
		res = s.applyOne(ctx, p, in.DeviceID, m)
		out.Results = append(out.Results, res)
	}
	return out, nil
}

func (s *Service) previousResult(ctx context.Context, orgID uuid.UUID, id uuid.UUID) *MutationResult {
	var r MutationResult
	var resp []byte
	var reason, detail *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT result, reason_code, reason_detail, server_version, response FROM sync_mutations WHERE client_mutation_id = $1`, id).Scan(&r.Status, &reason, &detail, &r.ServerVersion, &resp)
	})
	if err != nil {
		return nil
	}
	if reason != nil {
		r.ReasonCode = *reason
	}
	if detail != nil {
		r.Detail = *detail
	}
	r.ClientMutationID = id
	if r.Status == "applied" {
		r.Status = "duplicate"
	}
	if len(resp) > 0 {
		r.Response = resp
	}
	return &r
}

// record menyimpan hasil ke sync_mutations (transaksi terpisah agar tetap tercatat meski apply gagal).
func (s *Service) record(ctx context.Context, p *authctx.Principal, deviceID string, m Mutation, res MutationResult, tx pgx.Tx) {
	do := func(ctx context.Context, tx pgx.Tx) error {
		var resp []byte
		if len(res.Response) > 0 {
			resp = res.Response
		}
		payload := m.Payload
		if len(payload) == 0 {
			payload = json.RawMessage("{}")
		}
		_, err := tx.Exec(ctx, `INSERT INTO sync_mutations (client_mutation_id, organization_id, device_id, user_id, object_type, object_id, action, seq, payload, client_time, result, reason_code, reason_detail, server_version, response)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''),NULLIF($13,''),$14,$15) ON CONFLICT (client_mutation_id) DO NOTHING`,
			m.ClientMutationID, p.OrganizationID, deviceID, p.UserID, m.ObjectType, m.ObjectID, m.Action, m.Seq, payload, m.ClientTime, res.Status, res.ReasonCode, res.Detail, res.ServerVersion, nullBytes(resp))
		return err
	}
	if tx != nil {
		_ = do(ctx, tx)
		return
	}
	_ = s.DB.WithTx(ctx, do)
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// applyOne: satu transaksi; validasi terhadap state server; klasifikasi hasil C1–C10.
func (s *Service) applyOne(ctx context.Context, p *authctx.Principal, deviceID string, m Mutation) MutationResult {
	res := MutationResult{ClientMutationID: m.ClientMutationID}
	ctx = authctx.With(ctx, withSource(p, authctx.SourceSync))
	// C8 clock skew
	skew := false
	if m.ClientTime != nil && s.ClockSkew > 0 {
		d := time.Since(*m.ClientTime)
		if d > s.ClockSkew || d < -s.ClockSkew {
			skew = true
		}
	}
	var resp any
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if skew {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: m.ObjectType, ObjectID: m.ObjectID, Action: audit.ActClockSkew, Payload: map[string]any{"client_time": m.ClientTime, "action": m.Action}, ClientRecordedAt: m.ClientTime})
		}
		var err error
		resp, err = s.dispatch(ctx, tx, m)
		if err != nil {
			return err
		}
		res.Status = "applied"
		res.ServerVersion = s.version(ctx, tx, m.ObjectType, m.ObjectID)
		if resp != nil {
			b, _ := json.Marshal(resp)
			res.Response = b
		}
		s.record(ctx, p, deviceID, m, res, tx)
		return nil
	})
	if err == nil {
		return res
	}
	// klasifikasi kegagalan
	ae := apperr.From(err)
	switch {
	case ae.Code == "OBJECT_TERMINAL":
		res.Status, res.ReasonCode = "conflict", ReasonObjectTerminal
	case ae.Code == "WORKFLOW_INVALID_TRANSITION" || ae.Code == "WORKFLOW_GUARD_FAILED":
		res.Status, res.ReasonCode = "conflict", ReasonInvalidTransition
		if s.isReassigned(ctx, p, m) {
			res.ReasonCode = ReasonReassigned
		} else if m.Action == "start" && s.startedBySameUser(ctx, p, m) {
			res.ReasonCode = ReasonDuplicateSession
		}
	case ae.Status == 403:
		if s.isReassigned(ctx, p, m) {
			res.Status, res.ReasonCode = "conflict", ReasonReassigned
		} else {
			res.Status, res.ReasonCode = "rejected", ReasonForbidden
		}
	case ae.Status == 404:
		res.Status, res.ReasonCode = "rejected", ReasonNotFound
	case ae.Status == 400:
		res.Status, res.ReasonCode = "rejected", ReasonValidation
	default:
		res.Status, res.ReasonCode = "rejected", ReasonValidation
		res.Detail = "internal: " + ae.Detail
	}
	if res.Detail == "" {
		res.Detail = ae.Detail
	}
	// conflict → evidence tetap disimpan (C2/C3/C4/C5) + notifikasi supervisor + audit
	if res.Status == "conflict" {
		_ = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			s.preserveEvidence(ctx, tx, m, res.ReasonCode)
			s.record(ctx, p, deviceID, m, res, tx)
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditSyncConflict, EntityType: m.ObjectType, EntityID: &m.ObjectID, EntityLabel: m.Action, After: map[string]any{"reason": res.ReasonCode, "client_mutation_id": m.ClientMutationID}})
			if s.Jobs != nil {
				var prop *uuid.UUID
				if t := tableOf(m.ObjectType); t != "" {
					var pid uuid.UUID
					if tx.QueryRow(ctx, `SELECT property_id FROM `+t+` WHERE id = $1`, m.ObjectID).Scan(&pid) == nil {
						prop = &pid
					}
				}
				label, _, _ := operations.DescribeObject(ctx, tx, m.ObjectType, m.ObjectID)
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.SyncConflict, OrganizationID: p.OrganizationID, PropertyID: prop, ObjectType: m.ObjectType, ObjectID: m.ObjectID, ObjectLabel: label, ActorUserID: &p.UserID,
					Payload: map[string]any{"reason": res.ReasonCode, "action": m.Action, "client_mutation_id": m.ClientMutationID, "assignee_user_id": p.UserID}})
			}
			return nil
		})
		return res
	}
	s.record(ctx, p, deviceID, m, res, nil)
	return res
}

func tableOf(ot string) string {
	return map[string]string{"task": "tasks", "work_order": "work_orders", "incident": "incidents", "finding": "findings", "service_request": "service_requests"}[ot]
}

func withSource(p *authctx.Principal, src authctx.Source) *authctx.Principal {
	cl := *p
	cl.Source = src
	return &cl
}

func (s *Service) version(ctx context.Context, tx pgx.Tx, ot string, id uuid.UUID) *int {
	t := tableOf(ot)
	if t == "" {
		return nil
	}
	var v int
	if err := tx.QueryRow(ctx, `SELECT version FROM `+t+` WHERE id = $1`, id).Scan(&v); err != nil {
		return nil
	}
	return &v
}

func (s *Service) isReassigned(ctx context.Context, p *authctx.Principal, m Mutation) bool {
	t := tableOf(m.ObjectType)
	if t == "" || (m.ObjectType != "task" && m.ObjectType != "work_order") {
		return false
	}
	var au, at *uuid.UUID
	_ = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT assignee_user_id, assignee_team_id FROM `+t+` WHERE id = $1`, m.ObjectID).Scan(&au, &at)
	})
	if au != nil && *au == p.UserID {
		return false
	}
	if at != nil && p.IsMemberOfTeam(*at) {
		return false
	}
	return au != nil || at != nil
}

func (s *Service) startedBySameUser(ctx context.Context, p *authctx.Principal, m Mutation) bool {
	var actor *uuid.UUID
	_ = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT actor_user_id FROM activities WHERE object_type = $1 AND object_id = $2 AND action = 'status_changed' AND to_value = 'in_progress' ORDER BY occurred_at DESC LIMIT 1`, m.ObjectType, m.ObjectID).Scan(&actor)
	})
	return actor != nil && *actor == p.UserID
}

// preserveEvidence (prinsip 3 OD-004): payload mutation yang conflict tetap masuk activity timeline (evidence_recorded_offline / late_evidence),
// checklist result & foto disimpan bila memungkinkan.
func (s *Service) preserveEvidence(ctx context.Context, tx pgx.Tx, m Mutation, reason string) {
	var payload map[string]any
	_ = json.Unmarshal(m.Payload, &payload)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["action"] = m.Action
	payload["reason_code"] = reason
	payload["client_mutation_id"] = m.ClientMutationID
	act := audit.ActEvidenceOffline
	if reason == ReasonObjectTerminal {
		act = audit.ActLateEvidence
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: m.ObjectType, ObjectID: m.ObjectID, Action: act, Payload: payload, ClientRecordedAt: m.ClientTime})
	// checklist result: simpan nilai langsung tanpa guard status (evidence)
	if m.Action == "checklist_item_result" {
		var in struct {
			ItemID       uuid.UUID  `json:"item_id"`
			ResultValue  *string    `json:"result_value"`
			ResultNumber *float64   `json:"result_number"`
			ResultText   *string    `json:"result_text"`
			AttachmentID *uuid.UUID `json:"attachment_id"`
			Note         *string    `json:"note"`
		}
		if json.Unmarshal(m.Payload, &in) == nil && in.ItemID != uuid.Nil {
			p := authctx.Must(ctx)
			_, _ = tx.Exec(ctx, `UPDATE checklist_run_items SET result_value = COALESCE($2, result_value), result_number = COALESCE($3, result_number), result_text = COALESCE($4, result_text), attachment_id = COALESCE($5, attachment_id), note = COALESCE($6, note), answered_by = $7, answered_at = now(), answered_source = 'sync' WHERE id = $1 AND answered_at IS NULL`,
				in.ItemID, in.ResultValue, in.ResultNumber, in.ResultText, in.AttachmentID, in.Note, p.UserID)
		}
	}
	if m.Action == "attach_photo" {
		_, _ = s.attachPhoto(ctx, tx, m) // pending attachment tetap dibuat agar file bisa diunggah
	}
	if m.Action == "add_comment" {
		var in struct {
			Body string `json:"body"`
		}
		if json.Unmarshal(m.Payload, &in) == nil && in.Body != "" {
			_, _ = s.Ops.AddCommentTx(ctx, tx, m.ObjectType, m.ObjectID, in.Body, strPtr(m.ClientMutationID.String()), m.ClientTime, true)
		}
	}
}

func strPtr(s string) *string { return &s }

// dispatch: action → service call (semua FromSync=true).
func (s *Service) dispatch(ctx context.Context, tx pgx.Tx, m Mutation) (any, error) {
	switch m.Action {
	case "start", "hold", "resume", "complete":
		var in operations.TransitionInput
		if len(m.Payload) > 0 {
			if err := json.Unmarshal(m.Payload, &in); err != nil {
				return nil, apperr.Validation("payload tidak valid")
			}
		}
		in.FromSync = true
		in.ClientRecordedAt = m.ClientTime
		if m.ObjectType != operations.ObjTask && m.ObjectType != operations.ObjWorkOrder {
			return nil, apperr.Validation("object_type harus task|work_order")
		}
		w, err := s.Ops.TransitionTx(ctx, tx, m.ObjectType, m.ObjectID, m.Action, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": w.Status}, nil
	case "checklist_item_result":
		var in struct {
			ItemID uuid.UUID `json:"item_id"`
			operations.AnswerInput
		}
		if err := json.Unmarshal(m.Payload, &in); err != nil || in.ItemID == uuid.Nil {
			return nil, apperr.Validation("payload.item_id wajib")
		}
		in.FromSync = true
		in.ClientRecordedAt = m.ClientTime
		runID, err := s.Ops.AnswerItemTx(ctx, tx, in.ItemID, in.AnswerInput)
		if err != nil {
			return nil, err
		}
		return map[string]any{"run_id": runID}, nil
	case "checkpoint_scan":
		var in security.ScanInput
		if err := json.Unmarshal(m.Payload, &in); err != nil {
			return nil, apperr.Validation("payload tidak valid")
		}
		in.FromSync = true
		in.ClientRecordedAt = m.ClientTime
		if in.ClientScanID == nil {
			id := m.ClientMutationID.String()
			in.ClientScanID = &id
		}
		return nil, s.Security.ScanCheckpointTx(ctx, tx, m.ObjectID, in)
	case "add_comment":
		var in struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(m.Payload, &in); err != nil || in.Body == "" {
			return nil, apperr.Validation("payload.body wajib")
		}
		c, err := s.Ops.AddCommentTx(ctx, tx, m.ObjectType, m.ObjectID, in.Body, strPtr(m.ClientMutationID.String()), m.ClientTime, true)
		if err != nil {
			return nil, err
		}
		return map[string]any{"comment_id": c.ID}, nil
	case "attach_photo":
		return s.attachPhoto(ctx, tx, m)
	case "add_finding":
		var in operations.CreateFindingInput
		if err := json.Unmarshal(m.Payload, &in); err != nil {
			return nil, apperr.Validation("payload tidak valid")
		}
		in.FromSync = true
		in.ClientRecordedAt = m.ClientTime
		if in.SourceType == nil {
			in.SourceType = strPtr(m.ObjectType)
			in.SourceID = &m.ObjectID
		}
		id, err := s.Ops.CreateFindingTx(ctx, tx, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"finding_id": id}, nil
	case "report_incident":
		var in operations.CreateIncidentInput
		if err := json.Unmarshal(m.Payload, &in); err != nil {
			return nil, apperr.Validation("payload tidak valid")
		}
		in.FromSync = true
		in.ClientRecordedAt = m.ClientTime
		id, err := s.Ops.CreateIncidentTx(ctx, tx, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"incident_id": id}, nil
	}
	return nil, apperr.Validation("action tidak dikenal: " + m.Action)
}

// attachPhoto: mutation hanya metadata + client_attachment_id; file diunggah lewat presign setelah diterima (TAD §6.5).
func (s *Service) attachPhoto(ctx context.Context, tx pgx.Tx, m Mutation) (any, error) {
	var in struct {
		ClientAttachmentID string     `json:"client_attachment_id"`
		AttachmentType     string     `json:"attachment_type"`
		ContentType        string     `json:"content_type"`
		SizeBytes          int64      `json:"size_bytes"`
		SHA256             *string    `json:"sha256"`
		CapturedAt         *time.Time `json:"captured_at"`
		GPSLat             *float64   `json:"gps_lat"`
		GPSLng             *float64   `json:"gps_lng"`
		GPSStatus          string     `json:"gps_status"`
		Caption            *string    `json:"caption"`
	}
	if err := json.Unmarshal(m.Payload, &in); err != nil || in.ClientAttachmentID == "" {
		return nil, apperr.Validation("payload.client_attachment_id wajib")
	}
	if in.AttachmentType == "" {
		in.AttachmentType = "photo"
	}
	if in.ContentType == "" {
		in.ContentType = "image/jpeg"
	}
	if in.SizeBytes <= 0 {
		in.SizeBytes = 1
	}
	p := authctx.Must(ctx)
	// idempotent
	var id uuid.UUID
	var key string
	err := tx.QueryRow(ctx, `SELECT id, storage_key FROM attachments WHERE uploaded_by = $1 AND client_attachment_id = $2`, p.UserID, in.ClientAttachmentID).Scan(&id, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.Ops.ObjectAccess(ctx, tx, m.ObjectType, m.ObjectID, false); err != nil {
			return nil, err
		}
		id = uuid.Must(uuid.NewV7())
		now := time.Now().UTC()
		key = storage.ObjectKey(p.OrganizationID.String(), id.String(), now, attachments.ExtFor(in.ContentType))
		gps := in.GPSStatus
		if gps == "" {
			gps = "unavailable"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO attachments (id, organization_id, object_type, object_id, attachment_type, storage_key, content_type, size_bytes, sha256, captured_at, uploaded_by, gps_lat, gps_lng, gps_status, device_id, client_attachment_id, caption, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULL,$15,$16,'pending')`,
			id, p.OrganizationID, m.ObjectType, m.ObjectID, in.AttachmentType, key, in.ContentType, in.SizeBytes, in.SHA256, in.CapturedAt, p.UserID, in.GPSLat, in.GPSLng, gps, in.ClientAttachmentID, in.Caption); err != nil {
			return nil, err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: m.ObjectType, ObjectID: m.ObjectID, Action: audit.ActAttachmentAdded, Payload: map[string]any{"attachment_id": id, "attachment_type": in.AttachmentType, "gps_status": gps, "pending_upload": true}, ClientRecordedAt: in.CapturedAt})
	} else if err != nil {
		return nil, err
	}
	url, err := s.Storage.PresignPut(ctx, key, in.ContentType, in.SizeBytes, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	return map[string]any{"attachment_id": id, "upload_url": url, "storage_key": key, "confirm_path": "/api/v1/attachments/" + id.String() + "/confirm"}, nil
}

// ---------- Conflicts (supervisor) ----------

type Conflict struct {
	ClientMutationID uuid.UUID       `json:"client_mutation_id"`
	ObjectType       string          `json:"object_type"`
	ObjectID         uuid.UUID       `json:"object_id"`
	ObjectLabel      string          `json:"object_label"`
	ObjectTitle      string          `json:"object_title"`
	ObjectStatus     string          `json:"object_status"`
	Action           string          `json:"action"`
	ReasonCode       *string         `json:"reason_code"`
	Detail           *string         `json:"detail"`
	WorkerID         uuid.UUID       `json:"worker_id"`
	WorkerName       string          `json:"worker_name"`
	DeviceID         string          `json:"device_id"`
	Payload          json.RawMessage `json:"payload"`
	ClientTime       *time.Time      `json:"client_time"`
	ReceivedAt       time.Time       `json:"received_at"`
	AcknowledgedAt   *time.Time      `json:"acknowledged_at"`
	DeepLink         string          `json:"deep_link"`
}

func (s *Service) ListConflicts(ctx context.Context, propertyID *uuid.UUID, includeAcked bool) ([]Conflict, error) {
	p := authctx.Must(ctx)
	var out []Conflict
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT sm.client_mutation_id, sm.object_type, sm.object_id, sm.action, sm.reason_code, sm.reason_detail, sm.user_id, u.full_name, sm.device_id, sm.payload::text, sm.client_time, sm.received_at, sm.acknowledged_at
			FROM sync_mutations sm JOIN users u ON u.id = sm.user_id WHERE sm.result = 'conflict' AND ($1::bool OR sm.acknowledged_at IS NULL) ORDER BY sm.received_at DESC LIMIT 200`, includeAcked)
		if err != nil {
			return err
		}
		var list []Conflict
		for rows.Next() {
			var c Conflict
			var payload string
			if err := rows.Scan(&c.ClientMutationID, &c.ObjectType, &c.ObjectID, &c.Action, &c.ReasonCode, &c.Detail, &c.WorkerID, &c.WorkerName, &c.DeviceID, &payload, &c.ClientTime, &c.ReceivedAt, &c.AcknowledgedAt); err != nil {
				rows.Close()
				return fmt.Errorf("scan conflict: %w", err)
			}
			c.Payload = json.RawMessage(payload)
			list = append(list, c)
		}
		rows.Close()
		for _, c := range list {
			// scope property
			if t := tableOf(c.ObjectType); t != "" {
				var pid uuid.UUID
				if err := tx.QueryRow(ctx, `SELECT property_id FROM `+t+` WHERE id = $1`, c.ObjectID).Scan(&pid); err == nil {
					if propertyID != nil && pid != *propertyID {
						continue
					}
					if !p.HasOnProperty("sync.conflicts.view", pid) {
						continue
					}
				}
			}
			c.ObjectLabel, c.ObjectTitle, c.ObjectStatus = operations.DescribeObject(ctx, tx, c.ObjectType, c.ObjectID)
			c.DeepLink = map[string]string{"task": "/operations/tasks/", "work_order": "/operations/work-orders/"}[c.ObjectType] + c.ObjectID.String()
			out = append(out, c)
		}
		return nil
	})
	if out == nil {
		out = []Conflict{}
	}
	return out, err
}

func (s *Service) AcknowledgeConflict(ctx context.Context, id uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var ot string
		var oid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT object_type, object_id FROM sync_mutations WHERE client_mutation_id = $1 AND result = 'conflict'`, id).Scan(&ot, &oid); err != nil {
			return apperr.NotFound("Sync conflict")
		}
		if t := tableOf(ot); t != "" {
			var pid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT property_id FROM `+t+` WHERE id = $1`, oid).Scan(&pid); err == nil && !p.HasOnProperty("sync.conflicts.acknowledge", pid) {
				return apperr.Forbidden("")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE sync_mutations SET acknowledged_by = $2, acknowledged_at = now() WHERE client_mutation_id = $1`, id, p.UserID); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ot, ObjectID: oid, Action: "sync_conflict_acknowledged", Payload: map[string]any{"client_mutation_id": id}})
		return nil
	})
}
