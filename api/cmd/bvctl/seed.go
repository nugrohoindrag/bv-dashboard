package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/authctx"
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
	internal := fs.Bool("internal", false, "buat organization internal BuildingVision + user admin_internal (Website PRD §18: App Downloads)")
	internalEmail := fs.String("internal-email", "internal@buildingvision.id", "email Admin Internal")
	internalPass := fs.String("internal-password", "Internal12345!", "password Admin Internal")
	bvroomsDemo := fs.Bool("bvrooms-demo", false, "tambahkan properti demo BVRooms (hotel + apartemen + customer) ke organization --slug yang sudah ada")
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
	if n, err := seed.SyncExistingOrganizations(ctx, d, iam.NewService(d, nil, 0)); err != nil {
		return err
	} else if n > 0 {
		fmt.Printf("seed: role sistem & kategori default disinkronkan ke %d organization\n", n)
	}
	if *internal {
		if err := seed.SeedInternalOrganization(ctx, d, iam.NewService(d, nil, 0), *internalEmail, *internalPass); err != nil {
			return err
		}
		fmt.Printf("seed: organization internal 'BuildingVision Internal' · admin_internal %s / %s\n", *internalEmail, *internalPass)
	}
	if *demo {
		*orgName = "PT Graha Pangeran Property"
		*orgSlug = "graha-pangeran"
	}
	if *bvroomsDemo && !*demo {
		if *orgSlug == "" {
			return fmt.Errorf("--bvrooms-demo membutuhkan --slug organization yang sudah ada")
		}
		return seedBVRoomsExisting(ctx, d, *orgSlug)
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

// seedBVRoomsExisting: SeedBVRoomsDemo untuk organization yang sudah ada (DB dev yang di-seed sebelum BVRooms).
func seedBVRoomsExisting(ctx context.Context, d *db.DB, slug string) error {
	var orgID uuid.UUID
	if err := d.Pool.QueryRow(ctx, `SELECT id FROM organizations WHERE slug = $1`, slug).Scan(&orgID); err != nil {
		return fmt.Errorf("organization %s tidak ditemukan", slug)
	}
	return d.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bvrooms_property_listings WHERE slug = 'graha-pangeran-hotel')`).Scan(&exists)
		if exists {
			fmt.Println("seed bvrooms: properti demo sudah ada — dilewati")
			return nil
		}
		var adminID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT u.id FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id WHERE u.organization_id = $1 AND r.code = 'organization_admin' AND u.deleted_at IS NULL ORDER BY u.created_at LIMIT 1`, orgID).Scan(&adminID); err != nil {
			return fmt.Errorf("organization admin tidak ditemukan: %w", err)
		}
		ctx = authctx.With(ctx, &authctx.Principal{UserID: adminID, OrganizationID: orgID, IsSystem: true, FullName: "Seed", Source: authctx.SourceSystem})
		return seed.SeedBVRoomsDemo(ctx, tx, &seed.DemoRefs{OrgID: orgID, AdminID: adminID})
	})
}
