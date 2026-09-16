package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/buildingvision/api/internal/app"
	"github.com/buildingvision/api/internal/demo"
	"github.com/buildingvision/api/internal/platform/config"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/storage"
)

// runDemo: `bvctl demo seed|reset|verify|status [--profile hotel,apartment,office]` — Demo Seed Database (§34).
// Memakai koneksi aplikasi (bv_app, RLS aktif) seperti API; event domain dikirim sinkron ke Notification & Search.
func runDemo(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bvctl demo seed|reset|verify|status [--profile hotel,apartment,office] [--reseed] [--json]")
	}
	sub := args[0]
	fs := flag.NewFlagSet("demo "+sub, flag.ExitOnError)
	profile := fs.String("profile", "", "profile: hotel|apartment|office (koma = beberapa; kosong = semua)")
	reseed := fs.Bool("reseed", false, "reset: seed ulang setelah reset")
	asJSON := fs.Bool("json", false, "keluaran JSON")
	_ = fs.Parse(args[1:])
	var profiles []string
	for _, p := range strings.Split(*profile, ",") {
		if p = strings.TrimSpace(p); p != "" {
			profiles = append(profiles, p)
		}
	}
	d, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer d.Close()
	// RLS adalah pagar isolasi organization; superuser Postgres melewatinya → seed/verify harus memakai role aplikasi (bv_app).
	var super bool
	if err := d.Pool.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&super); err == nil && super {
		return fmt.Errorf("bvctl demo harus memakai koneksi role aplikasi (bv_app; BV_DATABASE_URL), bukan superuser — RLS dilewati superuser sehingga isolasi organization tidak berlaku")
	}
	enq, err := jobs.NewInsertOnlyClient(d.Pool)
	if err != nil {
		return err
	}
	var store storage.Storage
	if cfg.S3Endpoint == "memory" {
		store = storage.NewMemory(cfg.PublicURL)
	} else if s3, err := storage.NewS3(ctx, cfg); err == nil {
		store = s3
	} else {
		store = storage.NewMemory(cfg.PublicURL)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if os.Getenv("BV_LOG_LEVEL") == "debug" {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	a, err := app.New(app.Options{Cfg: cfg, Log: log, DB: d, Jobs: enq, Storage: store})
	if err != nil {
		return err
	}
	a.Use(app.DefaultExtensions(a)...)
	out := func(v any) {
		if *asJSON {
			b, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println(string(b))
		}
	}
	switch sub {
	case "seed":
		res, err := a.Demo.Seed(ctx, profiles)
		if err != nil {
			return err
		}
		if *asJSON {
			out(res)
			return nil
		}
		for _, l := range res.Log {
			fmt.Println("demo:", l)
		}
		printStatus(res.State)
	case "reset":
		res, err := a.Demo.Reset(ctx, profiles)
		if err != nil {
			return err
		}
		for _, l := range res.Log {
			fmt.Println("demo:", l)
		}
		if *reseed {
			ps := profiles
			if len(ps) == 0 {
				ps = nil
			}
			sr, err := a.Demo.Seed(ctx, ps)
			if err != nil {
				return err
			}
			for _, l := range sr.Log {
				fmt.Println("demo:", l)
			}
			res.State = sr.State
		}
		if *asJSON {
			out(res)
			return nil
		}
		printStatus(res.State)
	case "verify":
		rep, err := a.Demo.Verify(ctx)
		if err != nil {
			return err
		}
		if *asJSON {
			out(rep)
		} else {
			fmt.Print(rep.Text())
		}
		if !rep.Pass {
			os.Exit(3)
		}
	case "status":
		st, err := a.Demo.Status(ctx)
		if err != nil {
			return err
		}
		if *asJSON {
			out(st)
			return nil
		}
		printStatus(st)
	default:
		return fmt.Errorf("sub-perintah demo tidak dikenal: %s", sub)
	}
	return nil
}

func printStatus(st *demo.State) {
	if st == nil {
		return
	}
	fmt.Printf("demo environment: %s (organization %s) state=%s version=%s\n", st.OrgSlug, orgIDText(st), st.State, st.Version)
	for _, p := range demo.Profiles {
		ps := st.Profiles[p]
		if ps.PropertyID == nil {
			fmt.Printf("  %-10s belum di-seed\n", p)
			continue
		}
		fmt.Printf("  %-10s property=%s records=%d seeded=%s\n", p, ps.PropertyID, ps.RecordCount, ps.SeededAt.Format("2006-01-02 15:04"))
	}
}

func orgIDText(st *demo.State) string {
	if st.OrgID == nil {
		return "-"
	}
	return st.OrgID.String()
}
