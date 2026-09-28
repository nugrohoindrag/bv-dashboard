package asset

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
)

// ---------- Asset 360 — Cost · Parts · Vendor · PM compliance (PRD P2 v2.1 §5.5 P2-AST-04..06; Roadmap v2.1 §8.1) ----------

type CostBucket struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	WorkOrders  int    `json:"work_orders"`
	ActualCost  int64  `json:"actual_cost"`
	PartsCost   int64  `json:"parts_cost"`
	ServiceCost int64  `json:"service_cost"`
	OtherCost   int64  `json:"other_cost"`
}

type PartUsage struct {
	ItemID    uuid.UUID `json:"item_id"`
	ItemCode  string    `json:"item_code"`
	Name      string    `json:"name"`
	Unit      *string   `json:"unit"`
	Quantity  float64   `json:"quantity"`
	TotalCost int64     `json:"total_cost"`
	LastUsed  time.Time `json:"last_used_at"`
}

type VendorUsage struct {
	VendorID   uuid.UUID  `json:"vendor_id"`
	Name       string     `json:"name"`
	WorkOrders int        `json:"work_orders"`
	Completed  int        `json:"completed"`
	TotalCost  int64      `json:"total_cost"`
	LastAt     *time.Time `json:"last_at"`
}

type PMCompliance struct {
	Scheduled       int     `json:"scheduled"`
	Completed       int     `json:"completed"`
	CompletedOnTime int     `json:"completed_on_time"`
	Overdue         int     `json:"overdue"`
	Skipped         int     `json:"skipped"`
	CompliancePct   float64 `json:"compliance_pct"` // selesai tepat waktu / jatuh tempo atau sudah selesai (tanpa skipped)
}

type AssetInsight struct {
	AssetID      uuid.UUID     `json:"asset_id"`
	From         string        `json:"from"`
	To           string        `json:"to"`
	CurrencyCode string        `json:"currency_code"`
	Total        CostBucket    `json:"total"`
	ByMonth      []CostBucket  `json:"by_month"`
	ByType       []CostBucket  `json:"by_type"`
	Parts        []PartUsage   `json:"parts"`
	Vendors      []VendorUsage `json:"vendors"`
	PM           PMCompliance  `json:"pm"`
	Health       *Health       `json:"health"`
}

func scanBuckets(rows pgx.Rows) ([]CostBucket, error) {
	defer rows.Close()
	out := []CostBucket{}
	for rows.Next() {
		var b CostBucket
		if err := rows.Scan(&b.Key, &b.Label, &b.WorkOrders, &b.ActualCost, &b.PartsCost, &b.ServiceCost, &b.OtherCost); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Insight: biaya maintenance per asset dalam periode (default 12 bulan terakhir) + parts, vendor, PM compliance, health.
func (s *Service) Insight(ctx context.Context, assetID uuid.UUID, from, to string) (*AssetInsight, error) {
	now := time.Now().In(docLoc) // rentang default = tanggal kalender lokal (jadwal PM bertanggal lokal)
	if to == "" {
		to = now.Format("2006-01-02")
	}
	if from == "" {
		from = now.AddDate(-1, 0, 0).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", from); err != nil {
		return nil, apperr.Validation("from harus YYYY-MM-DD")
	}
	if _, err := time.Parse("2006-01-02", to); err != nil {
		return nil, apperr.Validation("to harus YYYY-MM-DD")
	}
	out := &AssetInsight{AssetID: assetID, From: from, To: to, CurrencyCode: "IDR", ByMonth: []CostBucket{}, ByType: []CostBucket{}, Parts: []PartUsage{}, Vendors: []VendorUsage{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := s.getTx(ctx, tx, assetID)
		if err != nil {
			return err
		}
		if !canViewAsset(ctx, tx, a) {
			return apperr.Forbidden("")
		}
		// WO selesai (completed/closed) dalam periode; biaya internal (tidak pernah ke tenant)
		const costCols = `count(*), COALESCE(sum(w.actual_cost_amount),0)::bigint, COALESCE(sum(w.parts_cost_amount),0)::bigint, COALESCE(sum(w.service_cost_amount),0)::bigint, COALESCE(sum(w.other_cost_amount),0)::bigint`
		const costWhere = ` FROM work_orders w WHERE w.asset_id = $1 AND w.completed_at IS NOT NULL AND w.completed_at >= $2::date AND w.completed_at < $3::date + 1`
		if err := tx.QueryRow(ctx, `SELECT 'total', 'Total', `+costCols+costWhere, assetID, from, to).
			Scan(&out.Total.Key, &out.Total.Label, &out.Total.WorkOrders, &out.Total.ActualCost, &out.Total.PartsCost, &out.Total.ServiceCost, &out.Total.OtherCost); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT to_char(date_trunc('month', w.completed_at), 'YYYY-MM'), to_char(date_trunc('month', w.completed_at), 'Mon YYYY'), `+costCols+costWhere+` GROUP BY 1, 2 ORDER BY 1`, assetID, from, to)
		if err != nil {
			return err
		}
		if out.ByMonth, err = scanBuckets(rows); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT w.work_order_type, w.work_order_type, `+costCols+costWhere+` GROUP BY 1 ORDER BY 4 DESC`, assetID, from, to)
		if err != nil {
			return err
		}
		if out.ByType, err = scanBuckets(rows); err != nil {
			return err
		}
		prows, err := tx.Query(ctx, `SELECT i.id, i.item_code, i.name, i.unit, COALESCE(sum(p.quantity),0)::float8, COALESCE(sum(p.total_cost),0)::bigint, max(p.recorded_at)
			FROM work_order_parts p JOIN work_orders w ON w.id = p.work_order_id JOIN inventory_items i ON i.id = p.item_id
			WHERE w.asset_id = $1 AND p.recorded_at >= $2::date AND p.recorded_at < $3::date + 1 GROUP BY i.id, i.item_code, i.name, i.unit ORDER BY 6 DESC LIMIT 50`, assetID, from, to)
		if err != nil {
			return err
		}
		for prows.Next() {
			var pu PartUsage
			if err := prows.Scan(&pu.ItemID, &pu.ItemCode, &pu.Name, &pu.Unit, &pu.Quantity, &pu.TotalCost, &pu.LastUsed); err != nil {
				prows.Close()
				return err
			}
			out.Parts = append(out.Parts, pu)
		}
		prows.Close()
		vrows, err := tx.Query(ctx, `SELECT v.id, v.name, count(*), count(*) FILTER (WHERE w.completed_at IS NOT NULL), COALESCE(sum(w.actual_cost_amount),0)::bigint, max(COALESCE(w.completed_at, w.created_at))
			FROM work_orders w JOIN vendors v ON v.id = w.vendor_id WHERE w.asset_id = $1 AND w.created_at >= $2::date AND w.created_at < $3::date + 1 GROUP BY v.id, v.name ORDER BY 3 DESC`, assetID, from, to)
		if err != nil {
			return err
		}
		for vrows.Next() {
			var vu VendorUsage
			if err := vrows.Scan(&vu.VendorID, &vu.Name, &vu.WorkOrders, &vu.Completed, &vu.TotalCost, &vu.LastAt); err != nil {
				vrows.Close()
				return err
			}
			out.Vendors = append(out.Vendors, vu)
		}
		vrows.Close()
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE ms.status <> 'skipped'), count(*) FILTER (WHERE ms.status = 'completed'),
			count(*) FILTER (WHERE ms.status = 'completed' AND ms.completed_at <= ms.due_at + interval '1 day'), count(*) FILTER (WHERE ms.status = 'overdue'), count(*) FILTER (WHERE ms.status = 'skipped')
			FROM maintenance_schedules ms WHERE ms.asset_id = $1 AND ms.due_at >= $2::date AND ms.due_at < $3::date + 1
			  AND (ms.due_at <= now() OR ms.status IN ('completed','skipped'))`, assetID, from, to).
			Scan(&out.PM.Scheduled, &out.PM.Completed, &out.PM.CompletedOnTime, &out.PM.Overdue, &out.PM.Skipped); err != nil {
			return err
		}
		if out.PM.Scheduled > 0 {
			out.PM.CompliancePct = float64(int(float64(out.PM.CompletedOnTime)/float64(out.PM.Scheduled)*1000+0.5)) / 10
		}
		out.Health, err = s.RecomputeHealthTx(ctx, tx, assetID)
		return err
	})
	return out, err
}
