package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/seed"
)

func runRiverMigrate(ctx context.Context, url string) error {
	d, err := db.Open(ctx, url)
	if err != nil {
		return err
	}
	defer d.Close()
	return jobs.Migrate(ctx, d.Pool)
}

func runKeygen() {
	priv, pub, err := iam.ExportKeysPEM()
	if err != nil {
		fail(err)
	}
	fmt.Println("# BV_JWT_PRIVATE_KEY (simpan di secret manager / SOPS)")
	fmt.Println(priv)
	fmt.Println("# BV_JWT_PUBLIC_KEY")
	fmt.Println(pub)
}

// runSeed: katalog permission (global), notification rules default, dan (opsional) organization demo lengkap.
func runSeed(ctx context.Context, url string, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	demo := fs.Bool("demo", false, "buat organization demo 'Graha Pangeran' dengan data lengkap")
	orgName := fs.String("org", "", "buat organization baru dengan nama ini (tanpa data demo)")
	orgSlug := fs.String("slug", "", "slug organization")
	adminEmail := fs.String("admin-email", "admin@example.com", "email Organization Admin")
	adminPass := fs.String("admin-password", "Admin12345!", "password Organization Admin")
	_ = fs.Parse(args)

	d, err := db.Open(ctx, url)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := seed.SeedGlobal(ctx, d); err != nil {
		return err
	}
	fmt.Println("seed: permission catalog + notification rules ok")
	if *demo {
		*orgName = "PT Graha Pangeran Property"
		*orgSlug = "graha-pangeran"
	}
	if *orgName == "" {
		return nil
	}
	if *orgSlug == "" {
		*orgSlug = strings.ToLower(strings.ReplaceAll(*orgName, " ", "-"))
	}
	orgID, created, err := seed.EnsureOrganization(ctx, d, *orgName, *orgSlug)
	if err != nil {
		return err
	}
	if !created {
		fmt.Printf("seed: organization %s sudah ada (%s) — dilewati\n", *orgSlug, orgID)
		return nil
	}
	iamSvc := iam.NewService(d, nil, 0)
	return d.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		adminID, err := seed.SeedOrganization(ctx, tx, iamSvc, orgID, *adminEmail, *adminPass)
		if err != nil {
			return err
		}
		fmt.Printf("seed: organization %s (%s) · admin %s / %s\n", *orgName, orgID, *adminEmail, *adminPass)
		if *demo {
			return seed.SeedDemo(ctx, tx, orgID, adminID)
		}
		return nil
	})
}
