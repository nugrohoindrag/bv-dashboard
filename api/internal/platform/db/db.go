// Package db membungkus pgxpool dan menyediakan transaksi yang otomatis men-set
// `app.organization_id` (RLS, TAD §5.5). Satu use case = satu transaksi.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buildingvision/api/internal/platform/authctx"
)

// Querier adalah subset pgx yang dipakai repository (bisa Tx atau Pool).
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type DB struct {
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.HealthCheckPeriod = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{Pool: pool}, nil
}

func (d *DB) Close() { d.Pool.Close() }

// TxFunc dijalankan di dalam transaksi dengan RLS org di-set dari principal di ctx.
type TxFunc func(ctx context.Context, tx pgx.Tx) error

// WithTx menjalankan fn dalam transaksi. organization_id diambil dari authctx (wajib ada).
func (d *DB) WithTx(ctx context.Context, fn TxFunc) error {
	p, ok := authctx.From(ctx)
	if !ok {
		return errors.New("db.WithTx: principal missing (organization scope required)")
	}
	return d.WithOrgTx(ctx, p.OrganizationID, fn)
}

// WithOrgTx menjalankan fn dalam transaksi dengan organization eksplisit (dipakai worker lintas org).
func (d *DB) WithOrgTx(ctx context.Context, orgID uuid.UUID, fn TxFunc) error {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.organization_id', $1, true)`, orgID.String()); err != nil {
		return fmt.Errorf("set org scope: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// WithAuthLookupTx: transaksi tanpa org (login / refresh) — policy mengizinkan lookup user & session.
func (d *DB) WithAuthLookupTx(ctx context.Context, fn TxFunc) error {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.auth_lookup', 'on', true)`); err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListOrganizationIDs — untuk worker yang iterasi per organization (RLS bypass tidak diperlukan karena
// organizations tidak ber-RLS).
func (d *DB) ListOrganizationIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := d.Pool.Query(ctx, `SELECT id FROM organizations WHERE is_active`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsExclusionViolation: EXCLUDE constraint (mis. anti double-booking) — SQLSTATE 23P01.
func IsExclusionViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23P01"
}
