// Package migrations meng-embed SQL migration (goose, forward-only) — TAD §7.7.
package migrations

import (
	"context"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var FS embed.FS

// Up menjalankan seluruh migration yang belum diterapkan. url harus koneksi owner (bukan bv_app).
func Up(ctx context.Context, url string) error {
	db, err := open(url)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	goose.SetBaseFS(FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, "migrations")
}

func Status(ctx context.Context, url string) error {
	db, err := open(url)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	goose.SetBaseFS(FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.StatusContext(ctx, db, "migrations")
}

func Down(ctx context.Context, url string) error {
	db, err := open(url)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	goose.SetBaseFS(FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.DownContext(ctx, db, "migrations")
}
