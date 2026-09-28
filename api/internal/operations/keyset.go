package operations

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/httpx"
)

// SortCol: kolom sort publik → ekspresi SQL + tipe cast untuk nilai cursor (PRD P0 v2 §17.3).
type SortCol struct {
	Expr string
	Cast string // timestamptz | int | text
}

// KeysetQuery: pagination cursor generik berbasis (sortExpr, id) — nilai cursor diambil langsung dari SQL
// sehingga setiap kolom sort yang diizinkan otomatis mendukung cursor.
type KeysetQuery struct {
	From  string // "FROM incidents i"
	Where string // " WHERE ..."
	IDCol string // "i.id"
	Args  []any
}

// ParseSortCols memvalidasi ?sort= terhadap cols (via httpx.ParseSort).
func ParseSortCols(r *http.Request, cols map[string]SortCol, def httpx.Sort) (httpx.Sort, SortCol, error) {
	allowed := make(map[string]string, len(cols))
	for k, c := range cols {
		allowed[k] = c.Expr
	}
	s, _, err := httpx.ParseSort(r, allowed, def)
	if err != nil {
		return s, SortCol{}, err
	}
	return s, cols[s.Field], nil
}

// KeysetIDs menjalankan query id terurut + cursor; mengembalikan id halaman ini dan cursor berikutnya.
func KeysetIDs(ctx context.Context, tx pgx.Tx, q KeysetQuery, col SortCol, desc bool, page httpx.Page) ([]uuid.UUID, *string, error) {
	args := append([]any(nil), q.Args...)
	add := func(v any) string { args = append(args, v); return "$" + itoa(len(args)) }
	dir, cmp := "ASC", ">"
	if desc {
		dir, cmp = "DESC", "<"
	}
	where := q.Where
	if page.Cursor != nil {
		where += " AND (" + col.Expr + ", " + q.IDCol + ") " + cmp + " (" + add(page.Cursor.Value) + "::" + col.Cast + ", " + add(page.Cursor.ID) + ")"
	}
	sql := "SELECT " + q.IDCol + ", (" + col.Expr + ")::text " + q.From + where +
		" ORDER BY " + col.Expr + " " + dir + " NULLS LAST, " + q.IDCol + " " + dir + " LIMIT " + add(page.Limit+1)
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	var vals []*string
	for rows.Next() {
		var id uuid.UUID
		var v *string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		vals = append(vals, v)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(ids) > page.Limit {
		ids = ids[:page.Limit]
		last := page.Limit - 1
		v := ""
		if vals[last] != nil {
			v = *vals[last]
		}
		c := httpx.EncodeCursor(v, ids[last])
		next = &c
	}
	return ids, next, nil
}

// IncidentSortCols / FindingSortCols: sort standar PRD §17.3 (created, updated, priority/severity, status).
var IncidentSortCols = map[string]SortCol{
	"created_at":  {"i.reported_at", "timestamptz"},
	"reported_at": {"i.reported_at", "timestamptz"},
	"updated_at":  {"i.updated_at", "timestamptz"},
	"priority":    {"CASE i.severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END", "int"},
	"severity":    {"CASE i.severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END", "int"},
	"status":      {"i.status", "text"},
	"title":       {"i.title", "text"},
}

var FindingSortCols = map[string]SortCol{
	"created_at":  {"f.reported_at", "timestamptz"},
	"reported_at": {"f.reported_at", "timestamptz"},
	"updated_at":  {"f.updated_at", "timestamptz"},
	"priority":    {"CASE f.severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END", "int"},
	"severity":    {"CASE f.severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END", "int"},
	"status":      {"f.status", "text"},
	"title":       {"f.title", "text"},
}
