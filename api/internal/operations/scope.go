package operations

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

// ---------- Access scope (PRD P0 v2 §8.4): Property · Building/Tower · Resource (vendor) ----------

// canAt: perm pada object di property + lokasi — grant property-wide atau grant ber-scope building yang memuat lokasi.
func canAt(ctx context.Context, q db.Querier, perm string, propertyID uuid.UUID, locationID *uuid.UUID) bool {
	return authctx.Must(ctx).HasOnPropertyAt(perm, propertyID, db.LocationPathFn(ctx, q, locationID))
}

// canWI: canAt + resource scope akun vendor (hanya Work Order milik vendor-nya).
func canWI(ctx context.Context, q db.Querier, perm string, w *WorkItem) bool {
	p := authctx.Must(ctx)
	if p.VendorID != nil && !p.IsSystem {
		if w.ObjectType != ObjWorkOrder || w.VendorID == nil || *w.VendorID != *p.VendorID {
			return false
		}
	}
	return canAt(ctx, q, perm, w.PropertyID, w.Location.ID)
}

// vendorScopeSQL: akun vendor tidak melihat task, dan hanya melihat WO vendor-nya.
func vendorScopeSQL(p *authctx.Principal, objectType, tb string, add func(any) string) string {
	if p.VendorID == nil || p.IsSystem {
		return ""
	}
	if objectType != ObjWorkOrder {
		return " AND FALSE"
	}
	return " AND " + tb + ".vendor_id = " + add(*p.VendorID)
}

// locationScopeSQL: kondisi scope untuk tabel ber-location_id (incidents, findings, assets, service requests).
func locationScopeSQL(p *authctx.Principal, perm, tb string, add func(any) string) string {
	if p.VendorID != nil && !p.IsSystem {
		return "FALSE"
	}
	return p.ScopeSQL(perm, tb+".property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = "+tb+".location_id)", add)
}

// LocationScopeSQL diekspor untuk modul domain (asset, tenantservice).
func LocationScopeSQL(p *authctx.Principal, perm, tb string, add func(any) string) string {
	return locationScopeSQL(p, perm, tb, add)
}

// CanAt diekspor untuk modul domain.
func CanAt(ctx context.Context, q db.Querier, perm string, propertyID uuid.UUID, locationID *uuid.UUID) bool {
	p := authctx.Must(ctx)
	if p.VendorID != nil && !p.IsSystem {
		return false
	}
	return canAt(ctx, q, perm, propertyID, locationID)
}

// ---------- Delete (PRD P0 v2 §8.3: Work Order → Delete) ----------

// deletable: hanya draft yang belum dikerjakan — belum dimulai, tanpa evidence/jawaban checklist/komentar.
func deletable(w *WorkItem) bool {
	return (w.Status == workflow.Draft || w.Status == workflow.New || w.Status == workflow.Scheduled || w.Status == workflow.Assigned || w.Status == workflow.Cancelled) &&
		w.StartedAt == nil && w.AttachmentCount == 0 && w.CommentCount == 0
}

// Delete menghapus draft Task/WO yang dibuat keliru. Riwayat tetap ada di audit log (immutable) berikut snapshot
// lengkap object; object yang sudah berjalan harus di-cancel, bukan dihapus.
func (s *Service) Delete(ctx context.Context, objectType string, id uuid.UUID, reason string) error {
	p := authctx.Must(ctx)
	if strings.TrimSpace(reason) == "" {
		return apperr.Validation("reason wajib untuk menghapus").WithField("reason", "wajib")
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		w, t, err := s.loadTx(ctx, tx, objectType, id, true)
		if err != nil {
			return err
		}
		if !canWI(ctx, tx, t.perm("delete"), w) {
			return apperr.Forbidden("Memerlukan permission " + t.perm("delete"))
		}
		if err := s.enrich(ctx, tx, w, t, true); err != nil {
			return err
		}
		var answered, parts int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM checklist_run_items i JOIN checklist_runs r ON r.id = i.run_id WHERE r.object_type = $1 AND r.object_id = $2 AND i.answered_at IS NOT NULL`, objectType, id).Scan(&answered)
		if objectType == ObjWorkOrder {
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM work_order_parts WHERE work_order_id = $1`, id).Scan(&parts)
		}
		if !deletable(w) || answered > 0 || parts > 0 {
			return apperr.Conflict("NOT_DELETABLE", objectLabel(objectType)+" sudah memiliki aktivitas/evidence; gunakan Cancel")
		}
		// anak tanpa FK cascade: bersihkan eksplisit (link, checklist run, SLA, assignment, activity, dokumen search)
		stmts := []string{
			`DELETE FROM object_links WHERE (from_type = $1 AND from_id = $2) OR (to_type = $1 AND to_id = $2)`,
			`DELETE FROM checklist_run_items WHERE run_id IN (SELECT id FROM checklist_runs WHERE object_type = $1 AND object_id = $2)`,
			`DELETE FROM checklist_runs WHERE object_type = $1 AND object_id = $2`,
			`DELETE FROM sla_tracking WHERE object_type = $1 AND object_id = $2`,
			`DELETE FROM assignments WHERE object_type = $1 AND object_id = $2`,
			`DELETE FROM activities WHERE object_type = $1 AND object_id = $2`,
			`DELETE FROM search_documents WHERE object_type = $1 AND object_id = $2`,
			`DELETE FROM notifications WHERE object_type = $1 AND object_id = $2`,
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q, objectType, id); err != nil {
				return err
			}
		}
		if objectType == ObjTask {
			for _, q := range []string{`DELETE FROM patrol_tasks WHERE task_id = $1`, `DELETE FROM cleaning_tasks WHERE task_id = $1`} {
				if _, err := tx.Exec(ctx, q, id); err != nil {
					return err
				}
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE maintenance_schedules SET work_order_id = NULL, status = CASE WHEN status IN ('in_progress','completed') THEN 'due' ELSE status END WHERE work_order_id = $1`, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM `+t.table+` WHERE id = $1`, id); err != nil {
			if strings.Contains(err.Error(), "foreign key") {
				return apperr.Conflict("NOT_DELETABLE", objectLabel(objectType)+" masih direferensikan data lain; gunakan Cancel")
			}
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: objectType, EntityID: &id, EntityLabel: w.Number,
			Before: w, After: map[string]any{"reason": reason, "deleted_by": p.UserID}})
	})
}

// ---------- Task → Work Order (PRD P0 v2 §11: Work Order dikaitkan dengan Task) ----------

// CreateWorkOrderFromTask: WO baru dari task (lokasi/asset/priority diturunkan) + link generated_from dua arah.
func (s *Service) CreateWorkOrderFromTask(ctx context.Context, taskID uuid.UUID, in CreateWorkOrderInput) (*WorkItem, error) {
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		task, t, err := s.loadTx(ctx, tx, ObjTask, taskID, false)
		if err != nil {
			return err
		}
		if !canWI(ctx, tx, t.perm("view"), task) {
			return apperr.Forbidden("")
		}
		if in.WorkOrderType == "" {
			in.WorkOrderType = "corrective"
		}
		if strings.TrimSpace(in.Title) == "" {
			in.Title = task.Title
		}
		if in.Description == nil {
			in.Description = task.Description
		}
		if in.LocationID == nil {
			in.LocationID = task.Location.ID
		}
		if in.AssetID == nil {
			in.AssetID = task.Asset.ID
		}
		if in.Priority == "" {
			in.Priority = task.Priority
		}
		pid := task.PropertyID
		in.PropertyID = &pid
		st := ObjTask
		in.SourceType = &st
		in.SourceID = &taskID
		in.LinkTo = &LinkRef{ObjectType: ObjTask, ObjectID: taskID, LinkType: "generated_from"}
		id, err := s.CreateWorkOrderTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.GetTx(ctx, tx, ObjWorkOrder, id)
		return err
	})
	return out, err
}
