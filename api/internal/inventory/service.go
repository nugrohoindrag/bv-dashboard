// Package inventory: Inventory / Spare Parts (PRD P1 v1.3 §25; NC §38) — Item Master, Stock Location, Stock, Stock In/Out/Adjustment,
// Parts Usage against Work Order (Asset → Work Order → Parts Usage → Inventory). Stok tidak boleh negatif (CHECK di DB).
package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
)

const EventLowStock = "inventory.low_stock"

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer
}

func New(d *db.DB, j jobs.Enqueuer) *Service { return &Service{DB: d, Jobs: j} }

// ---------- Item master ----------

type StockLevel struct {
	StockLocationID   uuid.UUID `json:"stock_location_id"`
	StockLocationName string    `json:"stock_location_name"`
	PropertyID        uuid.UUID `json:"property_id"`
	Quantity          float64   `json:"quantity"`
}

type Item struct {
	ID                    uuid.UUID    `json:"id"`
	ItemCode              string       `json:"item_code"`
	Name                  string       `json:"name"`
	Description           *string      `json:"description"`
	Category              string       `json:"category"`
	EquipmentCategoryCode *string      `json:"equipment_category_code"`
	Unit                  string       `json:"unit"`
	MinStock              float64      `json:"min_stock"`
	UnitCost              int64        `json:"unit_cost"`
	Barcode               *string      `json:"barcode"`
	IsActive              bool         `json:"is_active"`
	TotalQuantity         float64      `json:"total_quantity"`
	LowStock              bool         `json:"low_stock"`
	Levels                []StockLevel `json:"levels"`
	Version               int          `json:"version"`
}

const itemSelect = `SELECT i.id, i.item_code, i.name, i.description, i.category, i.equipment_category_code, i.unit, i.min_stock, i.unit_cost, i.barcode, i.is_active, i.version,
	COALESCE((SELECT sum(sl.quantity) FROM stock_levels sl WHERE sl.item_id = i.id), 0)
	FROM inventory_items i`

func scanItem(row pgx.Row) (*Item, error) {
	var it Item
	if err := row.Scan(&it.ID, &it.ItemCode, &it.Name, &it.Description, &it.Category, &it.EquipmentCategoryCode, &it.Unit, &it.MinStock, &it.UnitCost, &it.Barcode, &it.IsActive, &it.Version, &it.TotalQuantity); err != nil {
		return nil, err
	}
	it.LowStock = it.MinStock > 0 && it.TotalQuantity < it.MinStock
	it.Levels = []StockLevel{}
	return &it, nil
}

func (s *Service) loadLevels(ctx context.Context, tx pgx.Tx, it *Item, propertyID *uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT sl.stock_location_id, l.name, l.property_id, sl.quantity FROM stock_levels sl JOIN stock_locations l ON l.id = sl.stock_location_id WHERE sl.item_id = $1 AND ($2::uuid IS NULL OR l.property_id = $2) ORDER BY l.name`, it.ID, propertyID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var l StockLevel
		if err := rows.Scan(&l.StockLocationID, &l.StockLocationName, &l.PropertyID, &l.Quantity); err != nil {
			return err
		}
		it.Levels = append(it.Levels, l)
	}
	return rows.Err()
}

type ItemInput struct {
	Name                  *string  `json:"name"`
	Description           *string  `json:"description"`
	Category              *string  `json:"category"`
	EquipmentCategoryCode *string  `json:"equipment_category_code"`
	Unit                  *string  `json:"unit"`
	MinStock              *float64 `json:"min_stock"`
	UnitCost              *int64   `json:"unit_cost"`
	Barcode               *string  `json:"barcode"`
	IsActive              *bool    `json:"is_active"`
}

var itemCategories = map[string]bool{"spare_part": true, "consumable": true, "tool": true, "other": true}

func (s *Service) ListItems(ctx context.Context, q, category string, propertyID *uuid.UUID, lowStock bool, page httpx.Page) ([]Item, *string, error) {
	var out []Item
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE i.deleted_at IS NULL AND i.is_active"
		var args []any
		if q = strings.TrimSpace(q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (i.name ILIKE $%d OR i.item_code ILIKE $%d OR i.barcode = $%d)", len(args), len(args), len(args))
		}
		if category != "" {
			args = append(args, category)
			where += fmt.Sprintf(" AND i.category = $%d", len(args))
		}
		if lowStock {
			where += " AND i.min_stock > 0 AND COALESCE((SELECT sum(sl.quantity) FROM stock_levels sl WHERE sl.item_id = i.id),0) < i.min_stock"
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (i.name, i.id) > ($%d, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, itemSelect+where+fmt.Sprintf(" ORDER BY i.name, i.id LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []Item
		for rows.Next() {
			it, err := scanItem(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, *it)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.Name, last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			if err := s.loadLevels(ctx, tx, &items[i], propertyID); err != nil {
				return err
			}
		}
		out = items
		return nil
	})
	if out == nil {
		out = []Item{}
	}
	return out, next, err
}

func (s *Service) getItemTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Item, error) {
	it, err := scanItem(tx.QueryRow(ctx, itemSelect+` WHERE i.id = $1 AND i.deleted_at IS NULL`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Item")
		}
		return nil, err
	}
	return it, s.loadLevels(ctx, tx, it, nil)
}

func (s *Service) GetItem(ctx context.Context, id uuid.UUID) (*Item, error) {
	var out *Item
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.getItemTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) CreateItem(ctx context.Context, in ItemInput) (*Item, error) {
	p := authctx.Must(ctx)
	name := strings.TrimSpace(deref(in.Name))
	if name == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	cat := deref(in.Category)
	if cat == "" {
		cat = "spare_part"
	}
	if !itemCategories[cat] {
		return nil, apperr.Validation("category tidak valid")
	}
	unit := deref(in.Unit)
	if unit == "" {
		unit = "pcs"
	}
	var out *Item
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		code, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixItem)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO inventory_items (organization_id, item_code, name, description, category, equipment_category_code, unit, min_stock, unit_cost, barcode, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE($8,0),COALESCE($9,0),$10,$11,$11) RETURNING id`,
			p.OrganizationID, code, name, in.Description, cat, in.EquipmentCategoryCode, unit, in.MinStock, in.UnitCost, in.Barcode, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "inventory_item", EntityID: &id, EntityLabel: code + " " + name})
		out, err = s.getItemTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateItem(ctx context.Context, id uuid.UUID, in ItemInput, ifVersion *int) (*Item, error) {
	p := authctx.Must(ctx)
	var out *Item
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		it, err := s.getItemTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != it.Version {
			return apperr.StaleVersion()
		}
		if in.Category != nil && !itemCategories[*in.Category] {
			return apperr.Validation("category tidak valid")
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_items SET name = COALESCE(NULLIF(TRIM($2),''), name), description = COALESCE($3, description), category = COALESCE($4, category), equipment_category_code = COALESCE($5, equipment_category_code), unit = COALESCE(NULLIF($6,''), unit), min_stock = COALESCE($7, min_stock), unit_cost = COALESCE($8, unit_cost), barcode = COALESCE($9, barcode), is_active = COALESCE($10, is_active), updated_by = $11 WHERE id = $1`,
			id, deref(in.Name), in.Description, in.Category, in.EquipmentCategoryCode, deref(in.Unit), in.MinStock, in.UnitCost, in.Barcode, in.IsActive, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "inventory_item", EntityID: &id, EntityLabel: it.ItemCode, After: in})
		out, err = s.getItemTx(ctx, tx, id)
		return err
	})
	return out, err
}

// ---------- Stock locations ----------

type StockLocation struct {
	ID         uuid.UUID  `json:"id"`
	PropertyID uuid.UUID  `json:"property_id"`
	Name       string     `json:"name"`
	LocationID *uuid.UUID `json:"location_id"`
	IsDefault  bool       `json:"is_default"`
	IsActive   bool       `json:"is_active"`
	ItemCount  int        `json:"item_count"`
	Version    int        `json:"version"`
}

type StockLocationInput struct {
	PropertyID *uuid.UUID `json:"property_id"`
	Name       *string    `json:"name"`
	LocationID *uuid.UUID `json:"location_id"`
	IsDefault  *bool      `json:"is_default"`
	IsActive   *bool      `json:"is_active"`
}

func (s *Service) ListStockLocations(ctx context.Context, propertyID *uuid.UUID) ([]StockLocation, error) {
	p := authctx.Must(ctx)
	out := []StockLocation{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if propertyID != nil {
			if err := iam.CanOnProperty(ctx, "inventory.stock_locations.view", *propertyID); err != nil {
				return err
			}
			args = append(args, *propertyID)
			where += " AND l.property_id = $1"
		} else if pids, all := p.PropertyIDsFor("inventory.stock_locations.view"); !all {
			args = append(args, pids)
			where += " AND l.property_id = ANY($1)"
		}
		rows, err := tx.Query(ctx, `SELECT l.id, l.property_id, l.name, l.location_id, l.is_default, l.is_active, l.version, (SELECT count(*) FROM stock_levels sl WHERE sl.stock_location_id = l.id AND sl.quantity > 0) FROM stock_locations l`+where+` ORDER BY l.is_default DESC, l.name`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l StockLocation
			if err := rows.Scan(&l.ID, &l.PropertyID, &l.Name, &l.LocationID, &l.IsDefault, &l.IsActive, &l.Version, &l.ItemCount); err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateStockLocation(ctx context.Context, in StockLocationInput) (*StockLocation, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil {
		return nil, apperr.Validation("property_id wajib")
	}
	if err := iam.CanOnProperty(ctx, "inventory.stock_locations.create", *in.PropertyID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(deref(in.Name))
	if name == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	var out StockLocation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if in.LocationID != nil {
			pid, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
			if err != nil || pid != *in.PropertyID {
				return apperr.Validation("location_id tidak berada di property ini")
			}
		}
		isDefault := in.IsDefault != nil && *in.IsDefault
		if isDefault {
			_, _ = tx.Exec(ctx, `UPDATE stock_locations SET is_default = false WHERE property_id = $1`, *in.PropertyID)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO stock_locations (organization_id, property_id, name, location_id, is_default, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$6) RETURNING id, version`, p.OrganizationID, *in.PropertyID, name, in.LocationID, isDefault, p.UserID).Scan(&out.ID, &out.Version); err != nil {
			return err
		}
		out.PropertyID, out.Name, out.LocationID, out.IsDefault, out.IsActive = *in.PropertyID, name, in.LocationID, isDefault, true
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "stock_location", EntityID: &out.ID, EntityLabel: name})
		return nil
	})
	return &out, err
}

func (s *Service) UpdateStockLocation(ctx context.Context, id uuid.UUID, in StockLocationInput) (*StockLocation, error) {
	p := authctx.Must(ctx)
	var out StockLocation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id, property_id, name, location_id, is_default, is_active, version FROM stock_locations WHERE id = $1`, id).Scan(&out.ID, &out.PropertyID, &out.Name, &out.LocationID, &out.IsDefault, &out.IsActive, &out.Version); err != nil {
			return apperr.NotFound("Stock location")
		}
		if err := iam.CanOnProperty(ctx, "inventory.stock_locations.update", out.PropertyID); err != nil {
			return err
		}
		if in.IsDefault != nil && *in.IsDefault {
			_, _ = tx.Exec(ctx, `UPDATE stock_locations SET is_default = false WHERE property_id = $1`, out.PropertyID)
		}
		if _, err := tx.Exec(ctx, `UPDATE stock_locations SET name = COALESCE(NULLIF(TRIM($2),''), name), location_id = COALESCE($3, location_id), is_default = COALESCE($4, is_default), is_active = COALESCE($5, is_active), updated_by = $6 WHERE id = $1`, id, deref(in.Name), in.LocationID, in.IsDefault, in.IsActive, p.UserID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT name, location_id, is_default, is_active, version FROM stock_locations WHERE id = $1`, id).Scan(&out.Name, &out.LocationID, &out.IsDefault, &out.IsActive, &out.Version)
	})
	return &out, err
}

// ---------- Stock transactions ----------

type Transaction struct {
	ID                uuid.UUID  `json:"id"`
	TransactionNumber string     `json:"transaction_number"`
	PropertyID        uuid.UUID  `json:"property_id"`
	ItemID            uuid.UUID  `json:"item_id"`
	ItemCode          string     `json:"item_code"`
	ItemName          string     `json:"item_name"`
	Unit              string     `json:"unit"`
	StockLocationID   uuid.UUID  `json:"stock_location_id"`
	StockLocationName string     `json:"stock_location_name"`
	TransactionType   string     `json:"transaction_type"`
	Quantity          float64    `json:"quantity"`
	BalanceAfter      float64    `json:"balance_after"`
	UnitCost          *int64     `json:"unit_cost"`
	ReferenceType     *string    `json:"reference_type"`
	ReferenceID       *uuid.UUID `json:"reference_id"`
	ReferenceLabel    *string    `json:"reference_label"`
	Note              *string    `json:"note"`
	PerformedByName   *string    `json:"performed_by_name"`
	PerformedAt       time.Time  `json:"performed_at"`
}

const txSelect = `SELECT t.id, t.transaction_number, t.property_id, t.item_id, i.item_code, i.name, i.unit, t.stock_location_id, l.name, t.transaction_type, t.quantity, t.balance_after, t.unit_cost, t.reference_type, t.reference_id,
	CASE WHEN t.reference_type = 'work_order' THEN (SELECT work_order_number FROM work_orders w WHERE w.id = t.reference_id) END, t.note, u.full_name, t.performed_at
	FROM stock_transactions t JOIN inventory_items i ON i.id = t.item_id JOIN stock_locations l ON l.id = t.stock_location_id LEFT JOIN users u ON u.id = t.performed_by`

func scanTx(row pgx.Row) (*Transaction, error) {
	var t Transaction
	if err := row.Scan(&t.ID, &t.TransactionNumber, &t.PropertyID, &t.ItemID, &t.ItemCode, &t.ItemName, &t.Unit, &t.StockLocationID, &t.StockLocationName, &t.TransactionType, &t.Quantity, &t.BalanceAfter, &t.UnitCost, &t.ReferenceType, &t.ReferenceID, &t.ReferenceLabel, &t.Note, &t.PerformedByName, &t.PerformedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

type TransactionInput struct {
	ItemID          uuid.UUID  `json:"item_id"`
	StockLocationID uuid.UUID  `json:"stock_location_id"`
	TransactionType string     `json:"transaction_type"` // in | out | adjustment
	Quantity        float64    `json:"quantity"`         // in/out: positif; adjustment: nilai stok baru (absolut)
	UnitCost        *int64     `json:"unit_cost"`
	Note            *string    `json:"note"`
	ReferenceType   *string    `json:"reference_type"`
	ReferenceID     *uuid.UUID `json:"reference_id"`
}

// applyTx: mutasi stok atomik — UPDATE stock_levels (CHECK >= 0) + catat transaksi; 409 INSUFFICIENT_STOCK bila kurang.
func (s *Service) applyTx(ctx context.Context, tx pgx.Tx, in TransactionInput, propertyID uuid.UUID) (uuid.UUID, float64, error) {
	p := authctx.Must(ctx)
	var delta float64
	switch in.TransactionType {
	case "in", "transfer_in":
		if in.Quantity <= 0 {
			return uuid.Nil, 0, apperr.Validation("quantity harus > 0")
		}
		delta = in.Quantity
	case "out", "usage", "transfer_out":
		if in.Quantity <= 0 {
			return uuid.Nil, 0, apperr.Validation("quantity harus > 0")
		}
		delta = -in.Quantity
	case "adjustment":
		if in.Quantity < 0 {
			return uuid.Nil, 0, apperr.Validation("quantity (stok baru) tidak boleh negatif")
		}
		var cur float64
		_ = tx.QueryRow(ctx, `SELECT quantity FROM stock_levels WHERE item_id = $1 AND stock_location_id = $2`, in.ItemID, in.StockLocationID).Scan(&cur)
		delta = in.Quantity - cur
	default:
		return uuid.Nil, 0, apperr.Validation("transaction_type harus in|out|adjustment")
	}
	var balance float64
	err := tx.QueryRow(ctx, `INSERT INTO stock_levels (organization_id, item_id, stock_location_id, quantity) VALUES ($1,$2,$3,GREATEST($4,0))
		ON CONFLICT (item_id, stock_location_id) DO UPDATE SET quantity = stock_levels.quantity + $4, updated_at = now() RETURNING quantity`, p.OrganizationID, in.ItemID, in.StockLocationID, delta).Scan(&balance)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			return uuid.Nil, 0, apperr.Conflict("INSUFFICIENT_STOCK", "Stok tidak mencukupi")
		}
		return uuid.Nil, 0, err
	}
	if delta < 0 && balance < 0 { // pengaman ganda (INSERT path)
		return uuid.Nil, 0, apperr.Conflict("INSUFFICIENT_STOCK", "Stok tidak mencukupi")
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixStockTransaction, time.Now(), loc)
	if err != nil {
		return uuid.Nil, 0, err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO stock_transactions (organization_id, property_id, transaction_number, item_id, stock_location_id, transaction_type, quantity, balance_after, unit_cost, reference_type, reference_id, note, performed_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`,
		p.OrganizationID, propertyID, number, in.ItemID, in.StockLocationID, in.TransactionType, delta, balance, in.UnitCost, in.ReferenceType, in.ReferenceID, in.Note, actorOrNil(p)).Scan(&id); err != nil {
		return uuid.Nil, 0, err
	}
	// low stock alert (PRD §25 Minimum Stock) → supervisor engineering
	var minStock, total float64
	var itemName, itemCode string
	_ = tx.QueryRow(ctx, `SELECT i.min_stock, i.name, i.item_code, COALESCE((SELECT sum(quantity) FROM stock_levels WHERE item_id = i.id),0) FROM inventory_items i WHERE i.id = $1`, in.ItemID).Scan(&minStock, &itemName, &itemCode, &total)
	if delta < 0 && minStock > 0 && total < minStock && s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventLowStock, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "inventory_item", ObjectID: in.ItemID, ObjectLabel: itemCode + " " + itemName, ActorUserID: actorOrNil(p), Payload: map[string]any{"total": total, "min_stock": minStock, "domain": "engineering"}})
	}
	return id, balance, nil
}

func (s *Service) CreateTransaction(ctx context.Context, in TransactionInput) (*Transaction, error) {
	var out *Transaction
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var propertyID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM stock_locations WHERE id = $1 AND is_active`, in.StockLocationID).Scan(&propertyID); err != nil {
			return apperr.Validation("stock_location_id tidak ditemukan")
		}
		perm := "inventory.transactions.create"
		if in.TransactionType == "adjustment" {
			perm = "inventory.stock.adjust"
		}
		if err := iam.CanOnProperty(ctx, perm, propertyID); err != nil {
			return err
		}
		if _, err := s.getItemTx(ctx, tx, in.ItemID); err != nil {
			return err
		}
		if in.ReferenceType == nil {
			m := "manual"
			in.ReferenceType = &m
		}
		id, _, err := s.applyTx(ctx, tx, in, propertyID)
		if err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "stock_transaction", EntityID: &id, After: in})
		out, err = scanTx(tx.QueryRow(ctx, txSelect+` WHERE t.id = $1`, id))
		return err
	})
	return out, err
}

type TxFilter struct {
	PropertyID      *uuid.UUID
	ItemID          *uuid.UUID
	StockLocationID *uuid.UUID
	Types           []string
	From, To        *time.Time
}

func (s *Service) ListTransactions(ctx context.Context, f TxFilter, page httpx.Page) ([]Transaction, *string, error) {
	p := authctx.Must(ctx)
	var out []Transaction
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if err := iam.CanOnProperty(ctx, "inventory.transactions.view", *f.PropertyID); err != nil {
				return err
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND t.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("inventory.transactions.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND t.property_id = ANY($%d)", len(args))
		}
		if f.ItemID != nil {
			args = append(args, *f.ItemID)
			where += fmt.Sprintf(" AND t.item_id = $%d", len(args))
		}
		if f.StockLocationID != nil {
			args = append(args, *f.StockLocationID)
			where += fmt.Sprintf(" AND t.stock_location_id = $%d", len(args))
		}
		if len(f.Types) > 0 {
			args = append(args, f.Types)
			where += fmt.Sprintf(" AND t.transaction_type = ANY($%d)", len(args))
		}
		if f.From != nil {
			args = append(args, *f.From)
			where += fmt.Sprintf(" AND t.performed_at >= $%d", len(args))
		}
		if f.To != nil {
			args = append(args, *f.To)
			where += fmt.Sprintf(" AND t.performed_at <= $%d", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (t.performed_at, t.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, txSelect+where+fmt.Sprintf(" ORDER BY t.performed_at DESC, t.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []Transaction
		for rows.Next() {
			t, err := scanTx(rows)
			if err != nil {
				return err
			}
			items = append(items, *t)
		}
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.PerformedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		out = items
		return rows.Err()
	})
	if out == nil {
		out = []Transaction{}
	}
	return out, next, err
}

// ---------- Parts Usage against Work Order (PRD §25; Mobile Staff "Parts Usage") ----------

type PartUsage struct {
	ID              uuid.UUID `json:"id"`
	WorkOrderID     uuid.UUID `json:"work_order_id"`
	ItemID          uuid.UUID `json:"item_id"`
	ItemCode        string    `json:"item_code"`
	ItemName        string    `json:"item_name"`
	Unit            string    `json:"unit"`
	StockLocationID uuid.UUID `json:"stock_location_id"`
	StockLocation   string    `json:"stock_location_name"`
	Quantity        float64   `json:"quantity"`
	UnitCost        int64     `json:"unit_cost"`
	TotalCost       int64     `json:"total_cost"`
	Note            *string   `json:"note"`
	RecordedByName  *string   `json:"recorded_by_name"`
	RecordedAt      time.Time `json:"recorded_at"`
}

type PartUsageInput struct {
	ItemID          uuid.UUID  `json:"item_id"`
	StockLocationID *uuid.UUID `json:"stock_location_id"` // nil = gudang default property
	Quantity        float64    `json:"quantity"`
	Note            *string    `json:"note"`
	ClientPartID    *string    `json:"client_part_id"` // idempotensi dari mobile
}

const partSelect = `SELECT wp.id, wp.work_order_id, wp.item_id, i.item_code, i.name, i.unit, wp.stock_location_id, l.name, wp.quantity, wp.unit_cost, wp.total_cost, wp.note, u.full_name, wp.recorded_at
	FROM work_order_parts wp JOIN inventory_items i ON i.id = wp.item_id JOIN stock_locations l ON l.id = wp.stock_location_id LEFT JOIN users u ON u.id = wp.recorded_by`

func scanPart(row pgx.Row) (*PartUsage, error) {
	var x PartUsage
	if err := row.Scan(&x.ID, &x.WorkOrderID, &x.ItemID, &x.ItemCode, &x.ItemName, &x.Unit, &x.StockLocationID, &x.StockLocation, &x.Quantity, &x.UnitCost, &x.TotalCost, &x.Note, &x.RecordedByName, &x.RecordedAt); err != nil {
		return nil, err
	}
	return &x, nil
}

func (s *Service) ListParts(ctx context.Context, woID uuid.UUID) ([]PartUsage, int64, error) {
	out := []PartUsage{}
	var total int64
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var propertyID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM work_orders WHERE id = $1`, woID).Scan(&propertyID); err != nil {
			return apperr.NotFound("Work Order")
		}
		if err := iam.CanOnProperty(ctx, "operations.work_orders.view", propertyID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, partSelect+` WHERE wp.work_order_id = $1 ORDER BY wp.recorded_at`, woID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanPart(rows)
			if err != nil {
				return err
			}
			total += x.TotalCost
			out = append(out, *x)
		}
		return rows.Err()
	})
	return out, total, err
}

// AddPart: Parts Usage — WO harus non-terminal; pemakai = assignee/anggota tim atau pemegang manage; stok berkurang atomik;
// biaya part menambah actual_cost_amount WO (internal cost — tidak pernah ke tenant).
func (s *Service) AddPart(ctx context.Context, woID uuid.UUID, in PartUsageInput) (*PartUsage, error) {
	p := authctx.Must(ctx)
	if in.Quantity <= 0 {
		return nil, apperr.Validation("quantity harus > 0")
	}
	var out *PartUsage
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var propertyID uuid.UUID
		var status, number string
		var assigneeUser, assigneeTeam *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id, status, work_order_number, assignee_user_id, assignee_team_id FROM work_orders WHERE id = $1`, woID).Scan(&propertyID, &status, &number, &assigneeUser, &assigneeTeam); err != nil {
			return apperr.NotFound("Work Order")
		}
		if err := iam.CanOnProperty(ctx, "inventory.parts_usage.create", propertyID); err != nil {
			return err
		}
		if status == "closed" || status == "cancelled" {
			return apperr.Conflict("OBJECT_TERMINAL", "Work Order sudah "+status)
		}
		isAssignee := (assigneeUser != nil && *assigneeUser == p.UserID) || (assigneeTeam != nil && p.IsMemberOfTeam(*assigneeTeam))
		if !isAssignee && !p.HasOnProperty("operations.work_orders.manage", propertyID) && !p.HasOnProperty("inventory.transactions.create", propertyID) {
			return apperr.Forbidden("Hanya assignee Work Order (atau pemegang manage/inventory) yang dapat mencatat parts usage")
		}
		if in.ClientPartID != nil && *in.ClientPartID != "" {
			var existing uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM work_order_parts WHERE organization_id = $1 AND client_part_id = $2`, p.OrganizationID, *in.ClientPartID).Scan(&existing); err == nil {
				var err error
				out, err = scanPart(tx.QueryRow(ctx, partSelect+` WHERE wp.id = $1`, existing))
				return err // idempotent
			}
		}
		it, err := s.getItemTx(ctx, tx, in.ItemID)
		if err != nil {
			return err
		}
		locID := uuid.Nil
		if in.StockLocationID != nil {
			locID = *in.StockLocationID
			var lp uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT property_id FROM stock_locations WHERE id = $1 AND is_active`, locID).Scan(&lp); err != nil || lp != propertyID {
				return apperr.Validation("stock_location_id tidak berada di property ini")
			}
		} else {
			if err := tx.QueryRow(ctx, `SELECT id FROM stock_locations WHERE property_id = $1 AND is_active ORDER BY is_default DESC, created_at LIMIT 1`, propertyID).Scan(&locID); err != nil {
				return apperr.Validation("Property belum memiliki stock location")
			}
		}
		refType := "work_order"
		note := in.Note
		if note == nil {
			n := "Parts usage " + number
			note = &n
		}
		txID, _, err := s.applyTx(ctx, tx, TransactionInput{ItemID: in.ItemID, StockLocationID: locID, TransactionType: "usage", Quantity: in.Quantity, UnitCost: &it.UnitCost, Note: note, ReferenceType: &refType, ReferenceID: &woID}, propertyID)
		if err != nil {
			return err
		}
		total := int64(in.Quantity * float64(it.UnitCost))
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO work_order_parts (organization_id, work_order_id, item_id, stock_location_id, quantity, unit_cost, total_cost, stock_transaction_id, note, recorded_by, client_part_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
			p.OrganizationID, woID, in.ItemID, locID, in.Quantity, it.UnitCost, total, txID, in.Note, p.UserID, in.ClientPartID).Scan(&id); err != nil {
			return err
		}
		// ringkasan ke WO (parts_usage teks legacy + biaya aktual internal)
		_, _ = tx.Exec(ctx, `UPDATE work_orders SET actual_cost_amount = COALESCE(actual_cost_amount,0) + $2,
			parts_usage = COALESCE((SELECT string_agg(i.name || ' × ' || wp.quantity::text || ' ' || i.unit, ', ' ORDER BY wp.recorded_at) FROM work_order_parts wp JOIN inventory_items i ON i.id = wp.item_id WHERE wp.work_order_id = $1), parts_usage), updated_by = $3 WHERE id = $1`, woID, total, p.UserID)
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "work_order", ObjectID: woID, Action: "parts_used", Payload: map[string]any{"item": it.Name, "quantity": in.Quantity, "unit": it.Unit}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "work_order_part", EntityID: &id, EntityLabel: number + " · " + it.Name})
		out, err = scanPart(tx.QueryRow(ctx, partSelect+` WHERE wp.id = $1`, id))
		return err
	})
	return out, err
}

// RemovePart: koreksi (mengembalikan stok) — hanya bila WO belum completed/closed.
func (s *Service) RemovePart(ctx context.Context, woID, partID uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var propertyID uuid.UUID
		var status string
		if err := tx.QueryRow(ctx, `SELECT property_id, status FROM work_orders WHERE id = $1`, woID).Scan(&propertyID, &status); err != nil {
			return apperr.NotFound("Work Order")
		}
		if err := iam.CanOnProperty(ctx, "inventory.parts_usage.create", propertyID); err != nil {
			return err
		}
		if status == "completed" || status == "closed" || status == "cancelled" {
			return apperr.Conflict("OBJECT_TERMINAL", "Parts usage tidak dapat diubah setelah Work Order "+status)
		}
		var itemID, locID uuid.UUID
		var qty float64
		var total int64
		if err := tx.QueryRow(ctx, `SELECT item_id, stock_location_id, quantity, total_cost FROM work_order_parts WHERE id = $1 AND work_order_id = $2`, partID, woID).Scan(&itemID, &locID, &qty, &total); err != nil {
			return apperr.NotFound("Parts usage")
		}
		refType := "work_order"
		note := "Koreksi parts usage"
		if _, _, err := s.applyTx(ctx, tx, TransactionInput{ItemID: itemID, StockLocationID: locID, TransactionType: "in", Quantity: qty, Note: &note, ReferenceType: &refType, ReferenceID: &woID}, propertyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM work_order_parts WHERE id = $1`, partID); err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE work_orders SET actual_cost_amount = GREATEST(0, COALESCE(actual_cost_amount,0) - $2), updated_by = $3 WHERE id = $1`, woID, total, p.UserID)
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "work_order_part", EntityID: &partID})
		return nil
	})
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.IsSystem || p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
