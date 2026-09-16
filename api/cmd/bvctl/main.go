// bvctl: CLI operasional — migrate, seed, create-org, keygen (TAD §5.1).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	migrations "github.com/buildingvision/api/db"
	"github.com/buildingvision/api/internal/bvrooms"
	"github.com/buildingvision/api/internal/platform/config"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// migrate & seed memakai koneksi owner (BV_ADMIN_DATABASE_URL) bila ada
	adminURL := os.Getenv("BV_ADMIN_DATABASE_URL")
	if adminURL == "" {
		adminURL = cfg.DatabaseURL
	}

	switch os.Args[1] {
	case "healthcheck":
		// Dipakai Docker HEALTHCHECK pada image distroless (tanpa curl/wget).
		url := "http://127.0.0.1:8080/ready"
		if len(os.Args) > 2 {
			url = os.Args[2]
		}
		client := &http.Client{Timeout: 4 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			fail(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fail(fmt.Errorf("status %d", resp.StatusCode))
		}
		return
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ExitOnError)
		down := fs.Bool("down", false, "rollback satu migration")
		status := fs.Bool("status", false, "tampilkan status")
		_ = fs.Parse(os.Args[2:])
		switch {
		case *status:
			err = migrations.Status(ctx, adminURL)
		case *down:
			err = migrations.Down(ctx, adminURL)
		default:
			err = migrations.Up(ctx, adminURL)
			if err == nil {
				err = runRiverMigrate(ctx, adminURL)
			}
		}
		if err != nil {
			fail(err)
		}
		fmt.Println("migrate ok")
	case "seed":
		if err := runSeed(ctx, adminURL, os.Args[2:]); err != nil {
			fail(err)
		}
	case "demo":
		if err := runDemo(ctx, cfg, os.Args[2:]); err != nil {
			fail(err)
		}
	case "reindex":
		if err := runReindex(ctx, adminURL); err != nil {
			fail(err)
		}
	case "import":
		if err := runImport(ctx, adminURL, os.Args[2:]); err != nil {
			fail(err)
		}
	case "openapi":
		out := os.Stdout
		if len(os.Args) > 2 {
			fh, err := os.Create(os.Args[2])
			if err != nil {
				fail(err)
			}
			defer fh.Close()
			out = fh
		}
		if err := runOpenAPI(out); err != nil {
			fail(err)
		}
	case "vapid-keygen":
		priv, pub, err := bvrooms.GenerateVAPIDKeys()
		if err != nil {
			fail(err)
		}
		fmt.Printf("BV_VAPID_PUBLIC_KEY=%s\nBV_VAPID_PRIVATE_KEY=%s\n", pub, priv)
	case "keygen":
		runKeygen()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `bvctl <command>
  migrate [--status|--down]   jalankan migrasi (goose + River)
  seed [--demo]               seed katalog permission, org demo, role, equipment, SLA, SR categories
  demo seed|reset|verify|status [--profile hotel,apartment,office] [--reseed] [--json]
                              Demo Seed Database: 3 environment demo (Hotel/Apartment/Office) — docs/demo/BuildingVision-Demo-Guide.md
  reindex                     backfill search index seluruh organization
  import --org <slug> --type assets|locations --file x.csv [--property <id>] [--dry-run]   migrasi data (OD-008)
  openapi [out.yaml]          generate OpenAPI 3.1 dari router + struct
  keygen                      generate Ed25519 keypair (PEM) untuk BV_JWT_PRIVATE_KEY
  vapid-keygen                generate kunci VAPID Web Push BVRooms (BV_VAPID_PUBLIC_KEY / BV_VAPID_PRIVATE_KEY)
  healthcheck [url]           GET url (default /ready lokal), exit 0 bila 200 — untuk Docker HEALTHCHECK`)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
