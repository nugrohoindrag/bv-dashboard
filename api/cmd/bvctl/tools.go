package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/search"
)

// runReindex: backfill search_documents seluruh organization (TAD §5.15).
func runReindex(ctx context.Context, url string) error {
	d, err := db.Open(ctx, url)
	if err != nil {
		return err
	}
	defer d.Close()
	orgs, err := d.ListOrganizationIDs(ctx)
	if err != nil {
		return err
	}
	svc := &search.Service{DB: d}
	total := 0
	for _, org := range orgs {
		n, err := svc.Reindex(ctx, org)
		if err != nil {
			return fmt.Errorf("org %s: %w", org, err)
		}
		total += n
	}
	fmt.Printf("reindex ok: %d dokumen, %d organization\n", total, len(orgs))
	return nil
}

// runImport (OD-008): CSV import master data dengan dry-run & laporan error per baris.
//
//	bvctl import --org <slug> --property <property_id> --type assets|locations --file data.csv [--dry-run]
//
// Template assets.csv : asset_code(opsional),name,category_code,type_name,location_code,status,criticality,manufacturer,model,serial_number
// Template locations.csv: location_type,parent_code,name,code(opsional),floor_number,unit_number,area_type
func runImport(ctx context.Context, url string, args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	orgSlug := fs.String("org", "", "slug organization")
	propertyID := fs.String("property", "", "property id (uuid)")
	kind := fs.String("type", "assets", "assets|locations")
	file := fs.String("file", "", "path CSV")
	dryRun := fs.Bool("dry-run", false, "validasi saja, tidak menulis")
	_ = fs.Parse(args)
	if *orgSlug == "" || *file == "" {
		return fmt.Errorf("--org dan --file wajib")
	}
	d, err := db.Open(ctx, url)
	if err != nil {
		return err
	}
	defer d.Close()
	var orgID uuid.UUID
	if err := d.Pool.QueryRow(ctx, `SELECT id FROM organizations WHERE slug = $1`, *orgSlug).Scan(&orgID); err != nil {
		return fmt.Errorf("organization %s tidak ditemukan", *orgSlug)
	}
	fh, err := os.Open(*file)
	if err != nil {
		return err
	}
	defer func() { _ = fh.Close() }()
	r := csv.NewReader(fh)
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("header: %w", err)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(rec []string, k string) string {
		if i, ok := idx[k]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	sys := authctx.System(orgID)
	sys.FullName = "Import"
	ctx = authctx.With(ctx, sys)
	ok, failed := 0, 0
	err = d.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		assets := &asset.Service{DB: d}
		props := &property.Service{DB: d}
		line := 1
		for {
			rec, err := r.Read()
			if err == io.EOF {
				break
			}
			line++
			if err != nil {
				fmt.Printf("baris %d: CSV tidak valid: %v\n", line, err)
				failed++
				continue
			}
			var rowErr error
			switch *kind {
			case "assets":
				var locID uuid.UUID
				if err := tx.QueryRow(ctx, `SELECT id FROM locations WHERE code = $1 AND deleted_at IS NULL`, get(rec, "location_code")).Scan(&locID); err != nil {
					rowErr = fmt.Errorf("location_code %q tidak ditemukan", get(rec, "location_code"))
					break
				}
				var eqID uuid.UUID
				if err := tx.QueryRow(ctx, `SELECT id FROM equipment WHERE category_code = $1 AND COALESCE(type_name,'') = $2 AND deleted_at IS NULL`, strings.ToUpper(get(rec, "category_code")), get(rec, "type_name")).Scan(&eqID); err != nil {
					rowErr = fmt.Errorf("equipment %s/%s tidak ditemukan", get(rec, "category_code"), get(rec, "type_name"))
					break
				}
				in := asset.AssetInput{Name: strPtr(get(rec, "name")), EquipmentID: &eqID, LocationID: &locID}
				if v := get(rec, "asset_code"); v != "" {
					in.AssetCode = &v
				}
				for k, dst := range map[string]**string{"status": &in.Status, "criticality": &in.Criticality, "manufacturer": &in.Manufacturer, "model": &in.Model, "serial_number": &in.SerialNumber} {
					if v := get(rec, k); v != "" {
						*dst = strPtr(v)
					}
				}
				_, rowErr = assets.CreateAssetTx(ctx, tx, in)
			case "locations":
				lt := property.LocationType(get(rec, "location_type"))
				var parent *uuid.UUID
				if pc := get(rec, "parent_code"); pc != "" {
					var pid uuid.UUID
					if err := tx.QueryRow(ctx, `SELECT id FROM locations WHERE code = $1 AND deleted_at IS NULL`, pc).Scan(&pid); err != nil {
						rowErr = fmt.Errorf("parent_code %q tidak ditemukan", pc)
						break
					}
					parent = &pid
				} else if *propertyID != "" && lt != property.LTProperty {
					pid, err := uuid.Parse(*propertyID)
					if err != nil {
						return fmt.Errorf("--property tidak valid")
					}
					parent = &pid
				}
				details := map[string]any{}
				if v := get(rec, "floor_number"); v != "" {
					var n float64
					_, _ = fmt.Sscanf(v, "%f", &n)
					details["floor_number"] = n
				}
				if v := get(rec, "unit_number"); v != "" {
					details["unit_number"] = v
				}
				if v := get(rec, "area_type"); v != "" {
					details["area_type"] = v
				}
				_, rowErr = props.CreateLocationInTx(ctx, tx, property.CreateLocationInput{LocationType: lt, ParentID: parent, Name: get(rec, "name"), Details: details})
			default:
				return fmt.Errorf("--type harus assets|locations")
			}
			if rowErr != nil {
				fmt.Printf("baris %d: %v\n", line, rowErr)
				failed++
				continue
			}
			ok++
		}
		if *dryRun {
			fmt.Printf("dry-run: %d valid, %d gagal (tidak ada perubahan ditulis)\n", ok, failed)
			return fmt.Errorf("dry-run rollback")
		}
		return nil
	})
	if err != nil && err.Error() == "dry-run rollback" {
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Printf("import selesai: %d berhasil, %d gagal\n", ok, failed)
	return nil
}

func strPtr(s string) *string { return &s }
