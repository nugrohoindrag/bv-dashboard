package asset

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/events"
)

// ---------- Equipment Health (PRD P2 v2.1 §5.6 P2-EQH-02..04; NC §14; keputusan D-P2-01 P2, D-P2-02 tanpa downtime) ----------
// Skor 0–100 dari aturan yang dapat ditelusuri (Principle 9 & 12): setiap pengurang punya kode, jumlah, poin, dan tautan
// drill-down ke record penyebabnya. Status: Healthy 90–100 · Warning 70–89 · Critical < 70 · Offline (asset nonaktif /
// decommissioned) · Unknown (belum ada data maintenance sama sekali).

type HealthFactor struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Count  int    `json:"count"`
	Points int    `json:"points"` // pengurang (negatif)
	Link   string `json:"link,omitempty"`
}

type Health struct {
	AssetID   uuid.UUID      `json:"asset_id"`
	Score     *int           `json:"score"`
	Status    string         `json:"status"`
	Factors   []HealthFactor `json:"factors"`
	UpdatedAt *time.Time     `json:"updated_at"`
}

// HealthStatusFor: ambang NC §14.
func HealthStatusFor(score int) string {
	switch {
	case score >= 90:
		return "healthy"
	case score >= 70:
		return "warning"
	}
	return "critical"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// computeHealthTx menghitung skor tanpa menyimpan.
func computeHealthTx(ctx context.Context, tx pgx.Tx, assetID uuid.UUID) (*Health, error) {
	var status string
	var criticality *string
	var installed *time.Time
	if err := tx.QueryRow(ctx, `SELECT status, criticality, installed_at FROM assets WHERE id = $1 AND deleted_at IS NULL`, assetID).Scan(&status, &criticality, &installed); err != nil {
		return nil, apperr.NotFound("Asset")
	}
	h := &Health{AssetID: assetID, Factors: []HealthFactor{}}
	if status == "decommissioned" || status == "inactive" {
		h.Status = "offline"
		return h, nil
	}
	var pmOverdue, openCorrective, criticalOpen, findingsHigh, findingsOther, repeat, inspFail, history int
	var lastMaint *time.Time
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM maintenance_schedules WHERE asset_id = $1 AND status = 'overdue'),
		(SELECT count(*) FROM work_orders WHERE asset_id = $1 AND work_order_type IN ('corrective','repair') AND status NOT IN ('completed','closed','cancelled','draft')),
		(SELECT count(*) FROM work_orders WHERE asset_id = $1 AND priority = 'critical' AND status NOT IN ('completed','closed','cancelled','draft')),
		(SELECT count(*) FROM findings WHERE asset_id = $1 AND status IN ('open','in_progress') AND severity IN ('high','critical')),
		(SELECT count(*) FROM findings WHERE asset_id = $1 AND status IN ('open','in_progress') AND severity NOT IN ('high','critical')),
		(SELECT count(*) FROM work_orders WHERE asset_id = $1 AND work_order_type IN ('corrective','repair') AND created_at > now() - interval '90 days'),
		(SELECT count(*) FROM tasks t JOIN inspections i ON i.task_id = t.id WHERE t.asset_id = $1 AND i.result = 'fail' AND t.completed_at > now() - interval '30 days'),
		(SELECT count(*) FROM work_orders WHERE asset_id = $1) + (SELECT count(*) FROM maintenance_schedules WHERE asset_id = $1) + (SELECT count(*) FROM tasks WHERE asset_id = $1),
		GREATEST((SELECT max(completed_at) FROM work_orders WHERE asset_id = $1 AND work_order_type IN ('maintenance','preventive','corrective','repair','service') AND completed_at IS NOT NULL),
		         (SELECT max(completed_at) FROM maintenance_schedules WHERE asset_id = $1 AND completed_at IS NOT NULL))`, assetID).
		Scan(&pmOverdue, &openCorrective, &criticalOpen, &findingsHigh, &findingsOther, &repeat, &inspFail, &history, &lastMaint); err != nil {
		return nil, err
	}
	if history == 0 && findingsHigh+findingsOther == 0 && lastMaint == nil {
		h.Status = "unknown"
		return h, nil
	}
	aid := assetID.String()
	add := func(code, label string, count, points int, link string) {
		if points != 0 {
			h.Factors = append(h.Factors, HealthFactor{Code: code, Label: label, Count: count, Points: points, Link: link})
		}
	}
	add("pm_overdue", "PM overdue", pmOverdue, -minInt(20*pmOverdue, 40), "/engineering/preventive-maintenance?asset_id="+aid+"&status=overdue")
	add("open_corrective", "Corrective WO terbuka", openCorrective, -minInt(10*openCorrective, 30), "/operations/work-orders?asset_id="+aid+"&open=true&type=corrective,repair")
	if criticalOpen > 0 {
		add("critical_work_order", "WO prioritas critical terbuka", criticalOpen, -15, "/operations/work-orders?asset_id="+aid+"&open=true&priority=critical")
	}
	add("open_findings", "Finding terbuka", findingsHigh+findingsOther, -minInt(10*findingsHigh+5*findingsOther, 20), "/findings?asset_id="+aid+"&unresolved=true")
	if repeat >= 3 {
		add("repeat_failures", "Kerusakan berulang (≥3 corrective / 90 hari)", repeat, -10, "/operations/work-orders?asset_id="+aid+"&type=corrective,repair")
	}
	if inspFail > 0 {
		add("inspection_failed", "Inspeksi gagal (30 hari)", inspFail, -10, "/engineering/inspections?asset_id="+aid)
	}
	switch {
	case lastMaint != nil && time.Since(*lastMaint) > 365*24*time.Hour:
		add("maintenance_age", "Maintenance terakhir > 12 bulan", int(time.Since(*lastMaint).Hours()/24), -20, "/assets/"+aid)
	case lastMaint != nil && time.Since(*lastMaint) > 180*24*time.Hour:
		add("maintenance_age", "Maintenance terakhir > 6 bulan", int(time.Since(*lastMaint).Hours()/24), -10, "/assets/"+aid)
	case lastMaint == nil && installed != nil && time.Since(*installed) > 180*24*time.Hour:
		add("maintenance_age", "Belum pernah maintenance (terpasang > 6 bulan)", int(time.Since(*installed).Hours()/24), -10, "/assets/"+aid)
	}
	if criticality != nil && *criticality == "critical" && openCorrective > 0 {
		add("critical_asset", "Asset critical dengan kerusakan terbuka", openCorrective, -5, "/assets/"+aid)
	}
	score := 100
	for _, f := range h.Factors {
		score += f.Points
	}
	if score < 0 {
		score = 0
	}
	h.Score = &score
	h.Status = HealthStatusFor(score)
	return h, nil
}

// RecomputeHealthTx menghitung & menyimpan health; perubahan status tercatat di riwayat asset (§17 contoh 3 "Equipment
// Health Updated"); turun ke critical → event asset.health_changed (Engineering Supervisor).
func (s *Service) RecomputeHealthTx(ctx context.Context, tx pgx.Tx, assetID uuid.UUID) (*Health, error) {
	h, err := computeHealthTx(ctx, tx, assetID)
	if err != nil {
		return nil, err
	}
	var prevStatus string
	var prevScore *int
	var propertyID uuid.UUID
	var code, name string
	_ = tx.QueryRow(ctx, `SELECT health_status, health_score, property_id, asset_code, name FROM assets WHERE id = $1`, assetID).Scan(&prevStatus, &prevScore, &propertyID, &code, &name)
	raw, _ := json.Marshal(h.Factors)
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE assets SET health_score = $2, health_status = $3, health_factors = $4, health_updated_at = $5 WHERE id = $1`, assetID, h.Score, h.Status, raw, now); err != nil {
		return nil, err
	}
	h.UpdatedAt = &now
	if prevStatus != h.Status {
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: assetID, Action: "health_changed", From: prevStatus, To: h.Status, Payload: map[string]any{"score": h.Score, "previous_score": prevScore}})
		if h.Status == "critical" && s.Jobs != nil {
			p := authctx.Must(ctx)
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "asset.health_changed", OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "asset", ObjectID: assetID,
				ObjectLabel: code + " " + name, Payload: map[string]any{"domain": "engineering", "from": prevStatus, "to": h.Status, "score": h.Score, "reason": fmt.Sprintf("Health %s → %s (skor %d)", prevStatus, h.Status, *h.Score)}})
		}
	}
	return h, nil
}

// Health: skor + faktor; dihitung ulang bila belum ada/lebih dari 30 menit.
func (s *Service) Health(ctx context.Context, assetID uuid.UUID, force bool) (*Health, error) {
	var out *Health
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := s.getTx(ctx, tx, assetID)
		if err != nil {
			return err
		}
		if !canViewAsset(ctx, tx, a) {
			return apperr.Forbidden("")
		}
		if force && !authctx.Must(ctx).HasOnProperty("engineering.assets.update", a.PropertyID) {
			return apperr.Forbidden("Memerlukan engineering.assets.update")
		}
		out, err = s.RecomputeHealthTx(ctx, tx, assetID)
		return err
	})
	return out, err
}

// RecomputeAllHealth: job berkala (umur maintenance & PM overdue berubah seiring waktu).
func (s *Service) RecomputeAllHealth(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM assets WHERE deleted_at IS NULL`)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			if _, err := s.RecomputeHealthTx(ctx, tx, id); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
