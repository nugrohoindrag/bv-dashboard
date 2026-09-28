package inventory

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

// ---------- Consumable per Cleaning Task (PRD P2 v2.1 §7.5 P2-CNS-02, P2-MOB-05; Roadmap v2.1 §10, §39.2) ----------
// Housekeeping mencatat pemakaian consumable (sabun, tisu, cairan pembersih) pada cleaning task — Web & Staff App
// (offline: mutation use_consumable, idempoten lewat client_ref). Stok berkurang atomik (transaksi usage, reference
// task); low stock consumable diteruskan ke Housekeeping Supervisor (ItemDomain).

type ConsumableUsage struct {
	ID              uuid.UUID `json:"id"`
	TaskID          uuid.UUID `json:"task_id"`
	ItemID          uuid.UUID `json:"item_id"`
	ItemCode        string    `json:"item_code"`
	ItemName        string    `json:"item_name"`
	Unit            string    `json:"unit"`
	StockLocationID uuid.UUID `json:"stock_location_id"`
	StockLocation   string    `json:"stock_location_name"`
	Quantity        float64   `json:"quantity"`
	UnitCost        *int64    `json:"unit_cost"`
	TotalCost       *int64    `json:"total_cost"`
	Note            *string   `json:"note"`
	RecordedByName  *string   `json:"recorded_by_name"`
	RecordedAt      string    `json:"recorded_at"`
}

type ConsumableUsageInput struct {
	ItemID          uuid.UUID  `json:"item_id"`
	StockLocationID *uuid.UUID `json:"stock_location_id"` // nil = gudang default property
	Quantity        float64    `json:"quantity"`
	Note            *string    `json:"note"`
	ClientRef       *string    `json:"client_ref"` // idempotensi dari Staff App (client_mutation_id)
}

// ConsumableItem: katalog consumable + stok tersedia di gudang default property (cache offline Staff App).
type ConsumableItem struct {
	ID              uuid.UUID  `json:"id"`
	ItemCode        string     `json:"item_code"`
	Name            string     `json:"name"`
	Unit            string     `json:"unit"`
	PropertyID      uuid.UUID  `json:"property_id"`
	StockLocationID *uuid.UUID `json:"stock_location_id"`
	Available       float64    `json:"available"`
	LowStock        bool       `json:"low_stock"`
}

const consumableSelect = `SELECT c.id, c.task_id, c.item_id, i.item_code, i.name, i.unit, c.stock_location_id, l.name, c.quantity::float8, c.unit_cost, c.total_cost, c.note, u.full_name,
	to_char(c.recorded_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	FROM task_consumables c JOIN inventory_items i ON i.id = c.item_id JOIN stock_locations l ON l.id = c.stock_location_id LEFT JOIN users u ON u.id = c.recorded_by`

func scanConsumable(row pgx.Row) (*ConsumableUsage, error) {
	var x ConsumableUsage
	if err := row.Scan(&x.ID, &x.TaskID, &x.ItemID, &x.ItemCode, &x.ItemName, &x.Unit, &x.StockLocationID, &x.StockLocation, &x.Quantity, &x.UnitCost, &x.TotalCost, &x.Note, &x.RecordedByName, &x.RecordedAt); err != nil {
		return nil, err
	}
	return &x, nil
}

type cleaningTaskRef struct {
	propertyID                 uuid.UUID
	locationID                 *uuid.UUID
	number, status, taskType   string
	assigneeUser, assigneeTeam *uuid.UUID
}

// can: permission pada lokasi task (scope Building/Tower, P2-NFR-05).
func (t *cleaningTaskRef) can(ctx context.Context, tx pgx.Tx, perm string) bool {
	return authctx.Must(ctx).HasOnPropertyAt(perm, t.propertyID, db.LocationPathFn(ctx, tx, t.locationID))
}

func loadCleaningTask(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) (*cleaningTaskRef, error) {
	var t cleaningTaskRef
	if err := tx.QueryRow(ctx, `SELECT property_id, location_id, task_number, status, task_type, assignee_user_id, assignee_team_id FROM tasks WHERE id = $1`, taskID).
		Scan(&t.propertyID, &t.locationID, &t.number, &t.status, &t.taskType, &t.assigneeUser, &t.assigneeTeam); err != nil {
		return nil, apperr.NotFound("Task")
	}
	if t.taskType != "cleaning" {
		return nil, apperr.Validation("Consumable hanya dicatat pada cleaning task")
	}
	return &t, nil
}

func (s *Service) ListConsumables(ctx context.Context, taskID uuid.UUID) ([]ConsumableUsage, error) {
	out := []ConsumableUsage{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := loadCleaningTask(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if !t.can(ctx, tx, "inventory.consumable_usage.view") && !t.can(ctx, tx, "housekeeping.cleaning.view") {
			return apperr.Forbidden("Memerlukan inventory.consumable_usage.view")
		}
		rows, err := tx.Query(ctx, consumableSelect+` WHERE c.task_id = $1 ORDER BY c.recorded_at`, taskID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanConsumable(rows)
			if err != nil {
				return err
			}
			out = append(out, *x)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) AddConsumable(ctx context.Context, taskID uuid.UUID, in ConsumableUsageInput) (*ConsumableUsage, error) {
	var out *ConsumableUsage
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.AddConsumableTx(ctx, tx, taskID, in)
		return err
	})
	return out, err
}

// AddConsumableTx: dipakai HTTP & sync (mutation use_consumable). Pemakai = assignee/anggota tim, atau pemegang
// housekeeping.cleaning.manage / inventory.transactions.create. Task Closed/Cancelled → 409 OBJECT_TERMINAL.
func (s *Service) AddConsumableTx(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, in ConsumableUsageInput) (*ConsumableUsage, error) {
	p := authctx.Must(ctx)
	if in.Quantity <= 0 {
		return nil, apperr.Validation("quantity harus > 0").WithField("quantity", "> 0")
	}
	if in.ItemID == uuid.Nil {
		return nil, apperr.Validation("item_id wajib").WithField("item_id", "wajib")
	}
	if in.ClientRef != nil && *in.ClientRef != "" {
		var existing uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM task_consumables WHERE organization_id = $1 AND client_ref = $2`, p.OrganizationID, *in.ClientRef).Scan(&existing); err == nil {
			return scanConsumable(tx.QueryRow(ctx, consumableSelect+` WHERE c.id = $1`, existing)) // idempoten
		}
	}
	t, err := loadCleaningTask(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}
	if !t.can(ctx, tx, "inventory.consumable_usage.create") {
		return nil, apperr.Forbidden("Memerlukan inventory.consumable_usage.create")
	}
	if t.status == "closed" || t.status == "cancelled" {
		return nil, apperr.Conflict("OBJECT_TERMINAL", "Task sudah "+t.status)
	}
	isAssignee := (t.assigneeUser != nil && *t.assigneeUser == p.UserID) || (t.assigneeTeam != nil && p.IsMemberOfTeam(*t.assigneeTeam))
	if !isAssignee && !t.can(ctx, tx, "housekeeping.cleaning.manage") && !t.can(ctx, tx, "inventory.transactions.create") {
		return nil, apperr.Forbidden("Hanya assignee cleaning task (atau supervisor) yang dapat mencatat consumable")
	}
	it, err := s.getItemTx(ctx, tx, in.ItemID)
	if err != nil {
		return nil, err
	}
	if it.Category != "consumable" || !it.IsActive {
		return nil, apperr.Validation("item_id harus item consumable aktif").WithField("item_id", "bukan consumable")
	}
	locID := uuid.Nil
	if in.StockLocationID != nil {
		locID = *in.StockLocationID
		var lp uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM stock_locations WHERE id = $1 AND is_active`, locID).Scan(&lp); err != nil || lp != t.propertyID {
			return nil, apperr.Validation("stock_location_id tidak berada di property ini").WithField("stock_location_id", "tidak valid")
		}
	} else if err := tx.QueryRow(ctx, `SELECT id FROM stock_locations WHERE property_id = $1 AND is_active ORDER BY is_default DESC, created_at LIMIT 1`, t.propertyID).Scan(&locID); err != nil {
		return nil, apperr.Validation("Property belum memiliki stock location")
	}
	refType := "task"
	note := in.Note
	if note == nil {
		n := "Consumable " + t.number
		note = &n
	}
	txID, _, err := s.applyTx(ctx, tx, TransactionInput{ItemID: in.ItemID, StockLocationID: locID, TransactionType: "usage", Quantity: in.Quantity, UnitCost: &it.UnitCost, Note: note, ReferenceType: &refType, ReferenceID: &taskID}, t.propertyID)
	if err != nil {
		return nil, err
	}
	total := int64(in.Quantity * float64(it.UnitCost))
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO task_consumables (organization_id, task_id, item_id, stock_location_id, quantity, unit_cost, total_cost, stock_transaction_id, client_ref, note, recorded_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		p.OrganizationID, taskID, in.ItemID, locID, in.Quantity, it.UnitCost, total, txID, nilIfBlank(in.ClientRef), in.Note, actorOrNil(p)).Scan(&id); err != nil {
		return nil, err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "task", ObjectID: taskID, Action: "consumable_used", Payload: map[string]any{"item": it.Name, "quantity": in.Quantity, "unit": it.Unit}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "task_consumable", EntityID: &id, EntityLabel: t.number + " · " + it.Name})
	return scanConsumable(tx.QueryRow(ctx, consumableSelect+` WHERE c.id = $1`, id))
}

// RemoveConsumable: koreksi (stok dikembalikan) selama task belum Completed/Closed/Cancelled.
func (s *Service) RemoveConsumable(ctx context.Context, taskID, usageID uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := loadCleaningTask(ctx, tx, taskID)
		if err != nil {
			return err
		}
		p := authctx.Must(ctx)
		if !t.can(ctx, tx, "inventory.consumable_usage.create") {
			return apperr.Forbidden("Memerlukan inventory.consumable_usage.create")
		}
		if t.status == "completed" || t.status == "closed" || t.status == "cancelled" {
			return apperr.Conflict("OBJECT_TERMINAL", "Consumable tidak dapat diubah setelah task "+t.status)
		}
		var itemID, locID uuid.UUID
		var qty float64
		var recordedBy *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT item_id, stock_location_id, quantity::float8, recorded_by FROM task_consumables WHERE id = $1 AND task_id = $2`, usageID, taskID).Scan(&itemID, &locID, &qty, &recordedBy); err != nil {
			return apperr.NotFound("Pemakaian consumable")
		}
		if (recordedBy == nil || *recordedBy != p.UserID) && !t.can(ctx, tx, "housekeeping.cleaning.manage") && !t.can(ctx, tx, "inventory.transactions.create") {
			return apperr.Forbidden("Hanya pencatat (atau supervisor) yang dapat mengoreksi consumable")
		}
		refType := "task"
		note := "Koreksi consumable " + t.number
		if _, _, err := s.applyTx(ctx, tx, TransactionInput{ItemID: itemID, StockLocationID: locID, TransactionType: "in", Quantity: qty, Note: &note, ReferenceType: &refType, ReferenceID: &taskID}, t.propertyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM task_consumables WHERE id = $1`, usageID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "task_consumable", EntityID: &usageID, EntityLabel: t.number})
	})
}

// ConsumableCatalogTx: consumable housekeeping aktif (tanpa kategori equipment) + stok gudang default tiap property
// (work bundle Staff App). Consumable teknis (refrigerant, kabel, pipa) tetap dicatat lewat parts usage WO.
func (s *Service) ConsumableCatalogTx(ctx context.Context, tx pgx.Tx, propertyIDs []uuid.UUID) ([]ConsumableItem, error) {
	out := []ConsumableItem{}
	if len(propertyIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT i.id, i.item_code, i.name, i.unit, pr.location_id, sl.id, COALESCE(lv.quantity, 0)::float8, i.min_stock::float8
		FROM inventory_items i
		CROSS JOIN properties pr
		LEFT JOIN LATERAL (SELECT l.id FROM stock_locations l WHERE l.property_id = pr.location_id AND l.is_active ORDER BY l.is_default DESC, l.created_at LIMIT 1) sl ON true
		LEFT JOIN stock_levels lv ON lv.item_id = i.id AND lv.stock_location_id = sl.id
		WHERE i.category = 'consumable' AND COALESCE(i.equipment_category_code, '') = '' AND i.is_active AND i.deleted_at IS NULL AND pr.location_id = ANY($1::uuid[])
		ORDER BY i.name LIMIT 500`, propertyIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x ConsumableItem
		var minStock float64
		if err := rows.Scan(&x.ID, &x.ItemCode, &x.Name, &x.Unit, &x.PropertyID, &x.StockLocationID, &x.Available, &minStock); err != nil {
			return nil, err
		}
		x.LowStock = minStock > 0 && x.Available < minStock
		out = append(out, x)
	}
	return out, rows.Err()
}

func nilIfBlank(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}
